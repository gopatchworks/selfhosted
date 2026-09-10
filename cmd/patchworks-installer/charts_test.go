package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	yaml "go.yaml.in/yaml/v3"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
)

func TestParseChartOptions(t *testing.T) {
	for _, tc := range []struct {
		args            []string
		source, version string
	}{
		{[]string{"--chart-version", "latest"}, "", "latest"},
		{[]string{"install", "--chart-version=0.2.0"}, "", "0.2.0"},
		{[]string{"--chart-source", "bundled"}, "bundled", ""},
		{[]string{"unpack-charts", "--chart-source=github", "--chart-version=v0.2.0-rc.1"}, "github", "v0.2.0-rc.1"},
	} {
		opts, err := parseCLI(tc.args)
		if err != nil || opts.ChartSource != tc.source || opts.ChartVersion != tc.version {
			t.Fatalf("parse %v: %+v, %v", tc.args, opts, err)
		}
	}
	for _, args := range [][]string{
		{"--chart-version"}, {"--chart-source"}, {"--chart-version="}, {"--chart-source="},
		{"--chart-version", " "}, {"--chart-source", "unknown"}, {"--chart-version", "^0.2"},
		{"--chart-version", "0.2"}, {"--chart-version", "latest", "--chart-source", "bundled"},
		{"uninstall", "--chart-version", "latest"}, {"version", "--chart-source", "github"},
	} {
		if _, err := parseCLI(args); err == nil {
			t.Fatalf("accepted invalid arguments %v", args)
		}
	}
}

func testChartLock(source, chartVersion string) chartLock {
	return chartLock{SchemaVersion: 1, Source: source, Version: chartVersion, Commit: strings.Repeat("a", 40), Digests: map[string]string{
		"patchworks-infra": digest.FromString("infra").String(), "patchworks-app": digest.FromString("app").String(),
	}}
}

func TestChartSelectionPrecedence(t *testing.T) {
	public := testChartLock("github", "0.3.0")
	bundled := testChartLock("bundled", "0.1.0")
	for _, tc := range []struct {
		name     string
		opts     cliOptions
		defaults localConfig
		lock     *chartLock
		want     chartRequest
		pinned   bool
	}{
		{"new install", cliOptions{}, localConfig{}, nil, chartRequest{"github", "latest"}, false},
		{"repeat", cliOptions{}, localConfig{}, &public, chartRequest{"github", "0.3.0"}, true},
		{"refresh saved exact config", cliOptions{ChartVersion: "latest"}, localConfig{ChartSource: "github", ChartVersion: "0.2.0"}, &public, chartRequest{"github", "latest"}, false},
		{"repeat after refresh beats stale config", cliOptions{}, localConfig{ChartSource: "github", ChartVersion: "0.2.0"}, &public, chartRequest{"github", "0.3.0"}, true},
		{"moving config is pinned", cliOptions{}, localConfig{ChartVersion: "latest"}, &public, chartRequest{"github", "0.3.0"}, true},
		{"explicit version", cliOptions{ChartVersion: "v0.4.0"}, localConfig{}, &public, chartRequest{"github", "0.4.0"}, false},
		{"explicit same version preserves digest", cliOptions{ChartVersion: "0.3.0"}, localConfig{}, &public, chartRequest{"github", "0.3.0"}, true},
		{"offline override", cliOptions{ChartSource: "bundled"}, localConfig{ChartVersion: "latest"}, &public, chartRequest{"bundled", ""}, false},
		{"repeat offline", cliOptions{}, localConfig{}, &bundled, chartRequest{"bundled", "0.1.0"}, true},
		{"version overrides offline config", cliOptions{ChartVersion: "latest"}, localConfig{ChartSource: "bundled", ChartVersion: "0.1.0"}, &bundled, chartRequest{"github", "latest"}, false},
		{"source overrides offline config", cliOptions{ChartSource: "github"}, localConfig{ChartSource: "bundled", ChartVersion: "0.1.0"}, &bundled, chartRequest{"github", "latest"}, false},
		{"initial config version", cliOptions{}, localConfig{ChartVersion: "0.2.0"}, nil, chartRequest{"github", "0.2.0"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, pinned, err := chooseChartRequest(tc.opts, tc.defaults, tc.lock)
			if err != nil || got != tc.want || (pinned != nil) != tc.pinned {
				t.Fatalf("got %+v, pinned %v, err %v; want %+v, pinned %v", got, pinned != nil, err, tc.want, tc.pinned)
			}
		})
	}
}

func TestChartCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, api, minimum, current string
		valid                       bool
	}{
		{"legacy", "", "", "dev", true}, {"current API", "1", "", "dev", true},
		{"future API", "2", "", "1.2.0", false}, {"meets minimum", "1", "1.2.0", "v1.2.0", true},
		{"below minimum", "1", "1.2.0", "1.1.0", false}, {"prerelease below minimum", "1", "1.2.0", "1.2.0-rc.1", false},
		{"unknown build", "1", "1.2.0", "dev", false}, {"invalid minimum", "1", "banana", "1.2.0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chart := &chartv2.Chart{Metadata: &chartv2.Metadata{Name: "patchworks-app", Version: "0.2.0", APIVersion: "v2", Annotations: map[string]string{installerAPIAnnotation: tc.api, minimumInstallerAnnotation: tc.minimum}}}
			_, err := validateSelectedChart(chart, "patchworks-app", "0.2.0", tc.current)
			if (err == nil) != tc.valid {
				t.Fatalf("valid %v, got %v", tc.valid, err)
			}
		})
	}
	chart := &chartv2.Chart{Metadata: &chartv2.Metadata{Name: "patchworks-app", Version: "0.2.0", APIVersion: "v2", Dependencies: []*chartv2.Dependency{{Name: "missing", Version: "1.0.0"}}}}
	if _, err := validateSelectedChart(chart, "patchworks-app", "0.2.0", "1.0.0"); err == nil {
		t.Fatal("accepted missing dependency")
	}
}

func TestBundledChartsAndLockIntegrity(t *testing.T) {
	prepared, err := prepareChartsFromRepository(chartRequest{Source: "bundled"}, nil, chartRepositoryClient{})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "nested", "values.yaml.charts.lock.yaml")
	if err := writeChartLock(path, prepared.Lock); err != nil {
		t.Fatal(err)
	}
	lock, err := readChartLock(path)
	if err != nil || !reflect.DeepEqual(*lock, prepared.Lock) {
		t.Fatalf("round trip: %+v %v", lock, err)
	}
	if _, err := prepareChartsFromRepository(chartRequest{"bundled", lock.Version}, lock, chartRepositoryClient{}); err != nil {
		t.Fatal(err)
	}
	lock.Digests["patchworks-app"] = digest.FromString("changed").String()
	if _, err := prepareChartsFromRepository(chartRequest{"bundled", lock.Version}, lock, chartRepositoryClient{}); err == nil {
		t.Fatal("accepted changed bundled content")
	}
	if err := os.WriteFile(path, []byte("schemaVersion: 1\nsource: oci\nversion: latest\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readChartLock(path); err == nil {
		t.Fatal("accepted invalid lock")
	}
}

func TestSavedChartConfigAndManualCommands(t *testing.T) {
	config := localConfigFromInstallConfig(installConfig{Values: map[string]string{"chartSource": "github", "chartVersion": "0.2.0"}}, false)
	data, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var restored localConfig
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.ChartSource != "github" || restored.ChartVersion != "0.2.0" {
		t.Fatalf("lost chart settings: %s", data)
	}
	for _, infra := range []bool{true, false} {
		commands := selectedHelmCommandText("patchworks", "my values' file.yaml", infra)
		if !strings.Contains(commands, "unpack-charts --chart-lock") || !strings.Contains(commands, ".charts.lock.yaml") || strings.Contains(commands, "latest") || strings.Contains(commands, "oci://") {
			t.Fatalf("manual commands must use the saved lock: %s", commands)
		}
		if strings.Contains(commands, "patchworks-infra") != infra {
			t.Fatalf("wrong infra commands: %s", commands)
		}
		if !strings.Contains(commands, `-f 'my values'"'"' file.yaml'`) {
			t.Fatalf("path not quoted: %s", commands)
		}
	}
	if !bytes.Contains(data, []byte("chartVersion: 0.2.0")) {
		t.Fatalf("did not save exact version: %s", data)
	}
}

func TestUnpackChartsRefusesToMixVersions(t *testing.T) {
	output := filepath.Join(t.TempDir(), "charts")
	opts := cliOptions{ChartsOutputDir: output}
	if err := runUnpackCharts(opts); err != nil {
		t.Fatal(err)
	}
	lock, err := readChartLock(filepath.Join(output, "charts.lock.yaml"))
	if err != nil || lock.Source != "bundled" {
		t.Fatalf("export lock: %+v, %v", lock, err)
	}
	path := filepath.Join(output, "patchworks-app", "templates", "local-template.yaml")
	if err := os.WriteFile(path, []byte("keep my file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runUnpackCharts(opts); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing-directory error, got %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "keep my file" {
		t.Fatal("overwrote existing chart")
	}
}

func releaseSnapshot(t *testing.T, release string, change func(map[string][]byte)) []byte {
	t.Helper()
	files := map[string][]byte{}
	for _, name := range patchworksChartNames {
		files["charts/"+name+"/Chart.yaml"] = []byte("apiVersion: v2\nname: " + name + "\nversion: 0.1.0\n")
		files["charts/"+name+"/values.yaml"] = []byte("image:\n  tag: v" + release + "\n")
	}
	files["README.md"] = []byte("not a chart")
	if change != nil {
		change(files)
	}
	data, err := packChartFiles("selfhosted-commit", files)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPublicRepositoryReleaseSelectionAndPinning(t *testing.T) {
	currentSHA, previousSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	current, previous := releaseSnapshot(t, "0.1.39", nil), releaseSnapshot(t, "0.1.38", nil)
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("public downloads sent credentials")
		}
		calls = append(calls, r.URL.RequestURI())
		switch r.URL.Path {
		case "/repos/gopatchworks/selfhosted/commits":
			if r.URL.Query().Get("path") != "charts/patchworks-app/values.yaml" || r.URL.Query().Get("sha") != "main" {
				t.Error("incorrect history scope")
			}
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", `</commits?page=2>; rel="next"`)
				json.NewEncoder(w).Encode([]any{
					map[string]any{"sha": currentSHA, "commit": map[string]string{"message": "Update chart documentation"}},
					map[string]any{"sha": currentSHA, "commit": map[string]string{"message": "Update image tags for v0.2.0-rc.1"}},
					map[string]any{"sha": currentSHA, "commit": map[string]string{"message": "Update image tags for v0.1.39\n\nRelease automation"}},
				})
			} else {
				json.NewEncoder(w).Encode([]any{map[string]any{"sha": previousSHA, "commit": map[string]string{"message": "Update image tags for v0.1.38"}}})
			}
		case "/archive/" + currentSHA:
			w.Write(current)
		case "/archive/" + previousSHA:
			w.Write(previous)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := chartRepositoryClient{HTTP: server.Client(), APIURL: server.URL + "/repos/gopatchworks/selfhosted", ArchiveURL: server.URL + "/archive/"}
	prepared, err := prepareChartsFromRepository(chartRequest{"github", "latest"}, nil, client)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Lock.Version != "0.1.39" || prepared.Lock.Commit != currentSHA || len(prepared.Charts) != 2 {
		t.Fatalf("wrong release: %+v", prepared.Lock)
	}
	// Chart metadata is 0.1.0; the selected application release is 0.1.39.
	if len(calls) != 2 {
		t.Fatalf("unexpected requests: %v", calls)
	}
	calls = nil
	repeated, err := prepareChartsFromRepository(chartRequest{"github", "0.1.39"}, &prepared.Lock, client)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prepared.Lock, repeated.Lock) || !reflect.DeepEqual(calls, []string{"/archive/" + currentSHA}) {
		t.Fatalf("repeat changed selection or looked up latest: %v", calls)
	}
	older, err := prepareChartsFromRepository(chartRequest{"github", "0.1.38"}, nil, client)
	if err != nil {
		t.Fatal(err)
	}
	if older.Lock.Commit != previousSHA || older.Lock.Version != "0.1.38" {
		t.Fatalf("incorrect older release: %+v", older.Lock)
	}
	if _, err := prepareChartsFromRepository(chartRequest{"github", "0.1.99"}, nil, client); err == nil {
		t.Fatal("accepted unpublished version")
	}
}

func TestPublicReleaseFailuresDoNotFallBack(t *testing.T) {
	sha := strings.Repeat("c", 40)
	for _, failure := range []string{"rate limit", "bad sha", "missing app", "wrong image release", "corrupt archive", "incompatible"} {
		t.Run(failure, func(t *testing.T) {
			archive := releaseSnapshot(t, "0.1.39", func(files map[string][]byte) {
				switch failure {
				case "missing app":
					delete(files, "charts/patchworks-app/Chart.yaml")
				case "wrong image release":
					files["charts/patchworks-app/values.yaml"] = []byte("image:\n  tag: v0.1.38\n")
				case "incompatible":
					files["charts/patchworks-app/Chart.yaml"] = []byte("apiVersion: v2\nname: patchworks-app\nversion: 0.1.0\nannotations:\n  " + installerAPIAnnotation + ": '2'\n")
				}
			})
			if failure == "corrupt archive" {
				archive = []byte("bad archive")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/commits" {
					if failure == "rate limit" {
						w.Header().Set("X-RateLimit-Remaining", "0")
						w.Header().Set("X-RateLimit-Reset", "123")
						w.WriteHeader(http.StatusForbidden)
						return
					}
					commit := sha
					if failure == "bad sha" {
						commit = "main"
					}
					fmt.Fprintf(w, `[{"sha":%q,"commit":{"message":"Update image tags for v0.1.39"}}]`, commit)
					return
				}
				w.Write(archive)
			}))
			defer server.Close()
			client := chartRepositoryClient{server.Client(), server.URL, server.URL + "/archive/"}
			prepared, err := prepareChartsFromRepository(chartRequest{"github", "latest"}, nil, client)
			if err == nil || prepared != nil {
				t.Fatalf("accepted failed release: %+v %v", prepared, err)
			}
			if failure == "rate limit" && !strings.Contains(err.Error(), "anonymous request limit") {
				t.Fatalf("unclear rate limit error: %v", err)
			}
		})
	}
}

func TestSnapshotRejectsUnsafePaths(t *testing.T) {
	for _, unsafe := range []string{"../escape", "templates/../../escape", "/absolute", `templates\escape`} {
		snapshot := releaseSnapshot(t, "0.1.39", func(files map[string][]byte) { files["charts/patchworks-app/"+unsafe] = []byte("bad") })
		if _, err := chartsFromSnapshot(snapshot); err == nil {
			t.Fatalf("accepted unsafe chart entry %q", unsafe)
		}
	}
}

func TestChartLockCLI(t *testing.T) {
	opts, err := parseCLI([]string{"unpack-charts", "--chart-lock", "values.yaml.charts.lock.yaml"})
	if err != nil || opts.ChartLockPath != "values.yaml.charts.lock.yaml" {
		t.Fatalf("parse lock: %+v %v", opts, err)
	}
	for _, args := range [][]string{
		{"--chart-lock", "lock.yaml"}, {"unpack-charts", "--chart-lock="},
		{"unpack-charts", "--chart-lock", "lock.yaml", "--chart-version", "latest"},
		{"unpack-charts", "--chart-lock", "lock.yaml", "--chart-source", "bundled"},
	} {
		if _, err := parseCLI(args); err == nil {
			t.Fatalf("accepted invalid chart lock options: %v", args)
		}
	}
}
