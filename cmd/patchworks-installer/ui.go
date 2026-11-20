package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// The installer prints a trail of one line per finished step. Only the step
// that is running is drawn live, and it collapses into its trail line when it
// finishes, so nothing stale is left on screen.

var (
	brandStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	subtleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	keyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true)
	codeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	cardStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
	spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(primaryPurple))
)

const (
	// fallbackWidth is used when the terminal width is unknown.
	fallbackWidth     = 100
	maxRowNameWidth   = 34
	maxWarningsPerRun = 5
)

func printHeader() {
	fmt.Printf("\n%s %s\n\n", brandStyle.Render("Patchworks Self-Hosted"), subtleStyle.Render("installer "+version))
}

// doneLine is the trail line of a finished step.
func doneLine(text string, elapsed time.Duration) string {
	return successStyle.Render("✓") + " " + text + elapsedSuffix(elapsed)
}

func failedLine(text string, elapsed time.Duration) string {
	return errorStyle.Render("✗") + " " + text + elapsedSuffix(elapsed)
}

func warningLine(text string) string {
	return "  " + warningStyle.Render("! "+text)
}

func noteLine(text string) string {
	return "  " + subtleStyle.Render(text)
}

func elapsedSuffix(elapsed time.Duration) string {
	elapsed = elapsed.Round(time.Second)
	if elapsed <= 0 {
		return ""
	}
	return subtleStyle.Render(" · " + elapsed.String())
}

// printDone prints a finished step with optional context lines under it.
func printDone(text string, notes ...string) {
	fmt.Println(doneLine(text, 0))
	for _, note := range notes {
		fmt.Println(noteLine(note))
	}
}

func outputWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return fallbackWidth
	}
	return width
}

func truncateLine(line string, width int) string {
	if width <= 0 || lipgloss.Width(line) <= width {
		return line
	}
	return lipgloss.NewStyle().MaxWidth(width-1).Render(line) + "…"
}

// Background output: Helm logs through slog, client-go prints API warnings
// (such as PodSecurity warnings) and errors through klog. Written straight to
// the terminal while a step is drawn live, they shift the lines the renderer
// redraws and garble it. They are collected instead and printed, grouped,
// after the step.

type capturedMessage struct {
	group string
	text  string
	count int
}

type outputCapture struct {
	mu       sync.Mutex
	messages []*capturedMessage
	byGroup  map[string]*capturedMessage
	shown    map[string]bool
}

var backgroundOutput = &outputCapture{byGroup: map[string]*capturedMessage{}, shown: map[string]bool{}}

func (c *outputCapture) add(group, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	group = strings.TrimSpace(group)
	if group == "" {
		group = text
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.byGroup[group]; ok {
		existing.count++
		return
	}
	message := &capturedMessage{group: group, text: text, count: 1}
	c.byGroup[group] = message
	c.messages = append(c.messages, message)
}

type pendingMessage struct {
	text   string
	suffix string
}

func (m pendingMessage) String() string {
	return m.text + m.suffix
}

// pending returns the groups not printed yet, one each. A group is printed
// once per run, so repeats in later steps stay quiet. A repeated group shows
// its group text and how often it occurred.
func (c *outputCapture) pending() []pendingMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	var pending []pendingMessage
	for _, message := range c.messages {
		if c.shown[message.group] {
			continue
		}
		c.shown[message.group] = true
		if message.count > 1 {
			pending = append(pending, pendingMessage{text: message.group, suffix: fmt.Sprintf(" (%d times)", message.count)})
			continue
		}
		pending = append(pending, pendingMessage{text: message.text})
	}
	return pending
}

// printCapturedOutput prints what libraries logged since the last call, one
// line per message, cut to the terminal width but keeping the repeat count.
func printCapturedOutput() {
	pending := backgroundOutput.pending()
	width := outputWidth()
	for i, message := range pending {
		if i == maxWarningsPerRun {
			fmt.Println(warningLine(fmt.Sprintf("%d more warnings", len(pending)-maxWarningsPerRun)))
			return
		}
		available := width - lipgloss.Width(warningLine("")) - lipgloss.Width(message.suffix)
		fmt.Println(warningLine(truncateLine(message.text, available) + message.suffix))
	}
}

// captureBackgroundOutput routes library logging into backgroundOutput. Call
// it before any Helm configuration is created: action.Configuration.Init
// copies the default slog handler into its Kubernetes client.
func captureBackgroundOutput() {
	handler := captureHandler{capture: backgroundOutput}
	// This also routes the log package through the handler, below its level.
	slog.SetDefault(slog.New(handler))
	klog.SetSlogLogger(slog.New(handler))
	rest.SetDefaultWarningHandler(warningCapture{capture: backgroundOutput})
}

type warningCapture struct {
	capture *outputCapture
}

// HandleWarningHeader groups API warnings by the text before their first
// colon, so PodSecurity warnings for many workloads collapse into one line.
func (w warningCapture) HandleWarningHeader(code int, _ string, text string) {
	if code != 299 || strings.TrimSpace(text) == "" {
		return
	}
	group, _, found := strings.Cut(text, ": ")
	if !found {
		group = text
	}
	w.capture.add(group, text)
}

type captureHandler struct {
	capture *outputCapture
	attrs   []slog.Attr
}

func (h captureHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (h captureHandler) Handle(_ context.Context, record slog.Record) error {
	var b strings.Builder
	b.WriteString(record.Message)
	writeAttr := func(attr slog.Attr) bool {
		if attr.Key != "" {
			fmt.Fprintf(&b, " %s=%v", attr.Key, attr.Value.Any())
		}
		return true
	}
	for _, attr := range h.attrs {
		writeAttr(attr)
	}
	record.Attrs(writeAttr)
	h.capture.add(record.Message, b.String())
	return nil
}

func (h captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	combined := append(append([]slog.Attr{}, h.attrs...), attrs...)
	return captureHandler{capture: h.capture, attrs: combined}
}

func (h captureHandler) WithGroup(string) slog.Handler {
	return h
}

// Steps

type progressStep struct {
	// Title is shown while the step runs, for example "Installing infrastructure".
	Title string
	// Done is the trail line once the step succeeds. It defaults to Title.
	Done string
	// DoneFunc, when set and returning text, replaces Done. It runs after the
	// step, so it can describe the step's result.
	DoneFunc func() string
	// Detail is a context line shown under the title while the step runs.
	Detail string
	// Monitor, when set, is polled while the step runs to list components.
	Monitor func() []installComponentRow
}

func (s progressStep) doneText() string {
	if s.DoneFunc != nil {
		if text := s.DoneFunc(); text != "" {
			return text
		}
	}
	if s.Done != "" {
		return s.Done
	}
	return s.Title
}

type progressResult struct {
	err error
}

type progressPollMsg struct{}

type progressRowsMsg struct {
	rows []installComponentRow
}

type progressModel struct {
	step     progressStep
	run      func() error
	spinner  spinner.Model
	started  time.Time
	elapsed  time.Duration
	rows     []installComponentRow
	finished bool
	err      error
	width    int
	height   int
}

func newSpinner() spinner.Model {
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = spinnerStyle
	return spin
}

func runProgress(step progressStep, fn func() error) error {
	initial := progressModel{
		step:    step,
		run:     fn,
		spinner: newSpinner(),
		started: time.Now(),
	}

	finalModel, err := tea.NewProgram(initial).Run()
	if err != nil {
		printCapturedOutput()
		return err
	}
	model, ok := finalModel.(progressModel)
	if !ok {
		return fmt.Errorf("unexpected progress result")
	}
	fmt.Println(model.summary())
	printCapturedOutput()
	return model.err
}

func (m progressModel) Init() tea.Cmd {
	run := m.run
	cmds := []tea.Cmd{
		func() tea.Msg { return progressResult{err: run()} },
		m.spinner.Tick,
	}
	if m.step.Monitor != nil {
		cmds = append(cmds, m.poll())
	}
	return tea.Batch(cmds...)
}

func (m progressModel) poll() tea.Cmd {
	monitor := m.step.Monitor
	return func() tea.Msg { return progressRowsMsg{rows: monitor()} }
}

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case progressResult:
		m.err = msg.err
		m.finished = true
		m.elapsed = time.Since(m.started)
		return m, tea.Quit
	case progressPollMsg:
		if m.finished {
			return m, nil
		}
		return m, m.poll()
	case progressRowsMsg:
		// Keep the last good snapshot when a poll fails.
		if len(msg.rows) > 0 {
			m.rows = msg.rows
		}
		if m.finished {
			return m, nil
		}
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return progressPollMsg{} })
	case spinner.TickMsg:
		if m.finished {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	}
	return m, nil
}

// View draws the running step. Once the step has finished it draws nothing,
// so the live frame is cleared, and runProgress prints the summary in its
// place. Printed rather than drawn, the summary is not cut at the terminal
// width. Live frames end with a newline because the renderer clears the
// cursor's line when the program exits.
func (m progressModel) View() string {
	if m.finished {
		return ""
	}

	lines := []string{m.spinner.View() + m.step.Title + elapsedSuffix(time.Since(m.started))}
	if m.step.Detail != "" {
		lines = append(lines, noteLine(m.step.Detail))
	}
	if len(m.rows) > 0 {
		lines = append(lines, installRowLines(m.rows, m.rowBudget(), false)...)
	}
	return strings.Join(lines, "\n") + "\n"
}

// summary is the trail a finished step leaves: one line on success, and on
// failure the components that had not finished.
func (m progressModel) summary() string {
	if m.err == nil {
		return doneLine(m.step.doneText(), m.elapsed)
	}
	lines := []string{failedLine(m.step.Title+" failed", m.elapsed)}
	if len(m.rows) > 0 {
		lines = append(lines, installRowLines(m.rows, 0, true)...)
	}
	return strings.Join(lines, "\n")
}

// rowBudget is how many component rows fit under the title and detail lines.
func (m progressModel) rowBudget() int {
	if m.height <= 0 {
		return 0
	}
	budget := m.height - 3
	if budget < 2 {
		return 2
	}
	return budget
}

// Install component rows

func statusGlyph(status installComponentStatus) string {
	switch status {
	case installStatusComplete:
		return successStyle.Render("✓")
	case installStatusFailed:
		return errorStyle.Render("✗")
	case installStatusInProgress:
		return warningStyle.Render("•")
	default:
		return subtleStyle.Render("·")
	}
}

func statusLabel(status installComponentStatus) string {
	switch status {
	case installStatusComplete:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("complete")
	case installStatusFailed:
		return errorStyle.Render("failed")
	case installStatusInProgress:
		return warningStyle.Render("in progress")
	default:
		return subtleStyle.Render("waiting")
	}
}

func sortInstallRows(rows []installComponentRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
}

// formatInstallRows renders component rows alphabetically with aligned
// status columns. A budget above zero caps the number of lines (see
// installRowLines).
func formatInstallRows(rows []installComponentRow, budget int) string {
	return strings.Join(installRowLines(rows, budget, false), "\n")
}

// installRowLines lists every row when they fit the budget, or when the
// budget is zero. Otherwise, and always when condensed is set, it lists the
// failed and in-progress rows and counts the rest on a summary line.
func installRowLines(rows []installComponentRow, budget int, condensed bool) []string {
	sorted := append([]installComponentRow{}, rows...)
	sortInstallRows(sorted)

	nameWidth := 0
	for _, row := range sorted {
		if width := lipgloss.Width(row.Name); width > nameWidth {
			nameWidth = width
		}
	}
	if nameWidth > maxRowNameWidth {
		nameWidth = maxRowNameWidth
	}

	render := func(row installComponentRow) string {
		name := truncateLine(row.Name, nameWidth)
		line := fmt.Sprintf("  %s %s%s  %s", statusGlyph(row.Status), name, strings.Repeat(" ", nameWidth-lipgloss.Width(name)), statusLabel(row.Status))
		if row.Status != installStatusComplete && strings.TrimSpace(row.Detail) != "" {
			line += subtleStyle.Render(" · " + row.Detail)
		}
		return line
	}

	if !condensed && (budget <= 0 || len(sorted) <= budget) {
		lines := make([]string, 0, len(sorted))
		for _, row := range sorted {
			lines = append(lines, render(row))
		}
		return lines
	}

	var lines []string
	counts := map[installComponentStatus]int{}
	shown := 0
	for _, row := range sorted {
		if row.Status != installStatusFailed && row.Status != installStatusInProgress {
			counts[row.Status]++
			continue
		}
		if budget > 0 && shown >= budget-1 {
			counts[row.Status]++
			continue
		}
		lines = append(lines, render(row))
		shown++
	}

	var summary []string
	if n := counts[installStatusComplete]; n > 0 {
		summary = append(summary, fmt.Sprintf("%d complete", n))
	}
	if n := counts[installStatusFailed]; n > 0 {
		summary = append(summary, fmt.Sprintf("%d more failed", n))
	}
	if n := counts[installStatusInProgress]; n > 0 {
		summary = append(summary, fmt.Sprintf("%d more in progress", n))
	}
	if n := counts[installStatusWaiting]; n > 0 {
		summary = append(summary, fmt.Sprintf("%d waiting", n))
	}
	if len(summary) > 0 {
		lines = append(lines, noteLine(strings.Join(summary, " · ")))
	}
	return lines
}

// Cluster inspection

type inspectResult struct {
	Info clusterInfo
	Err  error
}

type inspectModel struct {
	info        clusterInfo
	kubeconfig  string
	contextName string
	err         error
	aborted     bool
	finished    bool
	quitConfirm bool
	spinner     spinner.Model
}

func runInspection(kubeconfig, contextName string) (clusterInfo, error) {
	initial := inspectModel{
		kubeconfig:  kubeconfig,
		contextName: contextName,
		spinner:     newSpinner(),
	}

	finalModel, err := tea.NewProgram(initial).Run()
	if err != nil {
		printCapturedOutput()
		return clusterInfo{}, err
	}

	model, ok := finalModel.(inspectModel)
	if !ok {
		return clusterInfo{}, fmt.Errorf("unexpected inspection result")
	}
	if model.aborted {
		return clusterInfo{}, huh.ErrUserAborted
	}
	fmt.Println(clusterSummaryLines(model.info, model.kubeconfig))
	printCapturedOutput()
	return model.info, model.err
}

func (m inspectModel) Init() tea.Cmd {
	kubeconfig, contextName := m.kubeconfig, m.contextName
	inspectCmd := func() tea.Msg {
		return inspectCluster(kubeconfig, contextName)
	}
	return tea.Batch(inspectCmd, m.spinner.Tick)
}

func (m inspectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case inspectResult:
		m.info = msg.Info
		m.err = msg.Err
		m.finished = true
		return m, tea.Quit
	case spinner.TickMsg:
		if m.finished {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if m.quitConfirm {
			switch msg.String() {
			case "y", "Y", "enter", "ctrl+c":
				m.aborted = true
				return m, tea.Quit
			case "n", "N", "esc":
				m.quitConfirm = false
				return m, nil
			}
			return m, nil
		}

		if msg.String() == "ctrl+c" || msg.String() == "esc" {
			m.quitConfirm = true
			return m, nil
		}
	}

	return m, nil
}

// View clears itself once the inspection ends; runInspection then prints the
// cluster summary.
func (m inspectModel) View() string {
	if m.aborted || m.finished {
		return ""
	}
	if m.quitConfirm {
		return fmt.Sprintf("%s\n%s\n",
			warningStyle.Render("Quit installer? Any unsaved selections will be lost."),
			noteLine("Y/Enter: quit  N/Esc: continue"),
		)
	}
	return fmt.Sprintf("%sInspecting cluster %s\n%s\n",
		m.spinner.View(),
		stringDefault(m.contextName, "(current context)"),
		noteLine(m.kubeconfig+"  ·  Esc/Ctrl+C: quit"),
	)
}

// clusterSummaryLines is the trail left by the inspection: the cluster, what
// it offers, and any warnings.
func clusterSummaryLines(info clusterInfo, kubeconfig string) string {
	var lines []string
	if info.Connected {
		lines = append(lines, doneLine("Cluster "+info.Context+subtleStyle.Render(" · "+kubeconfig), 0))
		lines = append(lines, noteLine(fmt.Sprintf("storage %s · ingress %s · cert-manager %s",
			strings.Join(defaultList(info.StorageClasses, "none"), ", "),
			strings.Join(defaultList(info.IngressClasses, "none"), ", "),
			installedLabel(info.HasCertManager),
		)))
	} else {
		lines = append(lines, warningStyle.Render("!")+" Could not inspect cluster "+info.Context+subtleStyle.Render(" · "+kubeconfig))
	}
	for _, warning := range info.Warnings {
		lines = append(lines, warningLine(warning))
	}
	return strings.Join(lines, "\n")
}

func installedLabel(installed bool) string {
	if installed {
		return "installed"
	}
	return "not installed"
}

// Result views

func keyValue(key, value string) string {
	if value == "" {
		value = "unset"
	}
	return fmt.Sprintf("%s %s", keyStyle.Render(key+":"), value)
}

func manualInstallView(commands string) string {
	lines := []string{"", keyStyle.Render("Manual install commands")}
	for _, command := range strings.Split(commands, "\n") {
		lines = append(lines, "  "+codeStyle.Render(command))
	}
	return strings.Join(lines, "\n") + "\n"
}

func accessDetailsView(values map[string]string, generatedAdminPassword string, installed bool) string {
	title := "Patchworks is installed"
	if !installed {
		title = "Access details after installing"
	}
	dashboard := dashboardURL(values)
	row := func(key, value string) string {
		if value == "" {
			value = "unset"
		}
		return keyStyle.Render(fmt.Sprintf("%-10s", key)) + value
	}
	body := strings.Join([]string{
		successStyle.Render(title),
		"",
		row("Dashboard", terminalLink(dashboard, dashboard)),
		row("Email", values["adminEmail"]),
		row("Password", adminPasswordSummary(values, generatedAdminPassword, installed)),
	}, "\n")
	return "\n" + cardStyle.Render(body) + "\n"
}
