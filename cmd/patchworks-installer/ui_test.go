package main

import (
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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

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
