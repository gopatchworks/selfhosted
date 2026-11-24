package main

import (
	"context"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	yaml "go.yaml.in/yaml/v3"
	"helm.sh/helm/v4/pkg/chart/common"
	chartutil "helm.sh/helm/v4/pkg/chart/common/util"
	helmloader "helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/engine"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// kubefaasValues are installer answers for bundled infrastructure with
// KubeFaaS, checked against the selected chart.
func kubefaasValues(registry bool, platform string) map[string]string {
	values := monoInboundValues("false", "false")
	values["infraKubefaasEnabled"] = "true"
	values["kubefaasRegistry"] = boolString(registry)
	values["kubefaasPlatform"] = platform
	values["kubefaasPlatformSupported"] = "true"
	values["kubefaasChecked"] = "true"
	return values
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// renderInfraObjects renders the real infra chart as the installer would.
func renderInfraObjects(t *testing.T, values map[string]any) []map[string]any {
	t.Helper()
	chart, err := helmloader.Load(filepath.Join("..", "..", "charts", "patchworks-infra"))
	if err != nil {
		t.Fatal(err)
	}
	renderValues, err := chartutil.ToRenderValues(chart, values, common.ReleaseOptions{Name: "patchworks-infra", Namespace: "patchworks", Revision: 1, IsInstall: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err := engine.Render(chart, renderValues)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var objects []map[string]any
	for _, path := range paths {
		if !strings.HasSuffix(path, ".yaml") {
			continue
		}
		decoder := yaml.NewDecoder(strings.NewReader(files[path]))
		for {
			var object map[string]any
			if err := decoder.Decode(&object); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("invalid rendered YAML in %s: %v", path, err)
			}
			if object["kind"] != nil {
				objects = append(objects, object)
			}
		}
	}
	return objects
}

func TestWriteValuesTurnsOnKubeFaaSWithTheBundledRegistry(t *testing.T) {
	rendered, parsed := writeMonoInboundValues(t, kubefaasValues(true, "linux/arm64"))
	want := "kubefaas:\n  enabled: true\n  builder:\n    platform: \"linux/arm64\"\n  registry:\n    bundled:\n      enabled: true\n\n"
	if !strings.Contains(rendered, want) {
		t.Fatalf("kubefaas block missing:\n%s", rendered)
	}

	// The real infra chart runs the registry, gives the builder the proxy and
	// platform, and gives every function namespace a pull Secret.
	objects := renderInfraObjects(t, parsed)
	if findObject(objects, "Deployment", "patchworks-kubefaas-image-registry") == nil {
		t.Fatalf("bundled registry missing")
	}
	builder := findObject(objects, "Deployment", "patchworks-kubefaas-builder")
	var containers []string
	for _, c := range builder["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any) {
		containers = append(containers, c.(map[string]any)["name"].(string))
	}
	if strings.Join(containers, ",") != "kubefaas-builder,registry-proxy,dind" {
		t.Fatalf("builder containers = %v", containers)
	}
	env := findObject(objects, "ConfigMap", "patchworks-kubefaas-builder-env")["data"].(map[string]any)
	if env["BASE_IMAGE_PLATFORM"] != "linux/arm64" {
		t.Fatalf("BASE_IMAGE_PLATFORM = %v", env["BASE_IMAGE_PLATFORM"])
	}
	pulls := 0
	for _, object := range objects {
		if object["kind"] == "Secret" && object["type"] == "kubernetes.io/dockerconfigjson" {
			pulls++
		}
	}
	if pulls != 6 { // the kubefaas namespace and its 5 function namespaces
		t.Fatalf("pull Secrets = %d, want 6", pulls)
	}
}

func TestWriteValuesKubeFaaSWithoutRegistryOrPlatform(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
		want   string
	}{
		{"off", withValue(kubefaasValues(true, "linux/arm64"), "infraKubefaasEnabled", "false"), "kubefaas:\n  enabled: false\n\n"},
		{"registry declined", kubefaasValues(false, ""), "kubefaas:\n  enabled: true\n\n"},
		{"registry unsupported", withValue(kubefaasValues(true, ""), "kubefaasRegistryUnsupported", "this chart release has no bundled registry"), "kubefaas:\n  enabled: true\n\n"},
		{"platform unsupported", withValue(kubefaasValues(false, "linux/arm64"), "kubefaasPlatformSupported", "false"), "kubefaas:\n  enabled: true\n\n"},
		{"not checked", withValue(kubefaasValues(true, ""), "kubefaasChecked", ""), "kubefaas:\n  enabled: true\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rendered, _ := writeMonoInboundValues(t, tc.values)
			if !strings.Contains(rendered, tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, rendered)
			}
		})
	}
}

func TestKubeFaaSPlatformAndRuntime(t *testing.T) {
	for _, tc := range []struct {
		name          string
		facts         nodeFacts
		platform      string
		mixed         bool
		runtimeIssued bool
	}{
		{"arm64 nodes", nodeFacts{Known: true, Architectures: []string{"arm64"}, Runtimes: []string{"containerd"}}, "linux/arm64", false, false},
		{"amd64 nodes", nodeFacts{Known: true, Architectures: []string{"amd64"}, Runtimes: []string{"containerd"}}, "", false, false},
		{"mixed nodes", nodeFacts{Known: true, Architectures: []string{"amd64", "arm64"}, Runtimes: []string{"containerd"}}, "", true, false},
		{"cri-o nodes", nodeFacts{Known: true, Architectures: []string{"amd64"}, Runtimes: []string{"cri-o"}}, "", false, true},
		{"nodes not listed", nodeFacts{}, "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			platform, mixed := kubefaasPlatform(tc.facts)
			if platform != tc.platform || mixed != tc.mixed {
				t.Fatalf("kubefaasPlatform = (%q, %t), want (%q, %t)", platform, mixed, tc.platform, tc.mixed)
			}
			if got := kubefaasRegistryRuntimeProblem(tc.facts) != ""; got != tc.runtimeIssued {
				t.Fatalf("runtime problem = %t, want %t", got, tc.runtimeIssued)
			}
		})
	}
}

func TestReadNodeFacts(t *testing.T) {
	node := func(name, arch, runtime string) *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}, Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{Architecture: arch, ContainerRuntimeVersion: runtime}}}
	}
	facts := readNodeFacts(context.Background(), fake.NewClientset(node("a", "arm64", "containerd://2.3.7-k3s1"), node("b", "arm64", "containerd://2.3.7-k3s1")))
	if !facts.Known || strings.Join(facts.Architectures, ",") != "arm64" || strings.Join(facts.Runtimes, ",") != "containerd" {
		t.Fatalf("facts = %+v", facts)
	}
	if facts := readNodeFacts(context.Background(), fake.NewClientset()); facts.Known {
		t.Fatalf("no nodes counted as known")
	}
}

func TestKubeFaaSSupport(t *testing.T) {
	load := func(t *testing.T, edit func(values map[string]any)) *chartv2.Chart {
		t.Helper()
		loaded, err := helmloader.Load(filepath.Join("..", "..", "charts", "patchworks-infra"))
		if err != nil {
			t.Fatal(err)
		}
		chart := loaded.(*chartv2.Chart)
		edit(chart.Values)
		return chart
	}
	kubefaas := func(values map[string]any) map[string]any { return values["kubefaas"].(map[string]any) }

	registry, platform, namespace := kubefaasSupport(load(t, func(map[string]any) {}))
	if !registry || !platform || namespace != "kubefaas" {
		t.Fatalf("current chart = (%t, %t, %q)", registry, platform, namespace)
	}
	registry, platform, namespace = kubefaasSupport(load(t, func(values map[string]any) {
		delete(kubefaas(values)["registry"].(map[string]any), "bundled")
		delete(kubefaas(values)["builder"].(map[string]any), "platform")
		kubefaas(values)["namespace"] = "functions"
	}))
	if registry || platform || namespace != "functions" {
		t.Fatalf("older chart = (%t, %t, %q)", registry, platform, namespace)
	}

	values := kubefaasValues(true, "linux/arm64")
	delete(values, "kubefaasChecked")
	values["kubefaasRegistryRuntimeProblem"] = "the nodes run cri-o; the bundled registry needs containerd"
	checkKubeFaaS(values, load(t, func(map[string]any) {}))
	if kubefaasRegistryEnabled(values) || values["kubefaasRegistryUnsupported"] == "" {
		t.Fatalf("registry kept on cri-o nodes: %v", values)
	}
}

func TestAddKubeFaaSValues(t *testing.T) {
	arm := nodeFacts{Known: true, Architectures: []string{"arm64"}, Runtimes: []string{"containerd"}}
	bundled := map[string]string{}
	addKubeFaaSValues(bundled, "bundled", true, true, arm)
	if bundled["infraKubefaasEnabled"] != "true" || bundled["kubefaasRegistry"] != "true" || bundled["kubefaasPlatform"] != "linux/arm64" {
		t.Fatalf("bundled = %v", bundled)
	}
	// KubeFaaS declined: the registry answer is kept for the saved config.
	declined := map[string]string{}
	addKubeFaaSValues(declined, "bundled", false, true, arm)
	if declined["infraKubefaasEnabled"] != "false" || declined["kubefaasRegistry"] != "true" || kubefaasRegistryEnabled(declined) {
		t.Fatalf("declined = %v", declined)
	}
	if components := infrastructureComponentsFromValues(declined); components.KubeFaaSRegistry.Enabled == nil || !*components.KubeFaaSRegistry.Enabled {
		t.Fatalf("saved registry answer = %+v", components.KubeFaaSRegistry)
	}
	// With your own infrastructure KubeFaaS stays as config.yaml has it, without the bundled registry.
	external := kubefaasValues(true, "")
	external["infrastructure"] = "external"
	delete(external, "kubefaasChecked")
	addKubeFaaSValues(external, "external", false, false, arm)
	checkKubeFaaS(external, nil)
	if external["infraKubefaasEnabled"] != "true" || external["kubefaasRegistry"] != "true" || kubefaasRegistryEnabled(external) || external["kubefaasRegistryUnsupported"] != "" {
		t.Fatalf("external = %v", external)
	}
}

func TestKubeFaaSSummary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
		want   string
	}{
		{"off", withValue(kubefaasValues(true, ""), "infraKubefaasEnabled", "false"), "off"},
		{"registry and arm64", kubefaasValues(true, "linux/arm64"), "on; bundled image registry; functions built for linux/arm64; the first install builds base images and can take a long time"},
		{"own registry", kubefaasValues(false, ""), "on; your own registry: set kubefaas.registry.config; the first install builds base images and can take a long time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := kubefaasSummary(tc.values); got != tc.want {
				t.Fatalf("summary = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLocalConfigKeepsTheKubeFaaSRegistry(t *testing.T) {
	data, err := yaml.Marshal(localConfig{InfraComponents: localConfigInfrastructure{KubeFaaS: localConfigComponent{Enabled: boolPtr(true)}, KubeFaaSRegistry: localConfigComponent{Enabled: boolPtr(false)}}})
	if err != nil {
		t.Fatal(err)
	}
	var loaded localConfig
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.InfraComponents.KubeFaaS.Enabled == nil || !*loaded.InfraComponents.KubeFaaS.Enabled ||
		loaded.InfraComponents.KubeFaaSRegistry.Enabled == nil || *loaded.InfraComponents.KubeFaaSRegistry.Enabled {
		t.Fatalf("saved config:\n%s", data)
	}
}

func TestPrepareKubeFaaSNamespace(t *testing.T) {
	ctx := context.Background()
	client := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "quay-credentials", Namespace: "patchworks"},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)},
	})
	if err := ensureReleaseNamespace(ctx, client, "kubefaas", "patchworks-infra", "patchworks"); err != nil {
		t.Fatal(err)
	}
	ns, err := client.CoreV1().Namespaces().Get(ctx, "kubefaas", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ns.Labels["app.kubernetes.io/managed-by"] != "Helm" || ns.Annotations["meta.helm.sh/release-name"] != "patchworks-infra" || ns.Annotations["meta.helm.sh/release-namespace"] != "patchworks" {
		t.Fatalf("namespace not ready for the release to adopt: %v %v", ns.Labels, ns.Annotations)
	}
	// Again: an existing namespace is left as it is.
	if err := ensureReleaseNamespace(ctx, client, "kubefaas", "patchworks-infra", "patchworks"); err != nil {
		t.Fatal(err)
	}
	for range 2 { // a second copy replaces the first
		if err := copyPullSecret(ctx, client, "quay-credentials", "patchworks", "kubefaas"); err != nil {
			t.Fatal(err)
		}
	}
	copied, err := client.CoreV1().Secrets("kubefaas").Get(ctx, "quay-credentials", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if copied.Type != corev1.SecretTypeDockerConfigJson || string(copied.Data[corev1.DockerConfigJsonKey]) != `{"auths":{}}` || copied.Labels[installerLabelKey] != installerLabelValue {
		t.Fatalf("copied secret = %+v", copied)
	}
}

func TestInfraInstallTimeout(t *testing.T) {
	if got := infraInstallTimeout(map[string]any{"kubefaas": map[string]any{"enabled": true}}); got != kubefaasInfraTimeout {
		t.Fatalf("with KubeFaaS = %v", got)
	}
	if got := infraInstallTimeout(map[string]any{}); got != 15*time.Minute {
		t.Fatalf("without KubeFaaS = %v", got)
	}
}

func TestManualCommandsPrepareKubeFaaS(t *testing.T) {
	values := kubefaasValues(true, "")
	values["namespace"] = "patchworks"
	values["pullSecret"] = "quay-credentials"
	commands := helmCommandText("patchworks", "patchworks.values.yaml", true, manualKubeFaaSFor(values, true))
	lines := strings.Split(commands, "\n")
	if len(lines) != 4 {
		t.Fatalf("commands:\n%s", commands)
	}
	for i, want := range []string{
		"kubectl get namespace 'kubefaas' >/dev/null 2>&1 || kubectl create namespace 'kubefaas' --dry-run=client -o yaml | kubectl label --local -f - app.kubernetes.io/managed-by=Helm -o yaml | kubectl annotate --local -f - meta.helm.sh/release-name=patchworks-infra meta.helm.sh/release-namespace='patchworks' -o yaml | kubectl create -f -",
		`kubectl get secret 'quay-credentials' -n 'patchworks' -o go-template='{{index .data ".dockerconfigjson" | base64decode}}' | kubectl create secret generic 'quay-credentials' -n 'kubefaas' --type=kubernetes.io/dockerconfigjson --from-file=.dockerconfigjson=/dev/stdin --dry-run=client -o yaml | kubectl apply -f -`,
	} {
		if lines[i] != want {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want)
		}
	}
	if !strings.Contains(lines[2], "patchworks-infra") || !strings.Contains(lines[2], "--timeout 60m") || !strings.Contains(lines[3], "--timeout 15m") {
		t.Fatalf("helm commands:\n%s", commands)
	}

	// No pull Secret: only the longer wait.
	values["pullSecret"] = ""
	commands = helmCommandText("patchworks", "patchworks.values.yaml", true, manualKubeFaaSFor(values, true))
	if strings.Contains(commands, "kubectl") || !strings.Contains(commands, "--timeout 60m") {
		t.Fatalf("without a pull secret:\n%s", commands)
	}
	// KubeFaaS off, or no infra release: as before.
	values["pullSecret"] = "quay-credentials"
	for _, kubefaas := range []manualKubeFaaS{manualKubeFaaSFor(withValue(values, "infraKubefaasEnabled", "false"), true), manualKubeFaaSFor(values, false)} {
		if kubefaas != (manualKubeFaaS{}) {
			t.Fatalf("kubefaas = %+v", kubefaas)
		}
	}
	if commands := helmCommandText("patchworks", "patchworks.values.yaml", true, manualKubeFaaS{}); strings.Contains(commands, "kubectl") || strings.Contains(commands, "60m") {
		t.Fatalf("without KubeFaaS:\n%s", commands)
	}
}
