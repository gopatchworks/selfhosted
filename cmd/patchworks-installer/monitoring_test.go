package main

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

func monitoringInfra(t *testing.T, values map[string]any) []map[string]any {
	t.Helper()
	files, err := renderDatabaseInfra(values)
	if err != nil {
		t.Fatal(err)
	}
	var objects []map[string]any
	for path, text := range files {
		if strings.HasSuffix(path, "NOTES.txt") {
			continue
		}
		decoder := yaml.NewDecoder(strings.NewReader(text))
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

func monitoringMatches(labels, selector map[string]any) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// Verify the complete monitor -> Service -> Deployment -> named container port
// chain. This catches label/namespace mistakes that produce silent empty targets.
func assertMonitoringTargets(t *testing.T, objects []map[string]any, count int) []map[string]any {
	t.Helper()
	var monitors []map[string]any
	for _, monitor := range objects {
		if monitor["kind"] != "ServiceMonitor" {
			continue
		}
		monitors = append(monitors, monitor)
		namespace := componentSelectionMap(monitor, "metadata")["namespace"]
		namespaces := componentSelectionMap(monitor, "spec", "namespaceSelector")["matchNames"]
		if !reflect.DeepEqual(namespaces, []any{namespace}) {
			t.Fatalf("monitor namespace mismatch: %v", namespaces)
		}
		var services []map[string]any
		for _, service := range objects {
			if service["kind"] == "Service" && componentSelectionMap(service, "metadata")["namespace"] == namespace && monitoringMatches(componentSelectionMap(service, "metadata", "labels"), componentSelectionMap(monitor, "spec", "selector", "matchLabels")) {
				services = append(services, service)
			}
		}
		if len(services) != 1 {
			t.Fatalf("monitor %v selected %d services", componentSelectionMap(monitor, "metadata"), len(services))
		}
		service := services[0]
		endpoints := componentSelectionMap(monitor, "spec")["endpoints"].([]any)
		for _, item := range endpoints {
			endpoint := item.(map[string]any)
			var targetPort any
			for _, portItem := range componentSelectionMap(service, "spec")["ports"].([]any) {
				port := portItem.(map[string]any)
				if port["name"] == endpoint["port"] {
					targetPort = port["targetPort"]
				}
			}
			if targetPort == nil {
				t.Fatalf("monitor port %v does not exist on service", endpoint["port"])
			}
			matched := false
			for _, deployment := range objects {
				if deployment["kind"] != "Deployment" || componentSelectionMap(deployment, "metadata")["namespace"] != namespace || !monitoringMatches(componentSelectionMap(deployment, "spec", "template", "metadata", "labels"), componentSelectionMap(service, "spec", "selector")) {
					continue
				}
				for _, containerItem := range componentSelectionMap(deployment, "spec", "template", "spec")["containers"].([]any) {
					ports, _ := containerItem.(map[string]any)["ports"].([]any)
					for _, portItem := range ports {
						if portItem.(map[string]any)["name"] == targetPort {
							matched = true
						}
					}
				}
			}
			if !matched {
				t.Fatalf("service port %v has no matching deployment container", targetPort)
			}
		}
	}
	if len(monitors) != count {
		t.Fatalf("got %d monitors, want %d", len(monitors), count)
	}
	return monitors
}

func TestMonocoreServiceMonitors(t *testing.T) {
	for _, tc := range []struct {
		name, workerType      string
		enabled, monitor, hub bool
		companies             []any
		count                 int
	}{
		{"default off", "mono", true, false, true, nil, 0},
		{"hub", "mono", true, true, true, nil, 1},
		{"hub and company namespaces", "mono", true, true, true, []any{map[string]any{"name": "acme", "namespace": "acme-workers"}}, 2},
		{"company only", "mono", true, true, false, []any{map[string]any{"name": "acme", "namespace": "acme-workers"}}, 1},
		{"disabled workers", "mono", false, true, true, nil, 0},
		{"no workers", "mono", true, true, false, nil, 0},
		{"PHP workers", "standalone", true, true, true, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := componentSelectionValues(t)
			componentSelectionSet(values, tc.enabled, "workers", "enabled")
			componentSelectionSet(values, tc.workerType, "workers", "type")
			componentSelectionSet(values, tc.hub, "workers", "hub", "enabled")
			componentSelectionSet(values, "worker-ns", "workers", "namespace")
			componentSelectionSet(values, tc.companies, "workers", "companies")
			componentSelectionSet(values, tc.monitor, "workers", "mono", "serviceMonitor", "enabled")
			componentSelectionSet(values, map[string]any{"release": "metrics"}, "workers", "mono", "serviceMonitor", "additionalLabels")
			componentSelectionSet(values, "45s", "workers", "mono", "serviceMonitor", "interval")
			monitors := assertMonitoringTargets(t, renderComponentSelection(t, "application", "release-ns", values), tc.count)
			for _, monitor := range monitors {
				if componentSelectionMap(monitor, "metadata", "labels")["release"] != "metrics" {
					t.Fatal("missing selector label")
				}
				endpoint := componentSelectionMap(monitor, "spec")["endpoints"].([]any)[0].(map[string]any)
				if endpoint["interval"] != "45s" || endpoint["path"] != "/metrics" || endpoint["port"] != "metrics" {
					t.Fatalf("unexpected endpoint: %v", endpoint)
				}
			}
		})
	}
}

func TestInfrastructureServiceMonitors(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, auth := range []bool{false, true} {
			t.Run(fmt.Sprintf("components=%v/auth=%v", enabled, auth), func(t *testing.T) {
				values := map[string]any{}
				componentSelectionSet(values, enabled, "rabbitmq", "enabled")
				componentSelectionSet(values, "rabbit-ns", "rabbitmq", "namespace")
				componentSelectionSet(values, true, "rabbitmq", "metrics", "enabled")
				componentSelectionSet(values, true, "rabbitmq", "metrics", "serviceMonitor", "enabled")
				componentSelectionSet(values, enabled, "pusher", "enabled")
				componentSelectionSet(values, true, "soketi", "metrics", "enabled")
				componentSelectionSet(values, true, "soketi", "metrics", "serviceMonitor", "enabled")
				componentSelectionSet(values, enabled, "kubefaas", "enabled")
				componentSelectionSet(values, "function-ns", "kubefaas", "namespace")
				componentSelectionSet(values, true, "kubefaas", "controller", "serviceMonitor", "enabled")
				componentSelectionSet(values, true, "kubefaas", "builder", "serviceMonitor", "enabled")
				componentSelectionSet(values, auth, "kubefaas", "auth", "enabled")
				componentSelectionSet(values, map[string]any{"name": "scrape-auth", "usernameKey": "user", "passwordKey": "pass"}, "kubefaas", "auth", "existingSecret")
				componentSelectionSet(values, "existingSecret", "kubefaas", "builder", "tls", "mode")
				componentSelectionSet(values, map[string]any{"serverTls": "server", "clientTls": "client"}, "kubefaas", "builder", "tls", "existingSecret")
				count := 0
				if enabled {
					count = 4
				}
				objects := monitoringInfra(t, values)
				monitors := assertMonitoringTargets(t, objects, count)
				for _, monitor := range monitors {
					name := fmt.Sprint(componentSelectionMap(monitor, "metadata")["name"])
					endpoint := componentSelectionMap(monitor, "spec")["endpoints"].([]any)[0].(map[string]any)
					if strings.Contains(name, "kubefaas") {
						expected := map[string]any{"username": map[string]any{"name": "scrape-auth", "key": "user"}, "password": map[string]any{"name": "scrape-auth", "key": "pass"}}
						if auth && !reflect.DeepEqual(endpoint["basicAuth"], expected) {
							t.Fatalf("wrong auth: %v", endpoint)
						}
						if !auth && endpoint["basicAuth"] != nil {
							t.Fatal("auth included when disabled")
						}
					}
					if strings.Contains(name, "rabbitmq") && endpoint["path"] != "/metrics/per-object" {
						t.Fatal("queue metrics must retain object labels")
					}
				}
				if enabled {
					pluginFound, soketiFound := false, false
					for _, object := range objects {
						if object["kind"] == "ConfigMap" && componentSelectionMap(object, "data")["enabled_plugins"] == "[rabbitmq_management,rabbitmq_prometheus]." {
							pluginFound = true
						}
						if object["kind"] == "Deployment" && componentSelectionMap(object, "metadata", "labels")["app.kubernetes.io/component"] == "soketi" {
							init := componentSelectionMap(object, "spec", "template", "spec")["initContainers"].([]any)[0].(map[string]any)
							command := fmt.Sprint(init["command"])
							soketiFound = strings.Contains(command, "'metrics.enabled': true") && strings.Contains(command, "'metrics.port': 9601")
						}
					}
					if !pluginFound || !soketiFound {
						t.Fatalf("exporters not activated: rabbit=%v soketi=%v", pluginFound, soketiFound)
					}
				}
			})
		}
	}
}

func TestInfrastructureMonitoringDefaultOff(t *testing.T) {
	assertMonitoringTargets(t, monitoringInfra(t, nil), 0)
	// A ServiceMonitor opt-in alone must not target a disabled exporter.
	values := map[string]any{}
	componentSelectionSet(values, true, "rabbitmq", "metrics", "serviceMonitor", "enabled")
	componentSelectionSet(values, true, "soketi", "metrics", "serviceMonitor", "enabled")
	assertMonitoringTargets(t, monitoringInfra(t, values), 0)
}
