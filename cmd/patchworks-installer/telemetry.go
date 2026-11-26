package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	helmchart "helm.sh/helm/v4/pkg/chart"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// lastCoreWithoutTelemetry is the last Patchworks release whose Core image has
// no platform telemetry: no telemetry:ship or telemetry:migrate command, so the
// shipper would crash and the Monitoring page would have nothing to read.
const lastCoreWithoutTelemetry = "0.2.20"

// telemetryDescription is the Monitoring question's description.
const telemetryDescription = "Record platform telemetry for the dashboard's Monitoring page (Process flows → Monitoring): requests, flow runs, usage, limits and forecasts. Adds ClickHouse to the bundled infrastructure and a telemetry shipper to Core, about 640Mi of memory and a 10Gi volume. Needs a Patchworks release later than v" + lastCoreWithoutTelemetry + "."

// checkTelemetry records why the selected release and answers cannot turn on
// platform telemetry, if they cannot.
func checkTelemetry(values map[string]string, appChart, infraChart helmchart.Charter) {
	if values["telemetry"] != "true" {
		return
	}
	values["telemetryUnsupported"] = telemetrySupport(appChart, infraChart, values)
	values["telemetryChecked"] = "true"
}

// telemetrySupport says why the selected charts and answers cannot record
// platform telemetry, or "" when they can.
func telemetrySupport(appChart, infraChart helmchart.Charter, values map[string]string) string {
	switch {
	case values["infrastructure"] == "external":
		return "the installer only sets up the bundled ClickHouse; set clickhouse.external.* and telemetry.enabled in the values file"
	case lightweightEnabled(values):
		return "the lightweight profile has no memory to spare for ClickHouse"
	}
	appValues := chartValuesMap(appChart)
	if _, ok := appValues["telemetry"].(map[string]any); !ok {
		return "this chart release has no platform telemetry"
	}
	if _, ok := chartValuesMap(infraChart)["clickhouse"].(map[string]any); !ok {
		return "this chart release has no bundled ClickHouse"
	}
	image, _ := appValues["image"].(map[string]any)
	tag, _ := image["tag"].(string)
	// A tag that is not a version (a branch build) cannot be compared.
	if version, err := semver.NewVersion(strings.TrimPrefix(tag, "v")); err == nil &&
		!version.GreaterThan(semver.MustParse(lastCoreWithoutTelemetry)) {
		return fmt.Sprintf("Core %s predates platform telemetry", tag)
	}
	return ""
}

// telemetryNeedsInfra is whether this run turns on platform telemetry for an
// installation with no ClickHouse yet. Every Core pod then reads the ClickHouse
// logins the infra chart generates, so the infra chart must be upgraded even
// when the user would otherwise leave it unchanged.
func telemetryNeedsInfra(values map[string]string, clickhouseDeployed bool) bool {
	return telemetryEnabled(values) && !clickhouseDeployed
}

// clickhouseDeployed is whether the bundled ClickHouse already runs in the
// namespace. An unreachable cluster counts as not deployed.
func clickhouseDeployed(access kubeAccess, namespace string) bool {
	client, err := kubernetesClient(access)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = client.AppsV1().Deployments(namespace).Get(ctx, "patchworks-clickhouse", metav1.GetOptions{})
	return err == nil
}

// chartValuesMap is a chart's default values, or nil when it has none.
func chartValuesMap(chart helmchart.Charter) map[string]any {
	if chart == nil {
		return nil
	}
	accessor, err := helmchart.NewAccessor(chart)
	if err != nil {
		return nil
	}
	return accessor.Values()
}

// telemetryEnabled is whether the values file turns on platform telemetry:
// the user asked for it and the selected release supports it.
func telemetryEnabled(values map[string]string) bool {
	return values["telemetry"] == "true" && values["telemetryChecked"] == "true" && values["telemetryUnsupported"] == ""
}

// writeTelemetryValues deploys the bundled ClickHouse and turns on Core's
// telemetry shipper, which together feed the Monitoring page.
func writeTelemetryValues(b io.Writer, values map[string]string) {
	if !telemetryEnabled(values) {
		return
	}
	fmt.Fprintf(b, "clickhouse:\n  enabled: true\n\n")
	fmt.Fprintf(b, "telemetry:\n  enabled: true\n\n")
}

func telemetrySummary(values map[string]string) string {
	switch {
	case values["telemetry"] != "true":
		return "off"
	case values["telemetryUnsupported"] != "":
		return "off: " + values["telemetryUnsupported"]
	case !telemetryEnabled(values):
		return "off"
	default:
		return "on: bundled ClickHouse and the telemetry shipper, for the Monitoring page"
	}
}

// optionalBool is a saved yes/no answer, or nil when the run never asked it.
func optionalBool(values map[string]string, key string) *bool {
	value, ok := values[key]
	if !ok {
		return nil
	}
	return boolPtr(value == "true")
}
