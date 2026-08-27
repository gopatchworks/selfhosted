package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
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
	if config.AdminPassword != "admin-password" {
		t.Fatalf("admin password was not loaded")
	}
	if config.Install == nil || !*config.Install {
		t.Fatalf("install flag was not loaded")
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
