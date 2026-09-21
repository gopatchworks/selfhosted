package main

import (
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
	"helm.sh/helm/v4/pkg/chart/common"
	chartutil "helm.sh/helm/v4/pkg/chart/common/util"
	helmloader "helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/engine"
)

// Render the real chart using the same Helm engine as the installer, without a
// Kubernetes client. Each profile starts with external dependencies and no
// lifecycle jobs, so unintended workloads cannot hide among prerequisites.
func renderComponentSelection(t *testing.T, release, namespace string, values map[string]any) []map[string]any {
	t.Helper()
	chart, err := helmloader.Load(filepath.Join("..", "..", "charts", "patchworks-app"))
	if err != nil {
		t.Fatal(err)
	}
	renderValues, err := chartutil.ToRenderValues(chart, values, common.ReleaseOptions{Name: release, Namespace: namespace, Revision: 1, IsInstall: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err := engine.Render(chart, renderValues)
	if err != nil {
		t.Fatal(err)
	}
	var objects []map[string]any
	var paths []string
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if strings.HasSuffix(path, "NOTES.txt") {
			continue
		}
		decoder := yaml.NewDecoder(strings.NewReader(files[path]))
		for {
			var object map[string]any
			err := decoder.Decode(&object)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("invalid rendered YAML in %s: %v", path, err)
			}
			if object["kind"] != nil {
				objects = append(objects, object)
			}
		}
	}
	return objects
}

func componentSelectionMap(object map[string]any, path ...string) map[string]any {
	for _, key := range path {
		object, _ = object[key].(map[string]any)
	}
	return object
}

func componentSelectionSet(values map[string]any, value any, path ...string) {
	for _, key := range path[:len(path)-1] {
		child, ok := values[key].(map[string]any)
		if !ok {
			child = map[string]any{}
			values[key] = child
		}
		values = child
	}
	values[path[len(path)-1]] = value
}

func componentSelectionValues(t *testing.T) map[string]any {
	t.Helper()
	var values map[string]any
	if err := yaml.Unmarshal([]byte(`
credentials: {autoGenerate: false}
web:
  gateway: {enabled: false}
  start: {enabled: false}
fabric:
  enabled: false
  migrations: {enabled: false}
  mysql:
    enabled: false
    external:
      host: fabric-db.example.test
      readHost: fabric-db-read.example.test
      existingSecret: {name: external-fabric-db}
workers:
  enabled: false
  mono:
    store:
      existingSecret: {name: external-store}
processorDeployments: {enabled: false}
scheduler: {enabled: false}
dashboard: {enabled: false}
migrations: {enabled: false}
seeds:
  fabric: {enabled: false}
  core: {enabled: false}
s3Manager: {enabled: false}
app:
  existingSecret: {name: external-app-key}
  license:
    existingSecret: {name: external-license}
passport:
  existingSecret: {name: external-passport}
mysql:
  enabled: false
  external:
    host: core-db.example.test
    existingSecret: {name: external-core-db}
redis:
  enabled: false
  external:
    host: redis.example.test
    existingSecret: {name: external-redis}
elasticsearch:
  enabled: false
  external:
    host: elasticsearch.example.test
    existingSecret: {name: external-elasticsearch}
rabbitmq:
  enabled: false
  topology: {enabled: false}
  external:
    host: rabbitmq.example.test
    existingSecret: {name: external-rabbitmq}
pusher:
  enabled: false
  external: {host: soketi.example.test}
  existingSecret: {name: external-pusher}
kubefaas:
  enabled: false
  host: http://kubefaas.example.test
  auth:
    existingSecret: {name: external-kubefaas}
s3:
  enabled: false
  external:
    endpoint: https://s3.example.test
    existingSecret: {name: external-s3}
`), &values); err != nil {
		t.Fatal(err)
	}
	return values
}

func componentSelectionIdentity(object map[string]any, defaultNamespace string) string {
	metadata := componentSelectionMap(object, "metadata")
	namespace, _ := metadata["namespace"].(string)
	if namespace == "" && object["kind"] != "ClusterRole" && object["kind"] != "ClusterRoleBinding" {
		namespace = defaultNamespace
	}
	return fmt.Sprintf("%s/%s/%v", object["kind"], namespace, metadata["name"])
}

func componentSelectionWorkloads(objects []map[string]any) []string {
	var got []string
	for _, object := range objects {
		switch object["kind"] {
		case "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob":
			got = append(got, fmt.Sprintf("%s/%v", object["kind"], componentSelectionMap(object, "metadata", "labels")["app.kubernetes.io/component"]))
		}
	}
	sort.Strings(got)
	return got
}

func componentSelectionContainerEnv(object map[string]any) map[string]map[string]any {
	podSpec := componentSelectionMap(object, "spec", "template", "spec")
	result := map[string]map[string]any{}
	for _, rawContainer := range podSpec["containers"].([]any) {
		container := rawContainer.(map[string]any)
		for _, rawEnv := range container["env"].([]any) {
			env := rawEnv.(map[string]any)
			result[env["name"].(string)] = env
		}
	}
	return result
}

func TestAppPreviousKeys(t *testing.T) {
	t.Run("omitted by default for existing APP_KEY secrets", func(t *testing.T) {
		values := componentSelectionValues(t)
		componentSelectionSet(values, true, "web", "gateway", "enabled")
		for _, object := range renderComponentSelection(t, "previous-keys", "apps", values) {
			if object["kind"] == "Deployment" && componentSelectionMap(object, "metadata", "labels")["app.kubernetes.io/component"] == "gateway" {
				if _, found := componentSelectionContainerEnv(object)["APP_PREVIOUS_KEYS"]; found {
					t.Fatal("APP_PREVIOUS_KEYS should be absent until explicitly configured")
				}
			}
		}
	})

	t.Run("existing secret key", func(t *testing.T) {
		values := componentSelectionValues(t)
		componentSelectionSet(values, true, "web", "gateway", "enabled")
		componentSelectionSet(values, "APP_PREVIOUS_KEYS", "app", "existingSecret", "previousKeysKey")
		for _, object := range renderComponentSelection(t, "previous-keys", "apps", values) {
			if object["kind"] != "Deployment" || componentSelectionMap(object, "metadata", "labels")["app.kubernetes.io/component"] != "gateway" {
				continue
			}
			ref := componentSelectionMap(componentSelectionContainerEnv(object)["APP_PREVIOUS_KEYS"], "valueFrom", "secretKeyRef")
			if ref["name"] != "external-app-key" || ref["key"] != "APP_PREVIOUS_KEYS" {
				t.Fatalf("unexpected previous key Secret reference: %v", ref)
			}
		}
	})

	t.Run("inline list for lifecycle jobs", func(t *testing.T) {
		values := componentSelectionValues(t)
		componentSelectionSet(values, true, "migrations", "enabled")
		componentSelectionSet(values, []any{"base64:old-one", "base64:old-two"}, "app", "previousKeys")
		for _, object := range renderComponentSelection(t, "previous-keys", "apps", values) {
			if object["kind"] != "Job" || componentSelectionMap(object, "metadata", "labels")["app.kubernetes.io/component"] != "core-migrations" {
				continue
			}
			env := componentSelectionContainerEnv(object)["APP_PREVIOUS_KEYS"]
			if env["value"] != "base64:old-one,base64:old-two" {
				t.Fatalf("unexpected inline APP_PREVIOUS_KEYS: %v", env)
			}
		}
	})
}

func TestComponentSelectionDefaultCompatibility(t *testing.T) {
	// Baseline identities plus the CPT-6321 scheduler processor and CronJob.
	// This protects ordinary monolithic installs while allowing new annotations
	// and selective namespace ownership to evolve without a full YAML snapshot.
	wantByKind := map[string][]string{
		"ConfigMap":      {"config", "fabric-config", "fabric-nginx", "processor-scheduler-supervisord", "processor-start-supervisord", "processor-gateway-supervisord", "processor-short-processor-supervisord", "processor-medium-processor-supervisord", "processor-long-processor-supervisord", "processor-logging-supervisord", "s3-manager-config", "workers-supervisord", "rabbitmq-topology"},
		"Service":        {"fabric", "gateway", "s3-manager", "start"},
		"Deployment":     {"fabric", "gateway", "processor-scheduler", "processor-start", "processor-gateway", "processor-short-processor", "processor-medium-processor", "processor-long-processor", "processor-logging", "s3-manager", "start", "workers"},
		"CronJob":        {"scheduler-scheduler", "start-scheduler", "gateway-scheduler", "short-processor-scheduler", "medium-processor-scheduler", "long-processor-scheduler"},
		"ServiceAccount": {"", "app-keygen", "passport-keygen", "pusher-auth-generator"},
		"Role":           {"app-keygen", "passport-keygen", "pusher-auth-generator"},
		"RoleBinding":    {"app-keygen", "passport-keygen", "pusher-auth-generator"},
		"Job":            {"app-keygen", "passport-keygen", "fabric-migrations", "core-migrations", "pusher-auth-generator", "rabbitmq-topology"},
	}
	var want, got []string
	for kind, suffixes := range wantByKind {
		for _, suffix := range suffixes {
			name := "patchworks"
			if suffix != "" {
				name += "-" + suffix
			}
			want = append(want, kind+"/patchworks/"+name)
		}
	}
	for _, object := range renderComponentSelection(t, "patchworks", "patchworks", nil) {
		got = append(got, componentSelectionIdentity(object, "patchworks"))
	}
	sort.Strings(want)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default resource identities changed\ngot:  %v\nwant: %v", got, want)
	}
}

func TestComponentSelectionAllDisabled(t *testing.T) {
	values := componentSelectionValues(t)
	// Empty credentials must not create keygen jobs when nothing consumes them.
	componentSelectionSet(values, "", "app", "existingSecret", "name")
	componentSelectionSet(values, "", "passport", "existingSecret", "name")
	componentSelectionSet(values, "", "pusher", "existingSecret", "name")
	componentSelectionSet(values, "", "pusher", "external", "host")
	componentSelectionSet(values, "", "workers", "mono", "store", "existingSecret", "name")
	for _, workerType := range []string{"standalone", "mono", "microservice"} {
		t.Run(workerType, func(t *testing.T) {
			componentSelectionSet(values, workerType, "workers", "type")
			objects := renderComponentSelection(t, "empty", "empty", values)
			if len(objects) != 0 {
				t.Fatalf("disabled release still owns %d resources: %v", len(objects), componentSelectionWorkloads(objects))
			}
		})
	}
}

type componentSelectionProfile struct {
	name       string
	path       []string
	workerType string
	want       []string
}

func componentSelectionProfiles() []componentSelectionProfile {
	return []componentSelectionProfile{
		{"gateway", []string{"web", "gateway", "enabled"}, "", []string{"Deployment/gateway"}},
		{"start", []string{"web", "start", "enabled"}, "", []string{"Deployment/start"}},
		{"fabric", []string{"fabric", "enabled"}, "", []string{"Deployment/fabric"}},
		{"dashboard", []string{"dashboard", "enabled"}, "", []string{"Deployment/dashboard"}},
		{"scheduler", []string{"scheduler", "enabled"}, "", []string{"CronJob/scheduler-scheduler", "CronJob/start-scheduler", "CronJob/gateway-scheduler", "CronJob/short-processor-scheduler", "CronJob/medium-processor-scheduler", "CronJob/long-processor-scheduler"}},
		{"processors", []string{"processorDeployments", "enabled"}, "", []string{"Deployment/processor-scheduler", "Deployment/processor-start", "Deployment/processor-gateway", "Deployment/processor-short-processor", "Deployment/processor-medium-processor", "Deployment/processor-long-processor", "Deployment/processor-logging"}},
		{"workers", []string{"workers", "enabled"}, "standalone", []string{"Deployment/workers"}},
		{"monocore", []string{"workers", "enabled"}, "mono", []string{"Deployment/workers"}},
		{"microservices", []string{"workers", "enabled"}, "microservice", []string{"Deployment/workers-assert"}},
		{"core-migrations", []string{"migrations", "enabled"}, "", []string{"Job/core-migrations"}},
		{"fabric-migrations", []string{"fabric", "migrations", "enabled"}, "", []string{"Job/fabric-migrations"}},
	}
}

func componentSelectionEnable(values map[string]any, profile componentSelectionProfile) {
	componentSelectionSet(values, true, profile.path...)
	componentSelectionSet(values, profile.name, "fullnameOverride")
	if profile.workerType != "" {
		componentSelectionSet(values, profile.workerType, "workers", "type")
	}
	if profile.workerType == "microservice" {
		componentSelectionSet(values, false, "workers", "microservices", "_default", "enabled")
		componentSelectionSet(values, true, "workers", "microservices", "assert", "enabled")
	}
}

func TestComponentSelectionIsolatedProfiles(t *testing.T) {
	identities := map[string]string{}
	for _, profile := range componentSelectionProfiles() {
		t.Run(profile.name, func(t *testing.T) {
			values := componentSelectionValues(t)
			componentSelectionEnable(values, profile)
			objects := renderComponentSelection(t, profile.name, profile.name, values)
			got := componentSelectionWorkloads(objects)
			want := append([]string(nil), profile.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("unintended or missing workloads: got %v, want %v", got, want)
			}
			for _, object := range objects {
				id := componentSelectionIdentity(object, profile.name)
				if previous, found := identities[id]; found {
					t.Errorf("resource %s is owned by both %s and %s", id, previous, profile.name)
				}
				identities[id] = profile.name
			}
			assertComponentSelectionReferences(t, objects, profile.name)
		})
	}
}

// Every pod reference must resolve in that pod's namespace, either to an object
// produced by this release or an explicitly configured existing dependency.
func assertComponentSelectionReferences(t *testing.T, objects []map[string]any, defaultNamespace string) {
	t.Helper()
	owned := map[string]bool{}
	for _, object := range objects {
		owned[componentSelectionIdentity(object, defaultNamespace)] = true
	}
	existingSecrets := map[string]bool{}
	for _, name := range []string{"app-key", "passport", "license", "core-db", "fabric-db", "redis", "elasticsearch", "rabbitmq", "pusher", "kubefaas", "s3", "store"} {
		existingSecrets["external-"+name] = true
	}
	for _, object := range objects {
		var podSpec map[string]any
		switch object["kind"] {
		case "Deployment", "StatefulSet", "DaemonSet", "Job":
			podSpec = componentSelectionMap(object, "spec", "template", "spec")
		case "CronJob":
			podSpec = componentSelectionMap(object, "spec", "jobTemplate", "spec", "template", "spec")
		default:
			continue
		}
		namespace, _ := componentSelectionMap(object, "metadata")["namespace"].(string)
		if namespace == "" {
			namespace = defaultNamespace
		}
		check := func(kind, name string) {
			if name == "" || (kind == "ServiceAccount" && name == "default") || (kind == "Secret" && existingSecrets[name]) {
				return
			}
			if !owned[kind+"/"+namespace+"/"+name] {
				t.Errorf("%s references unowned %s/%s/%s", componentSelectionIdentity(object, defaultNamespace), kind, namespace, name)
			}
		}
		var walk func(any)
		walk = func(value any) {
			switch node := value.(type) {
			case map[string]any:
				for key, value := range node {
					ref, _ := value.(map[string]any)
					switch key {
					case "secretKeyRef", "secretRef":
						name, _ := ref["name"].(string)
						check("Secret", name)
					case "configMapKeyRef", "configMapRef", "configMap":
						name, _ := ref["name"].(string)
						check("ConfigMap", name)
					case "secret":
						name, _ := ref["secretName"].(string)
						check("Secret", name)
					case "serviceAccountName":
						name, _ := value.(string)
						check("ServiceAccount", name)
					}
					walk(value)
				}
			case []any:
				for _, child := range node {
					walk(child)
				}
			}
		}
		walk(podSpec)
	}
}

func TestComponentSelectionNamespaceOverrides(t *testing.T) {
	for _, profile := range componentSelectionProfiles() {
		t.Run(profile.name, func(t *testing.T) {
			values := componentSelectionValues(t)
			componentSelectionEnable(values, profile)
			componentSelectionSet(values, "global-apps", "namespace")
			switch profile.name {
			case "gateway", "start":
				componentSelectionSet(values, "service-apps", "web", profile.name, "namespace")
			case "fabric", "fabric-migrations":
				componentSelectionSet(values, "service-apps", "fabric", "namespace")
			case "dashboard":
				componentSelectionSet(values, "service-apps", "dashboard", "namespace")
			case "core-migrations":
				componentSelectionSet(values, "service-apps", "migrations", "namespace")
			default:
				componentSelectionSet(values, "service-apps", "workers", "namespace")
			}
			objects := renderComponentSelection(t, profile.name, "helm-release", values)
			for _, object := range objects {
				if ns := componentSelectionMap(object, "metadata")["namespace"]; ns != "service-apps" {
					t.Errorf("%s should follow its only consumer into service-apps, got namespace %v", object["kind"], ns)
				}
			}
			assertComponentSelectionReferences(t, objects, "helm-release")
		})
	}
}

func TestComponentSelectionReloaderAnnotations(t *testing.T) {
	for _, profile := range componentSelectionProfiles() {
		if !strings.HasPrefix(profile.want[0], "Deployment/") {
			continue
		}
		t.Run(profile.name, func(t *testing.T) {
			values := componentSelectionValues(t)
			componentSelectionEnable(values, profile)
			componentSelectionSet(values, map[string]any{"reloader.stakater.com/auto": "true", "deployment.reloader.stakater.com/pause-period": "60s"}, "deploymentAnnotations")
			for _, object := range renderComponentSelection(t, profile.name, profile.name, values) {
				if object["kind"] != "Deployment" {
					continue
				}
				annotations := componentSelectionMap(object, "metadata", "annotations")
				if annotations["reloader.stakater.com/auto"] != "true" || annotations["deployment.reloader.stakater.com/pause-period"] != "60s" {
					t.Errorf("reloader metadata missing from %v", componentSelectionMap(object, "metadata")["name"])
				}
				podAnnotations := componentSelectionMap(object, "spec", "template", "metadata", "annotations")
				count := 0
				for key := range podAnnotations {
					if strings.HasPrefix(key, "checksum/") {
						count++
					}
					if key == "reloader.stakater.com/auto" || key == "deployment.reloader.stakater.com/pause-period" {
						t.Errorf("deployment annotation leaked onto pod template: %s", key)
					}
				}
				if count == 0 {
					t.Error("chart checksum annotations are missing")
				}
			}
		})
	}
}

func TestComponentSelectionGeneratedCredentialNamespaces(t *testing.T) {
	for _, credential := range []struct {
		setting string
		job     string
	}{
		{"app", "app-keygen"},
		{"passport", "passport-keygen"},
		{"pusher", "pusher-auth-generator"},
	} {
		t.Run(credential.setting, func(t *testing.T) {
			values := componentSelectionValues(t)
			componentSelectionSet(values, true, "web", "gateway", "enabled")
			componentSelectionSet(values, "gateway", "web", "gateway", "namespace")
			componentSelectionSet(values, "", credential.setting, "existingSecret", "name")
			if credential.setting == "pusher" {
				componentSelectionSet(values, true, "pusher", "enabled")
				componentSelectionSet(values, true, "credentials", "autoGenerate")
			}
			objects := renderComponentSelection(t, "credentials", "helm-release", values)
			want := []string{"Deployment/gateway", "Job/" + credential.job}
			if got := componentSelectionWorkloads(objects); !reflect.DeepEqual(got, want) {
				t.Fatalf("unexpected generated workloads: got %v, want %v", got, want)
			}
			for _, object := range objects {
				if namespace := componentSelectionMap(object, "metadata")["namespace"]; namespace != "gateway" {
					t.Errorf("generated credential resource %v has namespace %v; sole consumer is in gateway", object["kind"], namespace)
				}
			}

			// A second consumer must not silently receive a separately generated
			// APP_KEY or Passport key pair with different cryptographic identity.
			componentSelectionSet(values, true, "fabric", "enabled")
			componentSelectionSet(values, "fabric", "fabric", "namespace")
			chart, err := helmloader.Load(filepath.Join("..", "..", "charts", "patchworks-app"))
			if err != nil {
				t.Fatal(err)
			}
			renderValues, err := chartutil.ToRenderValues(chart, values, common.ReleaseOptions{Name: "credentials", Namespace: "helm-release", Revision: 1, IsInstall: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = engine.Render(chart, renderValues)
			if err == nil || !strings.Contains(err.Error(), credential.setting+".existingSecret") || !strings.Contains(err.Error(), "replicate") {
				t.Fatalf("multi-namespace credential generation should require replicated existingSecret, got %v", err)
			}
		})
	}
}

func TestComponentSelectionReplicatedCredentialsAcrossNamespaces(t *testing.T) {
	values := componentSelectionValues(t)
	componentSelectionSet(values, true, "web", "gateway", "enabled")
	componentSelectionSet(values, "gateway", "web", "gateway", "namespace")
	componentSelectionSet(values, true, "fabric", "enabled")
	componentSelectionSet(values, "fabric", "fabric", "namespace")
	objects := renderComponentSelection(t, "shared", "helm-release", values)
	want := []string{"Deployment/fabric", "Deployment/gateway"}
	if got := componentSelectionWorkloads(objects); !reflect.DeepEqual(got, want) {
		t.Fatalf("existing replicated credentials should avoid generator jobs: got %v, want %v", got, want)
	}
	assertComponentSelectionReferences(t, objects, "helm-release")
}

func TestComponentSelectionCompanyWorkerNamespaces(t *testing.T) {
	for _, profile := range componentSelectionProfiles()[6:9] {
		t.Run(profile.workerType, func(t *testing.T) {
			values := componentSelectionValues(t)
			componentSelectionEnable(values, profile)
			componentSelectionSet(values, "hub", "workers", "namespace")
			componentSelectionSet(values, []any{map[string]any{"name": "customer", "namespace": "tenant-workers", "queue": "customer-flows"}}, "workers", "companies")
			objects := renderComponentSelection(t, profile.name, "helm-release", values)
			workloadNamespaces := map[string]int{}
			for _, object := range objects {
				if object["kind"] == "Deployment" {
					namespace, _ := componentSelectionMap(object, "metadata")["namespace"].(string)
					workloadNamespaces[namespace]++
				}
			}
			if !reflect.DeepEqual(workloadNamespaces, map[string]int{"hub": 1, "tenant-workers": 1}) {
				t.Fatalf("company worker placement changed: %v", workloadNamespaces)
			}
			assertComponentSelectionReferences(t, objects, "helm-release")
		})
	}
}

func TestComponentSelectionIngressReferences(t *testing.T) {
	profiles := append([]componentSelectionProfile{{name: "disabled"}}, componentSelectionProfiles()[:4]...)
	for _, provider := range []string{"contour", "nginx"} {
		for _, mode := range []string{"host", "path"} {
			for _, profile := range profiles {
				t.Run(provider+"/"+mode+"/"+profile.name, func(t *testing.T) {
					values := componentSelectionValues(t)
					if profile.path != nil {
						componentSelectionEnable(values, profile)
					}
					componentSelectionSet(values, mode, "dashboard", "routingMode")
					componentSelectionSet(values, map[string]any{
						"enabled": true, "provider": provider,
						"hosts": map[string]any{"gateway": "gateway.example.test", "start": "start.example.test", "fabric": "fabric.example.test", "dashboard": "dashboard.example.test", "webhook": "webhook.example.test", "callback": "callback.example.test"},
					}, "ingress")
					objects := renderComponentSelection(t, profile.name, profile.name, values)
					if profile.path == nil && len(objects) != 0 {
						t.Fatalf("disabled components still created ingress resources: %v", objects)
					}
					services := map[string]bool{}
					for _, object := range objects {
						if object["kind"] == "Service" {
							services[componentSelectionIdentity(object, profile.name)] = true
						}
					}
					ingresses := 0
					for _, object := range objects {
						if object["kind"] != "Ingress" && object["kind"] != "HTTPProxy" {
							continue
						}
						ingresses++
						namespace, _ := componentSelectionMap(object, "metadata")["namespace"].(string)
						check := func(name string) {
							if !services["Service/"+namespace+"/"+name] {
								t.Errorf("%s references a disabled or unowned Service/%s/%s", componentSelectionIdentity(object, profile.name), namespace, name)
							}
						}
						var walk func(any)
						walk = func(value any) {
							switch node := value.(type) {
							case map[string]any:
								for key, child := range node {
									if key == "service" {
										if ref, ok := child.(map[string]any); ok {
											name, _ := ref["name"].(string)
											check(name)
										}
									}
									if key == "services" {
										if refs, ok := child.([]any); ok {
											for _, item := range refs {
												ref, _ := item.(map[string]any)
												name, _ := ref["name"].(string)
												check(name)
											}
										}
									}
									walk(child)
								}
							case []any:
								for _, child := range node {
									walk(child)
								}
							}
						}
						walk(object["spec"])
					}
					if profile.path != nil && ingresses == 0 {
						t.Fatal("enabled service lost its ingress")
					}
				})
			}
		}
	}
}

func TestComponentSelectionCompanyOnlyWorkers(t *testing.T) {
	for _, profile := range componentSelectionProfiles()[6:9] {
		for _, companyNamespace := range []string{"", "company-workers"} {
			t.Run(profile.workerType+"/"+companyNamespace, func(t *testing.T) {
				values := componentSelectionValues(t)
				componentSelectionEnable(values, profile)
				componentSelectionSet(values, false, "workers", "hub", "enabled")
				componentSelectionSet(values, "worker-default", "workers", "namespace")
				componentSelectionSet(values, []any{map[string]any{"name": "customer", "namespace": companyNamespace, "queue": "customer-flows"}}, "workers", "companies")
				objects := renderComponentSelection(t, profile.name, "helm-release", values)
				want := []string{profile.want[0] + "-customer"}
				if got := componentSelectionWorkloads(objects); !reflect.DeepEqual(got, want) {
					t.Fatalf("company-only release should not create hub workers: got %v, want %v", got, want)
				}
				wantNamespace := companyNamespace
				if wantNamespace == "" {
					wantNamespace = "worker-default"
				}
				for _, object := range objects {
					if namespace := componentSelectionMap(object, "metadata")["namespace"]; namespace != wantNamespace {
						t.Errorf("company-only resource %v/%v in %v, want %s", object["kind"], componentSelectionMap(object, "metadata")["name"], namespace, wantNamespace)
					}
				}
				assertComponentSelectionReferences(t, objects, "helm-release")
			})
		}
	}
}

func TestComponentSelectionDisabledHubWithNoCompanies(t *testing.T) {
	for _, profile := range componentSelectionProfiles()[6:9] {
		t.Run(profile.workerType, func(t *testing.T) {
			values := componentSelectionValues(t)
			componentSelectionEnable(values, profile)
			componentSelectionSet(values, false, "workers", "hub", "enabled")
			// No worker consumes these credentials, so their generators must not
			// survive after both hub and company selection are empty.
			componentSelectionSet(values, "", "app", "existingSecret", "name")
			componentSelectionSet(values, "", "passport", "existingSecret", "name")
			componentSelectionSet(values, "", "workers", "mono", "store", "existingSecret", "name")
			if objects := renderComponentSelection(t, profile.name, "empty", values); len(objects) != 0 {
				t.Fatalf("no selected workers should own no resources, got %v", objects)
			}
		})
	}
}

func TestComponentSelectionMonoCompanyStoreGenerator(t *testing.T) {
	values := componentSelectionValues(t)
	componentSelectionEnable(values, componentSelectionProfiles()[7])
	componentSelectionSet(values, false, "workers", "hub", "enabled")
	componentSelectionSet(values, "unused-hub", "workers", "namespace")
	componentSelectionSet(values, "", "workers", "mono", "store", "existingSecret", "name")
	componentSelectionSet(values, []any{
		map[string]any{"name": "first", "namespace": "tenant-a"},
		map[string]any{"name": "second", "namespace": "tenant-a"},
		map[string]any{"name": "third", "namespace": "tenant-b"},
	}, "workers", "companies")
	objects := renderComponentSelection(t, "company", "helm-release", values)
	seen := map[string]bool{}
	foundGenerator := false
	for _, object := range objects {
		identity := componentSelectionIdentity(object, "helm-release")
		if seen[identity] {
			t.Errorf("duplicate resource in company release: %s", identity)
		}
		seen[identity] = true
		metadata := componentSelectionMap(object, "metadata")
		if metadata["namespace"] == "unused-hub" {
			t.Errorf("disabled hub retained resource %s", identity)
		}
		if componentSelectionMap(metadata, "labels")["app.kubernetes.io/component"] != "workers-storegen" {
			continue
		}
		switch object["kind"] {
		case "ServiceAccount", "Job":
			if metadata["namespace"] != "tenant-a" {
				t.Errorf("store generator must run in its first selected consumer namespace: %s", identity)
			}
		case "ClusterRoleBinding":
			subjects, _ := object["subjects"].([]any)
			if len(subjects) != 1 || subjects[0].(map[string]any)["namespace"] != "tenant-a" {
				t.Errorf("store generator RBAC binds the wrong namespace: %v", subjects)
			}
		}
		if object["kind"] != "Job" {
			continue
		}
		foundGenerator = true
		containers := componentSelectionMap(object, "spec", "template", "spec")["containers"].([]any)
		command := containers[0].(map[string]any)["command"].([]any)
		script := command[len(command)-1].(string)
		for _, namespace := range []string{"tenant-a", "tenant-b"} {
			if strings.Count(script, "apply_store_secret \""+namespace+"\"") != 1 {
				t.Errorf("store Secret must be written once in %s: %s", namespace, script)
			}
		}
		if strings.Contains(script, "apply_store_secret \"unused-hub\"") {
			t.Error("store generator still writes the disabled hub Secret")
		}
	}
	if !foundGenerator {
		t.Fatal("missing company store generator")
	}
}

func TestComponentSelectionMonocoreRedisScheme(t *testing.T) {
	checksums := map[string]map[string]string{}
	for _, mode := range []string{"standalone", "sentinel", "cluster"} {
		for _, scheme := range []string{"", "tcp", "tls"} {
			t.Run(mode+"/"+scheme, func(t *testing.T) {
				checksums[mode+"/"+scheme] = map[string]string{}
				values := componentSelectionValues(t)
				componentSelectionSet(values, true, "workers", "enabled")
				componentSelectionSet(values, "mono", "workers", "type")
				componentSelectionSet(values, "hub", "workers", "namespace")
				componentSelectionSet(values, []any{map[string]any{"name": "acme", "namespace": "acme-workers"}}, "workers", "companies")
				componentSelectionSet(values, mode, "redis", "mode")
				if scheme != "" {
					componentSelectionSet(values, scheme, "redis", "scheme")
				}
				wantScheme := scheme
				if wantScheme == "" {
					wantScheme = "tcp"
				}
				seen := map[string]bool{}
				for _, object := range renderComponentSelection(t, "redis-scheme", "redis-scheme", values) {
					if object["kind"] == "Deployment" {
						namespace := fmt.Sprint(componentSelectionMap(object, "metadata")["namespace"])
						checksums[mode+"/"+scheme][namespace] = fmt.Sprint(componentSelectionMap(object, "spec", "template", "metadata", "annotations")["checksum/config"])
					}
					if object["kind"] != "ConfigMap" {
						continue
					}
					config, ok := componentSelectionMap(object, "data")["config.yaml"].(string)
					if !ok {
						continue
					}
					var parsed map[string]any
					if err := yaml.Unmarshal([]byte(config), &parsed); err != nil {
						t.Fatal(err)
					}
					redis := componentSelectionMap(parsed, "redis")
					if redis["scheme"] != wantScheme || redis["mode"] != mode {
						t.Errorf("Redis transport = mode %v / scheme %v, want %s / %s", redis["mode"], redis["scheme"], mode, wantScheme)
					}
					seen[fmt.Sprint(componentSelectionMap(object, "metadata")["namespace"])] = true
				}
				if len(seen) != 2 || !seen["hub"] || !seen["acme-workers"] {
					t.Fatalf("expected hub and company Redis configurations, got namespaces %v", seen)
				}
			})
		}
		for _, namespace := range []string{"hub", "acme-workers"} {
			if checksums[mode+"/tcp"][namespace] == checksums[mode+"/tls"][namespace] {
				t.Errorf("changing %s Redis from tcp to tls must roll worker in %s", mode, namespace)
			}
			if checksums[mode+"/"][namespace] != checksums[mode+"/tcp"][namespace] {
				t.Errorf("default Redis scheme must behave like explicit tcp in %s", namespace)
			}
		}
	}
}
