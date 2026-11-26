package main

import (
	"path/filepath"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
	helmloader "helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
)

// telemetryValues are installer answers for a bundled installation with
// Monitoring whose selected release was checked to support it.
func telemetryValues() map[string]string {
	values := monoInboundValues("false", "false")
	values["workerMode"] = "standalone"
	values["telemetry"] = "true"
	values["telemetryChecked"] = "true"
	return values
}

func loadChart(t *testing.T, name string, edit func(values map[string]any)) *chartv2.Chart {
	t.Helper()
	loaded, err := helmloader.Load(filepath.Join("..", "..", "charts", name))
	if err != nil {
		t.Fatal(err)
	}
	chart := loaded.(*chartv2.Chart)
	if edit != nil {
		edit(chart.Values)
	}
	return chart
}

func TestWriteValuesTurnsOnTelemetryWithBundledClickHouse(t *testing.T) {
	rendered, parsed := writeMonoInboundValues(t, telemetryValues())
	for _, want := range []string{"clickhouse:\n  enabled: true\n", "telemetry:\n  enabled: true\n"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("values file lacks %q:\n%s", want, rendered)
		}
	}

	// The real charts accept the one values file: infra runs ClickHouse and
	// creates its users, and the app ships telemetry into it.
	infra := renderInfraObjects(t, parsed)
	for _, want := range []struct{ kind, name string }{
		{"Deployment", "patchworks-clickhouse"},
		{"Service", "patchworks-clickhouse"},
		{"PersistentVolumeClaim", "patchworks-clickhouse"},
		{"Job", "patchworks-clickhouse-setup"},
	} {
		if findObject(infra, want.kind, want.name) == nil {
			t.Fatalf("infra chart has no %s %s", want.kind, want.name)
		}
	}
	app := renderComponentSelection(t, "patchworks", "patchworks", parsed)
	shipper := findObject(app, "Deployment", "patchworks-telemetry-shipper")
	if shipper == nil || findObject(app, "Job", "patchworks-telemetry-setup") == nil {
		t.Fatalf("telemetry shipper Deployment or setup Job missing")
	}
	config := findObject(app, "ConfigMap", "patchworks-config")["data"].(map[string]any)
	if config["TELEMETRY_ENABLED"] != "true" || config["TELEMETRY_CLICKHOUSE_HOST"] != "patchworks-clickhouse.patchworks.svc.cluster.local" {
		t.Fatalf("Core config has no telemetry: %v", config)
	}
}

func TestWriteValuesOmitsTelemetryUnlessSupported(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
	}{
		{"not chosen", withValue(telemetryValues(), "telemetry", "false")},
		{"support not checked", withValue(telemetryValues(), "telemetryChecked", "")},
		{"unsupported release", withValue(telemetryValues(), "telemetryUnsupported", "this chart release has no platform telemetry")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rendered, _ := writeMonoInboundValues(t, tc.values)
			if strings.Contains(rendered, "clickhouse:") || strings.Contains(rendered, "telemetry:") {
				t.Fatalf("telemetry written:\n%s", rendered)
			}
		})
	}
}

func TestTelemetrySupport(t *testing.T) {
	without := func(key string) func(map[string]any) {
		return func(values map[string]any) { delete(values, key) }
	}
	for _, tc := range []struct {
		name       string
		app, infra func(map[string]any)
		values     map[string]string
		want       string
	}{
		{"supported", withCoreTag("v0.2.21"), nil, telemetryValues(), ""},
		{"branch build", withCoreTag("feature-data-and-merge-shapes"), nil, telemetryValues(), ""},
		{"Core without telemetry", withCoreTag("v0.2.20"), nil, telemetryValues(), "Core v0.2.20 predates platform telemetry"},
		{"external infrastructure", nil, nil, withValue(telemetryValues(), "infrastructure", "external"), "the installer only sets up the bundled ClickHouse; set clickhouse.external.* and telemetry.enabled in the values file"},
		{"lightweight profile", nil, nil, withValue(withValue(telemetryValues(), "profile", "lightweight"), "profileChecked", "true"), "the lightweight profile has no memory to spare for ClickHouse"},
		{"app chart without telemetry", without("telemetry"), nil, telemetryValues(), "this chart release has no platform telemetry"},
		{"infra chart without ClickHouse", withCoreTag("v0.2.21"), without("clickhouse"), telemetryValues(), "this chart release has no bundled ClickHouse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := telemetrySupport(loadChart(t, "patchworks-app", tc.app), loadChart(t, "patchworks-infra", tc.infra), tc.values)
			if got != tc.want {
				t.Fatalf("telemetrySupport = %q, want %q", got, tc.want)
			}
		})
	}
}

func withCoreTag(tag string) func(map[string]any) {
	return func(values map[string]any) {
		values["image"].(map[string]any)["tag"] = tag
	}
}

func TestTelemetryNeedsInfraUpgradesAnInstallWithoutClickHouse(t *testing.T) {
	for _, tc := range []struct {
		name     string
		values   map[string]string
		deployed bool
		want     bool
	}{
		{"monitoring on, no ClickHouse yet", telemetryValues(), false, true},
		{"monitoring on, ClickHouse running", telemetryValues(), true, false},
		{"monitoring off", withValue(telemetryValues(), "telemetry", "false"), false, false},
		{"unsupported release", withValue(telemetryValues(), "telemetryUnsupported", "Core v0.2.20 predates platform telemetry"), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := telemetryNeedsInfra(tc.values, tc.deployed); got != tc.want {
				t.Fatalf("telemetryNeedsInfra = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestTelemetrySummary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
		want   string
	}{
		{"off", withValue(telemetryValues(), "telemetry", "false"), "off"},
		{"on", telemetryValues(), "on: bundled ClickHouse and the telemetry shipper, for the Monitoring page"},
		{"unsupported", withValue(telemetryValues(), "telemetryUnsupported", "this chart release has no platform telemetry"), "off: this chart release has no platform telemetry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := telemetrySummary(tc.values); got != tc.want {
				t.Fatalf("summary = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLocalConfigKeepsTelemetry(t *testing.T) {
	data, err := yaml.Marshal(localConfig{Telemetry: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "telemetry: true\n") {
		t.Fatalf("saved config:\n%s", data)
	}
	var loaded localConfig
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Telemetry == nil || !*loaded.Telemetry {
		t.Fatalf("loaded telemetry = %v", loaded.Telemetry)
	}
	if got := localConfigFromInstallConfig(installConfig{Values: map[string]string{}}, false).Telemetry; got != nil {
		t.Fatalf("telemetry saved for a run that never asked: %v", *got)
	}
}

func TestTelemetryJobsHaveProgressPhases(t *testing.T) {
	if got := phaseNamesForChart("infra")["clickhouse-setup"]; got != "Setting up ClickHouse telemetry users" {
		t.Fatalf("clickhouse-setup phase = %q", got)
	}
	if got := phaseNamesForChart("app")["telemetry-setup"]; got != "Migrating telemetry tables" {
		t.Fatalf("telemetry-setup phase = %q", got)
	}
}
