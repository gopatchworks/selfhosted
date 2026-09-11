package main

import (
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

func renderDatabaseInfra(values map[string]any) (map[string]string, error) {
	chart, err := helmloader.Load(filepath.Join("..", "..", "charts", "patchworks-infra"))
	if err != nil {
		return nil, err
	}
	renderValues, err := chartutil.ToRenderValues(chart, values, common.ReleaseOptions{
		Name: "databases", Namespace: "databases", Revision: 2, IsUpgrade: true,
	}, nil)
	if err != nil {
		return nil, err
	}
	return engine.Render(chart, renderValues)
}

func databaseInfraSetup(t *testing.T, values map[string]any) (map[string]any, string) {
	t.Helper()
	files, err := renderDatabaseInfra(values)
	if err != nil {
		t.Fatal(err)
	}
	rendered := files["patchworks-infra/templates/mysql-setup.yaml"]
	var object map[string]any
	if err := yaml.Unmarshal([]byte(rendered), &object); err != nil {
		t.Fatalf("invalid MySQL setup YAML: %v", err)
	}
	if object["kind"] != "Job" {
		t.Fatalf("expected setup Job, got %v", object["kind"])
	}
	spec := componentSelectionMap(object, "spec", "template", "spec")
	containers := spec["containers"].([]any)
	container := containers[0].(map[string]any)
	command := container["command"].([]any)
	if command[0] != "sh" || !strings.Contains(command[1].(string), "e") {
		t.Fatalf("setup must fail when SQL fails: %v", command)
	}
	return container, command[2].(string)
}

func TestManagedMySQLMultipleDatabases(t *testing.T) {
	values := map[string]any{}
	componentSelectionSet(values, true, "fabric", "mysql", "enabled")
	componentSelectionSet(values, []any{"landlord", "tenant_acme", "tenant_bravo", "tenant_acme"}, "mysql", "databases")
	_, script := databaseInfraSetup(t, values)
	for _, database := range []string{"core", "landlord", "tenant_acme", "tenant_bravo"} {
		create := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`;", database)
		if strings.Count(script, create) != 1 {
			t.Fatalf("expected exactly one %q in setup SQL:\n%s", create, script)
		}
		grant := fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.*", strings.ReplaceAll(database, "_", `\_`))
		if !strings.Contains(script, grant) {
			t.Fatalf("missing exact-schema grant %q:\n%s", grant, script)
		}
	}
	if strings.Contains(script, "CREATE DATABASE IF NOT EXISTS `fabric`;") {
		t.Fatalf("dedicated Fabric must not be provisioned on main MySQL:\n%s", script)
	}
}

func TestManagedMySQLSharedFabricStillProvisioned(t *testing.T) {
	_, script := databaseInfraSetup(t, nil)
	if !strings.Contains(script, "CREATE DATABASE IF NOT EXISTS `fabric`;") {
		t.Fatalf("shared Fabric database was not provisioned:\n%s", script)
	}
}

func TestManagedMySQLProvisioningBoundaries(t *testing.T) {
	for _, test := range []struct {
		name   string
		values map[string]any
	}{
		{
			name: "external mysql is never provisioned",
			values: map[string]any{
				"mysql":  map[string]any{"enabled": false, "databases": []any{"landlord", "tenant_acme"}},
				"fabric": map[string]any{"mysql": map[string]any{"enabled": true}},
			},
		},
		{
			name: "dedicated fabric with no additional schemas needs no setup",
			values: map[string]any{
				"fabric": map[string]any{"mysql": map[string]any{"enabled": true}},
			},
		},
		{
			name: "external fabric with no additional schemas needs no setup",
			values: map[string]any{
				"fabric": map[string]any{"mysql": map[string]any{"external": map[string]any{"host": "fabric.example.com"}}},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, err := renderDatabaseInfra(test.values)
			if err != nil {
				t.Fatal(err)
			}
			if rendered := strings.TrimSpace(files["patchworks-infra/templates/mysql-setup.yaml"]); rendered != "" {
				t.Fatalf("unexpected setup job:\n%s", rendered)
			}
		})
	}
}

func TestManagedMySQLSetupRootSecretAndUsernameEscaping(t *testing.T) {
	values := map[string]any{}
	componentSelectionSet(values, "app'user\\name", "mysql", "auth", "username")
	componentSelectionSet(values, "root-credentials", "mysql", "auth", "existingSecret", "name")
	componentSelectionSet(values, "administrator", "mysql", "auth", "existingSecret", "rootPasswordKey")
	container, script := databaseInfraSetup(t, values)
	if !strings.Contains(script, "SET SESSION sql_mode = 'NO_BACKSLASH_ESCAPES';") ||
		!strings.Contains(script, "TO 'app''user\\name'@'%';") {
		t.Fatalf("username not safely quoted:\n%s", script)
	}
	env := container["env"].([]any)
	root := env[0].(map[string]any)
	secret := componentSelectionMap(root, "valueFrom", "secretKeyRef")
	if root["name"] != "MYSQL_ROOT_PASSWORD" || secret["name"] != "root-credentials" || secret["key"] != "administrator" {
		t.Fatalf("root credentials did not retain existingSecret mapping: %v", root)
	}
}

func TestManagedMySQLSetupRejectsInvalidNames(t *testing.T) {
	for _, test := range []struct {
		name   string
		value  any
		path   []string
		needle string
	}{
		{"not a list", "tenant_acme", []string{"mysql", "databases"}, "must be a list"},
		{"not a string", []any{123}, []string{"mysql", "databases"}, "must be strings"},
		{"empty name", []any{""}, []string{"mysql", "databases"}, "must contain 1-64"},
		{"overlong name", []any{strings.Repeat("a", 65)}, []string{"mysql", "databases"}, "must contain 1-64"},
		{"SQL injection", []any{"tenant`; DROP DATABASE fabric; --"}, []string{"mysql", "databases"}, "must contain 1-64"},
		{"shell heredoc injection", []any{"tenant\nSQL\nid\nSQL"}, []string{"mysql", "databases"}, "must contain 1-64"},
		{"username heredoc injection", "user\nSQL\nid\nSQL", []string{"mysql", "auth", "username"}, "must not contain NUL or line breaks"},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]any{}
			componentSelectionSet(values, test.value, test.path...)
			_, err := renderDatabaseInfra(values)
			if err == nil || !strings.Contains(err.Error(), test.needle) {
				t.Fatalf("expected validation error containing %q, got %v", test.needle, err)
			}
		})
	}
}
