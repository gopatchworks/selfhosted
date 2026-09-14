package main

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
	"helm.sh/helm/v4/pkg/chart/common"
	chartutil "helm.sh/helm/v4/pkg/chart/common/util"
	helmloader "helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/engine"
)

// Resolve chart-owned envFrom resources as Kubernetes does. References to BYO
// Secrets remain symbolic, so these checks require neither cluster credentials
// nor stable randomly generated passwords.
func databaseAppEnvironment(t *testing.T, objects []map[string]any, workload map[string]any) map[string]string {
	t.Helper()
	namespace := componentSelectionMap(workload, "metadata")["namespace"]
	resourceData := func(kind, name string) map[string]string {
		for _, object := range objects {
			metadata := componentSelectionMap(object, "metadata")
			if object["kind"] != kind || metadata["name"] != name || metadata["namespace"] != namespace {
				continue
			}
			data := map[string]string{}
			for key, value := range componentSelectionMap(object, "data") {
				text := fmt.Sprint(value)
				if kind == "Secret" {
					decoded, err := base64.StdEncoding.DecodeString(text)
					if err != nil {
						t.Fatalf("invalid Secret %s data: %v", name, err)
					}
					text = string(decoded)
				}
				data[key] = text
			}
			for key, value := range componentSelectionMap(object, "stringData") {
				data[key] = fmt.Sprint(value)
			}
			return data
		}
		return nil
	}
	containers, _ := componentSelectionMap(workload, "spec", "template", "spec")["containers"].([]any)
	if len(containers) != 1 {
		t.Fatalf("expected one application container in %v", componentSelectionMap(workload, "metadata")["name"])
	}
	container := containers[0].(map[string]any)
	environment := map[string]string{}
	imports, _ := container["envFrom"].([]any)
	for _, item := range imports {
		entry := item.(map[string]any)
		for field, kind := range map[string]string{"configMapRef": "ConfigMap", "secretRef": "Secret"} {
			if ref := componentSelectionMap(entry, field); ref != nil {
				for key, value := range resourceData(kind, fmt.Sprint(ref["name"])) {
					environment[key] = value
				}
			}
		}
	}
	variables, _ := container["env"].([]any)
	seen := map[string]bool{}
	for _, item := range variables {
		entry := item.(map[string]any)
		name := fmt.Sprint(entry["name"])
		if seen[name] {
			t.Errorf("duplicate explicit environment variable %s", name)
		}
		seen[name] = true
		if value, ok := entry["value"]; ok {
			environment[name] = fmt.Sprint(value)
		} else if ref := componentSelectionMap(entry, "valueFrom", "secretKeyRef"); ref != nil {
			secret, key := fmt.Sprint(ref["name"]), fmt.Sprint(ref["key"])
			if value, ok := resourceData("Secret", secret)[key]; ok {
				environment[name] = value
			} else {
				environment[name] = "secret:" + secret + "/" + key
			}
		}
	}
	return environment
}

func assertDatabaseAppEnv(t *testing.T, got, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if actual, ok := got[name]; !ok || actual != value {
			t.Errorf("%s = %q (present=%v), want %q", name, actual, ok, value)
		}
	}
}

func databaseAppValues(t *testing.T) map[string]any {
	t.Helper()
	values := componentSelectionValues(t)
	componentSelectionSet(values, "databases", "fullnameOverride")
	componentSelectionSet(values, true, "web", "gateway", "enabled")
	componentSelectionSet(values, "legacy_core", "mysql", "external", "database")
	componentSelectionSet(values, "legacy_user", "mysql", "external", "username")
	return values
}

func databaseAppSeparateConnections(values map[string]any) {
	componentSelectionSet(values, map[string]any{
		"landlord": map[string]any{
			"host": "landlord.example.test", "readHost": "landlord-read.example.test", "port": 3307,
			"database": "landlord_shared", "username": "landlord_user", "password": "landlord-inline-test-password",
		},
		"tenant": map[string]any{
			"host": "tenant-default.example.test", "readHost": "tenant-default-read.example.test", "port": 3308,
			"username": "default_user", "existingSecret": map[string]any{"name": "tenant-default", "passwordKey": "custom-default-password"},
			"pool":            map[string]any{"maxSizePerServer": 23},
			"primaryServerId": 17,
			"servers": map[string]any{
				"blue": map[string]any{
					"host": "tenant-blue.example.test", "readHost": "tenant-blue-read.example.test", "username": "blue_user",
					"password": "blue-inline-test-password",
				},
				"Green_2": map[string]any{
					"host": "tenant-green.example.test", "port": 3310, "username": "green_user",
					"existingSecret": map[string]any{"name": "tenant-green", "passwordKey": "custom-green-password"},
				},
			},
		},
	}, "database")
}

func TestDatabaseAppDefaultCompatibility(t *testing.T) {
	objects := renderComponentSelection(t, "databases", "databases", databaseAppValues(t))
	for _, object := range objects {
		if object["kind"] != "Deployment" {
			continue
		}
		env := databaseAppEnvironment(t, objects, object)
		for _, prefix := range []string{"DB", "LANDLORD_DB", "TENANT_DB"} {
			assertDatabaseAppEnv(t, env, map[string]string{
				prefix + "_HOST": "core-db.example.test", prefix + "_PORT": "3306",
				prefix + "_USERNAME": "legacy_user", prefix + "_PASSWORD": "secret:external-core-db/password",
			})
		}
		assertDatabaseAppEnv(t, env, map[string]string{
			"DB_DATABASE": "legacy_core", "LANDLORD_DB_DATABASE": "legacy_core",
			"LANDLORD_DB_CONNECTION": "landlord", "TENANT_DB_DATABASE": "information_schema",
		})
		return
	}
	t.Fatal("gateway Deployment was not rendered")
}

func TestDatabaseAppCorePodsAndLifecycleShareConnections(t *testing.T) {
	values := databaseAppValues(t)
	databaseAppSeparateConnections(values)
	componentSelectionSet(values, true, "workers", "enabled")
	componentSelectionSet(values, "standalone", "workers", "type")
	componentSelectionSet(values, true, "migrations", "enabled")
	componentSelectionSet(values, true, "seeds", "core", "enabled")
	componentSelectionSet(values, false, "seeds", "tenant", "createDatabase")
	objects := renderComponentSelection(t, "databases", "databases", values)
	want := map[string]string{
		"DB_HOST": "landlord.example.test", "DB_DATABASE": "landlord_shared", "DB_PASSWORD": "landlord-inline-test-password",
		"LANDLORD_DB_CONNECTION": "landlord", "LANDLORD_DB_HOST": "landlord.example.test", "LANDLORD_DB_PORT": "3307",
		"LANDLORD_DB_DATABASE": "landlord_shared", "LANDLORD_DB_USERNAME": "landlord_user", "LANDLORD_DB_PASSWORD": "landlord-inline-test-password",
		"LANDLORD_DB_READ_HOST": "landlord-read.example.test",
		"TENANT_DB_HOST":        "tenant-default.example.test", "TENANT_DB_PORT": "3308", "TENANT_DB_USERNAME": "default_user",
		"TENANT_DB_PASSWORD": "secret:tenant-default/custom-default-password", "TENANT_DB_DATABASE": "information_schema",
		"TENANT_DB_READ_HOST": "tenant-default-read.example.test", "DATABASE_POOL_MAX_SIZE": "23", "PRIMARY_TENANT_DATABASE_SERVER_ID": "17",
		"TENANT_DB_HOST_blue": "tenant-blue.example.test", "TENANT_DB_PORT_blue": "3306", "TENANT_DB_USERNAME_blue": "blue_user",
		"TENANT_DB_PASSWORD_blue": "blue-inline-test-password", "TENANT_DB_READ_HOST_blue": "tenant-blue-read.example.test",
		"TENANT_DB_HOST_Green_2": "tenant-green.example.test", "TENANT_DB_PORT_Green_2": "3310", "TENANT_DB_USERNAME_Green_2": "green_user",
		"TENANT_DB_PASSWORD_Green_2": "secret:tenant-green/custom-green-password", "FABRIC_DB_HOST": "fabric-db.example.test",
	}
	seen := map[string]bool{}
	for _, object := range objects {
		component := fmt.Sprint(componentSelectionMap(object, "metadata", "labels")["app.kubernetes.io/component"])
		if object["kind"] == "Deployment" || object["kind"] == "Job" && (component == "core-migrations" || component == "core-seeds") {
			t.Run(component, func(t *testing.T) { assertDatabaseAppEnv(t, databaseAppEnvironment(t, objects, object), want) })
			seen[component] = true
		}
		if object["kind"] == "ConfigMap" {
			data, err := yaml.Marshal(componentSelectionMap(object, "data"))
			if err != nil {
				t.Fatal(err)
			}
			for _, password := range []string{"landlord-inline-test-password", "blue-inline-test-password"} {
				if strings.Contains(string(data), password) {
					t.Errorf("inline database credential exposed in ConfigMap %v", componentSelectionMap(object, "metadata")["name"])
				}
			}
		}
	}
	for _, component := range []string{"gateway", "workers", "core-migrations", "core-seeds"} {
		if !seen[component] {
			t.Errorf("missing %s workload", component)
		}
	}
}

func TestDatabaseAppMonocoreHubAndCompanyConnections(t *testing.T) {
	values := databaseAppValues(t)
	databaseAppSeparateConnections(values)
	componentSelectionSet(values, false, "web", "gateway", "enabled")
	componentSelectionSet(values, true, "workers", "enabled")
	componentSelectionSet(values, "mono", "workers", "type")
	componentSelectionSet(values, "hub", "workers", "namespace")
	componentSelectionSet(values, []any{map[string]any{"name": "acme", "namespace": "acme-workers"}}, "workers", "companies")
	objects := renderComponentSelection(t, "databases", "databases", values)
	namespaces := map[string]bool{}
	configs := 0
	for _, object := range objects {
		if object["kind"] == "ConfigMap" {
			if config, ok := componentSelectionMap(object, "data")["config.yaml"].(string); ok {
				var data map[string]any
				if err := yaml.Unmarshal([]byte(config), &data); err != nil {
					t.Fatal(err)
				}
				if name := componentSelectionMap(data, "db", "landlord")["name"]; name != "landlord_shared" {
					t.Errorf("Monocore landlord reset schema = %v", name)
				}
				configs++
			}
		}
		if object["kind"] != "Deployment" {
			continue
		}
		env := databaseAppEnvironment(t, objects, object)
		assertDatabaseAppEnv(t, env, map[string]string{
			"DB_LANDLORD_HOST": "landlord.example.test", "DB_LANDLORD_PORT": "3307", "DB_LANDLORD_DATABASE": "landlord_shared",
			"DB_LANDLORD_USERNAME": "landlord_user", "DB_LANDLORD_PASSWORD": "landlord-inline-test-password",
			"DB_TENANT_HOST": "tenant-default.example.test", "DB_TENANT_PORT": "3308", "DB_TENANT_USERNAME": "default_user",
			"DB_TENANT_PASSWORD":  "secret:tenant-default/custom-default-password",
			"DB_TENANT_blue_HOST": "tenant-blue.example.test", "DB_TENANT_blue_PORT": "3306", "DB_TENANT_blue_USERNAME": "blue_user",
			"DB_TENANT_blue_PASSWORD": "blue-inline-test-password",
			"DB_TENANT_Green_2_HOST":  "tenant-green.example.test", "DB_TENANT_Green_2_PORT": "3310", "DB_TENANT_Green_2_USERNAME": "green_user",
			"DB_TENANT_Green_2_PASSWORD": "secret:tenant-green/custom-green-password",
			"DB_FABRIC_HOST":             "fabric-db.example.test", "DB_FABRIC_PASSWORD": "secret:external-fabric-db/password",
		})
		for _, id := range []string{"", "blue", "Green_2"} {
			prefix, suffix := "DB_TENANT", ""
			if id != "" {
				prefix += "_" + id
				suffix = "_" + id
			}
			for _, field := range []string{"HOST", "PORT", "USERNAME", "PASSWORD"} {
				assertDatabaseAppEnv(t, env, map[string]string{"TENANT_DB_" + field + suffix: env[prefix+"_"+field]})
			}
		}
		container := componentSelectionMap(object, "spec", "template", "spec")["containers"].([]any)[0].(map[string]any)
		positions := map[string]int{}
		for index, item := range container["env"].([]any) {
			positions[fmt.Sprint(item.(map[string]any)["name"])] = index
		}
		for _, prefix := range []string{"DB_LANDLORD", "DB_FABRIC"} {
			wantDSN := fmt.Sprintf("%s:$(%s_PASSWORD)@tcp(%s:%s)/%s?parseTime=true", env[prefix+"_USERNAME"], prefix, env[prefix+"_HOST"], env[prefix+"_PORT"], env[prefix+"_DATABASE"])
			assertDatabaseAppEnv(t, env, map[string]string{prefix + "_DSN": wantDSN})
			passwordIndex, hasPassword := positions[prefix+"_PASSWORD"]
			dsnIndex, hasDSN := positions[prefix+"_DSN"]
			if !hasPassword || !hasDSN || passwordIndex >= dsnIndex {
				t.Errorf("%s_PASSWORD must precede %s_DSN for Kubernetes substitution", prefix, prefix)
			}
		}
		for _, obsolete := range []string{"DB_DSN", "DB_TENANT_DSN"} {
			if _, ok := env[obsolete]; ok {
				t.Errorf("%s would override the component configuration", obsolete)
			}
		}
		namespaces[fmt.Sprint(componentSelectionMap(object, "metadata")["namespace"])] = true
	}
	if len(namespaces) != 2 || !namespaces["hub"] || !namespaces["acme-workers"] || configs != 2 {
		t.Fatalf("expected hub and company connection/config coverage, got namespaces=%v configs=%d", namespaces, configs)
	}
}

func TestDatabaseAppInitialTenantCreationUsesTenantCredentials(t *testing.T) {
	values := databaseAppValues(t)
	databaseAppSeparateConnections(values)
	delete(componentSelectionMap(values, "database", "tenant"), "primaryServerId")
	componentSelectionSet(values, true, "seeds", "core", "enabled")
	componentSelectionSet(values, true, "seeds", "fabric", "enabled")
	componentSelectionSet(values, "Acme", "seeds", "tenant", "companyName")
	componentSelectionSet(values, "tenant_acme", "seeds", "tenant", "database")
	componentSelectionSet(values, "Administrator", "seeds", "tenant", "adminName")
	componentSelectionSet(values, "admin@example.test", "seeds", "tenant", "adminEmail")
	componentSelectionSet(values, "tenant-admin", "seeds", "tenant", "existingSecret", "name")
	objects := renderComponentSelection(t, "databases", "databases", values)
	for _, object := range objects {
		if object["kind"] != "Job" || componentSelectionMap(object, "metadata", "labels")["app.kubernetes.io/component"] != "tenant-database" {
			continue
		}
		assertDatabaseAppEnv(t, databaseAppEnvironment(t, objects, object), map[string]string{
			"MYSQL_USER": "default_user", "MYSQL_PASSWORD": "secret:tenant-default/custom-default-password",
		})
		spec := componentSelectionMap(object, "spec", "template", "spec")
		for _, kind := range []string{"containers", "initContainers"} {
			container := spec[kind].([]any)[0].(map[string]any)
			command := fmt.Sprint(container["command"])
			if !strings.Contains(command, "tenant-default.example.test") || !strings.Contains(command, "3308") || strings.Contains(command, "landlord.example.test") {
				t.Errorf("%s targets wrong tenant endpoint: %s", kind, command)
			}
			if kind == "containers" && (!strings.Contains(command, "CREATE DATABASE IF NOT EXISTS") || !strings.Contains(command, "tenant_acme")) {
				t.Errorf("tenant schema creation missing: %s", command)
			}
		}
		// Helm cannot resolve a Fabric server ID to its credential ID. Let Core
		// provision on the assigned server instead of creating a stray schema
		// on the default tenant server.
		componentSelectionSet(values, 17, "database", "tenant", "primaryServerId")
		assignedObjects := renderComponentSelection(t, "databases", "databases", values)
		for _, assignedObject := range assignedObjects {
			if assignedObject["kind"] == "Job" && componentSelectionMap(assignedObject, "metadata", "labels")["app.kubernetes.io/component"] == "tenant-database" {
				t.Error("initial tenant database Job must be skipped for a primaryServerId assignment")
			}
		}
		return
	}
	t.Fatal("initial tenant database Job was not rendered")
}

func TestDatabaseAppPasswordOverridePrecedence(t *testing.T) {
	for _, source := range []string{"external-secret", "managed-secret", "generated-secret", "inline"} {
		for _, override := range []string{"inherit", "inline", "empty", "secret"} {
			t.Run(source+"/"+override, func(t *testing.T) {
				values := databaseAppValues(t)
				inherited := "secret:external-core-db/password"
				if source != "external-secret" {
					componentSelectionSet(values, true, "mysql", "enabled")
					componentSelectionSet(values, "source-inline-test-password", "mysql", "auth", "password")
					inherited = "source-inline-test-password"
				}
				if source == "managed-secret" {
					componentSelectionSet(values, map[string]any{"name": "managed-db", "passwordKey": "db-password"}, "mysql", "auth", "existingSecret")
					inherited = "secret:managed-db/db-password"
				}
				if source == "generated-secret" {
					componentSelectionSet(values, true, "credentials", "autoGenerate")
					componentSelectionSet(values, "", "mysql", "auth", "password")
					inherited = "secret:databases-mysql-auth/password"
				}
				want := inherited
				switch override {
				case "inline", "empty":
					want = "override-inline-test-password"
					if override == "empty" {
						want = ""
					}
					componentSelectionSet(values, want, "database", "landlord", "password")
				case "secret":
					componentSelectionSet(values, "ignored-inline-password", "database", "landlord", "password")
					componentSelectionSet(values, map[string]any{"name": "landlord-password", "passwordKey": "custom-key"}, "database", "landlord", "existingSecret")
					want = "secret:landlord-password/custom-key"
				}
				objects := renderComponentSelection(t, "databases", "databases", values)
				for _, object := range objects {
					if object["kind"] == "Deployment" {
						assertDatabaseAppEnv(t, databaseAppEnvironment(t, objects, object), map[string]string{
							"DB_PASSWORD": want, "LANDLORD_DB_PASSWORD": want, "TENANT_DB_PASSWORD": inherited,
						})
						return
					}
				}
				t.Fatal("gateway Deployment was not rendered")
			})
		}
	}
}

func TestDatabaseAppRejectsInvalidServerConfiguration(t *testing.T) {
	for _, invalid := range []string{"credential-id", "host", "username", "password", "zero-password"} {
		t.Run(invalid, func(t *testing.T) {
			values := databaseAppValues(t)
			server := map[string]any{"host": "tenant.example.test", "username": "tenant", "password": "test-password"}
			id := "blue"
			switch invalid {
			case "credential-id":
				id = "blue-invalid"
			case "zero-password":
				server["password"] = "0"
			default:
				delete(server, invalid)
			}
			componentSelectionSet(values, map[string]any{id: server}, "database", "tenant", "servers")
			chart, err := helmloader.Load(filepath.Join("..", "..", "charts", "patchworks-app"))
			if err != nil {
				t.Fatal(err)
			}
			renderValues, err := chartutil.ToRenderValues(chart, values, common.ReleaseOptions{Name: "databases", Namespace: "databases", Revision: 1, IsInstall: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = engine.Render(chart, renderValues)
			if err == nil || !strings.Contains(err.Error(), "database.tenant.servers") {
				t.Fatalf("expected an actionable database.tenant.servers validation error, got %v", err)
			}
		})
	}
}

func TestDatabaseAppRejectsInvalidPoolSize(t *testing.T) {
	values := databaseAppValues(t)
	componentSelectionSet(values, map[string]any{"maxSizePerServer": -1}, "database", "tenant", "pool")
	chart, err := helmloader.Load(filepath.Join("..", "..", "charts", "patchworks-app"))
	if err != nil {
		t.Fatal(err)
	}
	renderValues, err := chartutil.ToRenderValues(chart, values, common.ReleaseOptions{Name: "databases", Namespace: "databases", Revision: 1, IsInstall: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Render(chart, renderValues)
	if err == nil || !strings.Contains(err.Error(), "database.tenant.pool.maxSizePerServer") {
		t.Fatalf("expected an actionable database.tenant.pool.maxSizePerServer validation error, got %v", err)
	}
}
