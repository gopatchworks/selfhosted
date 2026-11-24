package main

import (
	"path/filepath"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
	helmloader "helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	"k8s.io/apimachinery/pkg/api/resource"
)

// lightweightValues are installer answers for a lightweight installation whose
// selected release was checked; apart is lightweightSupport's webApart.
func lightweightValues(apart string) map[string]string {
	values := monoInboundValues("false", "false")
	values["profile"] = "lightweight"
	values["profileChecked"] = "true"
	values["lightweightWebApart"] = apart
	return values
}

func deploymentNames(objects []map[string]any) map[string]map[string]any {
	found := map[string]map[string]any{}
	for _, object := range objects {
		if object["kind"] == "Deployment" {
			found[object["metadata"].(map[string]any)["name"].(string)] = object
		}
	}
	return found
}

func containerEnv(deployment map[string]any) map[string]string {
	container := deployment["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
	env := map[string]string{}
	for _, entry := range container["env"].([]any) {
		e := entry.(map[string]any)
		if value, ok := e["value"].(string); ok {
			env[e["name"].(string)] = value
		}
	}
	return env
}

func TestWriteValuesSelectsTheLightweightProfile(t *testing.T) {
	rendered, parsed := writeMonoInboundValues(t, lightweightValues(""))
	if !strings.Contains(rendered, "profile: \"lightweight\"\n\nworkers:\n") {
		t.Fatalf("profile missing:\n%s", rendered)
	}
	if _, ok := parsed["lightweight"]; ok {
		t.Fatalf("lightweight block written for a release that combines web: %#v", parsed["lightweight"])
	}

	deployments := deploymentNames(renderComponentSelection(t, "patchworks", "patchworks", parsed))
	if _, ok := deployments["patchworks-processor"]; !ok {
		t.Fatalf("lightweight worker missing: %v", keys(deployments))
	}
	if _, ok := deployments["patchworks-start"]; ok {
		t.Fatalf("start Deployment rendered with web combined")
	}
	for name := range deployments {
		if strings.HasPrefix(name, "patchworks-processor-") {
			t.Fatalf("processor Deployment %s rendered in lightweight", name)
		}
	}
	if got := containerEnv(deployments["patchworks-gateway"])["APP_DOMAINS"]; got != "start" {
		t.Fatalf("gateway APP_DOMAINS = %q, want start", got)
	}
}

// memoryRequest is the first container's memory request, empty when unset.
func memoryRequest(deployment map[string]any) string {
	container := deployment["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
	resources, _ := container["resources"].(map[string]any)
	requests, _ := resources["requests"].(map[string]any)
	memory, _ := requests["memory"].(string)
	return memory
}

// The lightweight profile asks the scheduler for little enough memory that the
// app chart's pods fit next to the bundled infrastructure on a 4 GB node; the
// standard profile keeps its requests.
func TestLightweightProfileRequestsLessMemory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
		want   map[string]string
		limit  int64
	}{
		{"lightweight", lightweightValues(""), map[string]string{
			"patchworks-gateway":   "512Mi",
			"patchworks-processor": "384Mi",
			"patchworks-workers":   "128Mi",
		}, 1200 * 1000 * 1000},
		{"lightweight with web apart", lightweightValues("Core v0.2.16 serves one domain per process"), map[string]string{
			"patchworks-gateway":   "512Mi",
			"patchworks-start":     "512Mi",
			"patchworks-processor": "384Mi",
			"patchworks-workers":   "128Mi",
		}, 0},
		{"standard", monoInboundValues("false", "false"), map[string]string{
			"patchworks-gateway":             "900Mi",
			"patchworks-start":               "900Mi",
			"patchworks-processor-scheduler": "900Mi",
			"patchworks-workers":             "900Mi",
		}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, parsed := writeMonoInboundValues(t, tc.values)
			deployments := deploymentNames(renderComponentSelection(t, "patchworks", "patchworks", parsed))
			for name, want := range tc.want {
				deployment, ok := deployments[name]
				if !ok {
					t.Fatalf("%s missing: %v", name, keys(deployments))
				}
				if got := memoryRequest(deployment); got != want {
					t.Errorf("%s memory request = %q, want %q", name, got, want)
				}
			}
			if tc.limit == 0 {
				return
			}
			var total resource.Quantity
			for name, deployment := range deployments {
				if request := memoryRequest(deployment); request != "" {
					quantity, err := resource.ParseQuantity(request)
					if err != nil {
						t.Fatalf("%s memory request %q: %v", name, request, err)
					}
					total.Add(quantity)
				}
			}
			if total.Value() > tc.limit {
				t.Fatalf("app memory requests total %s, above %d bytes", total.String(), tc.limit)
			}
		})
	}
}

func TestWriteValuesKeepsWebApartForAnOlderCore(t *testing.T) {
	rendered, parsed := writeMonoInboundValues(t, lightweightValues("Core v0.2.16 serves one domain per process"))
	if !strings.Contains(rendered, "profile: \"lightweight\"\nlightweight:\n  web:\n    combine: false\n\n") {
		t.Fatalf("combine: false missing:\n%s", rendered)
	}
	deployments := deploymentNames(renderComponentSelection(t, "patchworks", "patchworks", parsed))
	for _, name := range []string{"patchworks-processor", "patchworks-start", "patchworks-gateway"} {
		if _, ok := deployments[name]; !ok {
			t.Fatalf("%s missing: %v", name, keys(deployments))
		}
	}
	if _, ok := containerEnv(deployments["patchworks-gateway"])["APP_DOMAINS"]; ok {
		t.Fatalf("APP_DOMAINS set with web apart")
	}
}

func TestWriteValuesOmitsTheProfileUnlessChecked(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
	}{
		{"standard", monoInboundValues("false", "false")},
		{"support not checked", withValue(lightweightValues(""), "profileChecked", "")},
		{"unsupported release", withValue(lightweightValues(""), "profileUnsupported", "this chart release has no lightweight profile")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rendered, _ := writeMonoInboundValues(t, tc.values)
			if strings.Contains(rendered, "profile:") || strings.Contains(rendered, "lightweight:") {
				t.Fatalf("profile written:\n%s", rendered)
			}
		})
	}
}

func TestProfileSummary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
		want   string
	}{
		{"standard", monoInboundValues("false", "false"), "standard"},
		{"lightweight", lightweightValues(""), "lightweight: one Core web pod and one Core worker pod"},
		{"web apart", lightweightValues("Core v0.2.16 serves one domain per process"), "lightweight: one Core worker pod; separate gateway and start pods, as Core v0.2.16 serves one domain per process"},
		{"unsupported", withValue(lightweightValues(""), "profileUnsupported", "this chart release has no lightweight profile"), "standard: this chart release has no lightweight profile; choose a newer release with --chart-version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := profileSummary(tc.values); got != tc.want {
				t.Fatalf("summary = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLightweightSupport(t *testing.T) {
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
	tag := func(value string) func(map[string]any) {
		return func(values map[string]any) { values["image"].(map[string]any)["tag"] = value }
	}
	for _, tc := range []struct {
		name                       string
		edit                       func(map[string]any)
		wantUnsupported, wantApart string
	}{
		{"later release", tag("v0.2.17"), "", ""},
		{"branch build", tag("main-22cb492"), "", ""},
		{"last release serving one domain per process", tag("v0.2.16"), "", "Core v0.2.16 serves one domain per process"},
		{"chart without the profile", func(values map[string]any) { tag("v0.2.17")(values); delete(values, "lightweight") }, "this chart release has no lightweight profile", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unsupported, apart := lightweightSupport(load(t, tc.edit))
			if unsupported != tc.wantUnsupported || apart != tc.wantApart {
				t.Fatalf("lightweightSupport = (%q, %q), want (%q, %q)", unsupported, apart, tc.wantUnsupported, tc.wantApart)
			}
		})
	}
}

func TestLocalConfigKeepsTheProfile(t *testing.T) {
	data, err := yaml.Marshal(localConfig{Profile: "lightweight"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "profile: lightweight\n") {
		t.Fatalf("saved config:\n%s", data)
	}
	var loaded localConfig
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Profile != "lightweight" {
		t.Fatalf("loaded profile = %q", loaded.Profile)
	}
	if got := defaultProfile("anything else"); got != "standard" {
		t.Fatalf("defaultProfile = %q, want standard", got)
	}
}

func keys(m map[string]map[string]any) []string {
	var names []string
	for name := range m {
		names = append(names, name)
	}
	return names
}
