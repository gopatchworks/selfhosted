package main

import (
	"path/filepath"
	"strings"
	"testing"

	helmloader "helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
)

func TestWriteValuesRunsEveryCompanyOnMonocore(t *testing.T) {
	values := withValue(monoInboundValues("false", "false"), "monoAllCompanies", "true")
	rendered, parsed := writeMonoInboundValues(t, values)
	if !strings.Contains(rendered, "workers:\n  type: \"mono\"\n  mono:\n    allCompanies:\n      enabled: true\n\n") {
		t.Fatalf("allCompanies block missing:\n%s", rendered)
	}
	mono := parsed["workers"].(map[string]any)["mono"].(map[string]any)
	if _, ok := mono["inbound"]; ok {
		t.Fatalf("inbound written while off: %#v", mono)
	}
}

func TestWriteValuesWritesInboundAndAllCompaniesUnderOneMonoKey(t *testing.T) {
	values := withValue(monoInboundValues("true", "true"), "monoAllCompanies", "true")
	rendered, parsed := writeMonoInboundValues(t, values)
	if strings.Count(rendered, "  mono:\n") != 1 {
		t.Fatalf("expected one workers.mono key:\n%s", rendered)
	}
	mono := parsed["workers"].(map[string]any)["mono"].(map[string]any)
	if mono["allCompanies"].(map[string]any)["enabled"] != true {
		t.Fatalf("allCompanies not enabled: %#v", mono)
	}
	if mono["inbound"].(map[string]any)["enabled"] != true {
		t.Fatalf("inbound not enabled: %#v", mono)
	}
}

func TestWriteValuesOmitsAllCompaniesUnlessCheckedAndMono(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
	}{
		{"support not checked", monoInboundValues("false", "false")},
		{"unsupported release", withValue(withValue(monoInboundValues("false", "false"), "monoAllCompanies", "false"), "monoAllCompaniesUnsupported", "Core v0.2.16 predates it")},
		{"standalone workers", withValue(withValue(monoInboundValues("false", "false"), "monoAllCompanies", "true"), "workerMode", "standalone")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rendered, _ := writeMonoInboundValues(t, tc.values)
			if strings.Contains(rendered, "allCompanies") {
				t.Fatalf("allCompanies written:\n%s", rendered)
			}
		})
	}
}

func TestMonoAllCompaniesSummary(t *testing.T) {
	base := func() map[string]string { return monoInboundValues("false", "false") }
	for _, tc := range []struct {
		name   string
		values map[string]string
		want   string
	}{
		{"on", withValue(base(), "monoAllCompanies", "true"), "on: Core turns Monocore and its scheduler on for each new company"},
		{"unsupported", withValue(base(), "monoAllCompaniesUnsupported", "Core v0.2.16 predates it"), "off: Core v0.2.16 predates it; turn Monocore on for each company"},
		{"not checked", base(), "off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := monoAllCompaniesSummary(tc.values); !strings.HasPrefix(got, tc.want) {
				t.Fatalf("summary = %q, want prefix %q", got, tc.want)
			}
		})
	}
}

func TestMonoAllCompaniesUnsupportedReason(t *testing.T) {
	load := func(t *testing.T, edit func(values map[string]any)) *chartv2.Chart {
		t.Helper()
		loaded, err := helmloader.Load(filepath.Join("..", "..", "charts", "patchworks-app"))
		if err != nil {
			t.Fatal(err)
		}
		chart := loaded.(*chartv2.Chart)
		edit(chart.Values)
		return chart
	}
	tag := func(value string) func(map[string]any) {
		return func(values map[string]any) { values["image"].(map[string]any)["tag"] = value }
	}
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{"last release without the command", tag("v0.2.16"), "Core v0.2.16 predates it"},
		{"older release", tag("v0.1.118"), "Core v0.1.118 predates it"},
		{"later release", tag("v0.2.17"), ""},
		{"branch build", tag("main-a438da0"), ""},
		{"chart without the setting", func(values map[string]any) {
			tag("v0.2.17")(values)
			delete(values["workers"].(map[string]any)["mono"].(map[string]any), "allCompanies")
		}, "this chart release cannot switch it on"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := monoAllCompaniesUnsupportedReason(load(t, tc.edit)); got != tc.want {
				t.Fatalf("reason = %q, want %q", got, tc.want)
			}
		})
	}
}
