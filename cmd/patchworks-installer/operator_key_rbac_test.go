package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestMonoOperatorKeyRBACAndEnvironment(t *testing.T) {
	values := componentSelectionValues(t)
	componentSelectionSet(values, true, "workers", "enabled")
	componentSelectionSet(values, "mono", "workers", "type")
	componentSelectionSet(values, true, "workers", "hub", "enabled")
	componentSelectionSet(values, "disabled", "workers", "mono", "scheduler", "mode")
	componentSelectionSet(values, true, "workers", "mono", "operatorKey", "enabled")
	componentSelectionSet(values, "copy-key", "workers", "mono", "operatorKey", "secretName")

	objects := renderComponentSelection(t, "operator-key", "release-ns", values)
	var role, binding, deployment map[string]any
	for _, object := range objects {
		name := fmt.Sprint(componentSelectionMap(object, "metadata")["name"])
		switch {
		case object["kind"] == "Role" && strings.HasSuffix(name, "-workers-operator-key"):
			role = object
		case object["kind"] == "RoleBinding" && strings.HasSuffix(name, "-workers-operator-key"):
			binding = object
		case object["kind"] == "Deployment" && strings.HasSuffix(name, "-workers"):
			deployment = object
		}
	}
	if role == nil || binding == nil || deployment == nil {
		t.Fatal("operator-key Role, RoleBinding or hub Deployment not rendered")
	}
	rules := role["rules"].([]any)
	if len(rules) != 2 {
		t.Fatalf("Role rules = %d, want 2", len(rules))
	}
	getRule := rules[0].(map[string]any)
	if fmt.Sprint(getRule["resourceNames"]) != "[copy-key]" || fmt.Sprint(getRule["verbs"]) != "[get]" {
		t.Fatalf("named get rule = %#v", getRule)
	}
	createRule := rules[1].(map[string]any)
	if _, exists := createRule["resourceNames"]; exists || fmt.Sprint(createRule["verbs"]) != "[create]" {
		t.Fatalf("create rule = %#v", createRule)
	}

	pod := componentSelectionMap(componentSelectionMap(componentSelectionMap(deployment, "spec"), "template"), "spec")
	if pod["automountServiceAccountToken"] != true {
		t.Fatalf("automountServiceAccountToken = %#v", pod["automountServiceAccountToken"])
	}
	containers := pod["containers"].([]any)
	env := map[string]string{}
	for _, raw := range containers[0].(map[string]any)["env"].([]any) {
		entry := raw.(map[string]any)
		env[fmt.Sprint(entry["name"])] = fmt.Sprint(entry["value"])
	}
	if env["API_KEY_SECRET"] != "copy-key" || !strings.HasSuffix(env["FABRIC_API_URL"], "/api/v2") || !strings.HasSuffix(env["CORE_API_URL"], "/api/v1/patchworks") {
		t.Fatalf("platform API env = %#v", env)
	}
}

func TestMonoOperatorKeyRBACDefaultOff(t *testing.T) {
	values := componentSelectionValues(t)
	componentSelectionSet(values, true, "workers", "enabled")
	componentSelectionSet(values, "mono", "workers", "type")
	componentSelectionSet(values, true, "workers", "hub", "enabled")
	for _, object := range renderComponentSelection(t, "operator-key-off", "release-ns", values) {
		if strings.Contains(fmt.Sprint(componentSelectionMap(object, "metadata")["name"]), "operator-key") {
			t.Fatalf("operator-key resource rendered by default: %#v", object)
		}
	}
}
