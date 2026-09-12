package main

import (
	"fmt"
	"testing"
)

func redisConfigurationValues(t *testing.T) map[string]any {
	t.Helper()
	values := componentSelectionValues(t)
	for _, path := range [][]string{
		{"web", "gateway", "enabled"}, {"web", "start", "enabled"},
		{"fabric", "enabled"}, {"fabric", "migrations", "enabled"},
		{"workers", "enabled"}, {"processorDeployments", "enabled"}, {"scheduler", "enabled"},
		{"migrations", "enabled"}, {"seeds", "fabric", "enabled"}, {"seeds", "core", "enabled"},
	} {
		componentSelectionSet(values, true, path...)
	}
	componentSelectionSet(values, "standalone", "workers", "type")
	componentSelectionSet(values, []any{map[string]any{"name": "processor", "queue": "redis-test"}}, "processors")
	componentSelectionSet(values, "Redis Test", "seeds", "tenant", "companyName")
	componentSelectionSet(values, "Admin", "seeds", "tenant", "adminName")
	componentSelectionSet(values, "admin@example.test", "seeds", "tenant", "adminEmail")
	componentSelectionSet(values, "external-tenant-admin", "seeds", "tenant", "existingSecret", "name")
	componentSelectionSet(values, false, "fabric", "redis", "enabled")
	componentSelectionSet(values, "fabric-redis.example.test", "fabric", "redis", "external", "host")
	componentSelectionSet(values, 6380, "fabric", "redis", "external", "port")
	componentSelectionSet(values, "external-fabric-redis", "fabric", "redis", "external", "existingSecret", "name")
	return values
}

func redisConfigurationEnv(container map[string]any) map[string]any {
	env := map[string]any{}
	entries, _ := container["env"].([]any)
	for _, entry := range entries {
		item := entry.(map[string]any)
		if value, ok := item["value"]; ok {
			env[item["name"].(string)] = value
		}
	}
	return env
}

func assertRedisConfiguration(t *testing.T, location string, env map[string]any, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if env[name] != value {
			t.Errorf("%s: %s = %v, want %q", location, name, env[name], value)
		}
	}
}

func TestRedisConfigurationPHPConsumers(t *testing.T) {
	checksums := map[string]map[string]string{}
	cases := []struct {
		name, coreMode, coreScheme, fabricMode, fabricScheme string
	}{
		{name: "defaults"},
		{name: "core-cluster", coreMode: "cluster"},
		{name: "core-cluster-tls", coreMode: "cluster", coreScheme: "tls"},
		{name: "fabric-cluster", fabricMode: "cluster"},
		{name: "fabric-cluster-tls", fabricMode: "cluster", fabricScheme: "tls"},
		{name: "core-sentinel", coreMode: "sentinel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := redisConfigurationValues(t)
			for _, setting := range []struct {
				value string
				path  []string
			}{
				{tc.coreMode, []string{"redis", "mode"}}, {tc.coreScheme, []string{"redis", "scheme"}},
				{tc.fabricMode, []string{"fabric", "redis", "mode"}}, {tc.fabricScheme, []string{"fabric", "redis", "scheme"}},
			} {
				if setting.value != "" {
					componentSelectionSet(values, setting.value, setting.path...)
				}
			}
			coreMode, coreScheme, fabricMode, fabricScheme := tc.coreMode, tc.coreScheme, tc.fabricMode, tc.fabricScheme
			if coreMode == "" {
				coreMode = "standalone"
			}
			if coreScheme == "" {
				coreScheme = "tcp"
			}
			if fabricMode == "" {
				fabricMode = "standalone"
			}
			if fabricScheme == "" {
				fabricScheme = "tcp"
			}
			coreWant := map[string]string{
				"REDIS_HOST": "redis.example.test", "REDIS_PORT": "6379", "REDIS_DB": "0",
				"REDIS_MODE": coreMode, "REDIS_SCHEME": coreScheme, "REDIS_CLIENT": "phpredis",
			}
			if coreMode == "sentinel" {
				componentSelectionSet(values, "primary", "redis", "sentinel", "master")
				coreWant["REDIS_CLIENT"] = "phpredis-sentinel"
				coreWant["REDIS_SENTINEL_SERVICE"] = "primary"
				coreWant["TENANT_REDIS_SENTINEL_SERVICE"] = "primary"
				coreWant["PAYLOAD_REDIS_SENTINEL_SERVICE"] = "primary"
			}
			fabricWant := map[string]string{
				"REDIS_HOST": "fabric-redis.example.test", "REDIS_PORT": "6380",
				"REDIS_MODE": fabricMode, "REDIS_SCHEME": fabricScheme,
			}
			if fabricMode == "cluster" {
				fabricWant["REDIS_CLIENT"] = "phpredis"
			}
			objects := renderComponentSelection(t, "redis-config", "redis-config", values)
			configMaps := map[string]map[string]any{}
			for _, object := range objects {
				if object["kind"] == "ConfigMap" {
					name := fmt.Sprint(componentSelectionMap(object, "metadata")["name"])
					configMaps[name] = componentSelectionMap(object, "data")
				}
			}
			coreConfig, fabricConfig := "patchworks-config", "patchworks-fabric-config"
			assertRedisConfiguration(t, "Core ConfigMap", configMaps[coreConfig], coreWant)
			assertRedisConfiguration(t, "Fabric ConfigMap", configMaps[fabricConfig], fabricWant)
			if fabricMode != "cluster" && configMaps[fabricConfig]["REDIS_CLIENT"] != nil {
				t.Error("standalone Fabric must retain its application's default Redis client")
			}
			checksums[tc.name] = map[string]string{}
			seen := map[string]bool{}
			for _, object := range objects {
				component := fmt.Sprint(componentSelectionMap(object, "metadata", "labels")["app.kubernetes.io/component"])
				var template map[string]any
				switch object["kind"] {
				case "Deployment", "Job":
					template = componentSelectionMap(object, "spec", "template")
				case "CronJob":
					template = componentSelectionMap(object, "spec", "jobTemplate", "spec", "template")
				default:
					continue
				}
				want, configName := coreWant, coreConfig
				if component == "fabric" || component == "fabric-migrations" || component == "fabric-seeds" || component == "fabric-company-seed" {
					want, configName = fabricWant, fabricConfig
				}
				spec := componentSelectionMap(template, "spec")
				containers, _ := spec["containers"].([]any)
				if component == "fabric" {
					initContainers, _ := spec["initContainers"].([]any)
					containers = append(containers, initContainers...)
				}
				for _, raw := range containers {
					container := raw.(map[string]any)
					name := fmt.Sprint(container["name"])
					if name == "fabric-nginx" {
						continue
					}
					location := component + "/" + name
					if object["kind"] == "Job" {
						// Lifecycle Jobs use inline env, so ConfigMap correctness alone cannot protect them.
						if component != "core-migrations" && component != "core-seeds" && component != "fabric-migrations" && component != "fabric-seeds" && component != "fabric-company-seed" {
							continue
						}
						assertRedisConfiguration(t, location, redisConfigurationEnv(container), want)
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
						// An inline env entry wins over envFrom; check that no workload overrides transport.
						for key, value := range redisConfigurationEnv(container) {
							if expected, ok := want[key]; ok && value != expected {
								t.Errorf("%s overrides %s with %v, want %s", location, key, value, expected)
							}
						}
						checksum, _ := componentSelectionMap(template, "metadata", "annotations")["checksum/config"].(string)
						if checksum == "" {
							t.Errorf("%s has no configuration rollout checksum", location)
						}
						checksums[tc.name][component] = checksum
					}
					seen[location] = true
				}
			}
			for _, location := range []string{
				"gateway/gateway", "start/start", "processor-redis-test/processor", "redis-test-scheduler/redis-test-scheduler", "workers/workers",
				"fabric/fabric", "fabric/fabric-init", "fabric-migrations/fabric-migrations", "fabric-seeds/fabric-seeds", "fabric-company-seed/fabric-company-seed",
				"core-migrations/core-migrations", "core-seeds/core-seeds",
			} {
				if !seen[location] {
					t.Errorf("missing Redis consumer %s", location)
				}
			}
		})
	}
	for _, component := range []string{"gateway", "start", "processor-redis-test", "redis-test-scheduler", "workers", "fabric"} {
		prefix, otherPrefix := "core", "fabric"
		if component == "fabric" {
			prefix, otherPrefix = "fabric", "core"
		}
		if checksums["defaults"][component] == checksums[prefix+"-cluster"][component] {
			t.Errorf("changing Redis mode must update %s's pod template", component)
		}
		if checksums[prefix+"-cluster"][component] == checksums[prefix+"-cluster-tls"][component] {
			t.Errorf("enabling Redis TLS must update %s's pod template", component)
		}
		if checksums["defaults"][component] != checksums[otherPrefix+"-cluster-tls"][component] {
			t.Errorf("%s must not inherit %s's independent Redis transport settings", component, otherPrefix)
		}
	}
}
