package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	helmchart "helm.sh/helm/v4/pkg/chart"
)

// KubeFaaS runs Script steps. With bundled infrastructure the installer can
// turn it on together with the infra chart's bundled image registry
// (kubefaas.registry.bundled), which the builder pushes function images to and
// nodes pull them from as localhost:<nodePort>/... without node configuration.

const (
	// defaultKubeFaaSNamespace is the infra chart's kubefaas.namespace default.
	defaultKubeFaaSNamespace = "kubefaas"
	// infraReleaseName is the release that owns the KubeFaaS namespace.
	infraReleaseName = "patchworks-infra"
	// kubefaasInfraTimeout is how long the infra install waits when KubeFaaS is
	// on: the builder reports ready only once it has built and pushed the base
	// image of every function template.
	kubefaasInfraTimeout = 60 * time.Minute
)

// nodeFacts are the architectures and container runtimes the cluster's nodes
// report. Known is false when the nodes could not be listed.
type nodeFacts struct {
	Known         bool
	Architectures []string
	Runtimes      []string
}

func readNodeFacts(ctx context.Context, client kubernetes.Interface) nodeFacts {
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil || len(nodes.Items) == 0 {
		return nodeFacts{}
	}
	architectures := map[string]bool{}
	runtimes := map[string]bool{}
	for _, node := range nodes.Items {
		architectures[node.Status.NodeInfo.Architecture] = true
		runtime, _, _ := strings.Cut(node.Status.NodeInfo.ContainerRuntimeVersion, "://")
		runtimes[runtime] = true
	}
	return nodeFacts{Known: true, Architectures: sortedKeys(architectures), Runtimes: sortedKeys(runtimes)}
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// kubefaasPlatform is the builder platform for the nodes: linux/arm64 when
// every node is arm64. Otherwise empty, keeping the builder's default
// (linux/amd64); mixed says the nodes mix architectures.
func kubefaasPlatform(facts nodeFacts) (platform string, mixed bool) {
	if !facts.Known || len(facts.Architectures) == 0 {
		return "", false
	}
	if len(facts.Architectures) > 1 {
		return "", true
	}
	if facts.Architectures[0] == "arm64" {
		return "linux/arm64", false
	}
	return "", false
}

// kubefaasRegistryRuntimeProblem says why the nodes may not pull from the
// bundled registry: it relies on containerd reaching localhost over plain HTTP.
func kubefaasRegistryRuntimeProblem(facts nodeFacts) string {
	if !facts.Known {
		return ""
	}
	for _, runtime := range facts.Runtimes {
		if runtime != "containerd" {
			return fmt.Sprintf("the nodes run %s; the bundled registry needs containerd", strings.Join(facts.Runtimes, ", "))
		}
	}
	return ""
}

// kubefaasSupport reads the selected infra chart: whether it has the bundled
// registry and the builder platform setting, and its KubeFaaS namespace.
func kubefaasSupport(chart helmchart.Charter) (registry, platform bool, namespace string) {
	namespace = defaultKubeFaaSNamespace
	accessor, err := helmchart.NewAccessor(chart)
	if err != nil {
		return false, false, namespace
	}
	kubefaas, _ := accessor.Values()["kubefaas"].(map[string]any)
	if ns, ok := kubefaas["namespace"].(string); ok && ns != "" {
		namespace = ns
	}
	registryValues, _ := kubefaas["registry"].(map[string]any)
	_, registry = registryValues["bundled"].(map[string]any)
	builder, _ := kubefaas["builder"].(map[string]any)
	_, platform = builder["platform"]
	return registry, platform, namespace
}

// kubefaasLightweightSupported is whether the selected infra chart's
// lightweight profile shrinks KubeFaaS (lightweight.kubefaas).
func kubefaasLightweightSupported(chart helmchart.Charter) bool {
	accessor, err := helmchart.NewAccessor(chart)
	if err != nil {
		return false
	}
	lightweight, _ := accessor.Values()["lightweight"].(map[string]any)
	_, ok := lightweight["kubefaas"].(map[string]any)
	return ok
}

// checkKubeFaaS records what the values file can turn on for KubeFaaS with the
// selected infra chart and the nodes the settings form saw.
func checkKubeFaaS(values map[string]string, chart helmchart.Charter) {
	if !kubefaasEnabled(values) {
		return
	}
	registry, platform, namespace := kubefaasSupport(chart)
	values["kubefaasNamespace"] = namespace
	values["kubefaasPlatformSupported"] = fmt.Sprintf("%t", platform)
	values["kubefaasLightweightSupported"] = fmt.Sprintf("%t", kubefaasLightweightSupported(chart))
	if values["kubefaasRegistry"] == "true" && values["infrastructure"] != "external" {
		switch {
		case !registry:
			values["kubefaasRegistryUnsupported"] = "this chart release has no bundled registry"
		case values["kubefaasRegistryRuntimeProblem"] != "":
			values["kubefaasRegistryUnsupported"] = values["kubefaasRegistryRuntimeProblem"]
		}
	}
	values["kubefaasChecked"] = "true"
}

func kubefaasEnabled(values map[string]string) bool {
	return values["infraKubefaasEnabled"] == "true"
}

// kubefaasRegistryEnabled is whether the values file turns on the bundled
// registry: the user's choice with bundled infrastructure, with the chart and
// nodes checked.
func kubefaasRegistryEnabled(values map[string]string) bool {
	return kubefaasEnabled(values) && values["infrastructure"] != "external" && values["kubefaasRegistry"] == "true" &&
		values["kubefaasChecked"] == "true" && values["kubefaasRegistryUnsupported"] == ""
}

// writeKubeFaaSValues writes the kubefaas block of the values file.
func writeKubeFaaSValues(b io.Writer, values map[string]string) {
	if !hasValue(values, "infraKubefaasEnabled") {
		return
	}
	fmt.Fprintf(b, "kubefaas:\n  enabled: %s\n", values["infraKubefaasEnabled"])
	if kubefaasEnabled(values) && values["kubefaasPlatform"] != "" && values["kubefaasPlatformSupported"] == "true" {
		fmt.Fprintf(b, "  builder:\n    platform: %s\n", quote(values["kubefaasPlatform"]))
	}
	if kubefaasRegistryEnabled(values) {
		fmt.Fprintf(b, "  registry:\n    bundled:\n      enabled: true\n")
	}
	fmt.Fprintf(b, "\n")
}

func kubefaasSummary(values map[string]string) string {
	if !kubefaasEnabled(values) {
		return "off"
	}
	parts := []string{"on"}
	switch {
	case kubefaasRegistryEnabled(values):
		parts = append(parts, "bundled image registry")
	case values["kubefaasRegistryUnsupported"] != "":
		parts = append(parts, "no bundled registry ("+values["kubefaasRegistryUnsupported"]+"): set kubefaas.registry.config to your own")
	default:
		parts = append(parts, "your own registry: set kubefaas.registry.config")
	}
	switch {
	case values["kubefaasPlatform"] != "" && values["kubefaasPlatformSupported"] == "true":
		parts = append(parts, "functions built for "+values["kubefaasPlatform"])
	case values["kubefaasPlatform"] != "":
		parts = append(parts, "functions built for linux/amd64: this release cannot build for "+values["kubefaasPlatform"])
	case values["kubefaasMixedArchitectures"] == "true":
		parts = append(parts, "functions built for linux/amd64, though the nodes mix architectures")
	}
	if part := kubefaasLightweightSummary(values); part != "" {
		parts = append(parts, part)
	}
	parts = append(parts, "the first install builds base images and can take a long time")
	return strings.Join(parts, "; ")
}

// kubefaasLightweightSummary is KubeFaaS's memory cost under the lightweight
// profile, empty under the standard one. The infra chart's lightweight.kubefaas
// requests 240Mi with the bundled registry (tests/lightweight_profile_test.rb);
// Core asks for 35M per script it deploys.
func kubefaasLightweightSummary(values map[string]string) string {
	switch {
	case !lightweightEnabled(values) || values["kubefaasChecked"] != "true":
		return ""
	case values["kubefaasLightweightSupported"] != "true":
		return "this release keeps KubeFaaS's standard memory requests under the lightweight profile, about 1.6 GiB, more than a 4 GB node has left; choose a newer release with --chart-version"
	default:
		return "lightweight: KubeFaaS requests about 240Mi of memory and each deployed script 35M, though image builds can use up to 2Gi"
	}
}

// ensureReleaseNamespace creates a namespace the release will own, labelled
// and annotated for Helm, so the release adopts it instead of refusing it. A
// namespace that already exists is left as it is.
func ensureReleaseNamespace(ctx context.Context, client kubernetes.Interface, name, releaseName, releaseNamespace string) error {
	if _, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{}); err == nil || !apierrors.IsNotFound(err) {
		return err
	}
	_, err := client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"app.kubernetes.io/managed-by": "Helm"},
			Annotations: map[string]string{
				"meta.helm.sh/release-name":      releaseName,
				"meta.helm.sh/release-namespace": releaseNamespace,
			},
		},
	}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// copyPullSecret copies an image pull Secret into another namespace, labelled
// as the installer's, replacing an earlier copy.
func copyPullSecret(ctx context.Context, client kubernetes.Interface, name, fromNamespace, toNamespace string) error {
	source, err := client.CoreV1().Secrets(fromNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read pull secret %s/%s: %w", fromNamespace, name, err)
	}
	copied := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: toNamespace, Labels: installerLabels()},
		Type:       source.Type,
		Data:       source.Data,
	}
	_, err = client.CoreV1().Secrets(toNamespace).Create(ctx, copied, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := client.CoreV1().Secrets(toNamespace).Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		existing.Type = source.Type
		existing.Data = source.Data
		if existing.Labels == nil {
			existing.Labels = map[string]string{}
		}
		for key, value := range installerLabels() {
			existing.Labels[key] = value
		}
		_, err = client.CoreV1().Secrets(toNamespace).Update(ctx, existing, metav1.UpdateOptions{})
	}
	return err
}

// prepareKubeFaaSNamespace gives the KubeFaaS namespace the image pull Secret
// the release namespace has, since the controller and builder images come from
// the same registry: it creates the namespace for the infra release to adopt,
// then copies the Secret.
func prepareKubeFaaSNamespace(ctx context.Context, access kubeAccess, values map[string]string) error {
	pullSecret := values["pullSecret"]
	if !kubefaasEnabled(values) || pullSecret == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client, err := kubernetesClient(access)
	if err != nil {
		return err
	}
	namespace := stringDefault(values["kubefaasNamespace"], defaultKubeFaaSNamespace)
	if namespace == values["namespace"] {
		return nil
	}
	if err := ensureReleaseNamespace(ctx, client, namespace, infraReleaseName, values["namespace"]); err != nil {
		return err
	}
	return copyPullSecret(ctx, client, pullSecret, values["namespace"], namespace)
}

// infraInstallTimeout is how long the infra install waits for readiness.
func infraInstallTimeout(values map[string]any) time.Duration {
	kubefaas, _ := values["kubefaas"].(map[string]any)
	if enabled, _ := kubefaas["enabled"].(bool); enabled {
		return kubefaasInfraTimeout
	}
	return 15 * time.Minute
}

// kubefaasRegistryDescription explains the bundled registry prompt, noting
// nodes it may not suit.
func kubefaasRegistryDescription(facts nodeFacts) string {
	description := "Run a container registry in the cluster for the function images KubeFaaS builds, behind a generated login. Nodes pull them as localhost:<port>/... through a NodePort, which needs containerd and kube-proxy in iptables mode, as on k3s. Choose No to set kubefaas.registry.config to your own registry later."
	if problem := kubefaasRegistryRuntimeProblem(facts); problem != "" {
		description += " Note: " + problem + "."
	}
	return description
}

// addKubeFaaSValues records the KubeFaaS answers. With bundled infrastructure
// the prompts decide; with your own, KubeFaaS stays as config.yaml has it and
// the bundled registry is off (kubefaasRegistryEnabled). The registry answer is
// kept as given, even with KubeFaaS off, so a saved config offers it again.
func addKubeFaaSValues(values map[string]string, infrastructureMode string, kubefaas, registry bool, facts nodeFacts) {
	if infrastructureMode == "bundled" {
		values["infraKubefaasEnabled"] = fmt.Sprintf("%t", kubefaas)
		values["kubefaasRegistry"] = fmt.Sprintf("%t", registry)
	}
	if !kubefaasEnabled(values) {
		return
	}
	platform, mixed := kubefaasPlatform(facts)
	values["kubefaasPlatform"] = platform
	values["kubefaasMixedArchitectures"] = fmt.Sprintf("%t", mixed)
	values["kubefaasRegistryRuntimeProblem"] = kubefaasRegistryRuntimeProblem(facts)
}

// manualKubeFaaS is what the printed Helm commands need for KubeFaaS: its
// namespace, and the pull Secret to copy there. The zero value is KubeFaaS off.
type manualKubeFaaS struct {
	Namespace  string
	PullSecret string
}

func manualKubeFaaSFor(values map[string]string, installInfrastructure bool) manualKubeFaaS {
	if !installInfrastructure || !kubefaasEnabled(values) {
		return manualKubeFaaS{}
	}
	return manualKubeFaaS{Namespace: stringDefault(values["kubefaasNamespace"], defaultKubeFaaSNamespace), PullSecret: values["pullSecret"]}
}

// kubefaasPullSecretCommands do by hand what prepareKubeFaaSNamespace does
// before a manual infra install: create the KubeFaaS namespace for
// patchworks-infra to adopt, leaving one that exists alone, and copy the release
// namespace's pull Secret to it.
func kubefaasPullSecretCommands(namespace string, kubefaas manualKubeFaaS) string {
	if kubefaas.Namespace == "" || kubefaas.PullSecret == "" || kubefaas.Namespace == namespace {
		return ""
	}
	return fmt.Sprintf(`kubectl get namespace %[2]s >/dev/null 2>&1 || kubectl create namespace %[2]s --dry-run=client -o yaml | kubectl label --local -f - app.kubernetes.io/managed-by=Helm -o yaml | kubectl annotate --local -f - meta.helm.sh/release-name=%[4]s meta.helm.sh/release-namespace=%[1]s -o yaml | kubectl create -f -
kubectl get secret %[3]s -n %[1]s -o go-template='{{index .data ".dockerconfigjson" | base64decode}}' | kubectl create secret generic %[3]s -n %[2]s --type=kubernetes.io/dockerconfigjson --from-file=.dockerconfigjson=/dev/stdin --dry-run=client -o yaml | kubectl apply -f -
`, shellQuote(namespace), shellQuote(kubefaas.Namespace), shellQuote(kubefaas.PullSecret), infraReleaseName)
}

// infraInstallDetail is the infra install's progress line.
func infraInstallDetail(values map[string]any) string {
	if infraInstallTimeout(values) == kubefaasInfraTimeout {
		return "Applying release patchworks-infra and waiting for readiness; the KubeFaaS builder first builds a base image per script language, which can take a long time"
	}
	return "Applying release patchworks-infra and waiting for readiness"
}
