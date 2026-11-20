package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

func newStepModel(step progressStep) (progressModel, context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	return progressModel{step: step, ctx: ctx, cancel: cancel, spinner: newSpinner(), started: time.Now()}, ctx
}

func send(model progressModel, msgs ...tea.Msg) (progressModel, tea.Cmd) {
	var cmd tea.Cmd
	for _, msg := range msgs {
		var next tea.Model
		next, cmd = model.Update(msg)
		model = next.(progressModel)
	}
	return model, cmd
}

var installStep = progressStep{
	Title:    "Installing infrastructure",
	Detail:   "Applying release patchworks-infra",
	StopNote: "Helm marks release patchworks-infra as failed and keeps what it created. Run the installer again to retry.",
}

func TestProgressStopAsksBeforeCancelling(t *testing.T) {
	model, ctx := newStepModel(installStep)

	for _, key := range []string{"ctrl+c", "esc"} {
		asking, _ := send(model, keyMsg(key))
		view := asking.View()
		if !asking.confirmStop || !strings.Contains(view, "Stop installing infrastructure?") || !strings.Contains(view, "marks release patchworks-infra as failed") {
			t.Fatalf("%s should ask before stopping:\n%s", key, view)
		}
		if ctx.Err() != nil {
			t.Fatalf("%s cancelled the step before it was confirmed", key)
		}
		for _, answer := range []string{"n", "esc"} {
			resumed, _ := send(asking, keyMsg(answer))
			if resumed.confirmStop || resumed.stopping || !strings.Contains(resumed.View(), "Applying release patchworks-infra") {
				t.Fatalf("%s should keep the step going:\n%s", answer, resumed.View())
			}
		}
	}
	if ctx.Err() != nil {
		t.Fatal("declining the stop cancelled the step")
	}
}

func TestProgressStopCancelsStepAndReportsStopped(t *testing.T) {
	for _, confirm := range []string{"y", "enter", "ctrl+c"} {
		model, ctx := newStepModel(installStep)
		stopping, cmd := send(model, keyMsg("ctrl+c"), keyMsg(confirm))
		if !stopping.stopping || ctx.Err() == nil || cmd == nil {
			t.Fatalf("%s should cancel the step and start the grace timer: stopping=%t ctx=%v", confirm, stopping.stopping, ctx.Err())
		}
		if view := stopping.View(); !strings.Contains(view, "Stopping: Installing infrastructure") || !strings.Contains(view, "Ctrl+C: quit now") {
			t.Fatalf("stopping view:\n%s", view)
		}

		stopped, _ := send(stopping, progressResult{err: context.Canceled})
		if !stopped.finished || !errors.Is(stopped.result(), huh.ErrUserAborted) {
			t.Fatalf("stopped step should finish as aborted: finished=%t result=%v", stopped.finished, stopped.result())
		}
		summary := stopped.summary()
		if !strings.Contains(summary, "✗ Installing infrastructure stopped") || !strings.Contains(summary, "Run the installer again to retry") {
			t.Fatalf("stopped summary:\n%s", summary)
		}
	}
}

func TestProgressCtrlCWhileStoppingQuitsNow(t *testing.T) {
	model, _ := newStepModel(installStep)
	quit, _ := send(model, keyMsg("ctrl+c"), keyMsg("y"), keyMsg("ctrl+c"))
	if !quit.finished || !errors.Is(quit.result(), huh.ErrUserAborted) {
		t.Fatalf("Ctrl+C while stopping should quit: finished=%t result=%v", quit.finished, quit.result())
	}
}

func TestProgressStopGivesUpAfterGrace(t *testing.T) {
	model, _ := newStepModel(installStep)
	quit, _ := send(model, keyMsg("ctrl+c"), keyMsg("y"), progressStopGraceMsg{})
	if !quit.finished || !errors.Is(quit.result(), huh.ErrUserAborted) {
		t.Fatalf("grace timeout should quit: finished=%t result=%v", quit.finished, quit.result())
	}
}

func TestProgressStepFinishingWhileStoppingStillStops(t *testing.T) {
	model, _ := newStepModel(installStep)
	done, _ := send(model, keyMsg("ctrl+c"), keyMsg("y"), progressResult{})
	if !strings.HasPrefix(done.summary(), "✓") {
		t.Fatalf("a step that finished should say so: %q", done.summary())
	}
	if !errors.Is(done.result(), huh.ErrUserAborted) {
		t.Fatalf("the installer should still stop after the step: %v", done.result())
	}
}

// Bubble Tea can report a lone Esc and the next key as one Alt key when they
// arrive in the same read; seen under a pty while the spinner redraws.
func TestProgressSplitsJoinedEscAndKey(t *testing.T) {
	model, ctx := newStepModel(installStep)
	stopped, _ := send(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y"), Alt: true})
	if !stopped.stopping || ctx.Err() == nil {
		t.Fatalf("alt+y should act as Esc then Y: confirm=%t stopping=%t", stopped.confirmStop, stopped.stopping)
	}

	model, ctx = newStepModel(installStep)
	toggled, _ := send(model, tea.KeyMsg{Type: tea.KeyEsc, Alt: true})
	if toggled.confirmStop || toggled.stopping || ctx.Err() != nil {
		t.Fatalf("alt+esc should act as two Esc presses: confirm=%t stopping=%t", toggled.confirmStop, toggled.stopping)
	}
}

func TestInspectSplitsJoinedEscAndKey(t *testing.T) {
	next, cmd := inspectModel{spinner: newSpinner()}.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y"), Alt: true})
	if model := next.(inspectModel); !model.aborted || cmd == nil {
		t.Fatalf("alt+y should act as Esc then Y: aborted=%t", model.aborted)
	}

	next, _ = inspectModel{spinner: newSpinner()}.Update(keyMsg("esc"))
	asking := next.(inspectModel)
	next, _ = asking.Update(keyMsg("n"))
	if model := next.(inspectModel); model.quitConfirm || model.aborted {
		t.Fatalf("N should dismiss the quit question: %#v", model)
	}
}

func TestProgressDefaultStopNote(t *testing.T) {
	model, _ := newStepModel(progressStep{Title: "Preparing Patchworks charts"})
	asking, _ := send(model, keyMsg("esc"))
	if view := asking.View(); !strings.Contains(view, "Stop preparing Patchworks charts?") || !strings.Contains(view, defaultStopNote) {
		t.Fatalf("default stop prompt:\n%s", view)
	}
}

func writeTestKubeconfig(t *testing.T, server string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	config := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: %s
contexts:
- name: test
  context:
    cluster: test
    user: test
users:
- name: test
  user:
    token: test
current-context: test
`, server)
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInspectClusterReportsReachableCluster(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			fmt.Fprint(w, `{"major":"1","minor":"36","gitVersion":"v1.36.0"}`)
		case "/apis/storage.k8s.io/v1/storageclasses":
			fmt.Fprint(w, `{"kind":"StorageClassList","apiVersion":"storage.k8s.io/v1","items":[{"metadata":{"name":"local-path"},"provisioner":"rancher.io/local-path"}]}`)
		case "/apis/networking.k8s.io/v1/ingressclasses":
			fmt.Fprint(w, `{"kind":"IngressClassList","apiVersion":"networking.k8s.io/v1","items":[{"metadata":{"name":"contour"},"spec":{"controller":"projectcontour.io/contour"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result := inspectCluster(writeTestKubeconfig(t, server.URL), "test")
	if !result.Info.Connected {
		t.Fatalf("reachable cluster not connected: %#v", result.Info)
	}
	if strings.Join(result.Info.StorageClasses, ",") != "local-path" || strings.Join(result.Info.IngressClasses, ",") != "contour" {
		t.Fatalf("inspection = %#v", result.Info)
	}
}

func TestInspectClusterTreatsForbiddenVersionAsConnected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	result := inspectCluster(writeTestKubeconfig(t, server.URL), "test")
	if !result.Info.Connected {
		t.Fatalf("forbidden /version should still count as connected: %#v", result.Info)
	}
}

func TestInspectClusterReportsUnreachableCluster(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()

	result := inspectCluster(writeTestKubeconfig(t, url), "test")
	if result.Info.Connected {
		t.Fatalf("closed server reported as connected: %#v", result.Info)
	}
	if len(result.Info.Warnings) != 1 || !strings.Contains(result.Info.Warnings[0], "could not connect to the cluster") {
		t.Fatalf("warnings = %#v", result.Info.Warnings)
	}
}

func TestProgressClearsLiveFrameAndSummarisesWhenDone(t *testing.T) {
	model := progressModel{
		step:     progressStep{Title: "Preparing Patchworks charts", Done: "Prepared charts", Detail: "Resolving the chart release"},
		spinner:  newSpinner(),
		started:  time.Now(),
		finished: true,
		elapsed:  1200 * time.Millisecond,
	}

	if view := model.View(); view != "" {
		t.Fatalf("finished view should clear the live frame, got %q", view)
	}
	if summary := model.summary(); summary != "✓ Prepared charts · 1s" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestProgressLiveFrameEndsWithNewline(t *testing.T) {
	// The renderer clears the cursor's line on exit, so a live frame's last
	// line must be the empty one after its content.
	model := progressModel{step: progressStep{Title: "Installing", Detail: "detail"}, spinner: newSpinner(), started: time.Now()}
	view := model.View()
	if !strings.HasSuffix(view, "\n") || !strings.Contains(view, "Installing") || !strings.Contains(view, "detail") {
		t.Fatalf("running view = %q", view)
	}
}

func TestProgressSummaryUsesDoneFunc(t *testing.T) {
	model := progressModel{
		step:     progressStep{Title: "Removing Quay pull secret", DoneFunc: func() string { return "Kept pull secret quay: not created by this installer" }},
		finished: true,
	}

	if summary := model.summary(); summary != "✓ Kept pull secret quay: not created by this installer" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestProgressSummaryKeepsUnfinishedRowsOnFailure(t *testing.T) {
	model := progressModel{
		step:     progressStep{Title: "Installing application"},
		finished: true,
		err:      errors.New("timed out"),
		elapsed:  15 * time.Minute,
		rows: []installComponentRow{
			{Name: "Running Core migrations", Status: installStatusFailed, Detail: "2 failed attempts"},
			{Name: "Generating app key", Status: installStatusComplete},
			{Name: "Running Core seeders", Status: installStatusWaiting},
		},
	}

	view := model.summary()
	for _, expected := range []string{"✗ Installing application failed · 15m0s", "Running Core migrations", "2 failed attempts", "1 complete", "1 waiting"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("failed view missing %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "Generating app key") {
		t.Fatalf("failed view should summarise complete rows:\n%s", view)
	}
}

func TestInstallRowsCondenseWhenTheyDoNotFit(t *testing.T) {
	rows := []installComponentRow{
		{Name: "Generating credentials", Status: installStatusComplete},
		{Name: "MySQL", Status: installStatusComplete, Detail: "1/1 ready"},
		{Name: "RabbitMQ", Status: installStatusInProgress, Detail: "0/1 ready"},
		{Name: "Redis", Status: installStatusComplete, Detail: "1/1 ready"},
		{Name: "Setting up object storage", Status: installStatusWaiting},
	}

	all := installRowLines(rows, len(rows), false)
	if len(all) != len(rows) {
		t.Fatalf("rows that fit should all be listed, got %d lines:\n%s", len(all), strings.Join(all, "\n"))
	}

	condensed := installRowLines(rows, 3, false)
	rendered := strings.Join(condensed, "\n")
	if len(condensed) > 3 {
		t.Fatalf("condensed rows exceed the budget:\n%s", rendered)
	}
	for _, expected := range []string{"RabbitMQ", "in progress", "0/1 ready", "3 complete", "1 waiting"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("condensed rows missing %q:\n%s", expected, rendered)
		}
	}
	if strings.Contains(rendered, "MySQL") {
		t.Fatalf("condensed rows should not list complete rows:\n%s", rendered)
	}
}

func TestInstallRowsHideDetailForCompleteRows(t *testing.T) {
	rendered := formatInstallRows([]installComponentRow{{Name: "MySQL", Status: installStatusComplete, Detail: "1/1 ready"}}, 0)
	if strings.Contains(rendered, "1/1 ready") {
		t.Fatalf("complete row should not repeat its detail: %q", rendered)
	}
}

func TestInspectViewHasNoFalseEnterHint(t *testing.T) {
	model := inspectModel{kubeconfig: "/home/me/.kube/config", contextName: "talos", spinner: newSpinner()}
	view := model.View()
	if !strings.Contains(view, "Inspecting cluster talos") || strings.Contains(view, "Enter") {
		t.Fatalf("running inspect view = %q", view)
	}
}

func TestInspectViewClearsWhenFinishedOrAborted(t *testing.T) {
	for name, model := range map[string]inspectModel{"finished": {finished: true}, "aborted": {aborted: true}} {
		if view := model.View(); view != "" {
			t.Fatalf("%s view = %q", name, view)
		}
	}
}

func TestClusterSummaryLines(t *testing.T) {
	summary := clusterSummaryLines(clusterInfo{
		Context:        "talos",
		Connected:      true,
		StorageClasses: []string{"local-path"},
		IngressClasses: []string{"contour"},
		Warnings:       []string{"no StorageClass detected; chart-managed persistence may fail"},
	}, "/home/me/.kube/config")

	for _, expected := range []string{"✓ Cluster talos · /home/me/.kube/config", "storage local-path · ingress contour · cert-manager not installed", "! no StorageClass detected"} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("cluster summary missing %q:\n%s", expected, summary)
		}
	}
}

func TestClusterSummaryFlagsUnreachableCluster(t *testing.T) {
	summary := clusterSummaryLines(clusterInfo{
		Context:  "talos",
		Warnings: []string{"could not connect to the cluster: dial tcp: i/o timeout"},
	}, "/home/me/.kube/config")

	if !strings.Contains(summary, "! Could not inspect cluster talos") || !strings.Contains(summary, "i/o timeout") || strings.Contains(summary, "✓") {
		t.Fatalf("unreachable cluster summary = %q", summary)
	}
}

func newTestCapture() *outputCapture {
	return &outputCapture{byGroup: map[string]*capturedMessage{}, shown: map[string]bool{}}
}

func pendingLines(capture *outputCapture) []string {
	var lines []string
	for _, message := range capture.pending() {
		lines = append(lines, message.String())
	}
	return lines
}

func TestWarningCaptureGroupsPodSecurityWarnings(t *testing.T) {
	capture := newTestCapture()
	handler := warningCapture{capture: capture}
	handler.HandleWarningHeader(299, "", `would violate PodSecurity "restricted:latest": allowPrivilegeEscalation != false (container "mysql" must set securityContext.allowPrivilegeEscalation=false)`)
	handler.HandleWarningHeader(299, "", `would violate PodSecurity "restricted:latest": runAsNonRoot != true (pod or container "redis" must set securityContext.runAsNonRoot=true)`)
	handler.HandleWarningHeader(299, "", "unknown field \"spec.foo\"")
	handler.HandleWarningHeader(199, "", "not a warning header")

	lines := pendingLines(capture)
	expected := []string{`would violate PodSecurity "restricted:latest" (2 times)`, `unknown field "spec.foo"`}
	if strings.Join(lines, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("pending = %#v, want %#v", lines, expected)
	}
	if again := pendingLines(capture); len(again) != 0 {
		t.Fatalf("warnings were printed twice: %#v", again)
	}

	handler.HandleWarningHeader(299, "", `would violate PodSecurity "restricted:latest": seccompProfile (pod "web")`)
	if again := pendingLines(capture); len(again) != 0 {
		t.Fatalf("a group already printed came back: %#v", again)
	}
}

func TestCaptureHandlerKeepsWarningsAndErrorsOnly(t *testing.T) {
	capture := newTestCapture()
	logger := slog.New(captureHandler{capture: capture})
	logger.Debug("waiting for resources")
	logger.Info("release installed")
	logger.Warn("resource exists on cluster but not in original release", "kind", "Secret", "name", "quay")
	logger.With("release", "patchworks-app").Error("job failed", "job", "core-migrations")

	lines := pendingLines(capture)
	expected := []string{
		"resource exists on cluster but not in original release kind=Secret name=quay",
		"job failed release=patchworks-app job=core-migrations",
	}
	if strings.Join(lines, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("pending = %#v, want %#v", lines, expected)
	}
}

func TestJobInstallStatusTreatsRetriesAsInProgress(t *testing.T) {
	retrying := batchv1.Job{Status: batchv1.JobStatus{Failed: 1}}
	if status := jobInstallStatus(retrying); status != installStatusInProgress {
		t.Fatalf("retrying job status = %q", status)
	}
	if detail := jobProgressDetail(retrying); detail != "1 failed attempt" {
		t.Fatalf("retrying job detail = %q", detail)
	}

	recovered := batchv1.Job{Status: batchv1.JobStatus{Failed: 2, Succeeded: 1}}
	if status := jobInstallStatus(recovered); status != installStatusComplete {
		t.Fatalf("recovered job status = %q", status)
	}

	failed := batchv1.Job{Status: batchv1.JobStatus{
		Failed:     3,
		Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}},
	}}
	if status := jobInstallStatus(failed); status != installStatusFailed {
		t.Fatalf("failed job status = %q", status)
	}
}

func TestAccessDetailsViewNamesItsState(t *testing.T) {
	values := map[string]string{"domain": "patchworks.example.com", "scheme": "https", "adminEmail": "admin@example.com"}

	before := accessDetailsView(values, "", false)
	if !strings.Contains(before, "Access details after installing") || !strings.Contains(before, "will be generated") {
		t.Fatalf("pre-install access details:\n%s", before)
	}

	after := accessDetailsView(values, "generated-password", true)
	for _, expected := range []string{"Patchworks is installed", "https://patchworks.example.com", "admin@example.com", "generated-password"} {
		if !strings.Contains(after, expected) {
			t.Fatalf("access details missing %q:\n%s", expected, after)
		}
	}
}
