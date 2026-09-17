package main

import (
	"fmt"
	"testing"
)

func TestFabricCoreDatabaseProvisioning(t *testing.T) {
	checksums := map[string]string{}
	cases := []struct {
		name, namespace, webNamespace, gatewayNamespace, fullname, url, wantURL string
		webPort, gatewayPort                                                    int
		disabled, external                                                      bool
	}{
		{name: "defaults", wantURL: "http://patchworks-gateway.release-ns.svc.cluster.local"},
		{name: "disabled", disabled: true, wantURL: "http://patchworks-gateway.release-ns.svc.cluster.local"},
		{name: "global-namespace", namespace: "global-ns", wantURL: "http://patchworks-gateway.global-ns.svc.cluster.local"},
		{name: "web-namespace-and-port", namespace: "global-ns", webNamespace: "web-ns", webPort: 8080, wantURL: "http://patchworks-gateway.web-ns.svc.cluster.local:8080"},
		{name: "gateway-overrides", namespace: "global-ns", webNamespace: "web-ns", gatewayNamespace: "gateway-ns", fullname: "custom", webPort: 9090, gatewayPort: 8081, wantURL: "http://custom-gateway.gateway-ns.svc.cluster.local:8081"},
		{name: "gateway-port-80", webPort: 9090, gatewayPort: 80, wantURL: "http://patchworks-gateway.release-ns.svc.cluster.local"},
		{name: "external-gateway", external: true, url: "https://core.example.test:8443", wantURL: "https://core.example.test:8443"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := componentSelectionValues(t)
			for _, path := range [][]string{
				{"fabric", "enabled"}, {"fabric", "migrations", "enabled"}, {"seeds", "fabric", "enabled"},
			} {
				componentSelectionSet(values, true, path...)
			}
			componentSelectionSet(values, !tc.external, "web", "gateway", "enabled")
			componentSelectionSet(values, "Test Company", "seeds", "tenant", "companyName")
			componentSelectionSet(values, "Admin", "seeds", "tenant", "adminName")
			componentSelectionSet(values, "admin@example.test", "seeds", "tenant", "adminEmail")
			componentSelectionSet(values, "external-admin", "seeds", "tenant", "existingSecret", "name")
			for _, setting := range []struct {
				value string
				path  []string
			}{
				{tc.namespace, []string{"namespace"}}, {tc.webNamespace, []string{"web", "namespace"}},
				{tc.gatewayNamespace, []string{"web", "gateway", "namespace"}}, {tc.fullname, []string{"fullnameOverride"}},
				{tc.url, []string{"fabric", "core", "gatewayUrl"}},
			} {
				if setting.value != "" {
					componentSelectionSet(values, setting.value, setting.path...)
				}
			}
			if tc.webPort != 0 {
				componentSelectionSet(values, tc.webPort, "web", "service", "port")
			}
			if tc.gatewayPort != 0 {
				componentSelectionSet(values, tc.gatewayPort, "web", "gateway", "service", "port")
			}
			if tc.disabled {
				// Explicit false must survive Helm's value merging.
				componentSelectionSet(values, false, "fabric", "core", "initialiseDatabases")
				componentSelectionSet(values, false, "fabric", "core", "createSubscription")
			}
			want := map[string]string{
				"CORE_INITIALISE_DATABASES": fmt.Sprint(!tc.disabled),
				"CORE_CREATE_SUBSCRIPTION":  fmt.Sprint(!tc.disabled),
				"CORE_GATEWAY_URL":          tc.wantURL,
			}
			assertEnv := func(location string, env map[string]any) {
				t.Helper()
				for key, expected := range want {
					if env[key] != expected {
						t.Errorf("%s: %s = %v, want %q", location, key, env[key], expected)
					}
				}
			}
			fullname := tc.fullname
			if fullname == "" {
				fullname = "patchworks"
			}
			configName := fullname + "-fabric-config"
			objects := renderComponentSelection(t, "fabric-core", "release-ns", values)
			seen := map[string]bool{}
			gatewayFound := false
			for _, object := range objects {
				metadata := componentSelectionMap(object, "metadata")
				component := fmt.Sprint(componentSelectionMap(metadata, "labels")["app.kubernetes.io/component"])
				if object["kind"] == "Service" && component == "gateway" {
					gatewayFound = true
					ports := componentSelectionMap(object, "spec")["ports"].([]any)
					port := fmt.Sprint(ports[0].(map[string]any)["port"])
					serviceURL := fmt.Sprintf("http://%s.%s.svc.cluster.local", metadata["name"], metadata["namespace"])
					if port != "80" {
						serviceURL += ":" + port
					}
					if serviceURL != tc.wantURL {
						t.Errorf("resolved URL %q does not match rendered Gateway Service %q", tc.wantURL, serviceURL)
					}
				}
				if object["kind"] == "ConfigMap" && metadata["name"] == configName {
					assertEnv(configName, componentSelectionMap(object, "data"))
					seen["config"] = true
				}
				if component != "fabric" && component != "fabric-migrations" && component != "fabric-seeds" && component != "fabric-company-seed" {
					continue
				}
				if object["kind"] != "Deployment" && object["kind"] != "Job" {
					continue
				}
				template := componentSelectionMap(object, "spec", "template")
				spec := componentSelectionMap(template, "spec")
				containers, _ := spec["containers"].([]any)
				if object["kind"] == "Deployment" {
					initContainers, _ := spec["initContainers"].([]any)
					containers = append(containers, initContainers...)
					checksums[tc.name], _ = componentSelectionMap(template, "metadata", "annotations")["checksum/config"].(string)
					if checksums[tc.name] == "" {
						t.Error("Fabric has no configuration rollout checksum")
					}
				}
				for _, raw := range containers {
					container := raw.(map[string]any)
					name := fmt.Sprint(container["name"])
					if name == "fabric-nginx" {
						continue
					}
					location := component + "/" + name
					inline := redisConfigurationEnv(container)
					if object["kind"] == "Job" {
						assertEnv(location, inline)
					} else {
						found := false
						refs, _ := container["envFrom"].([]any)
						for _, ref := range refs {
							if componentSelectionMap(ref.(map[string]any), "configMapRef")["name"] == configName {
								found = true
							}
						}
						if !found {
							t.Errorf("%s does not consume %s", location, configName)
						}
						for key, expected := range want {
							if value, ok := inline[key]; ok && value != expected {
								t.Errorf("%s overrides %s with %v", location, key, value)
							}
						}
					}
					seen[location] = true
				}
			}
			if gatewayFound == tc.external {
				t.Errorf("Gateway Service present = %t, want %t", gatewayFound, !tc.external)
			}
			for _, location := range []string{"config", "fabric/fabric", "fabric/fabric-init", "fabric-migrations/fabric-migrations", "fabric-seeds/fabric-seeds", "fabric-company-seed/fabric-company-seed"} {
				if !seen[location] {
					t.Errorf("missing Fabric Core configuration consumer: %s", location)
				}
			}
		})
	}
	for _, changed := range []string{"disabled", "external-gateway"} {
		if checksums["defaults"] == checksums[changed] {
			t.Errorf("changing %s must update Fabric's pod template", changed)
		}
	}
}
