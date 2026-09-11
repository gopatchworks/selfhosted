package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func migrationSyncWave(t *testing.T, object map[string]any) int {
	t.Helper()
	annotations := componentSelectionMap(object, "metadata", "annotations")
	if value, ok := annotations["argocd.argoproj.io/sync-wave"]; ok {
		wave, err := strconv.Atoi(fmt.Sprint(value))
		if err != nil {
			t.Fatalf("invalid sync wave for %v: %v", componentSelectionMap(object, "metadata")["name"], err)
		}
		return wave
	}
	return 0
}

func migrationOrderValues(t *testing.T) map[string]any {
	t.Helper()
	values := componentSelectionValues(t)
	componentSelectionSet(values, true, "web", "gateway", "enabled")
	componentSelectionSet(values, true, "fabric", "enabled")
	componentSelectionSet(values, true, "fabric", "migrations", "enabled")
	componentSelectionSet(values, true, "migrations", "enabled")
	// Exercise chart-owned Secrets as well as BYO Secrets; these values are synthetic.
	componentSelectionSet(values, "synthetic-key-01234567890123456789", "app", "key")
	componentSelectionSet(values, "", "app", "existingSecret", "name")
	componentSelectionSet(values, "synthetic-password", "fabric", "mysql", "external", "password")
	componentSelectionSet(values, "", "fabric", "mysql", "external", "existingSecret", "name")
	componentSelectionSet(values, "retained", "serviceAccount", "annotations", "custom-annotation")
	return values
}

func TestArgoMigrationPrerequisitesPrecedeJobsAndWorkloads(t *testing.T) {
	objects := renderComponentSelection(t, "ordered", "application", migrationOrderValues(t))
	var prerequisites, migrations, workloads []map[string]any
	seenKinds := map[string]bool{}
	for _, object := range objects {
		switch object["kind"] {
		case "ConfigMap", "Secret", "ServiceAccount", "Service":
			prerequisites = append(prerequisites, object)
			seenKinds[fmt.Sprint(object["kind"])] = true
		case "Job":
			migrations = append(migrations, object)
		case "Deployment":
			workloads = append(workloads, object)
		}
	}
	if len(migrations) != 2 || len(workloads) != 2 {
		t.Fatalf("expected Fabric and Gateway migrations/workloads, got %d/%d", len(migrations), len(workloads))
	}
	for _, kind := range []string{"ConfigMap", "Secret", "ServiceAccount", "Service"} {
		if !seenKinds[kind] {
			t.Fatalf("fixture did not render prerequisite kind %s", kind)
		}
	}
	for _, dependency := range prerequisites {
		annotations := componentSelectionMap(dependency, "metadata", "annotations")
		for _, key := range []string{"argocd.argoproj.io/hook", "argocd.argoproj.io/hook-delete-policy", "helm.sh/hook"} {
			if _, exists := annotations[key]; exists {
				t.Errorf("persistent prerequisite %v has lifecycle annotation %s", componentSelectionMap(dependency, "metadata")["name"], key)
			}
		}
		if dependency["kind"] == "ServiceAccount" && annotations["custom-annotation"] != "retained" {
			t.Error("ServiceAccount custom annotation was lost")
		}
		for _, migration := range migrations {
			if migrationSyncWave(t, dependency) >= migrationSyncWave(t, migration) {
				t.Errorf("prerequisite %v does not precede %v", componentSelectionMap(dependency, "metadata")["name"], componentSelectionMap(migration, "metadata")["name"])
			}
		}
	}
	for _, migration := range migrations {
		annotations := componentSelectionMap(migration, "metadata", "annotations")
		if annotations["argocd.argoproj.io/hook"] != "Sync" {
			t.Fatal("migrations must share the Sync phase with their persistent prerequisites")
		}
		policy := fmt.Sprint(annotations["argocd.argoproj.io/hook-delete-policy"])
		if !strings.Contains(policy, "BeforeHookCreation") || !strings.Contains(policy, "HookSucceeded") {
			t.Error("migration hooks must rerun on each full sync and support failed-job replacement")
		}
		if annotations["helm.sh/hook"] != "pre-install,pre-upgrade" {
			t.Error("standalone Helm migration lifecycle changed")
		}
		for _, workload := range workloads {
			if migrationSyncWave(t, migration) >= migrationSyncWave(t, workload) {
				t.Errorf("workload %v does not follow migrations", componentSelectionMap(workload, "metadata")["name"])
			}
		}
	}
}

func TestArgoOptionalSeedsKeepTheirOrderBetweenPrerequisitesAndWorkloads(t *testing.T) {
	values := migrationOrderValues(t)
	componentSelectionSet(values, true, "seeds", "fabric", "enabled")
	componentSelectionSet(values, true, "seeds", "core", "enabled")
	componentSelectionSet(values, true, "seeds", "tenant", "createDatabase")
	componentSelectionSet(values, "synthetic_tenant", "seeds", "tenant", "database")
	componentSelectionSet(values, "external-tenant-admin", "seeds", "tenant", "existingSecret", "name")
	objects := renderComponentSelection(t, "ordered", "application", values)
	jobs := map[string]map[string]any{}
	for _, object := range objects {
		if object["kind"] == "Job" {
			component := fmt.Sprint(componentSelectionMap(object, "metadata", "labels")["app.kubernetes.io/component"])
			jobs[component] = object
			if componentSelectionMap(object, "metadata", "annotations")["argocd.argoproj.io/hook"] != "Sync" {
				t.Errorf("seed/migration %s is not in the Sync phase", component)
			}
		}
	}
	order := []string{"fabric-migrations", "fabric-seeds", "fabric-company-seed", "tenant-database", "core-migrations", "core-seeds"}
	previous := -30
	for _, component := range order {
		job, exists := jobs[component]
		if !exists {
			t.Fatalf("missing job %s", component)
		}
		wave := migrationSyncWave(t, job)
		if wave <= previous || wave >= 0 {
			t.Errorf("%s wave %d is outside prerequisite/job/workload order", component, wave)
		}
		previous = wave
	}
}
