package main

import (
	"path/filepath"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
	helmloader "helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
)

// samplesValues are installer answers for a mono installation with samples
// whose selected release was checked; flags is samplesSupport's shapeFlags.
func samplesValues(flags string) map[string]string {
	values := monoInboundValues("false", "false")
	values["seedInstall"] = "true"
	values["monoAllCompanies"] = "true"
	values["samples"] = "true"
	values["samplesChecked"] = "true"
	values["samplesShapeFlags"] = flags
	return values
}

func findObject(objects []map[string]any, kind, name string) map[string]any {
	for _, object := range objects {
		if object["kind"] == kind && object["metadata"].(map[string]any)["name"] == name {
			return object
		}
	}
	return nil
}

func TestWriteValuesInstallsSamplesWithMonocoreShapeEnv(t *testing.T) {
	rendered, parsed := writeMonoInboundValues(t, samplesValues("extraEnv"))
	for _, want := range []string{
		"    allCompanies:\n      enabled: true\n",
		"    extraEnv:\n      - name: WORKER_ENABLE_MERGE\n        value: \"true\"\n",
		"      - name: WORKER_ENABLE_SYNC_SUBPROCESS\n        value: \"true\"\n",
		"samples:\n  enabled: true\n",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("values file lacks %q:\n%s", want, rendered)
		}
	}

	// The real chart accepts them: the samples Job renders, and Monocore gets the shapes.
	objects := renderComponentSelection(t, "patchworks", "patchworks", parsed)
	if findObject(objects, "Job", "patchworks-samples") == nil || findObject(objects, "ConfigMap", "patchworks-samples") == nil {
		t.Fatalf("samples Job or ConfigMap missing")
	}
	env := containerEnv(findObject(objects, "Deployment", "patchworks-workers"))
	for _, shape := range sampleShapes {
		if env[shape.env] != "true" {
			t.Fatalf("Monocore %s = %q, want true", shape.env, env[shape.env])
		}
	}
}

func TestWriteValuesUsesTheChartsShapeKeysWhenItHasThem(t *testing.T) {
	rendered, _ := writeMonoInboundValues(t, samplesValues("keys"))
	want := "    enable:\n      merge: true\n      dataTransform: true\n      crossReference: true\n      syncSubprocess: true\n"
	if !strings.Contains(rendered, want) || strings.Contains(rendered, "WORKER_ENABLE_") {
		t.Fatalf("values file does not use workers.mono.enable:\n%s", rendered)
	}
}

func TestWriteValuesOmitsSamplesUnlessChecked(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
	}{
		{"not chosen", withValue(samplesValues("extraEnv"), "samples", "false")},
		{"support not checked", withValue(samplesValues("extraEnv"), "samplesChecked", "")},
		{"unsupported release", withValue(samplesValues("extraEnv"), "samplesUnsupported", "this chart release has no samples")},
		{"standalone workers", withValue(samplesValues("extraEnv"), "workerMode", "standalone")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rendered, _ := writeMonoInboundValues(t, tc.values)
			if strings.Contains(rendered, "samples:") || strings.Contains(rendered, "WORKER_ENABLE_") || strings.Contains(rendered, "enable:") {
				t.Fatalf("samples written:\n%s", rendered)
			}
		})
	}
}

func TestSamplesSupport(t *testing.T) {
	load := func(t *testing.T, edit func(values map[string]any)) *chartv2.Chart {
		t.Helper()
		loaded, err := helmloader.Load(filepath.Join("..", "..", "charts", "patchworks-app"))
		if err != nil {
			t.Fatal(err)
		}
		chart := loaded.(*chartv2.Chart)
		edit(chart.Values)
		return chart
	}
	unchanged := func(map[string]any) {}
	withoutEnableKeys := func(values map[string]any) {
		delete(values["workers"].(map[string]any)["mono"].(map[string]any), "enable")
	}
	withEnableKeys := func(values map[string]any) {
		values["workers"].(map[string]any)["mono"].(map[string]any)["enable"] = map[string]any{"merge": nil}
	}
	for _, tc := range []struct {
		name                       string
		edit                       func(map[string]any)
		values                     map[string]string
		wantUnsupported, wantFlags string
	}{
		{"chart without shape keys", withoutEnableKeys, samplesValues(""), "", "extraEnv"},
		{"chart with shape keys", withEnableKeys, samplesValues(""), "", "keys"},
		{"chart without samples", func(values map[string]any) { delete(values, "samples") }, samplesValues(""), "this chart release has no samples", ""},
		{"standalone workers", unchanged, withValue(samplesValues(""), "workerMode", "standalone"), "samples run on Monocore workers only", ""},
		{"release without allCompanies", unchanged, withValue(samplesValues(""), "monoAllCompanies", "false"), "they need every company on Monocore, which this release cannot run", ""},
		{"no initial company", unchanged, withValue(samplesValues(""), "seedInstall", "false"), "they are created in the initial company, which this run does not create", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unsupported, flags := samplesSupport(load(t, tc.edit), tc.values)
			if unsupported != tc.wantUnsupported || flags != tc.wantFlags {
				t.Fatalf("samplesSupport = (%q, %q), want (%q, %q)", unsupported, flags, tc.wantUnsupported, tc.wantFlags)
			}
		})
	}
}

func TestSamplesSummary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
		want   string
	}{
		{"off", withValue(samplesValues("extraEnv"), "samples", "false"), "off"},
		{"on", samplesValues("extraEnv"), "on: sample flows in the Samples folder, with Monocore's optional shapes on"},
		{"unsupported", withValue(samplesValues("extraEnv"), "samplesUnsupported", "this chart release has no samples"), "off: this chart release has no samples"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := samplesSummary(tc.values); got != tc.want {
				t.Fatalf("summary = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLocalConfigKeepsSamples(t *testing.T) {
	data, err := yaml.Marshal(localConfig{Samples: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "samples: true\n") {
		t.Fatalf("saved config:\n%s", data)
	}
	var loaded localConfig
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Samples == nil || !*loaded.Samples {
		t.Fatalf("loaded samples = %v", loaded.Samples)
	}
	if got := monoSetting(withValue(samplesValues(""), "workerMode", "standalone"), "samples"); got != nil {
		t.Fatalf("samples kept for standalone workers: %v", *got)
	}
}

func TestSamplesJobHasAProgressPhase(t *testing.T) {
	if got := phaseNamesForChart("app")["samples"]; got != "Creating sample flows" {
		t.Fatalf("samples phase = %q", got)
	}
}
