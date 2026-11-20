package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
	helmloader "helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
)

// monoInboundValues are installer answers for a bundled mono installation.
func monoInboundValues(inbound, routes string) map[string]string {
	return map[string]string{
		"namespace":         "patchworks",
		"infrastructure":    "bundled",
		"domain":            "selfhosted.example.com",
		"scheme":            "https",
		"licenseKey":        "license-key",
		"licenseServerUrl":  "https://license.example.com",
		"ingressEnabled":    "true",
		"ingressProvider":   "contour",
		"ingressClass":      "contour",
		"dashboardEnabled":  "true",
		"routingMode":       "host",
		"workerMode":        "mono",
		"monoInbound":       inbound,
		"monoInboundRoutes": routes,
		"cookieDomain":      ".selfhosted.example.com",
		"seedInstall":       "false",
		"companyName":       "Self-Hosted",
		"adminName":         "Admin User",
		"adminEmail":        "admin@example.com",
		"userRole":          "superadmin",
	}
}

func writeMonoInboundValues(t *testing.T, values map[string]string) (string, map[string]any) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "patchworks.values.yaml")
	if err := writeValues(path, values); err != nil {
		t.Fatalf("write values: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read values: %v", err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("values file is not YAML: %v\n%s", err, data)
	}
	return string(data), parsed
}

func TestWriteValuesServesMonoInboundTriggers(t *testing.T) {
	rendered, values := writeMonoInboundValues(t, monoInboundValues("true", "true"))
	if !strings.Contains(rendered, "workers:\n  type: \"mono\"\n  mono:\n    inbound:\n      enabled: true\n      ingress:\n        enabled: true\n\n") {
		t.Fatalf("values file does not turn on inbound triggers and their routes:\n%s", rendered)
	}

	// The real chart then serves them from the hub and routes the webhook host's
	// /api/v1/ to it.
	objects := renderComponentSelection(t, "patchworks-app", "patchworks", values)
	var hubPort any
	var webhookRoutes []string
	for _, object := range objects {
		labels := componentSelectionMap(object, "metadata", "labels")
		if object["kind"] == "Deployment" && labels["app.kubernetes.io/component"] == "workers" {
			hubPort = componentSelectionContainerEnv(object)["PUBLIC_HTTP_PORT"]["value"]
		}
		if object["kind"] == "HTTPProxy" && componentSelectionMap(object, "metadata")["name"] == "patchworks-webhook" {
			for _, rawRoute := range componentSelectionMap(object, "spec")["routes"].([]any) {
				route := rawRoute.(map[string]any)
				prefix := route["conditions"].([]any)[0].(map[string]any)["prefix"]
				service := route["services"].([]any)[0].(map[string]any)["name"]
				webhookRoutes = append(webhookRoutes, prefix.(string)+" -> "+service.(string))
			}
		}
	}
	if hubPort != "8082" {
		t.Fatalf("hub PUBLIC_HTTP_PORT = %v, want 8082", hubPort)
	}
	want := []string{"/api/v1/otc/ -> patchworks-start", "/api/v1/mcp -> patchworks-start", "/api/v1/ -> patchworks-workers-inbound", "/ -> patchworks-start"}
	if !reflect.DeepEqual(webhookRoutes, want) {
		t.Fatalf("webhook routes = %v, want %v", webhookRoutes, want)
	}
}

func TestWriteValuesLeavesRoutesToCoreUntilAsked(t *testing.T) {
	rendered, _ := writeMonoInboundValues(t, monoInboundValues("true", "false"))
	if !strings.Contains(rendered, "    inbound:\n      enabled: true\n      ingress:\n        enabled: false\n") {
		t.Fatalf("values file does not keep the routes on Core:\n%s", rendered)
	}
}

func TestWriteValuesOmitsMonoInboundWhenOff(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"not chosen":  monoInboundValues("false", "false"),
		"unsupported": withValue(monoInboundValues("true", "true"), "monoInboundUnsupported", "Monocore v0.2.12 predates them"),
		"standalone":  withValue(monoInboundValues("true", "true"), "workerMode", "standalone"),
	} {
		t.Run(name, func(t *testing.T) {
			rendered, _ := writeMonoInboundValues(t, values)
			if strings.Contains(rendered, "inbound") || !strings.Contains(rendered, "  type: ") || strings.Contains(rendered, "  mono:\n") {
				t.Fatalf("values file should only set the worker type:\n%s", rendered)
			}
		})
	}
}

func withValue(values map[string]string, key, value string) map[string]string {
	values[key] = value
	return values
}

func TestMonoInboundSummary(t *testing.T) {
	for want, values := range map[string]map[string]string{
		"off":                              monoInboundValues("false", "false"),
		"on, ingress routes to Monocore":   monoInboundValues("true", "true"),
		"on, ingress routes still to Core": monoInboundValues("true", "false"),
		"off: Monocore v0.2.12 predates them; choose a newer release with --chart-version": withValue(monoInboundValues("true", "true"),
			"monoInboundUnsupported", "Monocore v0.2.12 predates them"),
	} {
		if got := monoInboundSummary(values); got != want {
			t.Errorf("summary = %q, want %q", got, want)
		}
	}
	if summary := installSummary(installConfig{Values: withValue(monoInboundValues("true", "true"), "workerMode", "standalone")}); strings.Contains(summary, "inbound") {
		t.Fatalf("summary lists inbound triggers for standalone workers:\n%s", summary)
	}
	if summary := installSummary(installConfig{Values: monoInboundValues("true", "false")}); !strings.Contains(summary, "Monocore inbound triggers") {
		t.Fatalf("summary does not list inbound triggers for mono workers:\n%s", summary)
	}
}

// The bundled chart at this commit has the setting; its image tag decides.
func TestMonoInboundUnsupportedReason(t *testing.T) {
	load := func(t *testing.T, edit func(mono map[string]any)) *chartv2.Chart {
		t.Helper()
		loaded, err := helmloader.Load(filepath.Join("..", "..", "charts", "patchworks-app"))
		if err != nil {
			t.Fatal(err)
		}
		chart := loaded.(*chartv2.Chart)
		edit(chart.Values["workers"].(map[string]any)["mono"].(map[string]any))
		return chart
	}
	tag := func(value string) func(map[string]any) {
		return func(mono map[string]any) { mono["image"].(map[string]any)["tag"] = value }
	}
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{"last release without the listener", tag("v0.2.12"), "Monocore v0.2.12 predates them"},
		{"older release", tag("v0.1.118"), "Monocore v0.1.118 predates them"},
		{"later release", tag("v0.2.13"), ""},
		{"branch build", tag("main-4f70a29"), ""},
		{"chart without the setting", func(mono map[string]any) { delete(mono, "inbound") }, "this chart release cannot serve them"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := monoInboundUnsupportedReason(load(t, tc.edit)); got != tc.want {
				t.Fatalf("reason = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLocalConfigKeepsMonoInboundAnswers(t *testing.T) {
	var config localConfig
	if err := yaml.Unmarshal([]byte("workers:\n  type: mono\n  inbound:\n    enabled: true\n    routeIngress: false\n"), &config); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if config.MonoInbound == nil || !*config.MonoInbound || config.MonoInboundRoutes == nil || *config.MonoInboundRoutes {
		t.Fatalf("inbound answers = %v, %v", config.MonoInbound, config.MonoInboundRoutes)
	}

	// A release that cannot serve them keeps the answers for a later run.
	values := withValue(monoInboundValues("true", "true"), "monoInboundUnsupported", "Monocore v0.2.12 predates them")
	data, err := yaml.Marshal(localConfigFromInstallConfig(installConfig{Values: values}, true))
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	var saved localConfig
	if err := yaml.Unmarshal(data, &saved); err != nil {
		t.Fatalf("unmarshal saved config: %v", err)
	}
	if saved.MonoInbound == nil || !*saved.MonoInbound || saved.MonoInboundRoutes == nil || !*saved.MonoInboundRoutes {
		t.Fatalf("saved config lost the inbound answers:\n%s", data)
	}

	data, err = yaml.Marshal(localConfigFromInstallConfig(installConfig{Values: withValue(monoInboundValues("true", "true"), "workerMode", "standalone")}, true))
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if strings.Contains(string(data), "inbound") {
		t.Fatalf("saved config has inbound answers for standalone workers:\n%s", data)
	}
}
