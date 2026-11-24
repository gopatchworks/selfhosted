package main

import (
	"testing"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/kube"
	rcommon "helm.sh/helm/v4/pkg/release/common"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
)

// The Helm CLI applies as "helm". Applying as anything else makes the
// installer and the printed `helm upgrade` commands conflict over every field
// the other last changed.
func TestInstallerAppliesAsTheHelmCLIFieldManager(t *testing.T) {
	if kube.ManagedFieldsManager != "helm" {
		t.Fatalf("kube.ManagedFieldsManager = %q, want %q", kube.ManagedFieldsManager, "helm")
	}
}

func storeTestRelease(t *testing.T, cfg *action.Configuration, name string, version int, status rcommon.Status, applyMethod string) {
	t.Helper()
	rel := &releasev1.Release{
		Name:        name,
		Namespace:   "default",
		Version:     version,
		Info:        &releasev1.Info{Status: status},
		ApplyMethod: applyMethod,
	}
	if err := cfg.Releases.Create(rel); err != nil {
		t.Fatalf("store release %s v%d: %v", name, version, err)
	}
}

// Helm rejects a forced client-side apply, so only releases Helm upgrades
// server-side force conflicts. Helm reads the apply method from the last
// revision, even a failed one.
func TestHelmUpgradeForcesConflictsOnlyForServerSideReleases(t *testing.T) {
	var delay time.Duration
	cfg := testHelmConfig(&delay)
	storeTestRelease(t, cfg, "ssa", 1, rcommon.StatusDeployed, "ssa")
	storeTestRelease(t, cfg, "csa", 1, rcommon.StatusDeployed, "csa")
	storeTestRelease(t, cfg, "helm3", 1, rcommon.StatusDeployed, "")
	storeTestRelease(t, cfg, "failed-ssa", 1, rcommon.StatusSuperseded, "csa")
	storeTestRelease(t, cfg, "failed-ssa", 2, rcommon.StatusFailed, "ssa")

	for name, want := range map[string]bool{
		"ssa":        true,
		"csa":        false,
		"helm3":      false,
		"failed-ssa": true,
		"missing":    false,
	} {
		upgrade := newHelmUpgrade(cfg, name, "default", time.Minute, nil)
		if upgrade.ForceConflicts != want {
			t.Errorf("%s: ForceConflicts = %v, want %v", name, upgrade.ForceConflicts, want)
		}
		if upgrade.ServerSideApply != "auto" {
			t.Errorf("%s: ServerSideApply = %q, want Helm's default \"auto\"", name, upgrade.ServerSideApply)
		}
	}
}
