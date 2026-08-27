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

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
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
	Warnings          []string
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
	Kubeconfig        string `yaml:"kubeconfig"`
	Context           string `yaml:"context"`
	Namespace         string `yaml:"namespace"`
	Domain            string `yaml:"domain"`
	Scheme            string `yaml:"scheme"`
	LicenseKey        string `yaml:"licenseKey"`
	LicenseServerURL  string `yaml:"licenseServerUrl"`
	DashboardEnabled  *bool  `yaml:"dashboardEnabled"`
	RoutingMode       string `yaml:"routingMode"`
	WorkerMode        string `yaml:"workerMode"`
	IngressEnabled    *bool  `yaml:"ingressEnabled"`
	IngressProvider   string `yaml:"ingressProvider"`
	IngressClass      string `yaml:"ingressClass"`
	CookieDomain      string `yaml:"cookieDomain"`
	PullSecretMode    string `yaml:"pullSecretMode"`
	PullSecret        string `yaml:"pullSecret"`
	QuayUsername      string `yaml:"quayUsername"`
	QuayPassword      string `yaml:"quayPassword"`
	QuayEmail         string `yaml:"quayEmail"`
	SeedInstall       *bool  `yaml:"seedInstall"`
	CompanyName       string `yaml:"companyName"`
	AdminName         string `yaml:"adminName"`
	AdminEmail        string `yaml:"adminEmail"`
	AdminPassword     string `yaml:"adminPassword"`
	UserRole          string `yaml:"userRole"`
	Output            string `yaml:"output"`
	Install           *bool  `yaml:"install"`
	ConfirmKubeAccess *bool  `yaml:"confirmKubeAccess"`
}

type localConfigFile struct {
	Kubernetes  localConfigKubernetes  `yaml:"kubernetes,omitempty"`
	Application localConfigApplication `yaml:"application,omitempty"`
	Workers     localConfigWorkers     `yaml:"workers,omitempty"`
	Ingress     localConfigIngress     `yaml:"ingress,omitempty"`
	Registry    localConfigRegistry    `yaml:"registry,omitempty"`
	Seed        localConfigSeed        `yaml:"seed,omitempty"`
	Output      localConfigOutput      `yaml:"output,omitempty"`
	Installer   localConfigInstaller   `yaml:"installer,omitempty"`

	Kubeconfig        string `yaml:"kubeconfig,omitempty"`
	Context           string `yaml:"context,omitempty"`
	Namespace         string `yaml:"namespace,omitempty"`
	Domain            string `yaml:"domain,omitempty"`
	Scheme            string `yaml:"scheme,omitempty"`
	LicenseKey        string `yaml:"licenseKey,omitempty"`
	LicenseServerURL  string `yaml:"licenseServerUrl,omitempty"`
	DashboardEnabled  *bool  `yaml:"dashboardEnabled,omitempty"`
	RoutingMode       string `yaml:"routingMode,omitempty"`
	WorkerMode        string `yaml:"workerMode,omitempty"`
	IngressEnabled    *bool  `yaml:"ingressEnabled,omitempty"`
	IngressProvider   string `yaml:"ingressProvider,omitempty"`
	IngressClass      string `yaml:"ingressClass,omitempty"`
	CookieDomain      string `yaml:"cookieDomain,omitempty"`
	PullSecretMode    string `yaml:"pullSecretMode,omitempty"`
	PullSecret        string `yaml:"pullSecret,omitempty"`
	QuayUsername      string `yaml:"quayUsername,omitempty"`
	QuayPassword      string `yaml:"quayPassword,omitempty"`
	QuayEmail         string `yaml:"quayEmail,omitempty"`
	SeedInstall       *bool  `yaml:"seedInstall,omitempty"`
	CompanyName       string `yaml:"companyName,omitempty"`
	AdminName         string `yaml:"adminName,omitempty"`
	AdminEmail        string `yaml:"adminEmail,omitempty"`
	AdminPassword     string `yaml:"adminPassword,omitempty"`
	UserRole          string `yaml:"userRole,omitempty"`
	ValuesFile        string `yaml:"valuesFile,omitempty"`
	InstallEnabled    *bool  `yaml:"install,omitempty"`
	ConfirmKubeAccess *bool  `yaml:"confirmKubeAccess,omitempty"`
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

type localConfigLicense struct {
	Key       string `yaml:"key,omitempty"`
	ServerURL string `yaml:"serverUrl,omitempty"`
}

type localConfigDashboard struct {
	Enabled     *bool  `yaml:"enabled,omitempty"`
	RoutingMode string `yaml:"routingMode,omitempty"`
}

type localConfigWorkers struct {
	Type string `yaml:"type,omitempty"`
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
	Enabled *bool `yaml:"enabled,omitempty"`
}

func (config *localConfig) UnmarshalYAML(value *yaml.Node) error {
	var file localConfigFile
	if err := value.Decode(&file); err != nil {
		return err
	}

	*config = localConfig{
		Kubeconfig:        file.Kubeconfig,
		Context:           file.Context,
		Namespace:         file.Namespace,
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
	overrideString(&config.Domain, file.Application.Domain)
	overrideString(&config.Scheme, file.Application.Scheme)
	overrideString(&config.LicenseKey, file.Application.License.Key)
	overrideString(&config.LicenseServerURL, file.Application.License.ServerURL)
	overrideBool(&config.DashboardEnabled, file.Application.Dashboard.Enabled)
	overrideString(&config.RoutingMode, file.Application.Dashboard.RoutingMode)
	overrideString(&config.CookieDomain, file.Application.CookieDomain)
	overrideString(&config.WorkerMode, file.Workers.Type)
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
		Kubernetes: localConfigKubernetes{
			Kubeconfig:    config.Kubeconfig,
			Context:       config.Context,
			Namespace:     config.Namespace,
			ConfirmAccess: config.ConfirmKubeAccess,
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
			Enabled: config.Install,
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
	Command         string
	ConfigPath      string
	SaveConfig      bool
	ChartsOutputDir string
}

type inspectResult struct {
	Info clusterInfo
	Err  error
}

type inspectModel struct {
	info        clusterInfo
	kubeconfig  string
	contextName string
	err         error
	aborted     bool
	quitConfirm bool
	spinner     spinner.Model
	width       int
}

type progressResult struct {
	err error
}

type progressPhaseMsg string

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

type progressModel struct {
	title   string
	detail  string
	run     func() error
	phase   func() string
	err     error
	spinner spinner.Model
	started time.Time
	width   int
}

var (
	brandStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	subtleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	panelStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(1, 2)
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	keyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true)
	codeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
)

const installerLabelKey = "selfhosted.patchworks.io/installed-by"
const installerLabelValue = "patchworks-installer"
const defaultLicenseServerURL = "https://license.wearepatchworks.com"
const primaryPurple = "#7C3AED"

var version = "dev"

func main() {
	opts, err := parseCLI(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if opts.Command == "uninstall" {
		if err := runUninstall(opts); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				return
			}
			fmt.Fprintf(os.Stderr, "uninstall failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if opts.Command == "version" {
		fmt.Println(version)
		return
	}
	if opts.Command == "unpack-charts" {
		if err := unpackEmbeddedCharts(opts.ChartsOutputDir); err != nil {
			fmt.Fprintf(os.Stderr, "unpack charts failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Charts unpacked to %s\n", opts.ChartsOutputDir)
		return
	}

	if err := runInstaller(opts); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return
		}
		fmt.Fprintf(os.Stderr, "installer failed: %v\n", err)
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
			return cliOptions{}, fmt.Errorf("usage: patchworks-installer [install|uninstall|unpack-charts|version] [--config config.yaml] [--save-config] [--output patchworks-charts]")
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
	return opts, nil
}

func runInstaller(opts cliOptions) error {
	defaults, loaded, err := loadLocalConfig(opts.ConfigPath)
	if err != nil {
		return err
	}
	if loaded {
		fmt.Printf("%s %s\n", successStyle.Render("Values loaded from"), opts.ConfigPath)
	}

	access, err := selectKubeAccess(defaults)
	if err != nil {
		return err
	}

	info, err := runInspection(access.Kubeconfig, access.Context)
	if err != nil {
		return err
	}

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
				fmt.Fprintf(os.Stderr, "%s %v\n", warningStyle.Render("Contour install failed:"), err)
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
			}
		}
	}

	config, err := runSettingsForm(info, access.Kubeconfig, access.Context, defaults)
	if err != nil {
		return err
	}

	if err := writeValues(config.Output, config.Values); err != nil {
		return err
	}

	runInstall := boolDefault(defaults.Install, true)
	if err := runFormWithQuitConfirm(huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Install summary").
				Description(installSummary(config)),
			huh.NewConfirm().
				Title("Install Patchworks now?").
				Description("The installer will use embedded Helm charts to install or upgrade the infra and app releases, then check rollout status. Helm CLI is only needed for manual commands.").
				Affirmative("Install").
				Negative("Only write values").
				Value(&runInstall),
		),
	)); err != nil {
		return err
	}

	if opts.SaveConfig {
		if err := saveLocalConfig(opts.ConfigPath, localConfigFromInstallConfig(config, runInstall)); err != nil {
			return err
		}
		fmt.Printf("%s %s\n", successStyle.Render("Values saved to"), opts.ConfigPath)
	}

	commands := helmCommandText(config.Values["namespace"], config.Output)
	fmt.Println(valuesCreatedView(96, config.Output, commands))
	fmt.Println(accessDetailsView(96, config.Values, "", false))

	if !runInstall {
		return nil
	}

	if config.Quay != nil {
		if err := runProgress("Creating Quay pull secret", fmt.Sprintf("Creating %s in namespace %s", config.Quay.SecretName, config.Values["namespace"]), func() error {
			return createQuayPullSecret(access, config.Values["namespace"], *config.Quay)
		}); err != nil {
			return err
		}
	}

	if err := installPatchworks(access, config.Values["namespace"], config.Output); err != nil {
		return err
	}

	status, err := workloadStatus(access, config.Values["namespace"])
	if err != nil {
		return err
	}
	fmt.Println(page(96, plainPanel(96, status)))
	generatedAdminPassword := ""
	if config.Values["adminPassword"] == "" {
		password, err := tenantAdminPassword(access, config.Values["namespace"])
		if err == nil {
			generatedAdminPassword = password
		}
	}
	fmt.Println(accessDetailsView(96, config.Values, generatedAdminPassword, true))
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
	fmt.Println(brandStyle.Render("Installing Contour"))

	settings, cfg, err := helmRuntime(access, "projectcontour")
	if err != nil {
		return err
	}

	var chart helmchart.Charter
	var values map[string]any
	if err := runProgress("Fetching Contour chart", "Downloading chart metadata and loading contour/contour", func() error {
		var loadErr error
		chart, values, loadErr = loadHelmChart(settings, "contour", "https://projectcontour.github.io/helm-charts/", "")
		return loadErr
	}); err != nil {
		return err
	}
	mergeCommonLabels(values, installerLabels())

	if err := runProgress("Installing Contour", "Applying release contour in namespace projectcontour", func() error {
		return helmUpgradeOrInstall(cfg, "contour", "projectcontour", chart, values, true, 10*time.Minute, installerLabels())
	}); err != nil {
		return err
	}

	fmt.Println(successStyle.Render("Contour installed. Ingress class: contour"))
	return nil
}

func installPatchworks(access kubeAccess, namespace, valuesFile string) error {
	fmt.Println(brandStyle.Render("Installing Patchworks"))

	settings, cfg, err := helmRuntime(access, namespace)
	if err != nil {
		return err
	}

	var infraChart helmchart.Charter
	var infraValues map[string]any
	if err := runProgress("Loading infra chart", "Reading embedded patchworks-infra chart and generated values", func() error {
		var loadErr error
		infraChart, infraValues, loadErr = loadEmbeddedPatchworksChart(settings, "patchworks-infra", valuesFile)
		return loadErr
	}); err != nil {
		return err
	}
	if err := runProgressWithPhase("Installing infrastructure", "Applying release patchworks-infra and waiting for readiness", installPhaseMonitor(access, namespace, "infra"), func() error {
		return helmUpgradeOrInstall(cfg, "patchworks-infra", namespace, infraChart, infraValues, true, 15*time.Minute, installerLabels())
	}); err != nil {
		return err
	}

	var appChart helmchart.Charter
	var appValues map[string]any
	if err := runProgress("Loading app chart", "Reading embedded patchworks-app chart and generated values", func() error {
		var loadErr error
		appChart, appValues, loadErr = loadEmbeddedPatchworksChart(settings, "patchworks-app", valuesFile)
		return loadErr
	}); err != nil {
		return err
	}
	if err := runProgressWithPhase("Installing application", "Applying release patchworks-app and waiting for hooks/readiness", installPhaseMonitor(access, namespace, "app"), func() error {
		return helmUpgradeOrInstall(cfg, "patchworks-app", namespace, appChart, appValues, false, 15*time.Minute, installerLabels())
	}); err != nil {
		printInstallFailureDiagnostics(access, namespace)
		return err
	}

	if err := runProgress("Checking infra release", "Reading Helm status for patchworks-infra", func() error {
		return helmPrintStatus(cfg, "patchworks-infra")
	}); err != nil {
		return err
	}
	return runProgress("Checking app release", "Reading Helm status for patchworks-app", func() error {
		return helmPrintStatus(cfg, "patchworks-app")
	})
}

func runUninstall(opts cliOptions) error {
	defaults, loaded, err := loadLocalConfig(opts.ConfigPath)
	if err != nil {
		return err
	}
	if loaded {
		fmt.Printf("%s %s\n", successStyle.Render("Values loaded from"), opts.ConfigPath)
	}

	access, err := selectKubeAccess(defaults)
	if err != nil {
		return err
	}

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
		if err := runProgress("Removing Quay pull secret", fmt.Sprintf("Deleting %s only if installer-labelled", pullSecretName), func() error {
			return deleteInstallerPullSecret(access, namespace, pullSecretName)
		}); err != nil {
			return err
		}
	}

	if removeContour {
		if err := uninstallContour(access); err != nil {
			return err
		}
	}

	fmt.Println(successStyle.Render("Uninstall complete"))
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

	if err := runProgress("Uninstalling application", "Removing release patchworks-app", func() error {
		return helmUninstall(cfg, "patchworks-app", 10*time.Minute)
	}); err != nil {
		return err
	}
	return runProgress("Uninstalling infrastructure", "Removing release patchworks-infra", func() error {
		return helmUninstall(cfg, "patchworks-infra", 10*time.Minute)
	})
}

func uninstallContour(access kubeAccess) error {
	_, cfg, err := helmRuntime(access, "projectcontour")
	if err != nil {
		return err
	}
	return runProgress("Uninstalling Contour", "Removing release contour from namespace projectcontour", func() error {
		return helmUninstall(cfg, "contour", 10*time.Minute)
	})
}

func helmRuntime(access kubeAccess, namespace string) (*cli.EnvSettings, *action.Configuration, error) {
	settings := cli.New()
	settings.KubeConfig = access.Kubeconfig
	settings.KubeContext = access.Context
	settings.SetNamespace(namespace)

	registryClient, err := registry.NewClient(
		registry.ClientOptWriter(os.Stdout),
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

func loadEmbeddedPatchworksChart(settings *cli.EnvSettings, chartName, valuesFile string) (helmchart.Charter, map[string]any, error) {
	chartPath, cleanup, err := embeddedChartPath(chartName)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()

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

func helmUpgradeOrInstall(cfg *action.Configuration, releaseName, namespace string, chart helmchart.Charter, values map[string]any, createNamespace bool, timeout time.Duration, labels map[string]string) error {
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
		_, err := install.Run(chart, values)
		return err
	}

	upgrade := action.NewUpgrade(cfg)
	upgrade.Namespace = namespace
	upgrade.WaitStrategy = kube.StatusWatcherStrategy
	upgrade.WaitForJobs = true
	upgrade.Timeout = timeout
	upgrade.DependencyUpdate = true
	upgrade.Labels = labels
	_, err := upgrade.Run(releaseName, chart, values)
	return err
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

func helmPrintStatus(cfg *action.Configuration, releaseName string) error {
	status := action.NewStatus(cfg)
	release, err := status.Run(releaseName)
	if err != nil {
		return err
	}
	if typedRelease, ok := release.(*releasev1.Release); ok && typedRelease.Info != nil {
		return nil
	}
	return nil
}

func helmCommandText(namespace, valuesFile string) string {
	return fmt.Sprintf(`helm dependency update charts/patchworks-infra
helm dependency update charts/patchworks-app
helm upgrade --install patchworks-infra ./charts/patchworks-infra -n %s --create-namespace -f %s --timeout 15m --wait
helm upgrade --install patchworks-app ./charts/patchworks-app -n %s -f %s --timeout 15m --wait`,
		namespace, valuesFile, namespace, valuesFile)
}

func valuesCreatedView(width int, output, commands string) string {
	return strings.Join([]string{
		successStyle.Render("Values file created"),
		keyValue("File", output),
		keyStyle.Render("Manual install commands:"),
		codeStyle.Render(commands),
	}, "\n\n") + "\n"
}

func accessDetailsView(width int, values map[string]string, generatedAdminPassword string, installed bool) string {
	dashboard := dashboardURL(values)
	body := strings.Join([]string{
		successStyle.Render("Access details"),
		"",
		keyValue("Dashboard", terminalLink(dashboard, dashboard)),
		keyValue("Email", values["adminEmail"]),
		keyValue("Password", adminPasswordSummary(values, generatedAdminPassword, installed)),
	}, "\n")
	return page(width, plainPanel(width, body))
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

	info.StorageClasses = storageClasses(ctx, client)
	info.IngressClasses = ingressClasses(ctx, client)
	info.HasContour = contains(info.IngressClasses, "contour") || apiResourceExists(client.Discovery(), "projectcontour.io", "v1", "httpproxies")
	info.HasCertManager = crdExists(ctx, dynamicClient, "certificates.cert-manager.io")

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

func createQuayPullSecret(access kubeAccess, namespace string, creds quayCredentials) error {
	if creds.SecretName == "" {
		return fmt.Errorf("pull secret name cannot be empty")
	}
	if creds.Username == "" {
		return fmt.Errorf("quay username cannot be empty")
	}
	if creds.Password == "" {
		return fmt.Errorf("quay password/token cannot be empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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

func deleteInstallerPullSecret(access kubeAccess, namespace, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := kubernetesClient(access)
	if err != nil {
		return err
	}

	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if secret.Labels[installerLabelKey] != installerLabelValue {
		fmt.Printf("%s %s is not labelled as installer-created; keeping it.\n", warningStyle.Render("Skipped:"), name)
		return nil
	}
	return client.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

func installPhaseMonitor(access kubeAccess, namespace, chart string) func() string {
	return func() string {
		phase, err := currentInstallPhase(access, namespace, chart)
		if err != nil || strings.TrimSpace(phase) == "" {
			switch chart {
			case "infra":
				return "Installing infrastructure resources and waiting for readiness"
			case "app":
				return "Installing application resources and waiting for hooks/readiness"
			default:
				return ""
			}
		}
		return phase
	}
}

func currentInstallPhase(access kubeAccess, namespace, chart string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := kubernetesClient(access)
	if err != nil {
		return "", err
	}

	jobs, err := listInstallJobs(ctx, client, namespace)
	if err != nil {
		return "", err
	}

	switch chart {
	case "infra":
		return infrastructureReadinessPhase(ctx, client, namespace, jobs.Items)
	case "app":
		return applicationReadinessPhase(ctx, client, namespace, jobs.Items)
	default:
		return "", nil
	}
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

func infrastructureReadinessPhase(ctx context.Context, client kubernetes.Interface, namespace string, jobs []batchv1.Job) (string, error) {
	deployments, err := listInstallDeployments(ctx, client, namespace, "infra")
	if err != nil {
		return "", err
	}
	rows := installComponentRows(jobs, deployments.Items, "infra")
	return formatInstallRows("Infrastructure progress", rows), nil
}

func applicationReadinessPhase(ctx context.Context, client kubernetes.Interface, namespace string, jobs []batchv1.Job) (string, error) {
	deployments, err := listInstallDeployments(ctx, client, namespace, "app")
	if err != nil {
		return "", err
	}
	rows := installComponentRows(jobs, deployments.Items, "app")
	return formatInstallRows("Application progress", rows), nil
}

func installComponentRows(jobs []batchv1.Job, deployments []appsv1.Deployment, chart string) []installComponentRow {
	rows := hookComponentRows(jobs, chart)
	rows = append(rows, deploymentComponentRows(deployments)...)
	sort.SliceStable(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
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
			Detail: jobStatusShort(job),
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
			Detail: fmt.Sprintf("%d/%d replicas ready", deployment.Status.ReadyReplicas, desired),
		})
	}
	return rows
}

func jobInstallStatus(job batchv1.Job) installComponentStatus {
	if job.Status.Failed > 0 {
		return installStatusFailed
	}
	if job.Status.Succeeded > 0 {
		return installStatusComplete
	}
	if job.Status.Active > 0 {
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

func formatInstallRows(title string, rows []installComponentRow) string {
	if len(rows) == 0 {
		return title
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", title)
	for _, row := range rows {
		label := statusLabel(row.Status)
		line := fmt.Sprintf("- %s %s", label, row.Name)
		if strings.TrimSpace(row.Detail) != "" {
			line = fmt.Sprintf("%s %s", line, subtleStyle.Render("("+row.Detail+")"))
		}
		fmt.Fprintln(&b, line)
	}
	return strings.TrimRight(b.String(), "\n")
}

func statusLabel(status installComponentStatus) string {
	switch status {
	case installStatusComplete:
		return successStyle.Render("complete")
	case installStatusFailed:
		return errorStyle.Render("failed")
	case installStatusInProgress:
		return warningStyle.Render("in progress")
	default:
		return warningStyle.Render("waiting")
	}
}

func jobStatusShort(job batchv1.Job) string {
	return fmt.Sprintf("active=%d succeeded=%d failed=%d", job.Status.Active, job.Status.Succeeded, job.Status.Failed)
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
		fmt.Printf("\n%s %v\n", warningStyle.Render("Unable to collect install diagnostics:"), err)
		return
	}
	if strings.TrimSpace(details) == "" {
		return
	}
	fmt.Println(page(96, plainPanel(96, details)))
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

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", successStyle.Render("Patchworks status"))
	fmt.Fprintf(&b, "%s\n", keyValue("Namespace", namespace))
	fmt.Fprintf(&b, "%s\n", keyValue("Deployments", deploymentSummary(deployments.Items)))
	fmt.Fprintf(&b, "%s\n", keyValue("Pods", podSummary(pods.Items)))
	fmt.Fprintf(&b, "%s\n", keyValue("Jobs", jobSummary(jobs.Items)))

	notReadyDeployments := notReadyDeploymentNames(deployments.Items)
	if len(notReadyDeployments) > 0 {
		fmt.Fprintf(&b, "\n%s\n", warningStyle.Render("Deployments not fully ready"))
		for _, name := range notReadyDeployments {
			fmt.Fprintf(&b, "- %s\n", name)
		}
	}

	problemPods := problemPodNames(pods.Items)
	if len(problemPods) > 0 {
		fmt.Fprintf(&b, "\n%s\n", warningStyle.Render("Pods needing attention"))
		for _, name := range problemPods {
			fmt.Fprintf(&b, "- %s\n", name)
		}
	}

	failedJobs := failedJobNames(jobs.Items)
	if len(failedJobs) > 0 {
		fmt.Fprintf(&b, "\n%s\n", warningStyle.Render("Failed jobs"))
		for _, name := range failedJobs {
			fmt.Fprintf(&b, "- %s\n", name)
		}
	}

	return b.String(), nil
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
			parts = append(parts, fmt.Sprintf("%s=%d", phase, counts[phase]))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func jobSummary(jobs []batchv1.Job) string {
	var succeeded, failed, active int32
	for _, job := range jobs {
		succeeded += job.Status.Succeeded
		failed += job.Status.Failed
		active += job.Status.Active
	}
	return fmt.Sprintf("succeeded=%d, active=%d, failed=%d", succeeded, active, failed)
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
		if job.Status.Failed > 0 {
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
		),
	).WithTheme(installerTheme()).WithWidth(88)); err != nil {
		return installConfig{}, err
	}

	info.QuaySecretPresent = secretExistsInNamespace(kubeconfig, contextName, namespace, "quay-credentials")
	pullSecret, quay, err := runPullSecretForm(info.QuaySecretPresent, defaults)
	if err != nil {
		return installConfig{}, err
	}

	ingressEnabled := boolDefault(defaults.IngressEnabled, true)
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
		Kubeconfig:        values["kubeconfig"],
		Context:           values["context"],
		Namespace:         values["namespace"],
		Domain:            values["domain"],
		Scheme:            values["scheme"],
		LicenseKey:        values["licenseKey"],
		LicenseServerURL:  stringDefault(values["licenseServerUrl"], defaultLicenseServerURL),
		DashboardEnabled:  boolPtr(parseBoolDefault(values["dashboardEnabled"], true)),
		RoutingMode:       values["routingMode"],
		WorkerMode:        defaultWorkerMode(values["workerMode"]),
		IngressEnabled:    boolPtr(parseBoolDefault(values["ingressEnabled"], true)),
		IngressProvider:   values["ingressProvider"],
		IngressClass:      values["ingressClass"],
		CookieDomain:      values["cookieDomain"],
		PullSecretMode:    values["pullSecretMode"],
		PullSecret:        values["pullSecret"],
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
		keyValue("Namespace", values["namespace"]),
		keyValue("Base domain", values["domain"]),
		keyValue("URL scheme", values["scheme"]),
		keyValue("License key", configuredSummary(values["licenseKey"])),
		keyValue("License server URL", configuredSummary(values["licenseServerUrl"])),
		keyValue("Ingress", fmt.Sprintf("%s (%s/%s)", values["ingressEnabled"], values["ingressProvider"], values["ingressClass"])),
		keyValue("Dashboard", fmt.Sprintf("%s (%s routing)", values["dashboardEnabled"], values["routingMode"])),
		keyValue("Worker mode", defaultWorkerMode(values["workerMode"])),
		keyValue("Cookie/session domain", values["cookieDomain"]),
		keyValue("Image pull secret", defaultListValue(values["pullSecret"], "none")),
		keyValue("Initial company/admin", values["seedInstall"]),
	}
	if config.Quay != nil {
		lines = append(lines, keyValue("Quay secret action", fmt.Sprintf("create/update %s", config.Quay.SecretName)))
	} else if values["pullSecret"] != "" {
		lines = append(lines, keyValue("Quay secret action", "use existing secret"))
	}
	return strings.Join(lines, "\n")
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

func runInspection(kubeconfig, contextName string) (clusterInfo, error) {
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	initial := inspectModel{
		kubeconfig:  kubeconfig,
		contextName: contextName,
		spinner:     spin,
	}

	finalModel, err := tea.NewProgram(initial).Run()
	if err != nil {
		return clusterInfo{}, err
	}

	model, ok := finalModel.(inspectModel)
	if !ok {
		return clusterInfo{}, fmt.Errorf("unexpected inspection result")
	}
	if model.aborted {
		return clusterInfo{}, huh.ErrUserAborted
	}
	return model.info, model.err
}

func runProgress(title, detail string, fn func() error) error {
	return runProgressWithPhase(title, detail, nil, fn)
}

func runProgressWithPhase(title, detail string, phase func() string, fn func() error) error {
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	initial := progressModel{
		title:   title,
		detail:  detail,
		run:     fn,
		phase:   phase,
		spinner: spin,
		started: time.Now(),
	}

	finalModel, err := tea.NewProgram(initial).Run()
	if err != nil {
		return err
	}

	model, ok := finalModel.(progressModel)
	if !ok {
		return fmt.Errorf("unexpected progress result")
	}
	if model.err != nil {
		return model.err
	}

	fmt.Printf("%s %s\n", successStyle.Render("Done:"), title)
	return nil
}

func (m progressModel) Init() tea.Cmd {
	runCmd := func() tea.Msg {
		return progressResult{err: m.run()}
	}
	return tea.Batch(runCmd, m.spinner.Tick, m.phaseTick())
}

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case progressResult:
		m.err = msg.err
		return m, tea.Quit
	case progressPhaseMsg:
		if strings.TrimSpace(string(msg)) != "" {
			m.detail = string(msg)
		}
		return m, m.phaseTick()
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	}
	return m, nil
}

func (m progressModel) phaseTick() tea.Cmd {
	if m.phase == nil {
		return nil
	}
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return progressPhaseMsg(m.phase())
	})
}

func (m progressModel) View() string {
	elapsed := time.Since(m.started).Round(time.Second)
	body := fmt.Sprintf("%s %s\n\n%s\n%s",
		m.spinner.View(),
		m.title,
		m.detail,
		subtleStyle.Render(fmt.Sprintf("Elapsed: %s", elapsed)),
	)
	return page(m.width, inspectPanel(m.width, body))
}

func (m inspectModel) Init() tea.Cmd {
	inspectCmd := func() tea.Msg {
		return inspectCluster(m.kubeconfig, m.contextName)
	}
	return tea.Batch(inspectCmd, m.spinner.Tick)
}

func (m inspectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case inspectResult:
		m.info = msg.Info
		m.err = msg.Err
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyMsg:
		if m.quitConfirm {
			switch msg.String() {
			case "y", "Y", "enter", "ctrl+c":
				m.aborted = true
				return m, tea.Quit
			case "n", "N", "esc":
				m.quitConfirm = false
				return m, nil
			}
			return m, nil
		}

		if msg.String() == "ctrl+c" || msg.String() == "esc" {
			m.quitConfirm = true
			return m, nil
		}
	}

	return m, nil
}

func (m inspectModel) View() string {
	if m.quitConfirm {
		body := fmt.Sprintf("%s\n\n%s\n\n%s",
			warningStyle.Render("Quit installer?"),
			"Any unsaved selections will be lost.",
			subtleStyle.Render("Y/Enter: quit  N/Esc: continue"),
		)
		return page(m.width, inspectPanel(m.width, body))
	}

	body := fmt.Sprintf("%s Inspecting cluster\n\n%s\n%s\n\n%s",
		m.spinner.View(),
		keyValue("Kubeconfig", m.kubeconfig),
		keyValue("Context", m.contextName),
		subtleStyle.Render("Enter: continue  Esc/Ctrl+C: quit"),
	)
	return page(m.width, inspectPanel(m.width, body))
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

func inspectPanel(width int, body string) string {
	return panelStyle.Width(panelOuterWidth(width)).Render(body)
}

func plainPanel(width int, body string) string {
	return panelStyle.Width(panelOuterWidth(width)).Render(body)
}

func panelOuterWidth(width int) int {
	panelWidth := width - 14
	if width <= 0 {
		return 76
	}
	if panelWidth < 28 {
		return 28
	}
	return panelWidth
}

func panelContentWidth(width int) int {
	contentWidth := panelOuterWidth(width) - panelStyle.GetHorizontalFrameSize()
	if contentWidth < 24 {
		return 24
	}
	return contentWidth
}

func keyValue(key, value string) string {
	if value == "" {
		value = "unset"
	}
	return fmt.Sprintf("%s %s", keyStyle.Render(key+":"), value)
}

func page(width int, body string) string {
	header := brandStyle.Render("Patchworks Self-Hosted")
	if width <= 0 {
		return header + "\n\n" + body + "\n"
	}
	contentWidth := width - 4
	if contentWidth < 30 {
		contentWidth = 30
	}
	return lipgloss.NewStyle().Width(contentWidth).Padding(1, 2).Render(header+"\n\n"+body) + "\n"
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

	fmt.Fprintf(&b, "web:\n  sessionDomain: %s\n\n", quote(cookieDomain))

	fmt.Fprintf(&b, "workers:\n")
	fmt.Fprintf(&b, "  type: %s\n\n", quote(defaultWorkerMode(values["workerMode"])))

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
