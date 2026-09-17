package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

// Monocore discovers database groups from Fabric at runtime and keeps one
// runtime ConfigMap per group, named "<prefix>-runtime-<group>". Those names
// cannot be enumerated at template time and Kubernetes RBAC has no resource
// name prefix match, so the runtime grants must stay namespace scoped.
func TestMonoSchedulerRBAC(t *testing.T) {
	values := componentSelectionValues(t)
	componentSelectionSet(values, true, "workers", "enabled")
	componentSelectionSet(values, "mono", "workers", "type")
	componentSelectionSet(values, true, "workers", "hub", "enabled")
	componentSelectionSet(values, "kubernetes", "workers", "mono", "scheduler", "mode")

	objects := renderComponentSelection(t, "scheduler-rbac", "release-ns", values)
	const prefix = "patchworks-scheduler"

	var role, desired map[string]any
	for _, object := range objects {
		name := fmt.Sprint(componentSelectionMap(object, "metadata")["name"])
		switch {
		case object["kind"] == "Role" && name == prefix:
			role = object
		case object["kind"] == "ConfigMap" && name == prefix+"-desired":
			desired = object
		}
	}
	if role == nil || desired == nil {
		t.Fatal("scheduler Role or desired ConfigMap not rendered")
	}

	// The grouped scheduler rejects any desired document below version 3.
	var document struct {
		Version int `json:"version"`
	}
	raw := fmt.Sprint(componentSelectionMap(desired, "data")["desired.json"])
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatalf("desired.json is not valid JSON: %v", err)
	}
	if document.Version != 3 {
		t.Errorf("desired.json version = %d, want 3", document.Version)
	}

	rules, _ := role["rules"].([]any)
	type grant struct{ verbs, names map[string]bool }
	configMaps := []grant{}
	leases, pods := map[string]bool{}, map[string]bool{}
	for _, raw := range rules {
		rule := raw.(map[string]any)
		verbs := map[string]bool{}
		for _, verb := range rule["verbs"].([]any) {
			verbs[fmt.Sprint(verb)] = true
		}
		names := map[string]bool{}
		resourceNames, _ := rule["resourceNames"].([]any)
		for _, name := range resourceNames {
			names[fmt.Sprint(name)] = true
		}
		for _, resource := range rule["resources"].([]any) {
			switch fmt.Sprint(resource) {
			case "configmaps":
				configMaps = append(configMaps, grant{verbs, names})
			case "leases":
				for verb := range verbs {
					leases[verb] = true
				}
			case "pods":
				for verb := range verbs {
					pods[verb] = true
				}
			}
		}
	}

	for _, verb := range []string{"get", "create", "update"} {
		if !leases[verb] {
			t.Errorf("Lease grant is missing %q", verb)
		}
	}
	for _, verb := range []string{"get", "list", "watch"} {
		if !pods[verb] {
			t.Errorf("pod grant is missing %q", verb)
		}
	}

	// Per-group runtime ConfigMaps are reachable only through unrestricted rules.
	unrestricted := map[string]bool{}
	for _, grant := range configMaps {
		if len(grant.names) > 0 {
			// A named rule may only pin the Helm-owned desired ConfigMap.
			if !grant.names[prefix+"-desired"] || len(grant.names) != 1 {
				t.Errorf("named ConfigMap rule pins %v; per-group runtime names are not knowable at template time", grant.names)
			}
			continue
		}
		for verb := range grant.verbs {
			unrestricted[verb] = true
		}
	}
	for _, verb := range []string{"get", "update", "create"} {
		if !unrestricted[verb] {
			t.Errorf("ConfigMap grant is missing unrestricted %q, so <prefix>-runtime-<group> access returns HTTP 403", verb)
		}
	}
}
