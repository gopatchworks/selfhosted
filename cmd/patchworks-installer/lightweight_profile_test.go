package main

import (
	"fmt"
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
			if total := requestedMemory(t, deployments); total.Value() > tc.limit {
				t.Fatalf("app memory requests total %s, above %d bytes", total.String(), tc.limit)
			}
		})
	}
}

// Under the lightweight profile the gateway, the Core worker and Monocore
// replace each pod in place, so an upgrade needs no memory beside the running
// pods; the standard profile keeps Kubernetes' default.
func TestLightweightProfileRollsWithoutSurge(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
		names  []string
		want   string
	}{
		{"lightweight", lightweightValues(""), []string{"patchworks-gateway", "patchworks-processor", "patchworks-workers"}, "map[rollingUpdate:map[maxSurge:0 maxUnavailable:1] type:RollingUpdate]"},
		{"standard", monoInboundValues("false", "false"), []string{"patchworks-gateway", "patchworks-start", "patchworks-workers"}, "<nil>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, parsed := writeMonoInboundValues(t, tc.values)
			deployments := deploymentNames(renderComponentSelection(t, "patchworks", "patchworks", parsed))
			for _, name := range tc.names {
				deployment, ok := deployments[name]
				if !ok {
					t.Fatalf("%s missing: %v", name, keys(deployments))
				}
				if got := fmt.Sprint(deployment["spec"].(map[string]any)["strategy"]); got != tc.want {
					t.Errorf("%s strategy = %s, want %s", name, got, tc.want)
				}
			}
		})
	}
}

// requestedMemory sums the Deployments' first-container memory requests.
func requestedMemory(t *testing.T, deployments map[string]map[string]any) resource.Quantity {
	t.Helper()
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
	return total
}

// The same values file sets the infra chart's profile: Elasticsearch, RabbitMQ
// and Soketi request less, and Elasticsearch's heap follows its request. With
// the app chart that is under 2 GiB, which leaves room for an ingress
// controller and k3s's own pods in the ~3 GiB a 4 GB Raspberry Pi 5 can
// allocate once the kubelet reserves memory for the system.
func TestLightweightProfileRequestsLessInfraMemory(t *testing.T) {
	javaOpts := func(deployment map[string]any) string { return containerEnv(deployment)["ES_JAVA_OPTS"] }
	for _, tc := range []struct {
		name     string
		values   map[string]string
		want     map[string]string
		javaOpts string
	}{
		{"lightweight", lightweightValues(""), map[string]string{
			"patchworks-elasticsearch": "512Mi",
			"patchworks-rabbitmq":      "256Mi",
			"patchworks-soketi":        "128Mi",
		}, "-Xms256m -Xmx256m"},
		{"standard", monoInboundValues("false", "false"), map[string]string{
			"patchworks-elasticsearch": "1Gi",
			"patchworks-rabbitmq":      "512Mi",
			"patchworks-soketi":        "",
		}, "-Xms512m -Xmx512m"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, parsed := writeMonoInboundValues(t, tc.values)
			infra := deploymentNames(renderInfraObjects(t, parsed))
			for name, want := range tc.want {
				deployment, ok := infra[name]
				if !ok {
					t.Fatalf("%s missing: %v", name, keys(infra))
				}
				if got := memoryRequest(deployment); got != want {
					t.Errorf("%s memory request = %q, want %q", name, got, want)
				}
			}
			if got := javaOpts(infra["patchworks-elasticsearch"]); got != tc.javaOpts {
				t.Errorf("ES_JAVA_OPTS = %q, want %q", got, tc.javaOpts)
			}
			if tc.name != "lightweight" {
				return
			}
			total := requestedMemory(t, infra)
			total.Add(requestedMemory(t, deploymentNames(renderComponentSelection(t, "patchworks", "patchworks", parsed))))
			if limit := resource.MustParse("2Gi"); total.Cmp(limit) > 0 {
				t.Fatalf("app and infra memory requests total %s, above %s", total.String(), limit.String())
			}
		})
	}
}

// lightweightKubeFaaSValues are installer answers for a lightweight
// installation with KubeFaaS and the bundled registry, checked against a chart
// whose lightweight profile shrinks KubeFaaS.
func lightweightKubeFaaSValues() map[string]string {
	values := lightweightValues("")
	for key, value := range kubefaasValues(true, "linux/arm64") {
		if strings.HasPrefix(strings.ToLower(key), "kubefaas") || key == "infraKubefaasEnabled" {
			values[key] = value
		}
	}
	values["kubefaasLightweightSupported"] = "true"
	return values
}

// namespaceMemory sums the memory requests of every container of every replica
// of the Deployments in a namespace.
func namespaceMemory(t *testing.T, objects []map[string]any, namespace string) resource.Quantity {
	t.Helper()
	var total resource.Quantity
	for _, object := range objects {
		metadata := object["metadata"].(map[string]any)
		if object["kind"] != "Deployment" || metadata["namespace"] != namespace {
			continue
		}
		spec := object["spec"].(map[string]any)
		replicas := int64(spec["replicas"].(int))
		for _, c := range spec["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any) {
			resources, _ := c.(map[string]any)["resources"].(map[string]any)
			requests, _ := resources["requests"].(map[string]any)
			if memory, ok := requests["memory"].(string); ok {
				quantity := resource.MustParse(memory)
				quantity.Mul(replicas)
				total.Add(quantity)
			}
		}
	}
	return total
}

// Under the lightweight profile KubeFaaS runs one controller and requests
// 240Mi instead of about 1.6 GiB, so it fits next to the rest of a lightweight
// release on a 4 GB node. Function pods that don't set their own request ask
// for 16M; Core's scripts ask for 35M. The install summary states the 240Mi.
func TestLightweightProfileRequestsLessKubeFaaSMemory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		values   map[string]string
		want     string
		function string
	}{
		{"lightweight", lightweightKubeFaaSValues(), "240Mi", "16M"},
		{"standard", kubefaasValues(true, "linux/arm64"), "1616Mi", "128M"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, parsed := writeMonoInboundValues(t, tc.values)
			objects := renderInfraObjects(t, parsed)
			if total := namespaceMemory(t, objects, "kubefaas"); total.Cmp(resource.MustParse(tc.want)) != 0 {
				t.Fatalf("KubeFaaS memory requests = %s, want %s", total.String(), tc.want)
			}
			var profile struct {
				Requests struct{ Memory string } `yaml:"requests"`
			}
			if err := yaml.Unmarshal([]byte(findObject(objects, "ConfigMap", "profile-default")["data"].(map[string]any)["resources"].(string)), &profile); err != nil {
				t.Fatal(err)
			}
			if profile.Requests.Memory != tc.function {
				t.Fatalf("function memory request = %q, want %q", profile.Requests.Memory, tc.function)
			}
			summary := kubefaasSummary(tc.values)
			if lightweight := tc.name == "lightweight"; lightweight != strings.Contains(summary, "requests about "+tc.want+" of memory") {
				t.Fatalf("summary = %q", summary)
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
