package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
	helmchart "helm.sh/helm/v4/pkg/chart"
	helmloader "helm.sh/helm/v4/pkg/chart/loader"
)

func TestLocalConfigLoadsGroupedConfig(t *testing.T) {
	data := []byte(`
kubernetes:
  kubeconfig: /tmp/kubeconfig
  context: selfhosted
  namespace: patchworks
  confirmAccess: true
application:
  domain: selfhosted.example.com
  scheme: https
  license:
    key: license-key
    serverUrl: https://license.example.com
  dashboard:
    enabled: true
    routingMode: path
  cookieDomain: selfhosted.example.com
workers:
  type: microservice
infrastructure:
  mode: bundled
  mysql:
    enabled: false
  redis:
    enabled: false
  rabbitmq:
    enabled: false
  elasticsearch:
    enabled: false
  s3:
    enabled: false
  pusher:
    enabled: false
ingress:
  enabled: true
  provider: contour
  className: contour
registry:
  pullSecret:
    mode: create
    name: quay-credentials
  quay:
    username: patchworks+robot
    password: secret
    email: unused@example.com
seed:
  enabled: true
  companyName: Self-Hosted
  admin:
    name: Admin User
    email: admin@example.com
    password: admin-password
    role: superadmin
output:
  valuesFile: patchworks.values.yaml
installer:
  enabled: true
`)

	var config localConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("unmarshal grouped config: %v", err)
	}

	if config.Kubeconfig != "/tmp/kubeconfig" {
		t.Fatalf("kubeconfig = %q", config.Kubeconfig)
	}
	if config.RoutingMode != "path" {
		t.Fatalf("routing mode = %q", config.RoutingMode)
	}
	if config.QuayUsername != "patchworks+robot" {
		t.Fatalf("quay username = %q", config.QuayUsername)
	}
	if config.LicenseKey != "license-key" {
		t.Fatalf("license key was not loaded")
	}
	if config.LicenseServerURL != "https://license.example.com" {
		t.Fatalf("license server url = %q", config.LicenseServerURL)
	}
	if config.WorkerMode != "microservice" {
		t.Fatalf("worker mode = %q", config.WorkerMode)
	}
	if config.Infrastructure != "bundled" {
		t.Fatalf("infrastructure mode = %q", config.Infrastructure)
	}
	if config.InfraComponents.MySQL.Enabled == nil || *config.InfraComponents.MySQL.Enabled {
		t.Fatalf("infrastructure mysql enabled was not loaded as false")
	}
	if config.AdminPassword != "admin-password" {
		t.Fatalf("admin password was not loaded")
	}
	if config.Install == nil || !*config.Install {
		t.Fatalf("install flag was not loaded")
	}
}

func TestAllInfrastructureComponentsDisabled(t *testing.T) {
	values := map[string]string{
		"infraMysqlEnabled":         "false",
		"infraRedisEnabled":         "false",
		"infraRabbitmqEnabled":      "false",
		"infraElasticsearchEnabled": "false",
		"infraS3Enabled":            "false",
		"infraPusherEnabled":        "false",
	}
	if !allInfrastructureComponentsDisabled(values) {
		t.Fatalf("expected infra to be disabled when all default-enabled components are false")
	}

	values["infraRabbitmqEnabled"] = "true"
	if allInfrastructureComponentsDisabled(values) {
		t.Fatalf("expected infra to be enabled when one component is true")
	}
}

func TestParseCLIAcceptsVersionCommand(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		opts, err := parseCLI(args)
		if err != nil {
			t.Fatalf("parseCLI(%v): %v", args, err)
		}
		if opts.Command != "version" {
			t.Fatalf("parseCLI(%v) command = %q", args, opts.Command)
		}
	}
}

func TestParseCLIAcceptsUnpackChartsCommand(t *testing.T) {
	opts, err := parseCLI([]string{"unpack-charts", "--output", "/tmp/patchworks-charts"})
	if err != nil {
		t.Fatalf("parseCLI unpack-charts: %v", err)
	}
	if opts.Command != "unpack-charts" {
		t.Fatalf("command = %q", opts.Command)
	}
	if opts.ChartsOutputDir != "/tmp/patchworks-charts" {
		t.Fatalf("charts output dir = %q", opts.ChartsOutputDir)
	}
}

func TestEmbeddedPatchworksChartsLoad(t *testing.T) {
	for _, chartName := range []string{"patchworks-infra", "patchworks-app"} {
		chartPath, cleanup, err := embeddedChartPath(chartName)
		if err != nil {
			t.Fatalf("embeddedChartPath(%q): %v", chartName, err)
		}
		defer cleanup()

		chart, err := helmloader.Load(chartPath)
		if err != nil {
			t.Fatalf("load embedded chart %q: %v", chartName, err)
		}
		accessor, err := helmchart.NewAccessor(chart)
		if err != nil {
			t.Fatalf("create accessor for embedded chart %q: %v", chartName, err)
		}
		if accessor.Name() != chartName {
			t.Fatalf("embedded chart %q metadata name = %q", chartName, accessor.Name())
		}
	}
}

func TestUnpackEmbeddedCharts(t *testing.T) {
	outputDir := t.TempDir()
	if err := unpackEmbeddedCharts(outputDir); err != nil {
		t.Fatalf("unpackEmbeddedCharts: %v", err)
	}

	for _, chartName := range []string{"patchworks-infra", "patchworks-app"} {
		chartPath := filepath.Join(outputDir, chartName)
		if _, err := os.Stat(filepath.Join(chartPath, "Chart.yaml")); err != nil {
			t.Fatalf("unpacked chart %q Chart.yaml: %v", chartName, err)
		}

		chart, err := helmloader.Load(chartPath)
		if err != nil {
			t.Fatalf("load unpacked chart %q: %v", chartName, err)
		}
		accessor, err := helmchart.NewAccessor(chart)
		if err != nil {
			t.Fatalf("create accessor for unpacked chart %q: %v", chartName, err)
		}
		if accessor.Name() != chartName {
			t.Fatalf("unpacked chart %q metadata name = %q", chartName, accessor.Name())
		}
	}
}

func TestLocalConfigStillLoadsLegacyFlatConfig(t *testing.T) {
	data := []byte(`
kubeconfig: /tmp/kubeconfig
context: selfhosted
namespace: patchworks
domain: selfhosted.example.com
scheme: https
licenseKey: legacy-license-key
licenseServerUrl: https://legacy-license.example.com
dashboardEnabled: true
routingMode: host
workerMode: mono
ingressEnabled: true
ingressProvider: contour
ingressClass: contour
cookieDomain: .selfhosted.example.com
pullSecretMode: existing
pullSecret: quay-credentials
seedInstall: true
companyName: Self-Hosted
adminName: Admin User
adminEmail: admin@example.com
adminPassword: legacy-admin-password
userRole: superadmin
valuesFile: patchworks.values.yaml
install: true
confirmKubeAccess: true
`)

	var config localConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("unmarshal legacy config: %v", err)
	}

	if config.RoutingMode != "host" {
		t.Fatalf("routing mode = %q", config.RoutingMode)
	}
	if config.WorkerMode != "mono" {
		t.Fatalf("worker mode = %q", config.WorkerMode)
	}
	if config.Install == nil || !*config.Install {
		t.Fatalf("install flag was not loaded")
	}
	if config.LicenseKey != "legacy-license-key" {
		t.Fatalf("legacy license key was not loaded")
	}
	if config.AdminPassword != "legacy-admin-password" {
		t.Fatalf("legacy admin password was not loaded")
	}
}

func TestLocalConfigSavesGroupedConfig(t *testing.T) {
	data, err := yaml.Marshal(localConfig{
		Kubeconfig:        "/tmp/kubeconfig",
		Context:           "selfhosted",
		Namespace:         "patchworks",
		Domain:            "selfhosted.example.com",
		Scheme:            "https",
		LicenseKey:        "license-key",
		LicenseServerURL:  "https://license.example.com",
		DashboardEnabled:  boolPtr(true),
		RoutingMode:       "path",
		WorkerMode:        "microservice",
		IngressEnabled:    boolPtr(true),
		IngressProvider:   "contour",
		IngressClass:      "contour",
		CookieDomain:      "selfhosted.example.com",
		PullSecretMode:    "existing",
		PullSecret:        "quay-credentials",
		SeedInstall:       boolPtr(true),
		CompanyName:       "Self-Hosted",
		AdminName:         "Admin User",
		AdminEmail:        "admin@example.com",
		AdminPassword:     "admin-password",
		UserRole:          "superadmin",
		Output:            "patchworks.values.yaml",
		Install:           boolPtr(true),
		ConfirmKubeAccess: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	rendered := string(data)
	for _, expected := range []string{
		"kubernetes:",
		"application:",
		"workers:",
		"  type: microservice",
		"license:",
		"registry:",
		"installer:",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("saved config missing %q:\n%s", expected, rendered)
		}
	}
	if strings.Contains(rendered, "dashboardEnabled") || strings.Contains(rendered, "pullSecretMode") {
		t.Fatalf("saved config used legacy flat keys:\n%s", rendered)
	}
}

func TestWriteValuesIncludesLicenseConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "patchworks.values.yaml")
	if err := writeValues(path, map[string]string{
		"namespace":        "patchworks",
		"domain":           "selfhosted.example.com",
		"scheme":           "https",
		"licenseKey":       "license-key",
		"licenseServerUrl": "https://license.example.com",
		"ingressEnabled":   "true",
		"ingressProvider":  "contour",
		"ingressClass":     "contour",
		"dashboardEnabled": "true",
		"routingMode":      "path",
		"workerMode":       "standalone",
		"cookieDomain":     "selfhosted.example.com",
		"seedInstall":      "true",
		"companyName":      "Self-Hosted",
		"adminName":        "Admin User",
		"adminEmail":       "admin@example.com",
		"adminPassword":    "admin-password",
		"userRole":         "superadmin",
	}); err != nil {
		t.Fatalf("write values: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read values: %v", err)
	}
	rendered := string(data)
	for _, expected := range []string{
		"app:",
		"  license:",
		"    key: \"license-key\"",
		"    serverUrl: \"https://license.example.com\"",
		"workers:",
		"  type: \"standalone\"",
		"    adminPassword: \"admin-password\"",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("values file missing %q:\n%s", expected, rendered)
		}
	}
}

func TestWriteValuesIncludesExternalInfrastructure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "patchworks.values.yaml")
	values := map[string]string{
		"namespace":                "patchworks",
		"infrastructure":           "external",
		"domain":                   "selfhosted.example.com",
		"scheme":                   "https",
		"licenseKey":               "license-key",
		"licenseServerUrl":         "https://license.example.com",
		"ingressEnabled":           "true",
		"ingressProvider":          "contour",
		"ingressClass":             "contour",
		"dashboardEnabled":         "true",
		"routingMode":              "host",
		"workerMode":               "standalone",
		"cookieDomain":             ".selfhosted.example.com",
		"seedInstall":              "false",
		"companyName":              "Self-Hosted",
		"adminName":                "Admin User",
		"adminEmail":               "admin@example.com",
		"userRole":                 "superadmin",
		"externalCredentialsMode":  "secret",
		"mysqlHost":                "mysql.example.com",
		"mysqlPort":                "3306",
		"mysqlDatabase":            "core",
		"mysqlUsername":            "patchworks",
		"mysqlSecretName":          "patchworks-db",
		"mysqlPasswordKey":         "password",
		"fabricMysqlHost":          "mysql.example.com",
		"fabricMysqlPort":          "3306",
		"fabricMysqlDatabase":      "fabric",
		"fabricMysqlUsername":      "patchworks",
		"fabricMysqlSecretName":    "patchworks-db",
		"fabricMysqlPasswordKey":   "password",
		"redisHost":                "redis.example.com",
		"redisPort":                "6379",
		"redisSecretName":          "patchworks-redis",
		"redisPasswordKey":         "password",
		"rabbitmqHost":             "rabbitmq.example.com",
		"rabbitmqPort":             "5672",
		"rabbitmqUsername":         "patchworks",
		"rabbitmqVhost":            "/",
		"rabbitmqSecretName":       "patchworks-rabbitmq",
		"rabbitmqPasswordKey":      "password",
		"elasticsearchHost":        "search.example.com",
		"elasticsearchPort":        "9200",
		"elasticsearchScheme":      "https",
		"elasticsearchUsername":    "elastic",
		"elasticsearchSecretName":  "patchworks-elasticsearch",
		"elasticsearchUsernameKey": "username",
		"elasticsearchPasswordKey": "password",
		"s3Endpoint":               "https://s3.example.com",
		"s3Region":                 "eu-west-2",
		"s3Bucket":                 "patchworks",
		"s3PathStyle":              "false",
		"s3SecretName":             "patchworks-s3",
		"s3AccessKeyKey":           "access-key",
		"s3SecretKeyKey":           "secret-key",
		"pusherHost":               "wss.example.com",
		"pusherPort":               "443",
		"pusherScheme":             "https",
		"pusherAppCluster":         "mt1",
		"pusherSecretName":         "patchworks-soketi-auth",
		"pusherAppIdKey":           "app-id",
		"pusherAppKeyKey":          "app-key",
		"pusherAppSecretKey":       "app-secret",
		"pusherAppClusterKey":      "app-cluster",
	}
	if err := writeValues(path, values); err != nil {
		t.Fatalf("write values: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read values: %v", err)
	}
	rendered := string(data)
	for _, expected := range []string{
		"mysql:\n  enabled: false",
		"    host: \"mysql.example.com\"",
		"fabric:\n  mysql:\n    enabled: false",
		"redis:\n  enabled: false",
		"rabbitmq:\n  enabled: false",
		"elasticsearch:\n  enabled: false",
		"s3:\n  enabled: false",
		"pusher:\n  enabled: false",
		"    host: \"wss.example.com\"",
		"    name: \"patchworks-soketi-auth\"",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("values file missing %q:\n%s", expected, rendered)
		}
	}
	if strings.Contains(rendered, "wss.selfhosted.example.com") {
		t.Fatalf("external pusher should not emit bundled websocket ingress:\n%s", rendered)
	}
}

func TestWriteValuesIncludesBundledInfrastructureOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "patchworks.values.yaml")
	values := map[string]string{
		"namespace":                 "patchworks",
		"infrastructure":            "bundled",
		"domain":                    "selfhosted.example.com",
		"scheme":                    "https",
		"licenseKey":                "license-key",
		"licenseServerUrl":          "https://license.example.com",
		"ingressEnabled":            "true",
		"ingressProvider":           "contour",
		"ingressClass":              "contour",
		"dashboardEnabled":          "true",
		"routingMode":               "host",
		"workerMode":                "standalone",
		"cookieDomain":              ".selfhosted.example.com",
		"seedInstall":               "false",
		"companyName":               "Self-Hosted",
		"adminName":                 "Admin User",
		"adminEmail":                "admin@example.com",
		"userRole":                  "superadmin",
		"infraMysqlEnabled":         "false",
		"infraRedisEnabled":         "false",
		"infraRabbitmqEnabled":      "false",
		"infraElasticsearchEnabled": "false",
		"infraS3Enabled":            "false",
		"infraPusherEnabled":        "false",
	}
	if err := writeValues(path, values); err != nil {
		t.Fatalf("write values: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read values: %v", err)
	}
	rendered := string(data)
	for _, expected := range []string{
		"mysql:\n  enabled: false",
		"redis:\n  enabled: false",
		"rabbitmq:\n  enabled: false",
		"elasticsearch:\n  enabled: false",
		"s3:\n  enabled: false",
		"pusher:\n  enabled: false",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("values file missing %q:\n%s", expected, rendered)
		}
	}
}

func TestLocalConfigFromInstallConfigDefaultsLicenseServerURL(t *testing.T) {
	config := localConfigFromInstallConfig(installConfig{
		Output: "patchworks.values.yaml",
		Values: map[string]string{
			"kubeconfig":        "/tmp/kubeconfig",
			"context":           "selfhosted",
			"namespace":         "patchworks",
			"domain":            "selfhosted.example.com",
			"scheme":            "https",
			"licenseKey":        "license-key",
			"dashboardEnabled":  "true",
			"routingMode":       "path",
			"workerMode":        "",
			"ingressEnabled":    "true",
			"ingressProvider":   "contour",
			"ingressClass":      "contour",
			"cookieDomain":      "selfhosted.example.com",
			"pullSecretMode":    "none",
			"seedInstall":       "true",
			"companyName":       "Self-Hosted",
			"adminName":         "Admin User",
			"adminEmail":        "admin@example.com",
			"adminPassword":     "admin-password",
			"userRole":          "superadmin",
			"confirmKubeAccess": "true",
		},
	}, true)

	if config.LicenseServerURL != defaultLicenseServerURL {
		t.Fatalf("license server url = %q", config.LicenseServerURL)
	}
	if config.WorkerMode != "standalone" {
		t.Fatalf("worker mode = %q", config.WorkerMode)
	}
}

func TestAdminPasswordSummaryDoesNotDisplayProvidedPassword(t *testing.T) {
	summary := adminPasswordSummary(map[string]string{"adminPassword": "secret-password"}, "", true)

	if strings.Contains(summary, "secret-password") {
		t.Fatalf("provided password was displayed: %s", summary)
	}
	if summary != "provided in values file" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestAdminPasswordSummaryDisplaysGeneratedPassword(t *testing.T) {
	summary := adminPasswordSummary(map[string]string{}, "generated-password", true)

	if summary != "generated-password" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestAdminPasswordSummaryShowsSecretLocationBeforeInstall(t *testing.T) {
	summary := adminPasswordSummary(map[string]string{}, "", false)

	if !strings.Contains(summary, "will be generated in Secret patchworks-tenant-admin key adminPassword") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestHumanizeComponentUsesExpectedCasing(t *testing.T) {
	cases := map[string]string{
		"elasticsearch":        "ElasticSearch",
		"mysql":                "MySQL",
		"rabbitmq":             "RabbitMQ",
		"kubefaas-controller":  "KubeFaaS Controller",
		"s3manager":            "S3 Manager",
		"core-web":             "Core Web",
		"rabbitmq-topology":    "RabbitMQ Topology",
		"tenant-admin-keygen":  "Tenant Admin Keygen",
		"passport-oauth-setup": "Passport OAuth Setup",
	}

	for input, expected := range cases {
		if actual := humanizeComponent(input); actual != expected {
			t.Fatalf("humanizeComponent(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestFormatInstallRowsKeepsAllRowsAlphabetically(t *testing.T) {
	rendered := formatInstallRows("Application progress", []installComponentRow{
		{Name: "RabbitMQ", Status: installStatusInProgress, Detail: "0/1 replicas ready"},
		{Name: "Dashboard", Status: installStatusComplete, Detail: "1/1 replicas ready"},
		{Name: "Fabric", Status: installStatusFailed, Detail: "0/1 replicas ready"},
	})

	dashboardIndex := strings.Index(rendered, "Dashboard")
	fabricIndex := strings.Index(rendered, "Fabric")
	rabbitIndex := strings.Index(rendered, "RabbitMQ")
	if dashboardIndex < 0 || fabricIndex < 0 || rabbitIndex < 0 {
		t.Fatalf("rendered rows missing expected components:\n%s", rendered)
	}
	if !(dashboardIndex < fabricIndex && fabricIndex < rabbitIndex) {
		t.Fatalf("rows were not alphabetical:\n%s", rendered)
	}
	for _, expected := range []string{"complete", "failed", "in progress"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("rendered rows missing %q:\n%s", expected, rendered)
		}
	}
}

func TestHelmCommandTextCanSkipInfrastructure(t *testing.T) {
	commands := helmCommandText("patchworks", "patchworks.values.yaml", false)

	if strings.Contains(commands, "patchworks-infra") {
		t.Fatalf("manual commands should not include infra when skipped:\n%s", commands)
	}
	if !strings.Contains(commands, "patchworks-app") {
		t.Fatalf("manual commands should include app:\n%s", commands)
	}
}

func TestHelmCommandTextIncludesInfrastructure(t *testing.T) {
	commands := helmCommandText("patchworks", "patchworks.values.yaml", true)

	if !strings.Contains(commands, "patchworks-infra") {
		t.Fatalf("manual commands should include infra:\n%s", commands)
	}
	if !strings.Contains(commands, "patchworks-app") {
		t.Fatalf("manual commands should include app:\n%s", commands)
	}
}
