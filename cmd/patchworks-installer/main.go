package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	embeddedcharts "github.com/patchworks/selfhosted/charts"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	yaml "go.yaml.in/yaml/v3"
	"helm.sh/helm/v4/pkg/action"
	helmchart "helm.sh/helm/v4/pkg/chart"
	helmloader "helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/cli"
	helmvalues "helm.sh/helm/v4/pkg/cli/values"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/registry"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
)

type clusterInfo struct {
	Context           string
	StorageClasses    []string
	IngressClasses    []string
	HasContour        bool
	HasCertManager    bool
	QuaySecretPresent bool
	// Nodes are the nodes' architectures and container runtimes, for KubeFaaS.
	Nodes nodeFacts
	// Connected is set once the cluster answered, so the inspection result
	// describes the cluster rather than a failed connection.
	Connected bool
	Warnings  []string
}

type kubeAccess struct {
	Kubeconfig string
	Context    string
}

type quayCredentials struct {
	SecretName string
	Username   string
	Password   string
	Email      string
}

type installConfig struct {
	Values map[string]string
	Output string
	Quay   *quayCredentials
}

type localConfig struct {
	ChartSource       string
	ChartVersion      string
	Kubeconfig        string `yaml:"kubeconfig"`
	Context           string `yaml:"context"`
	Namespace         string `yaml:"namespace"`
	Infrastructure    string `yaml:"infrastructure"`
	Domain            string `yaml:"domain"`
	Scheme            string `yaml:"scheme"`
	LicenseKey        string `yaml:"licenseKey"`
	LicenseServerURL  string `yaml:"licenseServerUrl"`
	DashboardEnabled  *bool  `yaml:"dashboardEnabled"`
	RoutingMode       string `yaml:"routingMode"`
	WorkerMode        string `yaml:"workerMode"`
	Profile           string `yaml:"profile"`
	Samples           *bool  `yaml:"samples"`
	MonoInbound       *bool
	MonoInboundRoutes *bool
	IngressEnabled    *bool  `yaml:"ingressEnabled"`
	IngressProvider   string `yaml:"ingressProvider"`
	IngressClass      string `yaml:"ingressClass"`
	CookieDomain      string `yaml:"cookieDomain"`
	PullSecretMode    string `yaml:"pullSecretMode"`
	PullSecret        string `yaml:"pullSecret"`
	QuayUsername      string `yaml:"quayUsername"`
	QuayPassword      string `yaml:"quayPassword"`
	QuayEmail         string `yaml:"quayEmail"`
	InfraComponents   localConfigInfrastructure
	External          localConfigExternal `yaml:"external"`
	SeedInstall       *bool               `yaml:"seedInstall"`
	CompanyName       string              `yaml:"companyName"`
	AdminName         string              `yaml:"adminName"`
	AdminEmail        string              `yaml:"adminEmail"`
	AdminPassword     string              `yaml:"adminPassword"`
	UserRole          string              `yaml:"userRole"`
	Output            string              `yaml:"output"`
	Install           *bool               `yaml:"install"`
	ConfirmKubeAccess *bool               `yaml:"confirmKubeAccess"`
}

type localConfigFile struct {
	// Profile is the chart's installation profile: standard or lightweight.
	Profile string `yaml:"profile,omitempty"`
	// Samples installs the sample flows (mono installations only).
	Samples        *bool                     `yaml:"samples,omitempty"`
	Kubernetes     localConfigKubernetes     `yaml:"kubernetes,omitempty"`
	Application    localConfigApplication    `yaml:"application,omitempty"`
	Infrastructure localConfigInfrastructure `yaml:"infrastructure,omitempty"`
	Workers        localConfigWorkers        `yaml:"workers,omitempty"`
	Ingress        localConfigIngress        `yaml:"ingress,omitempty"`
	Registry       localConfigRegistry       `yaml:"registry,omitempty"`
	Seed           localConfigSeed           `yaml:"seed,omitempty"`
	Output         localConfigOutput         `yaml:"output,omitempty"`
	Installer      localConfigInstaller      `yaml:"installer,omitempty"`

	Kubeconfig         string              `yaml:"kubeconfig,omitempty"`
	Context            string              `yaml:"context,omitempty"`
	Namespace          string              `yaml:"namespace,omitempty"`
	InfrastructureMode string              `yaml:"infrastructureMode,omitempty"`
	Domain             string              `yaml:"domain,omitempty"`
	Scheme             string              `yaml:"scheme,omitempty"`
	LicenseKey         string              `yaml:"licenseKey,omitempty"`
	LicenseServerURL   string              `yaml:"licenseServerUrl,omitempty"`
	DashboardEnabled   *bool               `yaml:"dashboardEnabled,omitempty"`
	RoutingMode        string              `yaml:"routingMode,omitempty"`
	WorkerMode         string              `yaml:"workerMode,omitempty"`
	IngressEnabled     *bool               `yaml:"ingressEnabled,omitempty"`
	IngressProvider    string              `yaml:"ingressProvider,omitempty"`
	IngressClass       string              `yaml:"ingressClass,omitempty"`
	CookieDomain       string              `yaml:"cookieDomain,omitempty"`
	PullSecretMode     string              `yaml:"pullSecretMode,omitempty"`
	PullSecret         string              `yaml:"pullSecret,omitempty"`
	QuayUsername       string              `yaml:"quayUsername,omitempty"`
	QuayPassword       string              `yaml:"quayPassword,omitempty"`
	QuayEmail          string              `yaml:"quayEmail,omitempty"`
	External           localConfigExternal `yaml:"external,omitempty"`
	SeedInstall        *bool               `yaml:"seedInstall,omitempty"`
	CompanyName        string              `yaml:"companyName,omitempty"`
	AdminName          string              `yaml:"adminName,omitempty"`
	AdminEmail         string              `yaml:"adminEmail,omitempty"`
	AdminPassword      string              `yaml:"adminPassword,omitempty"`
	UserRole           string              `yaml:"userRole,omitempty"`
	ValuesFile         string              `yaml:"valuesFile,omitempty"`
	InstallEnabled     *bool               `yaml:"install,omitempty"`
	ConfirmKubeAccess  *bool               `yaml:"confirmKubeAccess,omitempty"`
}

type localConfigKubernetes struct {
	Kubeconfig    string `yaml:"kubeconfig,omitempty"`
	Context       string `yaml:"context,omitempty"`
	Namespace     string `yaml:"namespace,omitempty"`
	ConfirmAccess *bool  `yaml:"confirmAccess,omitempty"`
}

type localConfigApplication struct {
	Domain       string               `yaml:"domain,omitempty"`
	Scheme       string               `yaml:"scheme,omitempty"`
	License      localConfigLicense   `yaml:"license,omitempty"`
	Dashboard    localConfigDashboard `yaml:"dashboard,omitempty"`
	CookieDomain string               `yaml:"cookieDomain,omitempty"`
}

type localConfigInfrastructure struct {
	Mode          string               `yaml:"mode,omitempty"`
	MySQL         localConfigComponent `yaml:"mysql,omitempty"`
	FabricMySQL   localConfigComponent `yaml:"fabricMysql,omitempty"`
	FabricRedis   localConfigComponent `yaml:"fabricRedis,omitempty"`
	Redis         localConfigComponent `yaml:"redis,omitempty"`
	RabbitMQ      localConfigComponent `yaml:"rabbitmq,omitempty"`
	ElasticSearch localConfigComponent `yaml:"elasticsearch,omitempty"`
	S3            localConfigComponent `yaml:"s3,omitempty"`
	KubeFaaS      localConfigComponent `yaml:"kubefaas,omitempty"`
	// KubeFaaSRegistry is the infra chart's bundled image registry for KubeFaaS.
	KubeFaaSRegistry localConfigComponent `yaml:"kubefaasRegistry,omitempty"`
	Pusher           localConfigComponent `yaml:"pusher,omitempty"`
}

type localConfigComponent struct {
	Enabled *bool `yaml:"enabled,omitempty"`
}

type localConfigExternal struct {
	CredentialsMode string                           `yaml:"credentialsMode,omitempty"`
	MySQL           localConfigExternalSQL           `yaml:"mysql,omitempty"`
	FabricMySQL     localConfigExternalSQL           `yaml:"fabricMysql,omitempty"`
	Redis           localConfigExternalRedis         `yaml:"redis,omitempty"`
	RabbitMQ        localConfigExternalRabbitMQ      `yaml:"rabbitmq,omitempty"`
	ElasticSearch   localConfigExternalElasticSearch `yaml:"elasticsearch,omitempty"`
	S3              localConfigExternalS3            `yaml:"s3,omitempty"`
	Pusher          localConfigExternalPusher        `yaml:"pusher,omitempty"`
}

type localConfigExternalSQL struct {
	Host        string `yaml:"host,omitempty"`
	Port        string `yaml:"port,omitempty"`
	Database    string `yaml:"database,omitempty"`
	Username    string `yaml:"username,omitempty"`
	Password    string `yaml:"password,omitempty"`
	SecretName  string `yaml:"secretName,omitempty"`
	PasswordKey string `yaml:"passwordKey,omitempty"`
}

type localConfigExternalRedis struct {
	Host        string `yaml:"host,omitempty"`
	Port        string `yaml:"port,omitempty"`
	Password    string `yaml:"password,omitempty"`
	SecretName  string `yaml:"secretName,omitempty"`
	PasswordKey string `yaml:"passwordKey,omitempty"`
}

type localConfigExternalRabbitMQ struct {
	Host        string `yaml:"host,omitempty"`
	Port        string `yaml:"port,omitempty"`
	Username    string `yaml:"username,omitempty"`
	Password    string `yaml:"password,omitempty"`
	Vhost       string `yaml:"vhost,omitempty"`
	SecretName  string `yaml:"secretName,omitempty"`
	PasswordKey string `yaml:"passwordKey,omitempty"`
}

type localConfigExternalElasticSearch struct {
	Host        string `yaml:"host,omitempty"`
	Port        string `yaml:"port,omitempty"`
	Scheme      string `yaml:"scheme,omitempty"`
	Username    string `yaml:"username,omitempty"`
	Password    string `yaml:"password,omitempty"`
	SecretName  string `yaml:"secretName,omitempty"`
	UsernameKey string `yaml:"usernameKey,omitempty"`
	PasswordKey string `yaml:"passwordKey,omitempty"`
}

type localConfigExternalS3 struct {
	Endpoint     string `yaml:"endpoint,omitempty"`
	Region       string `yaml:"region,omitempty"`
	Bucket       string `yaml:"bucket,omitempty"`
	AccessKey    string `yaml:"accessKey,omitempty"`
	SecretKey    string `yaml:"secretKey,omitempty"`
	PathStyle    *bool  `yaml:"pathStyle,omitempty"`
	SecretName   string `yaml:"secretName,omitempty"`
	AccessKeyKey string `yaml:"accessKeyKey,omitempty"`
	SecretKeyKey string `yaml:"secretKeyKey,omitempty"`
}

type localConfigExternalPusher struct {
	Host          string `yaml:"host,omitempty"`
	Port          string `yaml:"port,omitempty"`
	Scheme        string `yaml:"scheme,omitempty"`
	AppID         string `yaml:"appId,omitempty"`
	AppKey        string `yaml:"appKey,omitempty"`
	AppSecret     string `yaml:"appSecret,omitempty"`
	AppCluster    string `yaml:"appCluster,omitempty"`
	SecretName    string `yaml:"secretName,omitempty"`
	AppIDKey      string `yaml:"appIdKey,omitempty"`
	AppKeyKey     string `yaml:"appKeyKey,omitempty"`
	AppSecretKey  string `yaml:"appSecretKey,omitempty"`
	AppClusterKey string `yaml:"appClusterKey,omitempty"`
}

type localConfigLicense struct {
	Key       string `yaml:"key,omitempty"`
	ServerURL string `yaml:"serverUrl,omitempty"`
}

type localConfigDashboard struct {
	Enabled     *bool  `yaml:"enabled,omitempty"`
	RoutingMode string `yaml:"routingMode,omitempty"`
}

type localConfigWorkers struct {
	Type    string             `yaml:"type,omitempty"`
	Inbound localConfigInbound `yaml:"inbound,omitempty"`
}

// localConfigInbound is the mono hub serving Core's webhook, callback and start
// API endpoints (the chart's workers.mono.inbound).
type localConfigInbound struct {
	Enabled      *bool `yaml:"enabled,omitempty"`
	RouteIngress *bool `yaml:"routeIngress,omitempty"`
}

type localConfigIngress struct {
	Enabled   *bool  `yaml:"enabled,omitempty"`
	Provider  string `yaml:"provider,omitempty"`
	ClassName string `yaml:"className,omitempty"`
}

type localConfigRegistry struct {
	PullSecret localConfigPullSecret `yaml:"pullSecret,omitempty"`
	Quay       localConfigQuay       `yaml:"quay,omitempty"`
}

type localConfigPullSecret struct {
	Mode string `yaml:"mode,omitempty"`
	Name string `yaml:"name,omitempty"`
}

type localConfigQuay struct {
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	Email    string `yaml:"email,omitempty"`
}

type localConfigSeed struct {
	Enabled     *bool            `yaml:"enabled,omitempty"`
	CompanyName string           `yaml:"companyName,omitempty"`
	Admin       localConfigAdmin `yaml:"admin,omitempty"`
}

type localConfigAdmin struct {
	Name     string `yaml:"name,omitempty"`
	Email    string `yaml:"email,omitempty"`
	Password string `yaml:"password,omitempty"`
	Role     string `yaml:"role,omitempty"`
}

type localConfigOutput struct {
	ValuesFile string `yaml:"valuesFile,omitempty"`
}

type localConfigInstaller struct {
	ChartSource  string `yaml:"chartSource,omitempty"`
	ChartVersion string `yaml:"chartVersion,omitempty"`
	Enabled      *bool  `yaml:"enabled,omitempty"`
}

func (config *localConfig) UnmarshalYAML(value *yaml.Node) error {
	var file localConfigFile
	if err := value.Decode(&file); err != nil {
		return err
	}

	*config = localConfig{
		ChartSource:       file.Installer.ChartSource,
		ChartVersion:      file.Installer.ChartVersion,
		Kubeconfig:        file.Kubeconfig,
		Context:           file.Context,
		Namespace:         file.Namespace,
		Infrastructure:    file.InfrastructureMode,
		Domain:            file.Domain,
		Scheme:            file.Scheme,
		LicenseKey:        file.LicenseKey,
		LicenseServerURL:  file.LicenseServerURL,
		DashboardEnabled:  file.DashboardEnabled,
		RoutingMode:       file.RoutingMode,
		WorkerMode:        file.WorkerMode,
		IngressEnabled:    file.IngressEnabled,
		IngressProvider:   file.IngressProvider,
		IngressClass:      file.IngressClass,
		CookieDomain:      file.CookieDomain,
		PullSecretMode:    file.PullSecretMode,
		PullSecret:        file.PullSecret,
		QuayUsername:      file.QuayUsername,
		QuayPassword:      file.QuayPassword,
		QuayEmail:         file.QuayEmail,
		InfraComponents:   file.Infrastructure,
		External:          file.External,
		SeedInstall:       file.SeedInstall,
		CompanyName:       file.CompanyName,
		AdminName:         file.AdminName,
		AdminEmail:        file.AdminEmail,
		AdminPassword:     file.AdminPassword,
		UserRole:          file.UserRole,
		Output:            file.ValuesFile,
		Install:           file.InstallEnabled,
		ConfirmKubeAccess: file.ConfirmKubeAccess,
	}

	overrideString(&config.Kubeconfig, file.Kubernetes.Kubeconfig)
	overrideString(&config.Context, file.Kubernetes.Context)
	overrideString(&config.Namespace, file.Kubernetes.Namespace)
	overrideBool(&config.ConfirmKubeAccess, file.Kubernetes.ConfirmAccess)
	overrideString(&config.Infrastructure, file.Infrastructure.Mode)
	overrideString(&config.Domain, file.Application.Domain)
	overrideString(&config.Scheme, file.Application.Scheme)
	overrideString(&config.LicenseKey, file.Application.License.Key)
	overrideString(&config.LicenseServerURL, file.Application.License.ServerURL)
	overrideBool(&config.DashboardEnabled, file.Application.Dashboard.Enabled)
	overrideString(&config.RoutingMode, file.Application.Dashboard.RoutingMode)
	overrideString(&config.CookieDomain, file.Application.CookieDomain)
	overrideString(&config.WorkerMode, file.Workers.Type)
	overrideString(&config.Profile, file.Profile)
	overrideBool(&config.Samples, file.Samples)
	overrideBool(&config.MonoInbound, file.Workers.Inbound.Enabled)
	overrideBool(&config.MonoInboundRoutes, file.Workers.Inbound.RouteIngress)
	overrideBool(&config.IngressEnabled, file.Ingress.Enabled)
	overrideString(&config.IngressProvider, file.Ingress.Provider)
	overrideString(&config.IngressClass, file.Ingress.ClassName)
	overrideString(&config.PullSecretMode, file.Registry.PullSecret.Mode)
	overrideString(&config.PullSecret, file.Registry.PullSecret.Name)
	overrideString(&config.QuayUsername, file.Registry.Quay.Username)
	overrideString(&config.QuayPassword, file.Registry.Quay.Password)
	overrideString(&config.QuayEmail, file.Registry.Quay.Email)
	overrideBool(&config.SeedInstall, file.Seed.Enabled)
	overrideString(&config.CompanyName, file.Seed.CompanyName)
	overrideString(&config.AdminName, file.Seed.Admin.Name)
	overrideString(&config.AdminEmail, file.Seed.Admin.Email)
	overrideString(&config.AdminPassword, file.Seed.Admin.Password)
	overrideString(&config.UserRole, file.Seed.Admin.Role)
	overrideString(&config.Output, file.Output.ValuesFile)
	overrideBool(&config.Install, file.Installer.Enabled)
	return nil
}

func (config localConfig) MarshalYAML() (any, error) {
	return localConfigFile{
		Profile: config.Profile,
		Samples: config.Samples,
		Kubernetes: localConfigKubernetes{
			Kubeconfig:    config.Kubeconfig,
			Context:       config.Context,
			Namespace:     config.Namespace,
			ConfirmAccess: config.ConfirmKubeAccess,
		},
		Infrastructure: localConfigInfrastructure{
			Mode:             config.Infrastructure,
			MySQL:            config.InfraComponents.MySQL,
			FabricMySQL:      config.InfraComponents.FabricMySQL,
			FabricRedis:      config.InfraComponents.FabricRedis,
			Redis:            config.InfraComponents.Redis,
			RabbitMQ:         config.InfraComponents.RabbitMQ,
			ElasticSearch:    config.InfraComponents.ElasticSearch,
			S3:               config.InfraComponents.S3,
			KubeFaaS:         config.InfraComponents.KubeFaaS,
			KubeFaaSRegistry: config.InfraComponents.KubeFaaSRegistry,
			Pusher:           config.InfraComponents.Pusher,
		},
		Application: localConfigApplication{
			Domain: config.Domain,
			Scheme: config.Scheme,
			License: localConfigLicense{
				Key:       config.LicenseKey,
				ServerURL: config.LicenseServerURL,
			},
			Dashboard: localConfigDashboard{
				Enabled:     config.DashboardEnabled,
				RoutingMode: config.RoutingMode,
			},
			CookieDomain: config.CookieDomain,
		},
		Workers: localConfigWorkers{
			Type: config.WorkerMode,
			Inbound: localConfigInbound{
				Enabled:      config.MonoInbound,
				RouteIngress: config.MonoInboundRoutes,
			},
		},
		Ingress: localConfigIngress{
			Enabled:   config.IngressEnabled,
			Provider:  config.IngressProvider,
			ClassName: config.IngressClass,
		},
		Registry: localConfigRegistry{
			PullSecret: localConfigPullSecret{
				Mode: config.PullSecretMode,
				Name: config.PullSecret,
			},
			Quay: localConfigQuay{
				Username: config.QuayUsername,
				Password: config.QuayPassword,
				Email:    config.QuayEmail,
			},
		},
		External: config.External,
		Seed: localConfigSeed{
			Enabled:     config.SeedInstall,
			CompanyName: config.CompanyName,
			Admin: localConfigAdmin{
				Name:     config.AdminName,
				Email:    config.AdminEmail,
				Password: config.AdminPassword,
				Role:     config.UserRole,
			},
		},
		Output: localConfigOutput{
			ValuesFile: config.Output,
		},
		Installer: localConfigInstaller{
			ChartSource:  config.ChartSource,
			ChartVersion: config.ChartVersion,
			Enabled:      config.Install,
		},
	}, nil
}

func overrideString(target *string, value string) {
	if strings.TrimSpace(value) != "" {
		*target = value
	}
}

func overrideBool(target **bool, value *bool) {
	if value != nil {
		*target = value
	}
}

type cliOptions struct {
	ChartLockPath   string
	ChartSource     string
	ChartVersion    string
	Command         string
	ConfigPath      string
	SaveConfig      bool
	ChartsOutputDir string
}

type installComponentStatus string

const (
	installStatusWaiting    installComponentStatus = "waiting"
	installStatusInProgress installComponentStatus = "in progress"
	installStatusComplete   installComponentStatus = "complete"
	installStatusFailed     installComponentStatus = "failed"
)

type installComponentRow struct {
	Name   string
	Status installComponentStatus
	Detail string
}

const installerLabelKey = "selfhosted.patchworks.io/installed-by"
const installerLabelValue = "patchworks-installer"
const defaultLicenseServerURL = "https://license.wearepatchworks.com"
const primaryPurple = "#7C3AED"

var version = "dev"

// helmFieldManager is the field manager the installer's Helm actions apply
// as. It is the Helm CLI's, so the printed `helm upgrade` commands and the
// installer can take turns on a release without server-side apply conflicts.
// Left unset, Helm would name it after the binary.
const helmFieldManager = "helm"

func init() {
	kube.ManagedFieldsManager = helmFieldManager
}

func main() {
	opts, err := parseCLI(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if opts.Command == "uninstall" {
		captureBackgroundOutput()
		if err := runUninstall(opts); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				return
			}
			fmt.Fprintf(os.Stderr, "\n%s %v\n", errorStyle.Render("✗ Uninstall failed:"), err)
			os.Exit(1)
		}
		return
	}
	if opts.Command == "version" {
		fmt.Println(version)
		return
	}
	if opts.Command == "unpack-charts" {
		if err := runUnpackCharts(opts); err != nil {
			fmt.Fprintf(os.Stderr, "unpack charts failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Charts unpacked to %s\n", opts.ChartsOutputDir)
		return
	}

	captureBackgroundOutput()
	if err := runInstaller(opts); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return
		}
		fmt.Fprintf(os.Stderr, "\n%s %v\n", errorStyle.Render("✗ Install failed:"), err)
		os.Exit(1)
	}
}

func parseCLI(args []string) (cliOptions, error) {
	opts := cliOptions{Command: "install", ConfigPath: "config.yaml", ChartsOutputDir: "patchworks-charts"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "version" || arg == "--version":
			opts.Command = "version"
		case arg == "install" || arg == "uninstall" || arg == "unpack-charts":
			opts.Command = arg
		case arg == "--chart-version" || arg == "--chart-source":
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return cliOptions{}, fmt.Errorf("%s requires a value", arg)
			}
			if arg == "--chart-version" {
				opts.ChartVersion = args[i]
			} else {
				opts.ChartSource = args[i]
			}
		case strings.HasPrefix(arg, "--chart-version="):
			opts.ChartVersion = strings.TrimPrefix(arg, "--chart-version=")
			if opts.ChartVersion == "" {
				return cliOptions{}, fmt.Errorf("--chart-version requires a value")
			}
		case strings.HasPrefix(arg, "--chart-source="):
			opts.ChartSource = strings.TrimPrefix(arg, "--chart-source=")
			if opts.ChartSource == "" {
				return cliOptions{}, fmt.Errorf("--chart-source requires a value")
			}
		case arg == "--chart-lock":
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return cliOptions{}, fmt.Errorf("--chart-lock requires a path")
			}
			opts.ChartLockPath = args[i]
		case strings.HasPrefix(arg, "--chart-lock="):
			opts.ChartLockPath = strings.TrimPrefix(arg, "--chart-lock=")
			if strings.TrimSpace(opts.ChartLockPath) == "" {
				return cliOptions{}, fmt.Errorf("--chart-lock requires a path")
			}
		case arg == "--save-config":
			opts.SaveConfig = true
		case arg == "--output":
			i++
			if i >= len(args) {
				return cliOptions{}, fmt.Errorf("--output requires a path")
			}
			opts.ChartsOutputDir = args[i]
		case strings.HasPrefix(arg, "--output="):
			opts.ChartsOutputDir = strings.TrimPrefix(arg, "--output=")
		case arg == "--config":
			i++
			if i >= len(args) {
				return cliOptions{}, fmt.Errorf("--config requires a path")
			}
			opts.ConfigPath = args[i]
		case strings.HasPrefix(arg, "--config="):
			opts.ConfigPath = strings.TrimPrefix(arg, "--config=")
		case arg == "-h" || arg == "--help":
			return cliOptions{}, fmt.Errorf("usage: patchworks-installer [install|uninstall|unpack-charts|version] [--config config.yaml] [--save-config] [--output patchworks-charts] [--chart-version latest|X.Y.Z] [--chart-source github|bundled] [--chart-lock path]")
		default:
			return cliOptions{}, fmt.Errorf("unknown argument %q. Use install, uninstall, unpack-charts, or version", arg)
		}
	}
	if strings.TrimSpace(opts.ConfigPath) == "" {
		return cliOptions{}, fmt.Errorf("--config requires a non-empty path")
	}
	if strings.TrimSpace(opts.ChartsOutputDir) == "" {
		return cliOptions{}, fmt.Errorf("--output requires a non-empty path")
	}
	if opts.ChartLockPath != "" && (opts.Command != "unpack-charts" || opts.ChartSource != "" || opts.ChartVersion != "") {
		return cliOptions{}, fmt.Errorf("--chart-lock is only supported by unpack-charts and cannot be combined with chart selectors")
	}
	if err := validateChartOptions(opts.ChartSource, opts.ChartVersion); err != nil {
		return cliOptions{}, err
	}
	if (opts.ChartSource != "" || opts.ChartVersion != "") && opts.Command != "install" && opts.Command != "unpack-charts" {
		return cliOptions{}, fmt.Errorf("chart options are only supported for install and unpack-charts")
	}
	return opts, nil
}

func runInstaller(opts cliOptions) error {
	printHeader()
	defaults, loaded, err := loadLocalConfig(opts.ConfigPath)
	if err != nil {
		return err
	}
	if loaded {
		printDone("Loaded settings from " + opts.ConfigPath)
	}

	access, err := selectKubeAccess(defaults)
	if err != nil {
		return err
	}

	info, err := runInspection(access.Kubeconfig, access.Context)
	if err != nil {
		return err
	}

	// ingressService is the ingress controller's Service as last read, for the
	// warning shown when it has no LoadBalancer address.
	var ingressService *corev1.Service
	installedContour := false
	if !info.HasContour && len(info.IngressClasses) == 0 {
		installContourChoice := true
		if err := runFormWithQuitConfirm(huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("No ingress controller was detected. Install Contour?").
					Description("Contour is the reference ingress controller for this chart. This installs the projectcontour Helm chart into the projectcontour namespace.").
					Affirmative("Install Contour").
					Negative("Skip").
					Value(&installContourChoice),
			),
		)); err != nil {
			return err
		}
		if installContourChoice {
			if err := installContour(access); err != nil {
				if errors.Is(err, huh.ErrUserAborted) {
					return err
				}
				fmt.Println(warningLine(err.Error()))
				continueWithoutContour := true
				if confirmErr := runFormWithQuitConfirm(huh.NewForm(
					huh.NewGroup(
						huh.NewConfirm().
							Title("Continue without installing Contour?").
							Description("You can install Contour manually and re-run the generated Helm commands later.").
							Affirmative("Continue").
							Negative("Stop").
							Value(&continueWithoutContour),
					),
				)); confirmErr != nil {
					return confirmErr
				}
				if !continueWithoutContour {
					return err
				}
			} else {
				info.HasContour = true
				info.IngressClasses = append(info.IngressClasses, "contour")
				sort.Strings(info.IngressClasses)
				info.Warnings = removeWarning(info.Warnings, "no ingress class detected; public ingress may not work yet")
				installedContour = true
				ingressService, err = waitForContourAddress(access)
				if err != nil {
					return err
				}
				printLoadBalancerWarning(ingressService)
			}
		}
	}

	config, err := runSettingsForm(info, access.Kubeconfig, access.Context, defaults)
	if err != nil {
		return err
	}
	switch {
	case config.Values["ingressEnabled"] != "true":
		ingressService = nil
	case !installedContour || config.Values["ingressProvider"] != "contour":
		ingressService = existingIngressService(access, config.Values["ingressProvider"])
		printLoadBalancerWarning(ingressService)
	}
	if ingressService != nil {
		config.Values["ingressAddressNotice"] = loadBalancerNotice(*ingressService)
	}

	lockPath := cleanPath(config.Output) + ".charts.lock.yaml"
	previous, err := readChartLock(lockPath)
	if err != nil {
		return err
	}
	request, pinned, err := chooseChartRequest(opts, defaults, previous)
	if err != nil {
		return err
	}
	var charts *preparedCharts
	chartsNotice := ""
	if err := runProgress(progressStep{
		Title:  "Preparing Patchworks charts",
		Detail: "Resolving the chart release and validating both charts",
		DoneFunc: func() string {
			return fmt.Sprintf("Prepared charts %s (%s)", charts.Lock.Version, charts.Lock.Source)
		},
	}, func(_ context.Context) error {
		var prepareErr error
		charts, prepareErr = prepareCharts(request, pinned)
		if prepareErr != nil {
			return prepareErr
		}
		// A chart flag is an explicit choice. Otherwise a lock, a config pin or
		// GitHub's latest release can quietly select older charts than the ones
		// in this binary. The check never blocks the selected charts.
		if opts.ChartSource == "" && opts.ChartVersion == "" && charts.Lock.Source == "github" {
			if bundled, bundledErr := prepareCharts(chartRequest{Source: "bundled"}, nil); bundledErr == nil {
				chartsNotice = bundledChartsNotice(charts.Lock, bundled.Lock)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if chartsNotice != "" {
		fmt.Println(warningLine(chartsNotice))
		fmt.Println(noteLine(selectedChartsOrigin(request, pinned, charts.Lock.Version, lockPath, opts.ConfigPath)))
		config.Values["chartNotice"] = chartsNotice + "; run with --chart-source bundled to use them"
	}
	config.Values["chartSource"] = charts.Lock.Source
	config.Values["chartVersion"] = charts.Lock.Version
	if config.Values["monoInbound"] == "true" {
		config.Values["monoInboundUnsupported"] = monoInboundUnsupportedReason(charts.Charts["patchworks-app"])
	}
	if defaultProfile(config.Values["profile"]) == "lightweight" {
		config.Values["profileUnsupported"], config.Values["lightweightWebApart"] = lightweightSupport(charts.Charts["patchworks-app"])
		config.Values["profileChecked"] = "true"
	}
	if defaultWorkerMode(config.Values["workerMode"]) == "mono" {
		reason := monoAllCompaniesUnsupportedReason(charts.Charts["patchworks-app"])
		config.Values["monoAllCompaniesUnsupported"] = reason
		config.Values["monoAllCompanies"] = fmt.Sprintf("%t", reason == "")
	}
	checkKubeFaaS(config.Values, charts.Charts["patchworks-infra"])
	if config.Values["samples"] == "true" {
		config.Values["samplesUnsupported"], config.Values["samplesShapeFlags"] = samplesSupport(charts.Charts["patchworks-app"], config.Values)
		config.Values["samplesChecked"] = "true"
	}

	installInfrastructure, err := chooseInfrastructureInstall(access, config.Values["namespace"], config.Values)
	if err != nil {
		return err
	}
	config.Values["installInfrastructure"] = fmt.Sprintf("%t", installInfrastructure)

	runInstall := boolDefault(defaults.Install, true)
	if err := runFormWithQuitConfirm(huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Install summary").
				Description(installSummary(config)),
			huh.NewConfirm().
				Title("Install Patchworks now?").
				Description("The installer will use the selected charts to install or upgrade the infra and app releases, then check rollout status. Helm CLI is only needed for manual commands.").
				Affirmative("Install").
				Negative("Only write values").
				Value(&runInstall),
		),
	)); err != nil {
		return err
	}

	if err := writeValues(config.Output, config.Values); err != nil {
		return err
	}

	if err := writeChartLock(lockPath, charts.Lock); err != nil {
		return err
	}
	printDone("Wrote "+config.Output,
		strings.Join(nonEmpty([]string{
			"namespace " + config.Values["namespace"],
			dashboardURL(config.Values),
			"infrastructure " + infrastructureSummary(config.Values),
		}), " · "),
		"chart release pinned in "+lockPath,
	)

	if opts.SaveConfig {
		if err := saveLocalConfig(opts.ConfigPath, localConfigFromInstallConfig(config, runInstall)); err != nil {
			return err
		}
		printDone("Saved settings to " + opts.ConfigPath)
	}

	commands := selectedHelmCommandText(config.Values["namespace"], config.Output, installInfrastructure, manualKubeFaaSFor(config.Values, installInfrastructure))
	if !runInstall {
		fmt.Print(manualInstallView(commands))
		fmt.Print(accessDetailsView(config.Values, "", false))
		printLoadBalancerWarning(stillPendingIngressService(access, ingressService))
		return nil
	}

	if config.Quay != nil {
		if err := runProgress(progressStep{
			Title:  "Creating Quay pull secret",
			Done:   fmt.Sprintf("Created pull secret %s in %s", config.Quay.SecretName, config.Values["namespace"]),
			Detail: fmt.Sprintf("Creating %s in namespace %s", config.Quay.SecretName, config.Values["namespace"]),
		}, func(ctx context.Context) error {
			return createQuayPullSecret(ctx, access, config.Values["namespace"], *config.Quay)
		}); err != nil {
			return err
		}
	}

	if installInfrastructure && kubefaasEnabled(config.Values) && config.Values["pullSecret"] != "" {
		kubefaasNamespace := stringDefault(config.Values["kubefaasNamespace"], defaultKubeFaaSNamespace)
		if err := runProgress(progressStep{
			Title:  "Preparing the KubeFaaS namespace",
			Done:   fmt.Sprintf("Copied pull secret %s to %s", config.Values["pullSecret"], kubefaasNamespace),
			Detail: fmt.Sprintf("Creating namespace %s for patchworks-infra and copying pull secret %s", kubefaasNamespace, config.Values["pullSecret"]),
		}, func(ctx context.Context) error {
			return prepareKubeFaaSNamespace(ctx, access, config.Values)
		}); err != nil {
			return err
		}
	}

	if err := installPatchworks(access, config.Values["namespace"], config.Output, installInfrastructure, charts); err != nil {
		if !errors.Is(err, huh.ErrUserAborted) {
			fmt.Print(manualInstallView(commands))
		}
		return err
	}

	if status, err := workloadStatus(access, config.Values["namespace"]); err != nil {
		fmt.Println(warningLine(fmt.Sprintf("Could not read workload status: %v", err)))
	} else {
		fmt.Println(status)
	}
	generatedAdminPassword := ""
	if config.Values["adminPassword"] == "" {
		password, err := tenantAdminPassword(access, config.Values["namespace"])
		if err == nil {
			generatedAdminPassword = password
		}
	}
	printCapturedOutput()
	fmt.Print(accessDetailsView(config.Values, generatedAdminPassword, true))
	printLoadBalancerWarning(stillPendingIngressService(access, ingressService))
	return nil
}

func selectKubeAccess(defaults localConfig) (kubeAccess, error) {
	kubeconfig, contextName := defaultKubeAccess()
	kubeconfig = stringDefault(defaults.Kubeconfig, kubeconfig)
	contextName = stringDefault(defaults.Context, contextName)
	access := kubeAccess{Kubeconfig: kubeconfig, Context: contextName}
	useAccess := boolDefault(defaults.ConfirmKubeAccess, true)

	if err := runFormWithQuitConfirm(huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Use this kubeconfig and context?").
				Description(fmt.Sprintf("Kubeconfig: %s\nContext: %s", kubeconfig, contextName)).
				Affirmative("Yes").
				Negative("No").
				Value(&useAccess),
		),
	)); err != nil {
		return kubeAccess{}, err
	}

	if !useAccess {
		if err := runFormWithQuitConfirm(huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Kubeconfig path").
					Description("Use a local kubeconfig path or a KUBECONFIG path list.").
					Value(&kubeconfig).
					CharLimit(512),
			),
		)); err != nil {
			return kubeAccess{}, err
		}

		currentContext, contexts := kubeContextsFromKubeconfig(kubeconfig)
		if currentContext != "" {
			contextName = currentContext
		}

		if len(contexts) == 0 {
			if err := runFormWithQuitConfirm(huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Kubernetes context").
						Description("No contexts were found in the selected kubeconfig. Leave blank to use the kubeconfig current-context.").
						Value(&contextName).
						CharLimit(256),
				),
			)); err != nil {
				return kubeAccess{}, err
			}
		} else {
			if !contains(contexts, contextName) {
				contextName = contexts[0]
			}
			if err := runFormWithQuitConfirm(huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Kubernetes context").
						Description("Select one of the contexts available in the chosen kubeconfig.").
						Options(huhOptions(contexts)...).
						Value(&contextName),
				),
			)); err != nil {
				return kubeAccess{}, err
			}
		}
		access = kubeAccess{Kubeconfig: kubeconfig, Context: contextName}
	}

	return access, nil
}

func defaultKubeAccess() (string, string) {
	kubeconfig := envFirst("KUBECONFIG")
	if kubeconfig == "" && homedir.HomeDir() != "" {
		kubeconfig = filepath.Join(homedir.HomeDir(), ".kube", "config")
	}

	contextName := envFirst("KUBE_CONTEXT", "CONTEXT")
	if contextName == "" {
		contextName = currentContextFromKubeconfig(kubeconfig)
	}

	return kubeconfig, contextName
}

func currentContextFromKubeconfig(kubeconfig string) string {
	currentContext, _ := kubeContextsFromKubeconfig(kubeconfig)
	return currentContext
}

func kubeContextsFromKubeconfig(kubeconfig string) (string, []string) {
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	if strings.Contains(kubeconfig, string(os.PathListSeparator)) {
		rules = &clientcmd.ClientConfigLoadingRules{Precedence: filepath.SplitList(kubeconfig)}
	}

	config, err := rules.Load()
	if err != nil {
		return "", nil
	}

	contexts := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		contexts = append(contexts, name)
	}
	sort.Strings(contexts)

	return config.CurrentContext, contexts
}

func huhOptions(values []string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(values))
	for _, value := range values {
		options = append(options, huh.NewOption(value, value))
	}
	return options
}

func installContour(access kubeAccess) error {
	settings, cfg, err := helmRuntime(access, "projectcontour")
	if err != nil {
		return err
	}

	var chart helmchart.Charter
	var values map[string]any
	if err := runProgress(progressStep{
		Title:  "Fetching Contour chart",
		Done:   "Fetched Contour chart",
		Detail: "Downloading chart metadata and loading contour/contour",
	}, func(_ context.Context) error {
		var loadErr error
		chart, values, loadErr = loadHelmChart(settings, "contour", "https://projectcontour.github.io/helm-charts/", "")
		return loadErr
	}); err != nil {
		return err
	}
	mergeCommonLabels(values, installerLabels())

	return runProgress(progressStep{
		Title:    "Installing Contour",
		Done:     "Installed Contour · ingress class contour",
		Detail:   "Applying release contour in namespace projectcontour",
		StopNote: helmStopNote("contour"),
	}, func(ctx context.Context) error {
		return helmUpgradeOrInstall(ctx, cfg, "contour", "projectcontour", chart, values, true, 10*time.Minute, installerLabels())
	})
}

// helmStopNote explains what stopping a Helm install or upgrade leaves:
// Helm records the release as failed, which a later run can upgrade.
func helmStopNote(release string) string {
	return fmt.Sprintf("Helm marks release %s as failed and keeps what it created. Run the installer again to retry.", release)
}

func installPatchworks(access kubeAccess, namespace, valuesFile string, installInfrastructure bool, charts *preparedCharts) error {
	settings, cfg, err := helmRuntime(access, namespace)
	if err != nil {
		return err
	}

	values, err := mergeHelmValues(settings, valuesFile)
	if err != nil {
		return err
	}
	if installInfrastructure {
		if err := runProgress(progressStep{
			Title:    "Installing infrastructure",
			Done:     "Installed infrastructure",
			Detail:   infraInstallDetail(values),
			Monitor:  installPhaseMonitor(access, namespace, "infra"),
			StopNote: helmStopNote("patchworks-infra"),
		}, func(ctx context.Context) error {
			return helmUpgradeOrInstall(ctx, cfg, "patchworks-infra", namespace, charts.Charts["patchworks-infra"], values, true, infraInstallTimeout(values), installerLabels())
		}); err != nil {
			if !errors.Is(err, huh.ErrUserAborted) {
				printInstallFailureDiagnostics(access, namespace)
			}
			return err
		}
	}

	if err := runProgress(progressStep{
		Title:    "Installing application",
		Done:     "Installed application",
		Detail:   "Applying release patchworks-app and waiting for hooks and readiness",
		Monitor:  installPhaseMonitor(access, namespace, "app"),
		StopNote: helmStopNote("patchworks-app"),
	}, func(ctx context.Context) error {
		return helmUpgradeOrInstall(ctx, cfg, "patchworks-app", namespace, charts.Charts["patchworks-app"], values, false, 15*time.Minute, installerLabels())
	}); err != nil {
		if !errors.Is(err, huh.ErrUserAborted) {
			printInstallFailureDiagnostics(access, namespace)
		}
		return err
	}
	return nil
}

func chooseInfrastructureInstall(access kubeAccess, namespace string, values map[string]string) (bool, error) {
	if values["infrastructure"] == "external" || allInfrastructureComponentsDisabled(values) {
		return false, nil
	}

	_, cfg, err := helmRuntime(access, namespace)
	if err != nil {
		return false, err
	}
	if !helmReleaseExists(cfg, "patchworks-infra") {
		return true, nil
	}

	upgradeInfrastructure := false
	if err := runFormWithQuitConfirm(huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Upgrade bundled infrastructure?").
				Description("patchworks-infra is already installed. Leave it unchanged unless you are deliberately changing MySQL, RabbitMQ, Redis, ElasticSearch, S3, Soketi, or KubeFaaS infrastructure settings.").
				Affirmative("Upgrade infra").
				Negative("Leave infra unchanged").
				Value(&upgradeInfrastructure),
		),
	).WithTheme(installerTheme()).WithWidth(88)); err != nil {
		return false, err
	}
	return upgradeInfrastructure, nil
}

func allInfrastructureComponentsDisabled(values map[string]string) bool {
	defaults := map[string]bool{
		"infraMysqlEnabled":         true,
		"infraFabricMysqlEnabled":   false,
		"infraFabricRedisEnabled":   false,
		"infraRedisEnabled":         true,
		"infraRabbitmqEnabled":      true,
		"infraElasticsearchEnabled": true,
		"infraS3Enabled":            true,
		"infraKubefaasEnabled":      false,
		"infraPusherEnabled":        true,
	}
	for key, fallback := range defaults {
		if parseBoolDefault(values[key], fallback) {
			return false
		}
	}
	return true
}

func runUninstall(opts cliOptions) error {
	printHeader()
	defaults, loaded, err := loadLocalConfig(opts.ConfigPath)
	if err != nil {
		return err
	}
	if loaded {
		printDone("Loaded settings from " + opts.ConfigPath)
	}

	access, err := selectKubeAccess(defaults)
	if err != nil {
		return err
	}
	printDone("Context "+stringDefault(access.Context, "(current context)"), access.Kubeconfig)

	namespace := stringDefault(defaults.Namespace, "patchworks")
	pullSecretName := stringDefault(defaults.PullSecret, "quay-credentials")
	if err := runFormWithQuitConfirm(huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Patchworks namespace").
				Description("Namespace containing patchworks-app and patchworks-infra.").
				Value(&namespace).
				CharLimit(128),
			huh.NewInput().
				Title("Image pull secret name").
				Description("Only removed if it is labelled as created by this installer. Leave blank to skip secret removal.").
				Value(&pullSecretName).
				CharLimit(253),
		),
	).WithTheme(installerTheme()).WithWidth(88)); err != nil {
		return err
	}

	removeContour, contourMarked, err := shouldRemoveContour(access)
	if err != nil {
		return err
	}

	summary := uninstallSummary(namespace, pullSecretName, removeContour, contourMarked)
	confirmDelete := false
	if err := runFormWithQuitConfirm(huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Uninstall summary").
				Description(summary),
			huh.NewConfirm().
				Title("Delete these resources?").
				Description("This will uninstall Patchworks Helm releases and delete installer-created resources selected above.").
				Affirmative("Delete").
				Negative("Cancel").
				Value(&confirmDelete),
		),
	).WithTheme(installerTheme()).WithWidth(88)); err != nil {
		return err
	}
	if !confirmDelete {
		return huh.ErrUserAborted
	}

	if err := uninstallPatchworks(access, namespace); err != nil {
		return err
	}

	if strings.TrimSpace(pullSecretName) != "" {
		var outcome string
		if err := runProgress(progressStep{
			Title:    "Removing Quay pull secret",
			Detail:   fmt.Sprintf("Deleting %s only if installer-labelled", pullSecretName),
			DoneFunc: func() string { return outcome },
		}, func(ctx context.Context) error {
			var deleteErr error
			outcome, deleteErr = deleteInstallerPullSecret(ctx, access, namespace, pullSecretName)
			return deleteErr
		}); err != nil {
			return err
		}
	}

	if removeContour {
		if err := uninstallContour(access); err != nil {
			return err
		}
	}

	fmt.Println()
	printDone("Uninstall complete")
	return nil
}

func shouldRemoveContour(access kubeAccess) (bool, bool, error) {
	_, cfg, err := helmRuntime(access, "projectcontour")
	if err != nil {
		return false, false, err
	}

	labels, exists, err := helmReleaseLabels(cfg, "contour")
	if err != nil {
		return false, false, err
	}
	if !exists {
		return false, false, nil
	}

	marked := labels[installerLabelKey] == installerLabelValue
	remove := marked
	description := "The contour release is labelled as installed by this Patchworks installer."
	if !marked {
		description = "A contour release exists, but it is not labelled as installed by this Patchworks installer."
	}
	if err := runFormWithQuitConfirm(huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Remove Contour?").
				Description(description).
				Affirmative("Remove").
				Negative("Keep").
				Value(&remove),
		),
	).WithTheme(installerTheme()).WithWidth(88)); err != nil {
		return false, false, err
	}

	return remove, marked, nil
}

func uninstallPatchworks(access kubeAccess, namespace string) error {
	_, cfg, err := helmRuntime(access, namespace)
	if err != nil {
		return err
	}

	if err := runProgress(progressStep{
		Title:    "Uninstalling application",
		Done:     "Removed release patchworks-app",
		Detail:   "Removing release patchworks-app",
		StopNote: helmUninstallStopNote("patchworks-app"),
	}, func(_ context.Context) error {
		return helmUninstall(cfg, "patchworks-app", 10*time.Minute)
	}); err != nil {
		return err
	}
	return runProgress(progressStep{
		Title:    "Uninstalling infrastructure",
		Done:     "Removed release patchworks-infra",
		Detail:   "Removing release patchworks-infra",
		StopNote: helmUninstallStopNote("patchworks-infra"),
	}, func(_ context.Context) error {
		return helmUninstall(cfg, "patchworks-infra", 10*time.Minute)
	})
}

func uninstallContour(access kubeAccess) error {
	_, cfg, err := helmRuntime(access, "projectcontour")
	if err != nil {
		return err
	}
	return runProgress(progressStep{
		Title:    "Uninstalling Contour",
		Done:     "Removed release contour",
		Detail:   "Removing release contour from namespace projectcontour",
		StopNote: helmUninstallStopNote("contour"),
	}, func(_ context.Context) error {
		return helmUninstall(cfg, "contour", 10*time.Minute)
	})
}

// helmUninstallStopNote explains what stopping an uninstall leaves. Helm's
// uninstall takes no context, so the installer stops waiting rather than
// cancelling it.
func helmUninstallStopNote(release string) string {
	return fmt.Sprintf("Release %s may be left partly removed. Run uninstall again to finish.", release)
}

func helmRuntime(access kubeAccess, namespace string) (*cli.EnvSettings, *action.Configuration, error) {
	settings := cli.New()
	settings.KubeConfig = access.Kubeconfig
	settings.KubeContext = access.Context
	settings.SetNamespace(namespace)

	registryClient, err := registry.NewClient(
		registry.ClientOptWriter(io.Discard),
		registry.ClientOptCredentialsFile(settings.RegistryConfig),
	)
	if err != nil {
		return nil, nil, err
	}

	cfg := new(action.Configuration)
	cfg.RegistryClient = registryClient
	if err := cfg.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER")); err != nil {
		return nil, nil, err
	}
	cfg.SetHookOutputFunc(func(_, _, _ string) io.Writer { return io.Discard })
	return settings, cfg, nil
}

func loadHelmChart(settings *cli.EnvSettings, chartRef, repoURL, valuesFile string) (helmchart.Charter, map[string]any, error) {
	chartPathOptions := action.ChartPathOptions{RepoURL: repoURL}
	chartPath, err := chartPathOptions.LocateChart(chartRef, settings)
	if err != nil {
		return nil, nil, err
	}

	chart, err := helmloader.Load(chartPath)
	if err != nil {
		return nil, nil, err
	}

	values, err := mergeHelmValues(settings, valuesFile)
	if err != nil {
		return nil, nil, err
	}

	return chart, values, nil
}

func embeddedChartPath(chartName string) (string, func(), error) {
	chartFS, err := fs.Sub(embeddedcharts.FS, chartName)
	if err != nil {
		return "", nil, err
	}

	tempDir, err := os.MkdirTemp("", "patchworks-chart-*")
	if err != nil {
		return "", nil, err
	}

	cleanup := func() { _ = os.RemoveAll(tempDir) }
	if err := copyEmbeddedFS(tempDir, chartFS); err != nil {
		cleanup()
		return "", nil, err
	}

	return tempDir, cleanup, nil
}

func unpackEmbeddedCharts(dest string) error {
	if strings.TrimSpace(dest) == "" {
		return fmt.Errorf("output path cannot be empty")
	}
	return copyEmbeddedFS(dest, embeddedcharts.FS)
}

func copyEmbeddedFS(dest string, source fs.FS) error {
	return fs.WalkDir(source, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		target := filepath.Join(dest, filepath.FromSlash(path))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := fs.ReadFile(source, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func mergeHelmValues(settings *cli.EnvSettings, valuesFile string) (map[string]any, error) {
	valueOptions := &helmvalues.Options{}
	if valuesFile != "" {
		valueOptions.ValueFiles = []string{valuesFile}
	}
	return valueOptions.MergeValues(getter.All(settings))
}

func helmUpgradeOrInstall(ctx context.Context, cfg *action.Configuration, releaseName, namespace string, chart helmchart.Charter, values map[string]any, createNamespace bool, timeout time.Duration, labels map[string]string) error {
	exists := helmReleaseExists(cfg, releaseName)
	if !exists {
		install := action.NewInstall(cfg)
		install.ReleaseName = releaseName
		install.Namespace = namespace
		install.CreateNamespace = createNamespace
		install.WaitStrategy = kube.StatusWatcherStrategy
		install.WaitForJobs = true
		install.Timeout = timeout
		install.DependencyUpdate = true
		install.Labels = labels
		install.ForceConflicts = install.ServerSideApply
		_, err := install.RunWithContext(ctx, chart, values)
		return err
	}

	upgrade := newHelmUpgrade(cfg, releaseName, namespace, timeout, labels)
	_, err := upgrade.RunWithContext(ctx, releaseName, chart, values)
	return err
}

// newHelmUpgrade configures the upgrade of an existing release. The values
// this run renders are the ones the user chose, so a server-side apply forces
// its way past fields another field manager last set, as
// `helm upgrade --force-conflicts` does. Helm keeps a release on the apply
// method its last revision used and rejects a forced client-side apply.
func newHelmUpgrade(cfg *action.Configuration, releaseName, namespace string, timeout time.Duration, labels map[string]string) *action.Upgrade {
	upgrade := action.NewUpgrade(cfg)
	upgrade.Namespace = namespace
	upgrade.WaitStrategy = kube.StatusWatcherStrategy
	upgrade.WaitForJobs = true
	upgrade.Timeout = timeout
	upgrade.DependencyUpdate = true
	upgrade.Labels = labels
	upgrade.ForceConflicts = helmReleaseServerSide(cfg, releaseName)
	return upgrade
}

// helmReleaseServerSide reports whether Helm upgrades the release with
// server-side apply. Under the default ServerSideApply "auto" Helm follows the
// apply method of the last revision, whatever its status.
func helmReleaseServerSide(cfg *action.Configuration, releaseName string) bool {
	last, err := cfg.Releases.Last(releaseName)
	if err != nil {
		return false
	}
	release, ok := last.(*releasev1.Release)
	return ok && release.ApplyMethod == string(releasev1.ApplyMethodServerSideApply)
}

func installerLabels() map[string]string {
	return map[string]string{installerLabelKey: installerLabelValue}
}

func mergeCommonLabels(values map[string]any, labels map[string]string) {
	commonLabels := map[string]any{}
	switch existing := values["commonLabels"].(type) {
	case map[string]any:
		for key, value := range existing {
			commonLabels[key] = value
		}
	case map[string]string:
		for key, value := range existing {
			commonLabels[key] = value
		}
	}
	for key, value := range labels {
		commonLabels[key] = value
	}
	values["commonLabels"] = commonLabels
}

func helmReleaseExists(cfg *action.Configuration, releaseName string) bool {
	history := action.NewHistory(cfg)
	history.Max = 1
	_, err := history.Run(releaseName)
	return err == nil
}

func helmReleaseLabels(cfg *action.Configuration, releaseName string) (map[string]string, bool, error) {
	status := action.NewStatus(cfg)
	release, err := status.Run(releaseName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "release: not found") {
			return nil, false, nil
		}
		return nil, false, err
	}
	if typedRelease, ok := release.(*releasev1.Release); ok {
		return typedRelease.Labels, true, nil
	}
	return nil, true, nil
}

func helmUninstall(cfg *action.Configuration, releaseName string, timeout time.Duration) error {
	uninstall := action.NewUninstall(cfg)
	uninstall.IgnoreNotFound = true
	uninstall.WaitStrategy = kube.StatusWatcherStrategy
	uninstall.Timeout = timeout
	_, err := uninstall.Run(releaseName)
	return err
}

func helmCommandText(namespace, valuesFile string, installInfrastructure bool, kubefaas manualKubeFaaS) string {
	if !installInfrastructure {
		return fmt.Sprintf(`helm upgrade --install patchworks-app ./charts/patchworks-app -n %s --create-namespace -f %s --timeout 15m --wait`,
			shellQuote(namespace), shellQuote(cleanPath(valuesFile)))
	}
	infraTimeout := "15m"
	if kubefaas.Namespace != "" {
		infraTimeout = fmt.Sprintf("%dm", int(kubefaasInfraTimeout.Minutes()))
	}
	return kubefaasPullSecretCommands(namespace, kubefaas) + fmt.Sprintf(`helm upgrade --install patchworks-infra ./charts/patchworks-infra -n %s --create-namespace -f %s --timeout %s --wait
helm upgrade --install patchworks-app ./charts/patchworks-app -n %s -f %s --timeout 15m --wait`,
		shellQuote(namespace), shellQuote(cleanPath(valuesFile)), infraTimeout, shellQuote(namespace), shellQuote(cleanPath(valuesFile)))
}

func dashboardURL(values map[string]string) string {
	domain := values["domain"]
	if domain == "" {
		return ""
	}
	return fmt.Sprintf("%s://%s", stringDefault(values["scheme"], "https"), domain)
}

func adminPasswordSummary(values map[string]string, generatedAdminPassword string, installed bool) string {
	if values["adminPassword"] != "" {
		return "provided in values file"
	}
	if generatedAdminPassword != "" {
		return generatedAdminPassword
	}
	if installed {
		return "generated in Secret patchworks-tenant-admin key adminPassword"
	}
	return "will be generated in Secret patchworks-tenant-admin key adminPassword"
}

func terminalLink(text, url string) string {
	if text == "" || url == "" {
		return text
	}
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, text)
}

func inspectCluster(kubeconfigPath, contextName string) inspectResult {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	info := clusterInfo{Context: contextName}
	clientConfig := deferredClientConfig(kubeconfigPath, contextName)

	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		info.Warnings = append(info.Warnings, fmt.Sprintf("could not read kubeconfig: %v", err))
		return inspectResult{Info: info}
	}

	if info.Context == "" {
		info.Context = rawConfig.CurrentContext
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		info.Warnings = append(info.Warnings, fmt.Sprintf("could not create Kubernetes client: %v", err))
		return inspectResult{Info: info}
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		info.Warnings = append(info.Warnings, fmt.Sprintf("could not create Kubernetes clientset: %v", err))
		return inspectResult{Info: info}
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		info.Warnings = append(info.Warnings, fmt.Sprintf("could not create dynamic Kubernetes client: %v", err))
		return inspectResult{Info: info}
	}

	// A forbidden answer still means the cluster is there; RBAC may only
	// limit what the rest of the inspection can read.
	if _, err := client.Discovery().RESTClient().Get().AbsPath("/version").DoRaw(ctx); err != nil && !apierrors.IsForbidden(err) {
		info.Warnings = append(info.Warnings, fmt.Sprintf("could not connect to the cluster: %v", err))
		return inspectResult{Info: info}
	}
	info.Connected = true

	info.StorageClasses = storageClasses(ctx, client)
	info.IngressClasses = ingressClasses(ctx, client)
	info.HasContour = contains(info.IngressClasses, "contour") || apiResourceExists(client.Discovery(), "projectcontour.io", "v1", "httpproxies")
	info.HasCertManager = crdExists(ctx, dynamicClient, "certificates.cert-manager.io")
	info.Nodes = readNodeFacts(ctx, client)

	if len(info.StorageClasses) == 0 {
		info.Warnings = append(info.Warnings, "no StorageClass detected; chart-managed persistence may fail")
	}
	if len(info.IngressClasses) == 0 && !info.HasContour {
		info.Warnings = append(info.Warnings, "no ingress class detected; public ingress may not work yet")
	}

	sort.Strings(info.StorageClasses)
	sort.Strings(info.IngressClasses)

	return inspectResult{Info: info}
}

func storageClasses(ctx context.Context, client kubernetes.Interface) []string {
	list, err := client.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.Name)
	}
	return names
}

func ingressClasses(ctx context.Context, client kubernetes.Interface) []string {
	list, err := client.NetworkingV1().IngressClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.Name)
	}
	return names
}

func apiResourceExists(discoveryClient discovery.DiscoveryInterface, group, version, resource string) bool {
	list, err := discoveryClient.ServerResourcesForGroupVersion(group + "/" + version)
	if err != nil {
		return false
	}

	for _, apiResource := range list.APIResources {
		if apiResource.Name == resource {
			return true
		}
	}
	return false
}

func crdExists(ctx context.Context, client dynamic.Interface, name string) bool {
	gvr := schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}
	_, err := client.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	return err == nil
}

func secretExistsInNamespace(kubeconfigPath, contextName, namespace, name string) bool {
	if namespace == "" || name == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := kubernetesClient(kubeAccess{Kubeconfig: kubeconfigPath, Context: contextName})
	if err != nil {
		return false
	}

	_, err = client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	return err == nil
}

func createQuayPullSecret(ctx context.Context, access kubeAccess, namespace string, creds quayCredentials) error {
	if creds.SecretName == "" {
		return fmt.Errorf("pull secret name cannot be empty")
	}
	if creds.Username == "" {
		return fmt.Errorf("quay username cannot be empty")
	}
	if creds.Password == "" {
		return fmt.Errorf("quay password/token cannot be empty")
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	client, err := kubernetesClient(access)
	if err != nil {
		return err
	}

	_, err = client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		}, metav1.CreateOptions{})
	}
	if err != nil {
		return err
	}

	dockerConfig, err := dockerConfigJSON(creds)
	if err != nil {
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      creds.SecretName,
			Namespace: namespace,
			Labels:    installerLabels(),
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: dockerConfig,
		},
	}

	_, err = client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := client.CoreV1().Secrets(namespace).Get(ctx, creds.SecretName, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		existing.Type = corev1.SecretTypeDockerConfigJson
		existing.Data = secret.Data
		if existing.Labels == nil {
			existing.Labels = map[string]string{}
		}
		for key, value := range installerLabels() {
			existing.Labels[key] = value
		}
		_, err = client.CoreV1().Secrets(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	}
	return err
}

func dockerConfigJSON(creds quayCredentials) ([]byte, error) {
	auth := base64.StdEncoding.EncodeToString([]byte(creds.Username + ":" + creds.Password))
	payload := map[string]any{
		"auths": map[string]any{
			"quay.io": map[string]string{
				"username": creds.Username,
				"password": creds.Password,
				"email":    creds.Email,
				"auth":     auth,
			},
		},
	}
	return json.Marshal(payload)
}

func tenantAdminPassword(access kubeAccess, namespace string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := kubernetesClient(access)
	if err != nil {
		return "", err
	}

	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, "patchworks-tenant-admin", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	password := string(secret.Data["adminPassword"])
	if password == "" {
		return "", fmt.Errorf("patchworks-tenant-admin secret does not contain adminPassword")
	}
	return password, nil
}

// deleteInstallerPullSecret deletes the pull secret only when this installer
// created it, and returns what it did for the step's trail line.
func deleteInstallerPullSecret(ctx context.Context, access kubeAccess, namespace, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	client, err := kubernetesClient(access)
	if err != nil {
		return "", err
	}

	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return fmt.Sprintf("Pull secret %s not found", name), nil
	}
	if err != nil {
		return "", err
	}
	if secret.Labels[installerLabelKey] != installerLabelValue {
		return fmt.Sprintf("Kept pull secret %s: not created by this installer", name), nil
	}
	if err := client.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return "", err
	}
	return fmt.Sprintf("Removed pull secret %s", name), nil
}

// installPhaseMonitor lists the chart's hook Jobs and Deployments for the
// live install view. It returns nil when the cluster cannot be read, and the
// view keeps its last snapshot.
func installPhaseMonitor(access kubeAccess, namespace, chart string) func() []installComponentRow {
	return func() []installComponentRow {
		rows, err := currentInstallRows(access, namespace, chart)
		if err != nil {
			return nil
		}
		return rows
	}
}

func currentInstallRows(access kubeAccess, namespace, chart string) ([]installComponentRow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := kubernetesClient(access)
	if err != nil {
		return nil, err
	}

	jobs, err := listInstallJobs(ctx, client, namespace)
	if err != nil {
		return nil, err
	}
	deployments, err := listInstallDeployments(ctx, client, namespace, chart)
	if err != nil {
		return nil, err
	}
	return installComponentRows(jobs.Items, deployments.Items, chart), nil
}

func phaseJobs(jobs []batchv1.Job, chart string) []batchv1.Job {
	var filtered []batchv1.Job
	for _, job := range jobs {
		component := job.Labels["app.kubernetes.io/component"]
		if component == "" || !phaseComponentBelongsToChart(component, chart) {
			continue
		}
		filtered = append(filtered, job)
	}
	return filtered
}

func infraPhaseNames() map[string]string {
	return map[string]string{
		"credential-generator": "Generating credentials",
		"mysql-setup":          "Setting up Fabric database",
		"s3-setup":             "Setting up object storage",
	}
}

func appPhaseNames() map[string]string {
	return map[string]string{
		"app-keygen":          "Generating app key",
		"passport-keygen":     "Generating Fabric passport keys",
		"tenant-admin-keygen": "Generating admin user password",
		"workers-storegen":    "Generating Monocore store config",
		"workers-topology":    "Setting up Monocore topology",
		"rabbitmq-topology":   "Setting up RabbitMQ queues",
		"fabric-migrations":   "Running Fabric migrations",
		"fabric-seeds":        "Running Fabric seeders",
		"fabric-company-seed": "Creating Fabric company and admin user",
		"tenant-database":     "Creating Core tenant database",
		"core-migrations":     "Running Core migrations",
		"core-seeds":          "Running Core seeders",
		"samples":             "Creating sample flows",
	}
}

func phaseComponentBelongsToChart(component, chart string) bool {
	_, exists := phaseNamesForChart(chart)[component]
	return exists
}

func phaseNamesForChart(chart string) map[string]string {
	if chart == "infra" {
		return infraPhaseNames()
	}
	if chart == "app" {
		return appPhaseNames()
	}
	return map[string]string{}
}

func installComponentRows(jobs []batchv1.Job, deployments []appsv1.Deployment, chart string) []installComponentRow {
	rows := hookComponentRows(jobs, chart)
	rows = append(rows, deploymentComponentRows(deployments)...)
	sortInstallRows(rows)
	return rows
}

func hookComponentRows(jobs []batchv1.Job, chart string) []installComponentRow {
	latest := map[string]batchv1.Job{}
	for _, job := range phaseJobs(jobs, chart) {
		component := job.Labels["app.kubernetes.io/component"]
		existing, ok := latest[component]
		if !ok || jobLastTransition(job).After(jobLastTransition(existing)) {
			latest[component] = job
		}
	}

	var rows []installComponentRow
	for component, name := range phaseNamesForChart(chart) {
		job, exists := latest[component]
		if !exists {
			rows = append(rows, installComponentRow{Name: name, Status: installStatusWaiting})
			continue
		}
		rows = append(rows, installComponentRow{
			Name:   name,
			Status: jobInstallStatus(job),
			Detail: jobProgressDetail(job),
		})
	}
	return rows
}

func deploymentComponentRows(deployments []appsv1.Deployment) []installComponentRow {
	var rows []installComponentRow
	for _, deployment := range deployments {
		component := deployment.Labels["app.kubernetes.io/component"]
		name := deployment.Name
		if component != "" {
			name = humanizeComponent(component)
		}
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		rows = append(rows, installComponentRow{
			Name:   name,
			Status: deploymentInstallStatus(deployment, desired),
			Detail: fmt.Sprintf("%d/%d ready", deployment.Status.ReadyReplicas, desired),
		})
	}
	return rows
}

// jobInstallStatus treats a Job as failed only once Kubernetes gives up on it.
// A Job that failed an attempt and is retrying is still in progress.
func jobInstallStatus(job batchv1.Job) installComponentStatus {
	if job.Status.Succeeded > 0 {
		return installStatusComplete
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return installStatusFailed
		}
	}
	if job.Status.Active > 0 || job.Status.Failed > 0 {
		return installStatusInProgress
	}
	return installStatusWaiting
}

func deploymentInstallStatus(deployment appsv1.Deployment, desired int32) installComponentStatus {
	if deployment.Status.ReadyReplicas >= desired {
		return installStatusComplete
	}
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentReplicaFailure && condition.Status == corev1.ConditionTrue {
			return installStatusFailed
		}
		if condition.Type == appsv1.DeploymentProgressing && condition.Status == corev1.ConditionFalse {
			return installStatusFailed
		}
	}
	return installStatusInProgress
}

func jobProgressDetail(job batchv1.Job) string {
	switch {
	case job.Status.Failed == 1:
		return "1 failed attempt"
	case job.Status.Failed > 1:
		return fmt.Sprintf("%d failed attempts", job.Status.Failed)
	}
	return ""
}

func jobLastTransition(job batchv1.Job) time.Time {
	latest := job.CreationTimestamp.Time
	for _, condition := range job.Status.Conditions {
		if condition.LastTransitionTime.Time.After(latest) {
			latest = condition.LastTransitionTime.Time
		}
	}
	return latest
}

func listInstallDeployments(ctx context.Context, client kubernetes.Interface, namespace, chart string) (*appsv1.DeploymentList, error) {
	releaseName := "patchworks-" + chart
	deployments, err := client.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		deployments, err = client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, err
	}

	filtered := &appsv1.DeploymentList{}
	for _, deployment := range deployments.Items {
		if deployment.Labels["app.kubernetes.io/instance"] == releaseName {
			filtered.Items = append(filtered.Items, deployment)
		}
	}
	return filtered, nil
}

func humanizeComponent(component string) string {
	component = strings.TrimSpace(component)
	if name, ok := componentDisplayNames()[component]; ok {
		return name
	}
	words := strings.Fields(strings.ReplaceAll(component, "-", " "))
	for i, word := range words {
		if len(word) == 0 {
			continue
		}
		if name, ok := componentDisplayNames()[word]; ok {
			words[i] = name
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func componentDisplayNames() map[string]string {
	return map[string]string{
		"api":           "API",
		"aws":           "AWS",
		"cert":          "cert",
		"cert-manager":  "cert-manager",
		"core":          "Core",
		"dashboard":     "Dashboard",
		"elasticsearch": "ElasticSearch",
		"fabric":        "Fabric",
		"gateway":       "Gateway",
		"kubefaas":      "KubeFaaS",
		"minio":         "MinIO",
		"monocore":      "Monocore",
		"mysql":         "MySQL",
		"oauth":         "OAuth",
		"passport":      "Passport",
		"pwops":         "PW Ops",
		"rabbitmq":      "RabbitMQ",
		"redis":         "Redis",
		"s3":            "S3",
		"s3manager":     "S3 Manager",
		"soketi":        "Soketi",
		"ui":            "UI",
		"url":           "URL",
		"webhook":       "Webhook",
	}
}

func printInstallFailureDiagnostics(access kubeAccess, namespace string) {
	details, err := installFailureDiagnostics(access, namespace)
	if err != nil {
		fmt.Println(warningLine(fmt.Sprintf("Unable to collect install diagnostics: %v", err)))
		return
	}
	if strings.TrimSpace(details) == "" {
		return
	}
	fmt.Println()
	for _, line := range strings.Split(details, "\n") {
		fmt.Println(strings.TrimRight("  "+line, " "))
	}
	printCapturedOutput()
}

func installFailureDiagnostics(access kubeAccess, namespace string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := kubernetesClient(access)
	if err != nil {
		return "", err
	}

	jobs, err := listInstallJobs(ctx, client, namespace)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for _, job := range jobs.Items {
		if job.Status.Failed == 0 && job.Status.Succeeded == 0 {
			continue
		}
		if job.Status.Failed == 0 && !isHookJob(job) {
			continue
		}
		if b.Len() == 0 {
			fmt.Fprintf(&b, "%s\n\n", warningStyle.Render("Install failure diagnostics"))
		}
		fmt.Fprintf(&b, "%s\n", keyValue("Job", job.Name))
		if job.Namespace != namespace {
			fmt.Fprintf(&b, "%s\n", keyValue("Namespace", job.Namespace))
		}
		fmt.Fprintf(&b, "%s\n", keyValue("Status", jobStatusLine(job)))
		if job.Status.Failed > 0 {
			appendJobPodDiagnostics(ctx, client, job.Namespace, job, &b)
		}
		fmt.Fprintln(&b)
	}

	return strings.TrimSpace(b.String()), nil
}

func listInstallJobs(ctx context.Context, client kubernetes.Interface, namespace string) (*batchv1.JobList, error) {
	jobs, err := client.BatchV1().Jobs("").List(ctx, metav1.ListOptions{})
	if err == nil {
		return jobs, nil
	}
	return client.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
}

func isHookJob(job batchv1.Job) bool {
	annotations := job.Annotations
	if annotations["helm.sh/hook"] != "" || annotations["argocd.argoproj.io/hook"] != "" {
		return true
	}
	return strings.Contains(job.Labels["app.kubernetes.io/component"], "topology")
}

func jobStatusLine(job batchv1.Job) string {
	parts := []string{
		fmt.Sprintf("active=%d", job.Status.Active),
		fmt.Sprintf("succeeded=%d", job.Status.Succeeded),
		fmt.Sprintf("failed=%d", job.Status.Failed),
	}
	for _, condition := range job.Status.Conditions {
		if condition.Status == corev1.ConditionTrue {
			parts = append(parts, fmt.Sprintf("%s: %s", condition.Type, condition.Message))
		}
	}
	return strings.Join(parts, ", ")
}

func appendJobPodDiagnostics(ctx context.Context, client kubernetes.Interface, namespace string, job batchv1.Job, b *strings.Builder) {
	pods, err := podsForJob(ctx, client, namespace, job)
	if err != nil {
		fmt.Fprintf(b, "%s %v\n", keyValue("Pod lookup", "failed"), err)
		return
	}
	if len(pods.Items) == 0 {
		fmt.Fprintf(b, "%s\n", keyValue("Pods", "none found"))
		appendEvents(ctx, client, namespace, "Job", job.Name, b)
		return
	}
	for _, pod := range pods.Items {
		fmt.Fprintf(b, "%s\n", keyValue("Pod", fmt.Sprintf("%s (%s)", pod.Name, pod.Status.Phase)))
		appendEvents(ctx, client, namespace, "Pod", pod.Name, b)
		for _, status := range pod.Status.ContainerStatuses {
			fmt.Fprintf(b, "%s\n", keyValue("Container", containerStatusLine(status)))
			appendContainerLogs(ctx, client, namespace, pod.Name, status.Name, b)
		}
	}
}

func podsForJob(ctx context.Context, client kubernetes.Interface, namespace string, job batchv1.Job) (*corev1.PodList, error) {
	allPods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	pods := &corev1.PodList{}
	for _, pod := range allPods.Items {
		if pod.Labels["job-name"] == job.Name || pod.Labels["batch.kubernetes.io/job-name"] == job.Name || hasOwnerReference(pod.OwnerReferences, job.UID) {
			pods.Items = append(pods.Items, pod)
		}
	}
	return pods, nil
}

func hasOwnerReference(refs []metav1.OwnerReference, uid types.UID) bool {
	for _, ref := range refs {
		if ref.UID == uid {
			return true
		}
	}
	return false
}

func appendEvents(ctx context.Context, client kubernetes.Interface, namespace, kind, name string, b *strings.Builder) {
	events, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.kind=%s,involvedObject.name=%s", kind, name),
	})
	if err != nil || len(events.Items) == 0 {
		return
	}
	sort.Slice(events.Items, func(i, j int) bool {
		return events.Items[i].LastTimestamp.Time.Before(events.Items[j].LastTimestamp.Time)
	})
	fmt.Fprintf(b, "%s\n", keyStyle.Render(kind+" events"))
	start := 0
	if len(events.Items) > 5 {
		start = len(events.Items) - 5
	}
	for _, event := range events.Items[start:] {
		fmt.Fprintf(b, "- %s: %s\n", event.Reason, event.Message)
	}
}

func appendContainerLogs(ctx context.Context, client kubernetes.Interface, namespace, podName, containerName string, b *strings.Builder) {
	current, currentErr := podLogs(ctx, client, namespace, podName, containerName, false)
	previous, previousErr := podLogs(ctx, client, namespace, podName, containerName, true)

	if strings.TrimSpace(previous) != "" {
		fmt.Fprintf(b, "%s\n%s\n", keyStyle.Render("Previous logs"), sanitizeDiagnosticLogs(previous))
	}
	if strings.TrimSpace(current) != "" {
		fmt.Fprintf(b, "%s\n%s\n", keyStyle.Render("Logs"), sanitizeDiagnosticLogs(current))
	}
	if strings.TrimSpace(previous) == "" && strings.TrimSpace(current) == "" {
		errText := firstErrorText(previousErr, currentErr)
		if errText == "" {
			errText = "empty"
		}
		fmt.Fprintf(b, "%s %s\n", keyValue("Logs", "unavailable"), errText)
	}
}

func sanitizeDiagnosticLogs(logs string) string {
	var sanitized []string
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "client secret:"):
			sanitized = append(sanitized, replaceAfterColon(line, "[redacted]"))
		case strings.Contains(lower, "password="), strings.Contains(lower, "password:"):
			sanitized = append(sanitized, redactAfterSensitiveToken(line, "password"))
		case strings.Contains(lower, "secret="), strings.Contains(lower, "secret:"):
			sanitized = append(sanitized, redactAfterSensitiveToken(line, "secret"))
		case strings.Contains(lower, "token="), strings.Contains(lower, "token:"):
			sanitized = append(sanitized, redactAfterSensitiveToken(line, "token"))
		default:
			sanitized = append(sanitized, line)
		}
	}
	return strings.Join(sanitized, "\n")
}

func replaceAfterColon(line, replacement string) string {
	if idx := strings.Index(line, ":"); idx >= 0 {
		return line[:idx+1] + " " + replacement
	}
	return replacement
}

func redactAfterSensitiveToken(line, token string) string {
	lower := strings.ToLower(line)
	for _, sep := range []string{token + "=", token + ":"} {
		idx := strings.Index(lower, sep)
		if idx >= 0 {
			return line[:idx+len(sep)] + "[redacted]"
		}
	}
	return line
}

func containerStatusLine(status corev1.ContainerStatus) string {
	parts := []string{status.Name}
	if status.State.Waiting != nil {
		parts = append(parts, "waiting", status.State.Waiting.Reason, status.State.Waiting.Message)
	}
	if status.State.Terminated != nil {
		parts = append(parts, "terminated", fmt.Sprintf("exit=%d", status.State.Terminated.ExitCode), status.State.Terminated.Reason, status.State.Terminated.Message)
	}
	if status.LastTerminationState.Terminated != nil {
		parts = append(parts, "last terminated", fmt.Sprintf("exit=%d", status.LastTerminationState.Terminated.ExitCode), status.LastTerminationState.Terminated.Reason, status.LastTerminationState.Terminated.Message)
	}
	return strings.Join(nonEmpty(parts), ", ")
}

func podLogs(ctx context.Context, client kubernetes.Interface, namespace, podName, containerName string, previous bool) (string, error) {
	tailLines := int64(120)
	req := client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tailLines,
		Previous:  previous,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()

	var b bytes.Buffer
	if _, err := io.Copy(&b, stream); err != nil {
		return "", err
	}
	return b.String(), nil
}

func firstErrorText(errs ...error) string {
	for _, err := range errs {
		if err != nil {
			return err.Error()
		}
	}
	return ""
}

func nonEmpty(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func workloadStatus(access kubeAccess, namespace string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := kubernetesClient(access)
	if err != nil {
		return "", err
	}

	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	jobs, err := client.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	return workloadStatusLines(namespace, deployments.Items, pods.Items, jobs.Items), nil
}

// workloadStatusLines is the trail line for the namespace's workloads after
// an install, followed by anything that needs attention.
func workloadStatusLines(namespace string, deployments []appsv1.Deployment, pods []corev1.Pod, jobs []batchv1.Job) string {
	var problems []string
	if names := notReadyDeploymentNames(deployments); len(names) > 0 {
		problems = append(problems, "Not ready: "+strings.Join(names, ", "))
	}
	if names := problemPodNames(pods); len(names) > 0 {
		problems = append(problems, "Pods needing attention: "+strings.Join(names, ", "))
	}
	if names := failedJobNames(jobs); len(names) > 0 {
		problems = append(problems, "Failed jobs: "+strings.Join(names, ", "))
	}

	headline := fmt.Sprintf("Workloads in %s · %s · pods: %s", namespace, deploymentSummary(deployments), podSummary(pods))
	lines := []string{doneLine(headline, 0)}
	if len(problems) > 0 {
		lines[0] = warningStyle.Render("!") + " " + headline
	}
	for _, problem := range problems {
		lines = append(lines, warningLine(problem))
	}
	return strings.Join(lines, "\n")
}

func deploymentSummary(deployments []appsv1.Deployment) string {
	var ready, desired int32
	for _, deployment := range deployments {
		ready += deployment.Status.ReadyReplicas
		replicas := int32(1)
		if deployment.Spec.Replicas != nil {
			replicas = *deployment.Spec.Replicas
		}
		desired += replicas
	}
	return fmt.Sprintf("%d/%d replicas ready", ready, desired)
}

func podSummary(pods []corev1.Pod) string {
	counts := map[corev1.PodPhase]int{}
	for _, pod := range pods {
		counts[pod.Status.Phase]++
	}
	parts := []string{}
	for _, phase := range []corev1.PodPhase{corev1.PodRunning, corev1.PodPending, corev1.PodSucceeded, corev1.PodFailed, corev1.PodUnknown} {
		if counts[phase] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[phase], strings.ToLower(string(phase))))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func notReadyDeploymentNames(deployments []appsv1.Deployment) []string {
	names := []string{}
	for _, deployment := range deployments {
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		if deployment.Status.ReadyReplicas < desired {
			names = append(names, fmt.Sprintf("%s (%d/%d)", deployment.Name, deployment.Status.ReadyReplicas, desired))
		}
	}
	sort.Strings(names)
	return names
}

func problemPodNames(pods []corev1.Pod) []string {
	names := []string{}
	for _, pod := range pods {
		switch pod.Status.Phase {
		case corev1.PodFailed, corev1.PodUnknown:
			names = append(names, fmt.Sprintf("%s (%s)", pod.Name, pod.Status.Phase))
		case corev1.PodPending:
			names = append(names, fmt.Sprintf("%s (%s)", pod.Name, podPendingReason(pod)))
		}
	}
	sort.Strings(names)
	return names
}

func podPendingReason(pod corev1.Pod) string {
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil && status.State.Waiting.Reason != "" {
			return status.State.Waiting.Reason
		}
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if status.State.Waiting != nil && status.State.Waiting.Reason != "" {
			return status.State.Waiting.Reason
		}
	}
	return string(pod.Status.Phase)
}

func failedJobNames(jobs []batchv1.Job) []string {
	names := []string{}
	for _, job := range jobs {
		if jobInstallStatus(job) == installStatusFailed {
			names = append(names, fmt.Sprintf("%s (%d failed)", job.Name, job.Status.Failed))
		}
	}
	sort.Strings(names)
	return names
}

func deferredClientConfig(kubeconfigPath, contextName string) clientcmd.ClientConfig {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	if strings.Contains(kubeconfigPath, string(os.PathListSeparator)) {
		loadingRules = &clientcmd.ClientConfigLoadingRules{Precedence: filepath.SplitList(kubeconfigPath)}
	}

	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
}

func kubernetesClient(access kubeAccess) (kubernetes.Interface, error) {
	restConfig, err := deferredClientConfig(access.Kubeconfig, access.Context).ClientConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restConfig)
}

func runSettingsForm(info clusterInfo, kubeconfig, contextName string, defaults localConfig) (installConfig, error) {
	ingressProvider := "contour"
	if !info.HasContour && len(info.IngressClasses) > 0 {
		ingressProvider = "other"
	}
	ingressProvider = stringDefault(defaults.IngressProvider, ingressProvider)

	ingressClass := "contour"
	if len(info.IngressClasses) > 0 {
		ingressClass = info.IngressClasses[0]
	}
	ingressClass = stringDefault(defaults.IngressClass, ingressClass)

	namespace := stringDefault(defaults.Namespace, "patchworks")
	domain := stringDefault(defaults.Domain, "selfhosted.example.com")
	scheme := stringDefault(defaults.Scheme, "https")
	dashboardEnabled := boolDefault(defaults.DashboardEnabled, true)
	routingMode := stringDefault(defaults.RoutingMode, "host")
	workerMode := defaultWorkerMode(defaults.WorkerMode)
	profile := defaultProfile(defaults.Profile)
	infrastructureMode := stringDefault(defaults.Infrastructure, "bundled")
	kubefaas := boolDefault(defaults.InfraComponents.KubeFaaS.Enabled, false)
	kubefaasRegistry := boolDefault(defaults.InfraComponents.KubeFaaSRegistry.Enabled, true)
	if err := runFormWithQuitConfirm(huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Cluster inspection").
				Description(clusterSummaryCompact(info)),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Kubernetes namespace").
				Value(&namespace).
				CharLimit(128),
			huh.NewInput().
				Title("Base domain").
				Description("Example: selfhosted.example.com").
				Value(&domain).
				CharLimit(253),
			huh.NewSelect[string]().
				Title("Public URL scheme").
				Options(huh.NewOption("https", "https"), huh.NewOption("http", "http")).
				Value(&scheme),
			huh.NewConfirm().
				Title("Enable dashboard").
				Affirmative("Yes").
				Negative("No").
				Value(&dashboardEnabled),
			huh.NewSelect[string]().
				Title("Dashboard routing mode").
				Description("host uses dedicated service hosts; path uses dashboard /core-main, /core-start, /fabric routes").
				Options(huh.NewOption("host", "host"), huh.NewOption("path", "path")).
				Value(&routingMode),
			huh.NewSelect[string]().
				Title("Worker mode").
				Description("standalone runs PHP Core workers; microservice runs per-queue PHP workers; mono uses Monocore.").
				Options(
					huh.NewOption("standalone", "standalone"),
					huh.NewOption("microservice", "microservice"),
					huh.NewOption("mono", "mono"),
				).
				Value(&workerMode),
			huh.NewSelect[string]().
				Title("Installation size").
				Description("standard runs each Core service in its own pods; lightweight runs Core in one web pod and one worker pod, for small installations.").
				Options(
					huh.NewOption("standard", "standard"),
					huh.NewOption("lightweight", "lightweight"),
				).
				Value(&profile),
			huh.NewSelect[string]().
				Title("Infrastructure").
				Description("Use the bundled MySQL, Redis, RabbitMQ, ElasticSearch, S3, and Soketi stack, or connect to services you manage separately.").
				Options(
					huh.NewOption("Install bundled infrastructure", "bundled"),
					huh.NewOption("Use my own infrastructure", "external"),
				).
				Value(&infrastructureMode),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("KubeFaaS").
				Description("Run Script steps: a controller, a builder that turns each script into a container image, and Redis, in the kubefaas namespace. Adds roughly 1 to 2.5 GiB of memory, and the first install builds a base image for every script language.").
				Affirmative("Yes").
				Negative("No").
				Value(&kubefaas),
		).WithHideFunc(func() bool { return infrastructureMode != "bundled" }),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Bundled image registry").
				Description(kubefaasRegistryDescription(info.Nodes)).
				Affirmative("Yes").
				Negative("No").
				Value(&kubefaasRegistry),
		).WithHideFunc(func() bool { return infrastructureMode != "bundled" || !kubefaas }),
	).WithTheme(installerTheme()).WithWidth(88)); err != nil {
		return installConfig{}, err
	}

	external := defaults.External
	if infrastructureMode == "external" {
		var err error
		external, err = runExternalInfrastructureForm(defaults.External)
		if err != nil {
			return installConfig{}, err
		}
	}

	info.QuaySecretPresent = secretExistsInNamespace(kubeconfig, contextName, namespace, "quay-credentials")
	pullSecret, quay, err := runPullSecretForm(info.QuaySecretPresent, defaults)
	if err != nil {
		return installConfig{}, err
	}

	ingressEnabled := boolDefault(defaults.IngressEnabled, true)
	monoInbound := boolDefault(defaults.MonoInbound, false)
	samples := boolDefault(defaults.Samples, false)
	monoInboundRoutes := boolDefault(defaults.MonoInboundRoutes, false)
	cookieDomain := stringDefault(defaults.CookieDomain, defaultCookieDomainForRouting(domain, routingMode))
	licenseKey := defaults.LicenseKey
	licenseServerURL := stringDefault(defaults.LicenseServerURL, defaultLicenseServerURL)
	seedInstall := boolDefault(defaults.SeedInstall, true)
	companyName := stringDefault(defaults.CompanyName, "Example Company")
	adminName := stringDefault(defaults.AdminName, "Admin User")
	adminEmail := stringDefault(defaults.AdminEmail, "admin@example.com")
	adminPassword := defaults.AdminPassword
	userRole := stringDefault(defaults.UserRole, "superadmin")
	output := stringDefault(defaults.Output, "patchworks.values.yaml")

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable ingress").
				Affirmative("Yes").
				Negative("No").
				Value(&ingressEnabled),
			huh.NewSelect[string]().
				Title("Ingress provider").
				Options(huh.NewOption("contour", "contour"), huh.NewOption("nginx", "nginx"), huh.NewOption("other", "other")).
				Value(&ingressProvider),
			huh.NewInput().
				Title("Ingress class name").
				Value(&ingressClass).
				CharLimit(128),
			huh.NewInput().
				Title("Cookie/session domain").
				Description("Derived from base domain and dashboard routing mode by default. Leave blank to derive it again.").
				Value(&cookieDomain).
				CharLimit(253),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Serve inbound triggers from Monocore").
				Description("Run webhooks, callbacks and the start API on the Monocore hub instead of Core's start service. Core still serves the companies it runs. Needs a Patchworks release later than v"+lastMonocoreWithoutInbound+".").
				Affirmative("Yes").
				Negative("No").
				Value(&monoInbound),
		).WithHideFunc(func() bool { return workerMode != "mono" }),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Route inbound paths to Monocore").
				Description("Send the webhook, callback and start API paths to Monocore through the ingress. A new installation can answer Yes. When adding inbound triggers to a running installation, answer No, then Yes on a later run once the Monocore hub has restarted: until then nothing would answer them.").
				Affirmative("Yes").
				Negative("No").
				Value(&monoInboundRoutes),
		).WithHideFunc(func() bool { return workerMode != "mono" || !monoInbound || !ingressEnabled }),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Samples").
				Description("Install sample flows: one per shape and one that uses every shape, in a Samples folder of the initial company. Each ends in an Assert on its exact output, so running one checks the shape works. They are deployed with no schedule. Turns on Monocore's optional shapes, and needs the initial company/admin.").
				Affirmative("Yes").
				Negative("No").
				Value(&samples),
		).WithHideFunc(func() bool { return workerMode != "mono" }),
		huh.NewGroup(
			huh.NewInput().
				Title("License key").
				EchoMode(huh.EchoModePassword).
				Value(&licenseKey).
				CharLimit(2048).
				Validate(required("license key")),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Create initial company/admin").
				Affirmative("Yes").
				Negative("No").
				Value(&seedInstall),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Initial company name").
				Value(&companyName).
				CharLimit(256),
			huh.NewInput().
				Title("Initial admin name").
				Value(&adminName).
				CharLimit(256),
			huh.NewInput().
				Title("Initial admin email").
				Value(&adminEmail).
				CharLimit(320),
			huh.NewInput().
				Title("Initial admin password").
				Description("Leave blank to let the chart generate a stable password Secret.").
				EchoMode(huh.EchoModePassword).
				Value(&adminPassword).
				CharLimit(1024),
			huh.NewInput().
				Title("Initial admin role").
				Value(&userRole).
				CharLimit(128),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Output values file").
				Value(&output).
				CharLimit(512),
		),
	).WithTheme(installerTheme()).WithWidth(88)

	if err := runFormWithQuitConfirm(form); err != nil {
		return installConfig{}, err
	}

	if cookieDomain == "" {
		cookieDomain = defaultCookieDomainForRouting(domain, routingMode)
	}

	values := map[string]string{
		"kubeconfig":        kubeconfig,
		"context":           contextName,
		"namespace":         namespace,
		"infrastructure":    infrastructureMode,
		"domain":            domain,
		"scheme":            scheme,
		"licenseKey":        licenseKey,
		"licenseServerUrl":  licenseServerURL,
		"ingressEnabled":    fmt.Sprintf("%t", ingressEnabled),
		"ingressProvider":   ingressProvider,
		"ingressClass":      ingressClass,
		"dashboardEnabled":  fmt.Sprintf("%t", dashboardEnabled),
		"routingMode":       routingMode,
		"workerMode":        workerMode,
		"profile":           profile,
		"monoInbound":       fmt.Sprintf("%t", workerMode == "mono" && monoInbound),
		"monoInboundRoutes": fmt.Sprintf("%t", workerMode == "mono" && monoInbound && ingressEnabled && monoInboundRoutes),
		"samples":           fmt.Sprintf("%t", workerMode == "mono" && samples),
		"cookieDomain":      cookieDomain,
		"pullSecretMode":    pullSecretModeFromSelection(pullSecret, quay),
		"pullSecret":        pullSecret,
		"seedInstall":       fmt.Sprintf("%t", seedInstall),
		"companyName":       companyName,
		"adminName":         adminName,
		"adminEmail":        adminEmail,
		"adminPassword":     adminPassword,
		"userRole":          userRole,
		"confirmKubeAccess": "true",
	}
	addExternalValues(values, external)
	addInfrastructureComponentValues(values, defaults.InfraComponents)
	addKubeFaaSValues(values, infrastructureMode, kubefaas, kubefaasRegistry, info.Nodes)

	return installConfig{Values: values, Output: output, Quay: quay}, nil
}

func runPullSecretForm(quaySecretPresent bool, defaults localConfig) (string, *quayCredentials, error) {
	mode := "create"
	if quaySecretPresent {
		mode = "existing"
	}
	mode = stringDefault(defaults.PullSecretMode, mode)
	secretName := stringDefault(defaults.PullSecret, "quay-credentials")

	if err := runFormWithQuitConfirm(huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Image pull secret").
				Description("Use an existing secret, create one from Quay credentials, or skip if the cluster can already pull quay.io/patchworks images.").
				Options(
					huh.NewOption("Use existing secret", "existing"),
					huh.NewOption("Create from Quay credentials", "create"),
					huh.NewOption("No pull secret", "none"),
				).
				Value(&mode),
			huh.NewInput().
				Title("Image pull secret name").
				Description("Used for the generated values file and for secret creation when selected.").
				Value(&secretName).
				CharLimit(253),
		),
	).WithTheme(installerTheme()).WithWidth(88)); err != nil {
		return "", nil, err
	}

	switch mode {
	case "none":
		return "", nil, nil
	case "existing":
		if strings.TrimSpace(secretName) == "" {
			return "", nil, fmt.Errorf("image pull secret name is required when using an existing secret")
		}
		return secretName, nil, nil
	}
	if strings.TrimSpace(secretName) == "" {
		return "", nil, fmt.Errorf("image pull secret name is required when creating a secret")
	}

	creds := &quayCredentials{
		SecretName: secretName,
		Username:   defaults.QuayUsername,
		Password:   defaults.QuayPassword,
		Email:      stringDefault(defaults.QuayEmail, "unused@example.com"),
	}
	if err := runFormWithQuitConfirm(huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Quay username").
				Description("For robot accounts this is usually patchworks+name.").
				Value(&creds.Username).
				CharLimit(256).
				Validate(required("quay username")),
			huh.NewInput().
				Title("Quay password/token").
				EchoMode(huh.EchoModePassword).
				Value(&creds.Password).
				CharLimit(1024).
				Validate(required("quay password/token")),
			huh.NewInput().
				Title("Docker config email").
				Description("Docker registry secrets require this field; it is not used for auth by Quay.").
				Value(&creds.Email).
				CharLimit(320).
				Validate(required("docker config email")),
		),
	).WithTheme(installerTheme()).WithWidth(88)); err != nil {
		return "", nil, err
	}

	return secretName, creds, nil
}

func runExternalInfrastructureForm(defaults localConfigExternal) (localConfigExternal, error) {
	external := defaults
	external.CredentialsMode = stringDefault(external.CredentialsMode, "secret")

	external.MySQL.Host = stringDefault(external.MySQL.Host, "mysql.example.com")
	external.MySQL.Port = stringDefault(external.MySQL.Port, "3306")
	external.MySQL.Database = stringDefault(external.MySQL.Database, "core")
	external.MySQL.Username = stringDefault(external.MySQL.Username, "patchworks")
	external.MySQL.PasswordKey = stringDefault(external.MySQL.PasswordKey, "password")

	external.FabricMySQL.Host = stringDefault(external.FabricMySQL.Host, external.MySQL.Host)
	external.FabricMySQL.Port = stringDefault(external.FabricMySQL.Port, external.MySQL.Port)
	external.FabricMySQL.Database = stringDefault(external.FabricMySQL.Database, "fabric")
	external.FabricMySQL.Username = stringDefault(external.FabricMySQL.Username, external.MySQL.Username)
	external.FabricMySQL.PasswordKey = stringDefault(external.FabricMySQL.PasswordKey, "password")

	external.Redis.Host = stringDefault(external.Redis.Host, "redis.example.com")
	external.Redis.Port = stringDefault(external.Redis.Port, "6379")
	external.Redis.PasswordKey = stringDefault(external.Redis.PasswordKey, "password")

	external.RabbitMQ.Host = stringDefault(external.RabbitMQ.Host, "rabbitmq.example.com")
	external.RabbitMQ.Port = stringDefault(external.RabbitMQ.Port, "5672")
	external.RabbitMQ.Username = stringDefault(external.RabbitMQ.Username, "patchworks")
	external.RabbitMQ.Vhost = stringDefault(external.RabbitMQ.Vhost, "/")
	external.RabbitMQ.PasswordKey = stringDefault(external.RabbitMQ.PasswordKey, "password")

	external.ElasticSearch.Host = stringDefault(external.ElasticSearch.Host, "elasticsearch.example.com")
	external.ElasticSearch.Port = stringDefault(external.ElasticSearch.Port, "9200")
	external.ElasticSearch.Scheme = stringDefault(external.ElasticSearch.Scheme, "https")
	external.ElasticSearch.UsernameKey = stringDefault(external.ElasticSearch.UsernameKey, "username")
	external.ElasticSearch.PasswordKey = stringDefault(external.ElasticSearch.PasswordKey, "password")

	external.S3.Endpoint = stringDefault(external.S3.Endpoint, "https://s3.amazonaws.com")
	external.S3.Region = stringDefault(external.S3.Region, "eu-west-2")
	external.S3.Bucket = stringDefault(external.S3.Bucket, "patchworks")
	external.S3.AccessKeyKey = stringDefault(external.S3.AccessKeyKey, "access-key")
	external.S3.SecretKeyKey = stringDefault(external.S3.SecretKeyKey, "secret-key")
	s3PathStyle := boolDefault(external.S3.PathStyle, false)

	external.Pusher.Host = stringDefault(external.Pusher.Host, "wss.example.com")
	external.Pusher.Port = stringDefault(external.Pusher.Port, "443")
	external.Pusher.Scheme = stringDefault(external.Pusher.Scheme, "https")
	external.Pusher.AppCluster = stringDefault(external.Pusher.AppCluster, "mt1")
	external.Pusher.AppIDKey = stringDefault(external.Pusher.AppIDKey, "app-id")
	external.Pusher.AppKeyKey = stringDefault(external.Pusher.AppKeyKey, "app-key")
	external.Pusher.AppSecretKey = stringDefault(external.Pusher.AppSecretKey, "app-secret")
	external.Pusher.AppClusterKey = stringDefault(external.Pusher.AppClusterKey, "app-cluster")

	if err := runFormWithQuitConfirm(huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("External credential source").
				Description("Plaintext writes passwords into the generated values file. Existing Secrets only writes secret names and keys.").
				Options(
					huh.NewOption("Existing Kubernetes Secrets", "secret"),
					huh.NewOption("Plaintext values", "plaintext"),
				).
				Value(&external.CredentialsMode),
		),
		huh.NewGroup(
			huh.NewInput().Title("MySQL host").Value(&external.MySQL.Host).CharLimit(253).Validate(required("mysql host")),
			huh.NewInput().Title("MySQL port").Value(&external.MySQL.Port).CharLimit(16).Validate(required("mysql port")),
			huh.NewInput().Title("Core database").Value(&external.MySQL.Database).CharLimit(128).Validate(required("core database")),
			huh.NewInput().Title("MySQL username").Value(&external.MySQL.Username).CharLimit(128).Validate(required("mysql username")),
			huh.NewInput().Title("Fabric MySQL host").Description("Use the same host as MySQL unless Fabric has a separate instance.").Value(&external.FabricMySQL.Host).CharLimit(253).Validate(required("fabric mysql host")),
			huh.NewInput().Title("Fabric MySQL port").Value(&external.FabricMySQL.Port).CharLimit(16).Validate(required("fabric mysql port")),
			huh.NewInput().Title("Fabric database").Value(&external.FabricMySQL.Database).CharLimit(128).Validate(required("fabric database")),
			huh.NewInput().Title("Fabric MySQL username").Value(&external.FabricMySQL.Username).CharLimit(128).Validate(required("fabric mysql username")),
		),
		huh.NewGroup(
			huh.NewInput().Title("Redis host").Value(&external.Redis.Host).CharLimit(253).Validate(required("redis host")),
			huh.NewInput().Title("Redis port").Value(&external.Redis.Port).CharLimit(16).Validate(required("redis port")),
			huh.NewInput().Title("RabbitMQ host").Value(&external.RabbitMQ.Host).CharLimit(253).Validate(required("rabbitmq host")),
			huh.NewInput().Title("RabbitMQ port").Value(&external.RabbitMQ.Port).CharLimit(16).Validate(required("rabbitmq port")),
			huh.NewInput().Title("RabbitMQ username").Value(&external.RabbitMQ.Username).CharLimit(128).Validate(required("rabbitmq username")),
			huh.NewInput().Title("RabbitMQ vhost").Value(&external.RabbitMQ.Vhost).CharLimit(128).Validate(required("rabbitmq vhost")),
		),
		huh.NewGroup(
			huh.NewInput().Title("ElasticSearch host").Value(&external.ElasticSearch.Host).CharLimit(253).Validate(required("elasticsearch host")),
			huh.NewInput().Title("ElasticSearch port").Value(&external.ElasticSearch.Port).CharLimit(16).Validate(required("elasticsearch port")),
			huh.NewSelect[string]().Title("ElasticSearch scheme").Options(huh.NewOption("https", "https"), huh.NewOption("http", "http")).Value(&external.ElasticSearch.Scheme),
			huh.NewInput().Title("ElasticSearch username").Value(&external.ElasticSearch.Username).CharLimit(128),
			huh.NewInput().Title("S3 endpoint").Value(&external.S3.Endpoint).CharLimit(512).Validate(required("s3 endpoint")),
			huh.NewInput().Title("S3 region").Value(&external.S3.Region).CharLimit(64).Validate(required("s3 region")),
			huh.NewInput().Title("S3 bucket").Value(&external.S3.Bucket).CharLimit(256).Validate(required("s3 bucket")),
			huh.NewConfirm().Title("Use S3 path-style URLs").Affirmative("Yes").Negative("No").Value(&s3PathStyle),
		),
		huh.NewGroup(
			huh.NewInput().Title("Pusher/Soketi host").Description("Public WebSocket host, without scheme.").Value(&external.Pusher.Host).CharLimit(253).Validate(required("pusher host")),
			huh.NewInput().Title("Pusher/Soketi port").Value(&external.Pusher.Port).CharLimit(16).Validate(required("pusher port")),
			huh.NewSelect[string]().Title("Pusher/Soketi scheme").Options(huh.NewOption("https", "https"), huh.NewOption("http", "http")).Value(&external.Pusher.Scheme),
			huh.NewInput().Title("Pusher app cluster").Value(&external.Pusher.AppCluster).CharLimit(64).Validate(required("pusher app cluster")),
		),
	).WithTheme(installerTheme()).WithWidth(88)); err != nil {
		return localConfigExternal{}, err
	}
	external.S3.PathStyle = boolPtr(s3PathStyle)

	if external.CredentialsMode == "secret" {
		if err := runExternalSecretForm(&external); err != nil {
			return localConfigExternal{}, err
		}
		return external, nil
	}
	if err := runExternalPlaintextSecretForm(&external); err != nil {
		return localConfigExternal{}, err
	}
	return external, nil
}

func runExternalSecretForm(external *localConfigExternal) error {
	external.MySQL.SecretName = stringDefault(external.MySQL.SecretName, "patchworks-db")
	external.FabricMySQL.SecretName = stringDefault(external.FabricMySQL.SecretName, external.MySQL.SecretName)
	external.Redis.SecretName = stringDefault(external.Redis.SecretName, "patchworks-redis")
	external.RabbitMQ.SecretName = stringDefault(external.RabbitMQ.SecretName, "patchworks-rabbitmq")
	external.ElasticSearch.SecretName = stringDefault(external.ElasticSearch.SecretName, "patchworks-elasticsearch")
	external.S3.SecretName = stringDefault(external.S3.SecretName, "patchworks-s3")
	external.Pusher.SecretName = stringDefault(external.Pusher.SecretName, "patchworks-soketi-auth")

	return runFormWithQuitConfirm(huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("MySQL password Secret").Value(&external.MySQL.SecretName).CharLimit(253).Validate(required("mysql password secret")),
			huh.NewInput().Title("MySQL password key").Value(&external.MySQL.PasswordKey).CharLimit(253).Validate(required("mysql password key")),
			huh.NewInput().Title("Fabric MySQL password Secret").Value(&external.FabricMySQL.SecretName).CharLimit(253).Validate(required("fabric mysql password secret")),
			huh.NewInput().Title("Fabric MySQL password key").Value(&external.FabricMySQL.PasswordKey).CharLimit(253).Validate(required("fabric mysql password key")),
			huh.NewInput().Title("Redis password Secret").Value(&external.Redis.SecretName).CharLimit(253),
			huh.NewInput().Title("Redis password key").Value(&external.Redis.PasswordKey).CharLimit(253),
		),
		huh.NewGroup(
			huh.NewInput().Title("RabbitMQ password Secret").Value(&external.RabbitMQ.SecretName).CharLimit(253).Validate(required("rabbitmq password secret")),
			huh.NewInput().Title("RabbitMQ password key").Value(&external.RabbitMQ.PasswordKey).CharLimit(253).Validate(required("rabbitmq password key")),
			huh.NewInput().Title("ElasticSearch credential Secret").Value(&external.ElasticSearch.SecretName).CharLimit(253),
			huh.NewInput().Title("ElasticSearch username key").Value(&external.ElasticSearch.UsernameKey).CharLimit(253),
			huh.NewInput().Title("ElasticSearch password key").Value(&external.ElasticSearch.PasswordKey).CharLimit(253),
		),
		huh.NewGroup(
			huh.NewInput().Title("S3 credential Secret").Value(&external.S3.SecretName).CharLimit(253).Validate(required("s3 credential secret")),
			huh.NewInput().Title("S3 access key field").Value(&external.S3.AccessKeyKey).CharLimit(253).Validate(required("s3 access key field")),
			huh.NewInput().Title("S3 secret key field").Value(&external.S3.SecretKeyKey).CharLimit(253).Validate(required("s3 secret key field")),
			huh.NewInput().Title("Pusher credential Secret").Value(&external.Pusher.SecretName).CharLimit(253).Validate(required("pusher credential secret")),
			huh.NewInput().Title("Pusher app id key").Value(&external.Pusher.AppIDKey).CharLimit(253).Validate(required("pusher app id key")),
			huh.NewInput().Title("Pusher app key key").Value(&external.Pusher.AppKeyKey).CharLimit(253).Validate(required("pusher app key key")),
			huh.NewInput().Title("Pusher app secret key").Value(&external.Pusher.AppSecretKey).CharLimit(253).Validate(required("pusher app secret key")),
			huh.NewInput().Title("Pusher app cluster key").Value(&external.Pusher.AppClusterKey).CharLimit(253).Validate(required("pusher app cluster key")),
		),
	).WithTheme(installerTheme()).WithWidth(88))
}

func runExternalPlaintextSecretForm(external *localConfigExternal) error {
	return runFormWithQuitConfirm(huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("MySQL password").EchoMode(huh.EchoModePassword).Value(&external.MySQL.Password).CharLimit(1024).Validate(required("mysql password")),
			huh.NewInput().Title("Fabric MySQL password").EchoMode(huh.EchoModePassword).Value(&external.FabricMySQL.Password).CharLimit(1024).Validate(required("fabric mysql password")),
			huh.NewInput().Title("Redis password").EchoMode(huh.EchoModePassword).Value(&external.Redis.Password).CharLimit(1024),
			huh.NewInput().Title("RabbitMQ password").EchoMode(huh.EchoModePassword).Value(&external.RabbitMQ.Password).CharLimit(1024).Validate(required("rabbitmq password")),
			huh.NewInput().Title("ElasticSearch password").EchoMode(huh.EchoModePassword).Value(&external.ElasticSearch.Password).CharLimit(1024),
			huh.NewInput().Title("S3 access key").EchoMode(huh.EchoModePassword).Value(&external.S3.AccessKey).CharLimit(1024).Validate(required("s3 access key")),
			huh.NewInput().Title("S3 secret key").EchoMode(huh.EchoModePassword).Value(&external.S3.SecretKey).CharLimit(1024).Validate(required("s3 secret key")),
			huh.NewInput().Title("Pusher app id").EchoMode(huh.EchoModePassword).Value(&external.Pusher.AppID).CharLimit(1024).Validate(required("pusher app id")),
			huh.NewInput().Title("Pusher app key").EchoMode(huh.EchoModePassword).Value(&external.Pusher.AppKey).CharLimit(1024).Validate(required("pusher app key")),
			huh.NewInput().Title("Pusher app secret").EchoMode(huh.EchoModePassword).Value(&external.Pusher.AppSecret).CharLimit(1024).Validate(required("pusher app secret")),
		),
	).WithTheme(installerTheme()).WithWidth(88))
}

func required(label string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", label)
		}
		return nil
	}
}

func loadLocalConfig(path string) (localConfig, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return localConfig{}, false, nil
	}
	if err != nil {
		return localConfig{}, false, err
	}

	var config localConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return localConfig{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	return config, true, nil
}

func saveLocalConfig(path string, config localConfig) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func localConfigFromInstallConfig(config installConfig, install bool) localConfig {
	values := config.Values
	local := localConfig{
		ChartSource:      values["chartSource"],
		ChartVersion:     values["chartVersion"],
		Kubeconfig:       values["kubeconfig"],
		Context:          values["context"],
		Namespace:        values["namespace"],
		Infrastructure:   values["infrastructure"],
		Domain:           values["domain"],
		Scheme:           values["scheme"],
		LicenseKey:       values["licenseKey"],
		LicenseServerURL: stringDefault(values["licenseServerUrl"], defaultLicenseServerURL),
		DashboardEnabled: boolPtr(parseBoolDefault(values["dashboardEnabled"], true)),
		RoutingMode:      values["routingMode"],
		WorkerMode:       defaultWorkerMode(values["workerMode"]),
		// The user's choice, kept when the selected release has no
		// lightweight profile, so a later run with a newer release applies it.
		Profile: defaultProfile(values["profile"]),
		// The user's answers, kept when the selected release cannot serve
		// inbound triggers, so a later run with a newer release applies them.
		MonoInbound:       monoSetting(values, "monoInbound"),
		MonoInboundRoutes: monoSetting(values, "monoInboundRoutes"),
		// The user's choice, kept when the selected release cannot install
		// samples, so a later run with a newer release applies it.
		Samples:         monoSetting(values, "samples"),
		IngressEnabled:  boolPtr(parseBoolDefault(values["ingressEnabled"], true)),
		IngressProvider: values["ingressProvider"],
		IngressClass:    values["ingressClass"],
		CookieDomain:    values["cookieDomain"],
		PullSecretMode:  values["pullSecretMode"],
		PullSecret:      values["pullSecret"],
		InfraComponents: infrastructureComponentsFromValues(values),
		External: localConfigExternal{
			CredentialsMode: values["externalCredentialsMode"],
			MySQL: localConfigExternalSQL{
				Host:        values["mysqlHost"],
				Port:        values["mysqlPort"],
				Database:    values["mysqlDatabase"],
				Username:    values["mysqlUsername"],
				Password:    values["mysqlPassword"],
				SecretName:  values["mysqlSecretName"],
				PasswordKey: values["mysqlPasswordKey"],
			},
			FabricMySQL: localConfigExternalSQL{
				Host:        values["fabricMysqlHost"],
				Port:        values["fabricMysqlPort"],
				Database:    values["fabricMysqlDatabase"],
				Username:    values["fabricMysqlUsername"],
				Password:    values["fabricMysqlPassword"],
				SecretName:  values["fabricMysqlSecretName"],
				PasswordKey: values["fabricMysqlPasswordKey"],
			},
			Redis: localConfigExternalRedis{
				Host:        values["redisHost"],
				Port:        values["redisPort"],
				Password:    values["redisPassword"],
				SecretName:  values["redisSecretName"],
				PasswordKey: values["redisPasswordKey"],
			},
			RabbitMQ: localConfigExternalRabbitMQ{
				Host:        values["rabbitmqHost"],
				Port:        values["rabbitmqPort"],
				Username:    values["rabbitmqUsername"],
				Password:    values["rabbitmqPassword"],
				Vhost:       values["rabbitmqVhost"],
				SecretName:  values["rabbitmqSecretName"],
				PasswordKey: values["rabbitmqPasswordKey"],
			},
			ElasticSearch: localConfigExternalElasticSearch{
				Host:        values["elasticsearchHost"],
				Port:        values["elasticsearchPort"],
				Scheme:      values["elasticsearchScheme"],
				Username:    values["elasticsearchUsername"],
				Password:    values["elasticsearchPassword"],
				SecretName:  values["elasticsearchSecretName"],
				UsernameKey: values["elasticsearchUsernameKey"],
				PasswordKey: values["elasticsearchPasswordKey"],
			},
			S3: localConfigExternalS3{
				Endpoint:     values["s3Endpoint"],
				Region:       values["s3Region"],
				Bucket:       values["s3Bucket"],
				AccessKey:    values["s3AccessKey"],
				SecretKey:    values["s3SecretKey"],
				PathStyle:    boolPtr(parseBoolDefault(values["s3PathStyle"], false)),
				SecretName:   values["s3SecretName"],
				AccessKeyKey: values["s3AccessKeyKey"],
				SecretKeyKey: values["s3SecretKeyKey"],
			},
			Pusher: localConfigExternalPusher{
				Host:          values["pusherHost"],
				Port:          values["pusherPort"],
				Scheme:        values["pusherScheme"],
				AppID:         values["pusherAppId"],
				AppKey:        values["pusherAppKey"],
				AppSecret:     values["pusherAppSecret"],
				AppCluster:    values["pusherAppCluster"],
				SecretName:    values["pusherSecretName"],
				AppIDKey:      values["pusherAppIdKey"],
				AppKeyKey:     values["pusherAppKeyKey"],
				AppSecretKey:  values["pusherAppSecretKey"],
				AppClusterKey: values["pusherAppClusterKey"],
			},
		},
		SeedInstall:       boolPtr(parseBoolDefault(values["seedInstall"], true)),
		CompanyName:       values["companyName"],
		AdminName:         values["adminName"],
		AdminEmail:        values["adminEmail"],
		AdminPassword:     values["adminPassword"],
		UserRole:          values["userRole"],
		Output:            config.Output,
		Install:           boolPtr(install),
		ConfirmKubeAccess: boolPtr(true),
	}
	if config.Quay != nil {
		local.QuayUsername = config.Quay.Username
		local.QuayPassword = config.Quay.Password
		local.QuayEmail = config.Quay.Email
	}
	return local
}

func infrastructureComponentsFromValues(values map[string]string) localConfigInfrastructure {
	return localConfigInfrastructure{
		MySQL:            componentFromValue(values, "infraMysqlEnabled"),
		FabricMySQL:      componentFromValue(values, "infraFabricMysqlEnabled"),
		FabricRedis:      componentFromValue(values, "infraFabricRedisEnabled"),
		Redis:            componentFromValue(values, "infraRedisEnabled"),
		RabbitMQ:         componentFromValue(values, "infraRabbitmqEnabled"),
		ElasticSearch:    componentFromValue(values, "infraElasticsearchEnabled"),
		S3:               componentFromValue(values, "infraS3Enabled"),
		KubeFaaS:         componentFromValue(values, "infraKubefaasEnabled"),
		KubeFaaSRegistry: componentFromValue(values, "kubefaasRegistry"),
		Pusher:           componentFromValue(values, "infraPusherEnabled"),
	}
}

func componentFromValue(values map[string]string, key string) localConfigComponent {
	value, ok := values[key]
	if !ok {
		return localConfigComponent{}
	}
	return localConfigComponent{Enabled: boolPtr(parseBoolDefault(value, false))}
}

func pullSecretModeFromSelection(pullSecret string, quay *quayCredentials) string {
	if pullSecret == "" {
		return "none"
	}
	if quay != nil {
		return "create"
	}
	return "existing"
}

func boolPtr(value bool) *bool {
	return &value
}

func parseBoolDefault(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true":
		return true
	case "false":
		return false
	default:
		return fallback
	}
}

func addExternalValues(values map[string]string, external localConfigExternal) {
	values["externalCredentialsMode"] = external.CredentialsMode
	values["mysqlHost"] = external.MySQL.Host
	values["mysqlPort"] = external.MySQL.Port
	values["mysqlDatabase"] = external.MySQL.Database
	values["mysqlUsername"] = external.MySQL.Username
	values["mysqlPassword"] = external.MySQL.Password
	values["mysqlSecretName"] = external.MySQL.SecretName
	values["mysqlPasswordKey"] = external.MySQL.PasswordKey
	values["fabricMysqlHost"] = external.FabricMySQL.Host
	values["fabricMysqlPort"] = external.FabricMySQL.Port
	values["fabricMysqlDatabase"] = external.FabricMySQL.Database
	values["fabricMysqlUsername"] = external.FabricMySQL.Username
	values["fabricMysqlPassword"] = external.FabricMySQL.Password
	values["fabricMysqlSecretName"] = external.FabricMySQL.SecretName
	values["fabricMysqlPasswordKey"] = external.FabricMySQL.PasswordKey
	values["redisHost"] = external.Redis.Host
	values["redisPort"] = external.Redis.Port
	values["redisPassword"] = external.Redis.Password
	values["redisSecretName"] = external.Redis.SecretName
	values["redisPasswordKey"] = external.Redis.PasswordKey
	values["rabbitmqHost"] = external.RabbitMQ.Host
	values["rabbitmqPort"] = external.RabbitMQ.Port
	values["rabbitmqUsername"] = external.RabbitMQ.Username
	values["rabbitmqPassword"] = external.RabbitMQ.Password
	values["rabbitmqVhost"] = external.RabbitMQ.Vhost
	values["rabbitmqSecretName"] = external.RabbitMQ.SecretName
	values["rabbitmqPasswordKey"] = external.RabbitMQ.PasswordKey
	values["elasticsearchHost"] = external.ElasticSearch.Host
	values["elasticsearchPort"] = external.ElasticSearch.Port
	values["elasticsearchScheme"] = external.ElasticSearch.Scheme
	values["elasticsearchUsername"] = external.ElasticSearch.Username
	values["elasticsearchPassword"] = external.ElasticSearch.Password
	values["elasticsearchSecretName"] = external.ElasticSearch.SecretName
	values["elasticsearchUsernameKey"] = external.ElasticSearch.UsernameKey
	values["elasticsearchPasswordKey"] = external.ElasticSearch.PasswordKey
	values["s3Endpoint"] = external.S3.Endpoint
	values["s3Region"] = external.S3.Region
	values["s3Bucket"] = external.S3.Bucket
	values["s3AccessKey"] = external.S3.AccessKey
	values["s3SecretKey"] = external.S3.SecretKey
	values["s3PathStyle"] = fmt.Sprintf("%t", boolDefault(external.S3.PathStyle, false))
	values["s3SecretName"] = external.S3.SecretName
	values["s3AccessKeyKey"] = external.S3.AccessKeyKey
	values["s3SecretKeyKey"] = external.S3.SecretKeyKey
	values["pusherHost"] = external.Pusher.Host
	values["pusherPort"] = external.Pusher.Port
	values["pusherScheme"] = external.Pusher.Scheme
	values["pusherAppId"] = external.Pusher.AppID
	values["pusherAppKey"] = external.Pusher.AppKey
	values["pusherAppSecret"] = external.Pusher.AppSecret
	values["pusherAppCluster"] = external.Pusher.AppCluster
	values["pusherSecretName"] = external.Pusher.SecretName
	values["pusherAppIdKey"] = external.Pusher.AppIDKey
	values["pusherAppKeyKey"] = external.Pusher.AppKeyKey
	values["pusherAppSecretKey"] = external.Pusher.AppSecretKey
	values["pusherAppClusterKey"] = external.Pusher.AppClusterKey
}

func addInfrastructureComponentValues(values map[string]string, infra localConfigInfrastructure) {
	setComponentEnabledValue(values, "infraMysqlEnabled", infra.MySQL.Enabled)
	setComponentEnabledValue(values, "infraFabricMysqlEnabled", infra.FabricMySQL.Enabled)
	setComponentEnabledValue(values, "infraFabricRedisEnabled", infra.FabricRedis.Enabled)
	setComponentEnabledValue(values, "infraRedisEnabled", infra.Redis.Enabled)
	setComponentEnabledValue(values, "infraRabbitmqEnabled", infra.RabbitMQ.Enabled)
	setComponentEnabledValue(values, "infraElasticsearchEnabled", infra.ElasticSearch.Enabled)
	setComponentEnabledValue(values, "infraS3Enabled", infra.S3.Enabled)
	setComponentEnabledValue(values, "infraKubefaasEnabled", infra.KubeFaaS.Enabled)
	setComponentEnabledValue(values, "kubefaasRegistry", infra.KubeFaaSRegistry.Enabled)
	setComponentEnabledValue(values, "infraPusherEnabled", infra.Pusher.Enabled)
}

func setComponentEnabledValue(values map[string]string, key string, enabled *bool) {
	if enabled != nil {
		values[key] = fmt.Sprintf("%t", *enabled)
	}
}

func stringDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func boolDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func installSummary(config installConfig) string {
	values := config.Values
	lines := []string{
		keyValue("Values file", config.Output),
		keyValue("Chart release", fmt.Sprintf("%s (%s)", values["chartVersion"], values["chartSource"])),
	}
	if values["chartNotice"] != "" {
		lines = append(lines, warningStyle.Render("! "+values["chartNotice"]))
	}
	lines = append(lines,
		keyValue("Namespace", values["namespace"]),
		keyValue("Infrastructure", infrastructureSummary(values)),
		keyValue("Base domain", values["domain"]),
		keyValue("URL scheme", values["scheme"]),
		keyValue("License key", configuredSummary(values["licenseKey"])),
		keyValue("License server URL", configuredSummary(values["licenseServerUrl"])),
		keyValue("Ingress", fmt.Sprintf("%s (%s/%s)", values["ingressEnabled"], values["ingressProvider"], values["ingressClass"])),
	)
	if values["ingressAddressNotice"] != "" {
		lines = append(lines, warningStyle.Render("! "+values["ingressAddressNotice"]))
	}
	lines = append(lines,
		keyValue("Dashboard", fmt.Sprintf("%s (%s routing)", values["dashboardEnabled"], values["routingMode"])),
		keyValue("Worker mode", defaultWorkerMode(values["workerMode"])),
		keyValue("Installation size", profileSummary(values)),
		keyValue("Cookie/session domain", values["cookieDomain"]),
		keyValue("Image pull secret", defaultListValue(values["pullSecret"], "none")),
		keyValue("Initial company/admin", values["seedInstall"]),
	)
	if hasValue(values, "infraKubefaasEnabled") {
		lines = append(lines, keyValue("KubeFaaS", kubefaasSummary(values)))
	}
	if defaultWorkerMode(values["workerMode"]) == "mono" {
		lines = append(lines, keyValue("Monocore inbound triggers", monoInboundSummary(values)))
		lines = append(lines, keyValue("Monocore for every company", monoAllCompaniesSummary(values)))
		lines = append(lines, keyValue("Samples", samplesSummary(values)))
	}
	if config.Quay != nil {
		lines = append(lines, keyValue("Quay secret action", fmt.Sprintf("create/update %s", config.Quay.SecretName)))
	} else if values["pullSecret"] != "" {
		lines = append(lines, keyValue("Quay secret action", "use existing secret"))
	}
	return strings.Join(lines, "\n")
}

func infrastructureSummary(values map[string]string) string {
	if values["infrastructure"] != "external" {
		if allInfrastructureComponentsDisabled(values) {
			return "bundled, all infra components disabled"
		}
		if values["installInfrastructure"] == "false" {
			return "bundled, leave existing infra unchanged"
		}
		return "bundled, install/upgrade infra"
	}
	return fmt.Sprintf("external (%s credentials)", stringDefault(values["externalCredentialsMode"], "secret"))
}

func uninstallSummary(namespace, pullSecretName string, removeContour, contourMarked bool) string {
	lines := []string{
		keyValue("Namespace", namespace),
		keyValue("Remove release", "patchworks-app"),
		keyValue("Remove release", "patchworks-infra"),
	}
	if strings.TrimSpace(pullSecretName) != "" {
		lines = append(lines, keyValue("Remove pull secret", fmt.Sprintf("%s, only if installer-labelled", pullSecretName)))
	}
	contourAction := "keep"
	if removeContour {
		contourAction = "remove"
	}
	contourLabel := "not detected"
	if contourMarked {
		contourLabel = "installer-labelled"
	}
	lines = append(lines, keyValue("Contour", fmt.Sprintf("%s (%s)", contourAction, contourLabel)))
	return strings.Join(lines, "\n")
}

func defaultListValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func configuredSummary(value string) string {
	if strings.TrimSpace(value) == "" {
		return "not configured"
	}
	return "configured"
}

func installerTheme() *huh.Theme {
	theme := huh.ThemeCharm()
	purple := lipgloss.Color(primaryPurple)
	cream := lipgloss.Color("#FFFDF5")

	theme.Focused.FocusedButton = theme.Focused.FocusedButton.
		Foreground(cream).
		Background(purple).
		Bold(true)
	theme.Focused.Next = theme.Focused.FocusedButton
	theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(purple)
	theme.Focused.MultiSelectSelector = theme.Focused.MultiSelectSelector.Foreground(purple)
	theme.Focused.SelectedOption = theme.Focused.SelectedOption.Foreground(purple)
	theme.Focused.SelectedPrefix = theme.Focused.SelectedPrefix.Foreground(purple)
	theme.Focused.TextInput.Cursor = theme.Focused.TextInput.Cursor.Foreground(purple)
	theme.Focused.TextInput.Prompt = theme.Focused.TextInput.Prompt.Foreground(purple)

	return theme
}

func runFormWithQuitConfirm(form *huh.Form) error {
	for {
		err := form.Run()
		if err == nil {
			return nil
		}
		if !errors.Is(err, huh.ErrUserAborted) {
			return err
		}

		quit := true
		confirmErr := huh.NewConfirm().
			Title("Quit installer?").
			Description("Any unsaved selections will be lost.").
			Affirmative("Quit").
			Negative("Continue").
			Value(&quit).
			Run()
		if confirmErr != nil {
			if errors.Is(confirmErr, huh.ErrUserAborted) {
				return huh.ErrUserAborted
			}
			continue
		}
		if quit {
			return huh.ErrUserAborted
		}
	}
}

func clusterSummaryCompact(info clusterInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", keyStyle.Render("Cluster"))
	fmt.Fprintf(&b, "%s\n", keyValue("Context", info.Context))
	fmt.Fprintf(&b, "%s\n", keyValue("Storage", strings.Join(defaultList(info.StorageClasses, "none"), ", ")))
	fmt.Fprintf(&b, "%s\n", keyValue("Ingress", strings.Join(defaultList(info.IngressClasses, "none"), ", ")))
	fmt.Fprintf(&b, "%s\n", keyValue("Contour", yesNo(info.HasContour)))
	fmt.Fprintf(&b, "%s\n", keyValue("cert-manager", yesNo(info.HasCertManager)))
	if len(info.Warnings) > 0 {
		fmt.Fprintf(&b, "\n%s\n", warningStyle.Render("Warnings"))
		for _, warning := range info.Warnings {
			fmt.Fprintf(&b, "%s\n", warningStyle.Render("- "+warning))
		}
	}
	return b.String()
}

func writeValues(path string, values map[string]string) error {
	if path == "" {
		return fmt.Errorf("output path cannot be empty")
	}

	if err := os.MkdirAll(filepath.Dir(cleanPath(path)), 0o755); err != nil {
		return err
	}

	var b bytes.Buffer
	namespace := values["namespace"]
	domain := values["domain"]
	scheme := values["scheme"]
	cookieDomain := values["cookieDomain"]
	if cookieDomain == "" {
		cookieDomain = defaultCookieDomain(domain)
	}

	if values["chartVersion"] != "" {
		fmt.Fprintf(&b, "# Patchworks chart release: %s (%s)\n# Exact artifacts are recorded in the adjacent .charts.lock.yaml file.\n", values["chartVersion"], values["chartSource"])
	}
	fmt.Fprintf(&b, "namespace: %s\n\n", quote(namespace))

	if values["pullSecret"] != "" {
		fmt.Fprintf(&b, "image:\n  pullSecrets:\n    - name: %s\n\n", quote(values["pullSecret"]))
	}

	fmt.Fprintf(&b, "app:\n")
	fmt.Fprintf(&b, "  url: %s\n", quote(fmt.Sprintf("%s://%s", scheme, domain)))
	fmt.Fprintf(&b, "  license:\n")
	if values["licenseKey"] != "" {
		fmt.Fprintf(&b, "    key: %s\n", quote(values["licenseKey"]))
	}
	if values["licenseServerUrl"] != "" {
		fmt.Fprintf(&b, "    serverUrl: %s\n", quote(values["licenseServerUrl"]))
	}
	fmt.Fprintf(&b, "\n")

	if values["infrastructure"] == "external" {
		writeExternalInfrastructureValues(&b, values)
	} else {
		writeBundledInfrastructureValues(&b, values)
	}

	fmt.Fprintf(&b, "ingress:\n")
	fmt.Fprintf(&b, "  enabled: %s\n", values["ingressEnabled"])
	fmt.Fprintf(&b, "  provider: %s\n", quote(values["ingressProvider"]))
	fmt.Fprintf(&b, "  scheme: %s\n", quote(scheme))
	fmt.Fprintf(&b, "  className: %s\n", quote(values["ingressClass"]))
	fmt.Fprintf(&b, "  hosts:\n")
	fmt.Fprintf(&b, "    dashboard: %s\n", quote(domain))
	fmt.Fprintf(&b, "    gateway: %s\n", quote("gateway."+domain))
	fmt.Fprintf(&b, "    start: %s\n", quote("start."+domain))
	fmt.Fprintf(&b, "    fabric: %s\n", quote("fabric."+domain))
	fmt.Fprintf(&b, "    webhook: %s\n", quote("webhooks."+domain))
	fmt.Fprintf(&b, "    callback: %s\n\n", quote("callbacks."+domain))

	fmt.Fprintf(&b, "dashboard:\n")
	fmt.Fprintf(&b, "  enabled: %s\n", values["dashboardEnabled"])
	fmt.Fprintf(&b, "  routingMode: %s\n", quote(values["routingMode"]))
	fmt.Fprintf(&b, "  authCookieDomain: %s\n\n", quote(cookieDomain))

	fmt.Fprintf(&b, "pusher:\n")
	if values["infrastructure"] == "external" {
		fmt.Fprintf(&b, "  enabled: false\n")
		fmt.Fprintf(&b, "  appCluster: %s\n", quote(values["pusherAppCluster"]))
		if values["externalCredentialsMode"] == "secret" {
			fmt.Fprintf(&b, "  existingSecret:\n")
			fmt.Fprintf(&b, "    name: %s\n", quote(values["pusherSecretName"]))
			fmt.Fprintf(&b, "    appIdKey: %s\n", quote(values["pusherAppIdKey"]))
			fmt.Fprintf(&b, "    appKeyKey: %s\n", quote(values["pusherAppKeyKey"]))
			fmt.Fprintf(&b, "    appSecretKey: %s\n", quote(values["pusherAppSecretKey"]))
			fmt.Fprintf(&b, "    appClusterKey: %s\n", quote(values["pusherAppClusterKey"]))
		} else {
			fmt.Fprintf(&b, "  appId: %s\n", quote(values["pusherAppId"]))
			fmt.Fprintf(&b, "  appKey: %s\n", quote(values["pusherAppKey"]))
			fmt.Fprintf(&b, "  appSecret: %s\n", quote(values["pusherAppSecret"]))
		}
		fmt.Fprintf(&b, "  external:\n")
		fmt.Fprintf(&b, "    host: %s\n", quote(values["pusherHost"]))
		fmt.Fprintf(&b, "    port: %s\n", values["pusherPort"])
		fmt.Fprintf(&b, "    scheme: %s\n\n", quote(values["pusherScheme"]))
	} else {
		fmt.Fprintf(&b, "  enabled: true\n")
		fmt.Fprintf(&b, "  ingress:\n")
		fmt.Fprintf(&b, "    enabled: %s\n", values["ingressEnabled"])
		fmt.Fprintf(&b, "    provider: %s\n", quote(values["ingressProvider"]))
		fmt.Fprintf(&b, "    className: %s\n", quote(values["ingressClass"]))
		fmt.Fprintf(&b, "    host: %s\n\n", quote("wss."+domain))
	}

	fmt.Fprintf(&b, "web:\n  sessionDomain: %s\n\n", quote(cookieDomain))

	if lightweightEnabled(values) {
		fmt.Fprintf(&b, "profile: \"lightweight\"\n")
		if values["lightweightWebApart"] != "" {
			fmt.Fprintf(&b, "lightweight:\n  web:\n    combine: false\n")
		}
		fmt.Fprintf(&b, "\n")
	}

	fmt.Fprintf(&b, "workers:\n")
	fmt.Fprintf(&b, "  type: %s\n", quote(defaultWorkerMode(values["workerMode"])))
	if monoInboundEnabled(values) || monoAllCompaniesEnabled(values) || samplesEnabled(values) {
		fmt.Fprintf(&b, "  mono:\n")
	}
	if monoInboundEnabled(values) {
		fmt.Fprintf(&b, "    inbound:\n")
		fmt.Fprintf(&b, "      enabled: true\n")
		fmt.Fprintf(&b, "      ingress:\n")
		fmt.Fprintf(&b, "        enabled: %t\n", values["monoInboundRoutes"] == "true")
	}
	if monoAllCompaniesEnabled(values) {
		fmt.Fprintf(&b, "    allCompanies:\n")
		fmt.Fprintf(&b, "      enabled: true\n")
	}
	if samplesEnabled(values) {
		writeSampleShapeFlags(&b, values["samplesShapeFlags"])
	}
	fmt.Fprintf(&b, "\n")
	if samplesEnabled(values) {
		fmt.Fprintf(&b, "samples:\n  enabled: true\n\n")
	}

	seedEnabled := values["seedInstall"]
	fmt.Fprintf(&b, "seeds:\n")
	fmt.Fprintf(&b, "  fabric:\n    enabled: %s\n", seedEnabled)
	fmt.Fprintf(&b, "  core:\n    enabled: %s\n", seedEnabled)
	fmt.Fprintf(&b, "  tenant:\n")
	fmt.Fprintf(&b, "    companyName: %s\n", quote(values["companyName"]))
	fmt.Fprintf(&b, "    tier: %s\n", quote("Professional"))
	fmt.Fprintf(&b, "    adminName: %s\n", quote(values["adminName"]))
	fmt.Fprintf(&b, "    adminEmail: %s\n", quote(values["adminEmail"]))
	if values["adminPassword"] != "" {
		fmt.Fprintf(&b, "    adminPassword: %s\n", quote(values["adminPassword"]))
	}
	fmt.Fprintf(&b, "    userRole: %s\n", quote(values["userRole"]))

	return os.WriteFile(cleanPath(path), b.Bytes(), 0o644)
}

func writeExternalInfrastructureValues(b *bytes.Buffer, values map[string]string) {
	secretMode := values["externalCredentialsMode"] == "secret"

	fmt.Fprintf(b, "mysql:\n")
	fmt.Fprintf(b, "  enabled: false\n")
	fmt.Fprintf(b, "  external:\n")
	fmt.Fprintf(b, "    host: %s\n", quote(values["mysqlHost"]))
	fmt.Fprintf(b, "    port: %s\n", values["mysqlPort"])
	fmt.Fprintf(b, "    database: %s\n", quote(values["mysqlDatabase"]))
	fmt.Fprintf(b, "    username: %s\n", quote(values["mysqlUsername"]))
	if secretMode {
		fmt.Fprintf(b, "    existingSecret:\n")
		fmt.Fprintf(b, "      name: %s\n", quote(values["mysqlSecretName"]))
		fmt.Fprintf(b, "      passwordKey: %s\n", quote(values["mysqlPasswordKey"]))
	} else {
		fmt.Fprintf(b, "    password: %s\n", quote(values["mysqlPassword"]))
	}
	fmt.Fprintf(b, "\n")

	fmt.Fprintf(b, "fabric:\n")
	fmt.Fprintf(b, "  mysql:\n")
	fmt.Fprintf(b, "    enabled: false\n")
	fmt.Fprintf(b, "    external:\n")
	fmt.Fprintf(b, "      host: %s\n", quote(values["fabricMysqlHost"]))
	fmt.Fprintf(b, "      port: %s\n", values["fabricMysqlPort"])
	fmt.Fprintf(b, "      database: %s\n", quote(values["fabricMysqlDatabase"]))
	fmt.Fprintf(b, "      username: %s\n", quote(values["fabricMysqlUsername"]))
	if secretMode {
		fmt.Fprintf(b, "      existingSecret:\n")
		fmt.Fprintf(b, "        name: %s\n", quote(values["fabricMysqlSecretName"]))
		fmt.Fprintf(b, "        passwordKey: %s\n", quote(values["fabricMysqlPasswordKey"]))
	} else {
		fmt.Fprintf(b, "      password: %s\n", quote(values["fabricMysqlPassword"]))
	}
	fmt.Fprintf(b, "\n")

	fmt.Fprintf(b, "redis:\n")
	fmt.Fprintf(b, "  enabled: false\n")
	fmt.Fprintf(b, "  external:\n")
	fmt.Fprintf(b, "    host: %s\n", quote(values["redisHost"]))
	fmt.Fprintf(b, "    port: %s\n", values["redisPort"])
	if secretMode {
		if values["redisSecretName"] != "" {
			fmt.Fprintf(b, "    existingSecret:\n")
			fmt.Fprintf(b, "      name: %s\n", quote(values["redisSecretName"]))
			fmt.Fprintf(b, "      passwordKey: %s\n", quote(values["redisPasswordKey"]))
		}
	} else if values["redisPassword"] != "" {
		fmt.Fprintf(b, "    password: %s\n", quote(values["redisPassword"]))
	}
	fmt.Fprintf(b, "\n")

	fmt.Fprintf(b, "rabbitmq:\n")
	fmt.Fprintf(b, "  enabled: false\n")
	fmt.Fprintf(b, "  topology:\n")
	fmt.Fprintf(b, "    enabled: true\n")
	fmt.Fprintf(b, "  external:\n")
	fmt.Fprintf(b, "    host: %s\n", quote(values["rabbitmqHost"]))
	fmt.Fprintf(b, "    port: %s\n", values["rabbitmqPort"])
	fmt.Fprintf(b, "    username: %s\n", quote(values["rabbitmqUsername"]))
	fmt.Fprintf(b, "    vhost: %s\n", quote(values["rabbitmqVhost"]))
	if secretMode {
		fmt.Fprintf(b, "    existingSecret:\n")
		fmt.Fprintf(b, "      name: %s\n", quote(values["rabbitmqSecretName"]))
		fmt.Fprintf(b, "      passwordKey: %s\n", quote(values["rabbitmqPasswordKey"]))
	} else {
		fmt.Fprintf(b, "    password: %s\n", quote(values["rabbitmqPassword"]))
	}
	fmt.Fprintf(b, "\n")

	fmt.Fprintf(b, "elasticsearch:\n")
	fmt.Fprintf(b, "  enabled: false\n")
	fmt.Fprintf(b, "  external:\n")
	fmt.Fprintf(b, "    host: %s\n", quote(values["elasticsearchHost"]))
	fmt.Fprintf(b, "    port: %s\n", values["elasticsearchPort"])
	fmt.Fprintf(b, "    scheme: %s\n", quote(values["elasticsearchScheme"]))
	if values["elasticsearchUsername"] != "" {
		fmt.Fprintf(b, "    username: %s\n", quote(values["elasticsearchUsername"]))
	}
	if secretMode {
		if values["elasticsearchSecretName"] != "" {
			fmt.Fprintf(b, "    existingSecret:\n")
			fmt.Fprintf(b, "      name: %s\n", quote(values["elasticsearchSecretName"]))
			fmt.Fprintf(b, "      usernameKey: %s\n", quote(values["elasticsearchUsernameKey"]))
			fmt.Fprintf(b, "      passwordKey: %s\n", quote(values["elasticsearchPasswordKey"]))
		}
	} else if values["elasticsearchPassword"] != "" {
		fmt.Fprintf(b, "    password: %s\n", quote(values["elasticsearchPassword"]))
	}
	fmt.Fprintf(b, "\n")

	fmt.Fprintf(b, "s3:\n")
	fmt.Fprintf(b, "  enabled: false\n")
	fmt.Fprintf(b, "  external:\n")
	fmt.Fprintf(b, "    endpoint: %s\n", quote(values["s3Endpoint"]))
	fmt.Fprintf(b, "    region: %s\n", quote(values["s3Region"]))
	fmt.Fprintf(b, "    bucket: %s\n", quote(values["s3Bucket"]))
	fmt.Fprintf(b, "    pathStyle: %s\n", values["s3PathStyle"])
	if secretMode {
		fmt.Fprintf(b, "    existingSecret:\n")
		fmt.Fprintf(b, "      name: %s\n", quote(values["s3SecretName"]))
		fmt.Fprintf(b, "      accessKeyKey: %s\n", quote(values["s3AccessKeyKey"]))
		fmt.Fprintf(b, "      secretKeyKey: %s\n", quote(values["s3SecretKeyKey"]))
	} else {
		fmt.Fprintf(b, "    accessKey: %s\n", quote(values["s3AccessKey"]))
		fmt.Fprintf(b, "    secretKey: %s\n", quote(values["s3SecretKey"]))
	}
	fmt.Fprintf(b, "\n")
}

func writeBundledInfrastructureValues(b *bytes.Buffer, values map[string]string) {
	writeComponentEnabledValue(b, "mysql", values, "infraMysqlEnabled")
	if hasValue(values, "infraFabricMysqlEnabled") || hasValue(values, "infraFabricRedisEnabled") {
		fmt.Fprintf(b, "fabric:\n")
		writeNestedComponentEnabledValue(b, "mysql", values, "infraFabricMysqlEnabled")
		writeNestedComponentEnabledValue(b, "redis", values, "infraFabricRedisEnabled")
		fmt.Fprintf(b, "\n")
	}
	writeComponentEnabledValue(b, "redis", values, "infraRedisEnabled")
	writeComponentEnabledValue(b, "rabbitmq", values, "infraRabbitmqEnabled")
	writeComponentEnabledValue(b, "elasticsearch", values, "infraElasticsearchEnabled")
	writeComponentEnabledValue(b, "s3", values, "infraS3Enabled")
	writeKubeFaaSValues(b, values)
	writeComponentEnabledValue(b, "pusher", values, "infraPusherEnabled")
}

func writeComponentEnabledValue(b *bytes.Buffer, name string, values map[string]string, key string) {
	if !hasValue(values, key) {
		return
	}
	fmt.Fprintf(b, "%s:\n", name)
	fmt.Fprintf(b, "  enabled: %s\n\n", values[key])
}

func writeNestedComponentEnabledValue(b *bytes.Buffer, name string, values map[string]string, key string) {
	if !hasValue(values, key) {
		return
	}
	fmt.Fprintf(b, "  %s:\n", name)
	fmt.Fprintf(b, "    enabled: %s\n", values[key])
}

func hasValue(values map[string]string, key string) bool {
	_, ok := values[key]
	return ok
}

func cleanPath(path string) string {
	if dir := filepath.Dir(path); dir == "." || dir == "" {
		return path
	}
	return filepath.Clean(path)
}

func envFirst(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func defaultList(values []string, fallback string) []string {
	if len(values) == 0 {
		return []string{fallback}
	}
	return values
}

func removeWarning(warnings []string, warning string) []string {
	filtered := warnings[:0]
	for _, existing := range warnings {
		if existing != warning {
			filtered = append(filtered, existing)
		}
	}
	return filtered
}

func defaultCookieDomain(domain string) string {
	if domain == "" {
		return ""
	}
	if strings.HasPrefix(domain, ".") {
		return domain
	}
	return "." + domain
}

func defaultCookieDomainForRouting(domain, routingMode string) string {
	if routingMode == "path" {
		return strings.TrimPrefix(domain, ".")
	}
	return defaultCookieDomain(domain)
}

// lastMonocoreWithoutInbound is the last Patchworks release whose Monocore
// image has no public listener for inbound triggers: gopatchworks/monocore#470
// merged after its build.
const lastMonocoreWithoutInbound = "0.2.12"

// monoInboundUnsupportedReason says why the selected app chart cannot serve
// inbound triggers from Monocore, or "" when it can: a chart without the
// setting would ignore it, and an older Monocore image has no listener, so
// its routes would reach nothing.
func monoInboundUnsupportedReason(chart helmchart.Charter) string {
	accessor, err := helmchart.NewAccessor(chart)
	if err != nil {
		return "the app chart could not be read"
	}
	workers, _ := accessor.Values()["workers"].(map[string]any)
	mono, _ := workers["mono"].(map[string]any)
	if _, ok := mono["inbound"].(map[string]any); !ok {
		return "this chart release cannot serve them"
	}
	image, _ := mono["image"].(map[string]any)
	tag, _ := image["tag"].(string)
	// A tag that is not a version (a branch build) cannot be compared.
	if version, err := semver.NewVersion(strings.TrimPrefix(tag, "v")); err == nil &&
		!version.GreaterThan(semver.MustParse(lastMonocoreWithoutInbound)) {
		return fmt.Sprintf("Monocore %s predates them", tag)
	}
	return ""
}

// lastCoreWithoutMonocoreAutoEnable is the last Patchworks release whose Core
// image has no monocore:auto-enable command or new-company listener, so its
// companies would not get Monocore switched on.
const lastCoreWithoutMonocoreAutoEnable = "0.2.16"

// monoAllCompaniesUnsupportedReason says why the selected app chart cannot run
// every company on Monocore (workers.mono.allCompanies), or "" when it can. A
// mono installation deploys no Core flow workers, so this is written without
// asking whenever the release supports it.
func monoAllCompaniesUnsupportedReason(chart helmchart.Charter) string {
	accessor, err := helmchart.NewAccessor(chart)
	if err != nil {
		return "the app chart could not be read"
	}
	values := accessor.Values()
	workers, _ := values["workers"].(map[string]any)
	mono, _ := workers["mono"].(map[string]any)
	if _, ok := mono["allCompanies"].(map[string]any); !ok {
		return "this chart release cannot switch it on"
	}
	image, _ := values["image"].(map[string]any)
	tag, _ := image["tag"].(string)
	// A tag that is not a version (a branch build) cannot be compared.
	if version, err := semver.NewVersion(strings.TrimPrefix(tag, "v")); err == nil &&
		!version.GreaterThan(semver.MustParse(lastCoreWithoutMonocoreAutoEnable)) {
		return fmt.Sprintf("Core %s predates it", tag)
	}
	return ""
}

// monoAllCompaniesEnabled is whether the values file runs every company on
// Monocore: mono workers and a selected release checked to support it.
func monoAllCompaniesEnabled(values map[string]string) bool {
	return defaultWorkerMode(values["workerMode"]) == "mono" && values["monoAllCompanies"] == "true"
}

func monoAllCompaniesSummary(values map[string]string) string {
	switch {
	case monoAllCompaniesEnabled(values):
		return "on: Core turns Monocore and its scheduler on for each new company; Core's short processor and gateway schedule are off"
	case values["monoAllCompaniesUnsupported"] != "":
		return "off: " + values["monoAllCompaniesUnsupported"] + "; turn Monocore on for each company from its feature settings, or choose a newer release with --chart-version"
	default:
		return "off"
	}
}

// monoInboundEnabled is whether the values file turns inbound triggers on:
// mono workers, the user's answer, and a release that can serve them.
func monoInboundEnabled(values map[string]string) bool {
	return defaultWorkerMode(values["workerMode"]) == "mono" && values["monoInbound"] == "true" && values["monoInboundUnsupported"] == ""
}

func monoInboundSummary(values map[string]string) string {
	switch {
	case values["monoInbound"] != "true":
		return "off"
	case values["monoInboundUnsupported"] != "":
		return "off: " + values["monoInboundUnsupported"] + "; choose a newer release with --chart-version"
	case values["monoInboundRoutes"] == "true":
		return "on, ingress routes to Monocore"
	default:
		return "on, ingress routes still to Core"
	}
}

// monoSetting is a saved answer to a mono-only question, nil for other worker modes.
func monoSetting(values map[string]string, key string) *bool {
	if defaultWorkerMode(values["workerMode"]) != "mono" {
		return nil
	}
	return boolPtr(values[key] == "true")
}

// lastCoreWithoutAppDomains is the last Patchworks release whose Core image
// serves one domain per process, so its gateway and start routes cannot share
// the lightweight profile's web pod.
const lastCoreWithoutAppDomains = "0.2.16"

func defaultProfile(value string) string {
	if strings.TrimSpace(value) == "lightweight" {
		return "lightweight"
	}
	return "standard"
}

// lightweightSupport says why the selected app chart cannot install the
// lightweight profile (unsupported), or why its web pods stay apart (webApart):
// a Core image up to lastCoreWithoutAppDomains loads only one domain's routes
// per process, so the worker pod applies but the web pod is not combined.
func lightweightSupport(chart helmchart.Charter) (unsupported, webApart string) {
	accessor, err := helmchart.NewAccessor(chart)
	if err != nil {
		return "the app chart could not be read", ""
	}
	values := accessor.Values()
	if _, ok := values["lightweight"].(map[string]any); !ok {
		return "this chart release has no lightweight profile", ""
	}
	image, _ := values["image"].(map[string]any)
	tag, _ := image["tag"].(string)
	// A tag that is not a version (a branch build) cannot be compared.
	if version, err := semver.NewVersion(strings.TrimPrefix(tag, "v")); err == nil &&
		!version.GreaterThan(semver.MustParse(lastCoreWithoutAppDomains)) {
		return "", fmt.Sprintf("Core %s serves one domain per process", tag)
	}
	return "", ""
}

// lightweightEnabled is whether the values file selects the lightweight
// profile: the user's choice and a selected release checked to support it.
func lightweightEnabled(values map[string]string) bool {
	return defaultProfile(values["profile"]) == "lightweight" && values["profileUnsupported"] == "" && values["profileChecked"] == "true"
}

func profileSummary(values map[string]string) string {
	switch {
	case defaultProfile(values["profile"]) != "lightweight":
		return "standard"
	case values["profileUnsupported"] != "":
		return "standard: " + values["profileUnsupported"] + "; choose a newer release with --chart-version"
	case !lightweightEnabled(values):
		return "standard"
	case values["lightweightWebApart"] != "":
		return "lightweight: one Core worker pod; separate gateway and start pods, as " + values["lightweightWebApart"]
	default:
		return "lightweight: one Core web pod and one Core worker pod"
	}
}

// sampleShapes are Monocore's optional shapes the samples use, as the chart's
// workers.mono.enable keys and the environment variables Monocore reads.
var sampleShapes = []struct{ key, env string }{
	{"merge", "WORKER_ENABLE_MERGE"},
	{"dataTransform", "WORKER_ENABLE_DATA_TRANSFORM"},
	{"crossReference", "WORKER_ENABLE_CROSS_REFERENCE"},
	{"syncSubprocess", "WORKER_ENABLE_SYNC_SUBPROCESS"},
}

// samplesSupport says why the selected app chart and answers cannot install the
// sample flows (unsupported), and how to turn Monocore's optional shapes on
// (shapeFlags): "keys" when the chart has workers.mono.enable, else "extraEnv"
// with the variables Monocore reads.
func samplesSupport(chart helmchart.Charter, values map[string]string) (unsupported, shapeFlags string) {
	if defaultWorkerMode(values["workerMode"]) != "mono" {
		return "samples run on Monocore workers only", ""
	}
	accessor, err := helmchart.NewAccessor(chart)
	if err != nil {
		return "the app chart could not be read", ""
	}
	chartValues := accessor.Values()
	if _, ok := chartValues["samples"].(map[string]any); !ok {
		return "this chart release has no samples", ""
	}
	if values["monoAllCompanies"] != "true" {
		return "they need every company on Monocore, which this release cannot run", ""
	}
	if !parseBoolDefault(values["seedInstall"], true) {
		return "they are created in the initial company, which this run does not create", ""
	}
	workers, _ := chartValues["workers"].(map[string]any)
	mono, _ := workers["mono"].(map[string]any)
	if _, ok := mono["enable"].(map[string]any); ok {
		return "", "keys"
	}
	return "", "extraEnv"
}

// samplesEnabled is whether the values file installs the sample flows: the
// user's choice, on a mono installation, with a release checked to support it.
func samplesEnabled(values map[string]string) bool {
	return defaultWorkerMode(values["workerMode"]) == "mono" && values["samples"] == "true" &&
		values["samplesChecked"] == "true" && values["samplesUnsupported"] == ""
}

// writeSampleShapeFlags turns on the Monocore shapes the samples use, under the
// workers.mono block writeValues is writing.
func writeSampleShapeFlags(b io.Writer, mode string) {
	if mode == "keys" {
		fmt.Fprintf(b, "    enable:\n")
		for _, shape := range sampleShapes {
			fmt.Fprintf(b, "      %s: true\n", shape.key)
		}
		return
	}
	fmt.Fprintf(b, "    # Monocore's optional shapes, which the samples use.\n")
	fmt.Fprintf(b, "    extraEnv:\n")
	for _, shape := range sampleShapes {
		fmt.Fprintf(b, "      - name: %s\n        value: \"true\"\n", shape.env)
	}
}

func samplesSummary(values map[string]string) string {
	switch {
	case values["samples"] != "true":
		return "off"
	case values["samplesUnsupported"] != "":
		return "off: " + values["samplesUnsupported"]
	case !samplesEnabled(values):
		return "off"
	default:
		return "on: sample flows in the Samples folder, with Monocore's optional shapes on"
	}
}

func defaultWorkerMode(value string) string {
	switch strings.TrimSpace(value) {
	case "standalone", "microservice", "mono":
		return strings.TrimSpace(value)
	default:
		return "standalone"
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func quote(value string) string {
	return fmt.Sprintf("%q", value)
}
