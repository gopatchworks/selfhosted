package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/opencontainers/go-digest"
	embeddedcharts "github.com/patchworks/selfhosted/charts"
	yaml "go.yaml.in/yaml/v3"
	"helm.sh/helm/v4/pkg/action"
	helmchart "helm.sh/helm/v4/pkg/chart"
	helmloader "helm.sh/helm/v4/pkg/chart/loader"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
)

const publicChartRepository = "gopatchworks/selfhosted"

var releaseCommitPattern = regexp.MustCompile(`^Update image tags for (v?[^\s]+)$`)
var commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

const installerAPIAnnotation = "selfhosted.patchworks.io/installer-api-version"
const minimumInstallerAnnotation = "selfhosted.patchworks.io/min-installer-version"
const supportedInstallerAPI = "1"

var patchworksChartNames = []string{"patchworks-infra", "patchworks-app"}

type chartRequest struct {
	Source  string
	Version string
}

// A lock describes the selected artifacts, not whether installation succeeded.
// Digests identify deterministically packed chart contents from one source commit.
type chartLock struct {
	SchemaVersion int               `yaml:"schemaVersion"`
	Source        string            `yaml:"source"`
	Commit        string            `yaml:"commit,omitempty"`
	Version       string            `yaml:"version"`
	Digests       map[string]string `yaml:"digests"`
}

type preparedCharts struct {
	Lock     chartLock
	Charts   map[string]helmchart.Charter
	Archives map[string][]byte
}

type chartRepositoryClient struct {
	HTTP       *http.Client
	APIURL     string
	ArchiveURL string
}

func publicChartClient() chartRepositoryClient {
	return chartRepositoryClient{
		HTTP:       &http.Client{Timeout: 45 * time.Second},
		APIURL:     "https://api.github.com/repos/" + publicChartRepository,
		ArchiveURL: "https://codeload.github.com/" + publicChartRepository + "/tar.gz/",
	}
}

func exactChartVersion(value string) (string, error) {
	parsed, err := semver.StrictNewVersion(strings.TrimPrefix(value, "v"))
	if err != nil {
		return "", fmt.Errorf("chart version %q must be an exact semantic version (X.Y.Z) or latest", value)
	}
	return parsed.String(), nil
}

func validateChartOptions(source, chartVersion string) error {
	if source != "" && source != "github" && source != "bundled" {
		return fmt.Errorf("chart source must be github or bundled, got %q", source)
	}
	if source == "bundled" && chartVersion != "" {
		return fmt.Errorf("--chart-version cannot be combined with --chart-source bundled")
	}
	if chartVersion != "" && chartVersion != "latest" {
		_, err := exactChartVersion(chartVersion)
		return err
	}
	return nil
}

func chooseChartRequest(opts cliOptions, defaults localConfig, previous *chartLock) (chartRequest, *chartLock, error) {
	if err := validateChartOptions(opts.ChartSource, opts.ChartVersion); err != nil {
		return chartRequest{}, nil, err
	}
	if opts.ChartSource == "bundled" {
		return chartRequest{Source: "bundled"}, nil, nil
	}
	// The lock wins over config defaults, including a stale config saved before
	// an explicit latest run. Changing versions always requires an explicit flag.
	if previous != nil && opts.ChartVersion == "" && (opts.ChartSource == "" || opts.ChartSource == previous.Source) {
		return chartRequest{Source: previous.Source, Version: previous.Version}, previous, nil
	}
	source, requested := defaults.ChartSource, defaults.ChartVersion
	if opts.ChartVersion != "" {
		source, requested = "github", opts.ChartVersion
	} else if opts.ChartSource == "github" {
		if source == "bundled" {
			requested = ""
		}
		source = "github"
	}
	if source != "" && source != "bundled" && source != "github" {
		return chartRequest{}, nil, fmt.Errorf("installer.chartSource must be github or bundled")
	}
	if source == "" && requested != "" {
		source = "github"
	}
	if source == "" && previous != nil {
		source = previous.Source
	}
	if source == "" {
		source = "github"
	}
	if source == "bundled" {
		if requested == "latest" {
			return chartRequest{}, nil, fmt.Errorf("bundled charts cannot use version latest")
		}
	} else if requested == "" {
		requested = "latest"
	}
	if requested != "" && requested != "latest" {
		var err error
		requested, err = exactChartVersion(requested)
		if err != nil {
			return chartRequest{}, nil, err
		}
	}

	// Only an explicit CLI latest refreshes a saved moving selection.
	if previous != nil && previous.Source == source && opts.ChartVersion != "latest" &&
		(requested == "" || requested == "latest" || requested == previous.Version) {
		return chartRequest{Source: source, Version: previous.Version}, previous, nil
	}
	return chartRequest{Source: source, Version: requested}, nil, nil
}

// GitHub's public commit history is the release index used by the existing
// automation. Check the downloaded values too; a commit message alone is not
// sufficient evidence of a release. No GitHub token or local login is used.
func (client chartRepositoryClient) resolveRelease(requested string) (string, string, error) {
	for page := 1; page <= 100; page++ {
		url := fmt.Sprintf("%s/commits?sha=main&path=charts/patchworks-app/values.yaml&per_page=100&page=%d", client.APIURL, page)
		data, headers, err := client.get(url, 8<<20)
		if err != nil {
			return "", "", err
		}
		var commits []struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message string `json:"message"`
			} `json:"commit"`
		}
		if err := json.Unmarshal(data, &commits); err != nil {
			return "", "", fmt.Errorf("read public release history: %w", err)
		}
		for _, commit := range commits {
			subject := strings.SplitN(commit.Commit.Message, "\n", 2)[0]
			match := releaseCommitPattern.FindStringSubmatch(subject)
			if len(match) != 2 {
				continue
			}
			releaseVersion, err := exactChartVersion(match[1])
			if err != nil {
				continue
			}
			parsed, _ := semver.StrictNewVersion(releaseVersion)
			if requested == "latest" && parsed.Prerelease() != "" {
				continue
			}
			if requested != "latest" && requested != releaseVersion {
				continue
			}
			if !commitSHAPattern.MatchString(commit.SHA) {
				return "", "", fmt.Errorf("invalid commit SHA in public release history")
			}
			return releaseVersion, commit.SHA, nil
		}
		if !strings.Contains(headers.Get("Link"), `rel="next"`) {
			break
		}
	}
	return "", "", fmt.Errorf("release %s was not found in the public values-update history", requested)
}

func (client chartRepositoryClient) get(url string, limit int64) ([]byte, http.Header, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("User-Agent", "patchworks-installer/"+version)
	response, err := client.HTTP.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("download public charts: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, nil, fmt.Errorf("GitHub's anonymous request limit was reached; retry after the reset (Unix timestamp %s), or use --chart-source bundled", response.Header.Get("X-RateLimit-Reset"))
		}
		return nil, nil, fmt.Errorf("download public charts: HTTP %d from %s", response.StatusCode, url)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > limit {
		return nil, nil, fmt.Errorf("public chart download exceeds %d bytes", limit)
	}
	return data, response.Header, nil
}

func prepareCharts(request chartRequest, pinned *chartLock) (*preparedCharts, error) {
	return prepareChartsFromRepository(request, pinned, publicChartClient())
}

func prepareChartsFromRepository(request chartRequest, pinned *chartLock, client chartRepositoryClient) (*preparedCharts, error) {
	if request.Source != "bundled" && request.Source != "github" {
		return nil, fmt.Errorf("unknown chart source %q", request.Source)
	}
	if pinned != nil {
		if err := validateChartLock(*pinned); err != nil {
			return nil, err
		}
		if pinned.Source != request.Source || pinned.Version != request.Version {
			return nil, fmt.Errorf("chart request does not match its lock")
		}
	}
	commit := ""
	var chartFiles map[string]map[string][]byte
	if request.Source == "github" {
		if pinned != nil {
			commit = pinned.Commit
		} else {
			var err error
			request.Version, commit, err = client.resolveRelease(request.Version)
			if err != nil {
				return nil, err
			}
		}
		data, _, err := client.get(client.ArchiveURL+commit, 32<<20)
		if err != nil {
			return nil, err
		}
		chartFiles, err = chartsFromSnapshot(data)
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		chartFiles, err = bundledChartFiles()
		if err != nil {
			return nil, err
		}
	}
	result := &preparedCharts{
		Lock:   chartLock{SchemaVersion: 1, Source: request.Source, Version: request.Version, Commit: commit, Digests: map[string]string{}},
		Charts: map[string]helmchart.Charter{}, Archives: map[string][]byte{},
	}
	chartMetadataVersion := ""
	for _, name := range patchworksChartNames {
		archive, err := packChartFiles(name, chartFiles[name])
		if err != nil {
			return nil, err
		}
		chart, err := helmloader.LoadArchive(bytes.NewReader(archive))
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", name, err)
		}
		artifactDigest := digest.FromBytes(archive).String()
		if pinned != nil && artifactDigest != pinned.Digests[name] {
			return nil, fmt.Errorf("%s contents differ from the saved chart lock", name)
		}
		chartMetadataVersion, err = validateSelectedChart(chart, name, chartMetadataVersion, version)
		if err != nil {
			return nil, err
		}
		accessor, err := helmchart.NewAccessor(chart)
		if err != nil {
			return nil, err
		}
		imageValues, _ := accessor.Values()["image"].(map[string]any)
		imageTag, _ := imageValues["tag"].(string)
		releaseVersion, err := exactChartVersion(imageTag)
		if err != nil {
			return nil, fmt.Errorf("%s image.tag: %w", name, err)
		}
		if result.Lock.Version == "" {
			result.Lock.Version = releaseVersion
		}
		if releaseVersion != result.Lock.Version {
			return nil, fmt.Errorf("%s image.tag %s does not match selected release %s", name, imageTag, result.Lock.Version)
		}
		result.Charts[name], result.Archives[name] = chart, archive
		result.Lock.Digests[name] = artifactDigest
	}
	return result, nil
}

func bundledChartFiles() (map[string]map[string][]byte, error) {
	files := map[string]map[string][]byte{}
	for _, name := range patchworksChartNames {
		files[name] = map[string][]byte{}
		err := fs.WalkDir(embeddedcharts.FS, name, func(file string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			data, err := embeddedcharts.FS.ReadFile(file)
			if err == nil {
				files[name][strings.TrimPrefix(file, name+"/")] = data
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func chartsFromSnapshot(data []byte) (map[string]map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("read repository snapshot: %w", err)
	}
	defer gz.Close()
	expanded := &io.LimitedReader{R: gz, N: 128 << 20}
	reader := tar.NewReader(expanded)
	files := map[string]map[string][]byte{"patchworks-app": {}, "patchworks-infra": {}}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			if expanded.N == 0 {
				return nil, fmt.Errorf("repository snapshot exceeds 128 MiB expanded")
			}
			break
		}
		if err != nil {
			return nil, err
		}
		parts := strings.SplitN(header.Name, "/", 4)
		if len(parts) != 4 || parts[1] != "charts" {
			continue
		}
		chartFiles, wanted := files[parts[2]]
		if !wanted {
			continue
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		relative := parts[3]
		if header.Typeflag != tar.TypeReg || relative == "." || path.Clean(relative) != relative || strings.HasPrefix(relative, "/") || strings.Contains(relative, "\\") || strings.HasPrefix(relative, "../") {
			return nil, fmt.Errorf("unsupported chart snapshot entry %q", header.Name)
		}
		if _, exists := chartFiles[relative]; exists {
			return nil, fmt.Errorf("duplicate chart file %q", header.Name)
		}
		if header.Size > 16<<20 {
			return nil, fmt.Errorf("chart file %s exceeds 16 MiB", header.Name)
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		chartFiles[relative] = content
	}
	return files, nil
}

func packChartFiles(name string, files map[string][]byte) ([]byte, error) {
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gz)
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		data := files[key]
		if err := writer.WriteHeader(&tar.Header{Name: name + "/" + key, Mode: 0644, Size: int64(len(data))}); err != nil {
			return nil, err
		}
		if _, err := writer.Write(data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func validateSelectedChart(chart helmchart.Charter, name, expectedVersion, installerVersion string) (string, error) {
	accessor, err := helmchart.NewAccessor(chart)
	if err != nil {
		return "", err
	}
	metadata := accessor.MetadataAsMap()
	chartVersion, _ := metadata["Version"].(string)
	annotations, _ := metadata["Annotations"].(map[string]string)
	meta := struct {
		Name        string
		Version     string
		Annotations map[string]string
	}{accessor.Name(), chartVersion, annotations}
	if meta.Name != name {
		return "", fmt.Errorf("expected chart %s, received %s", name, meta.Name)
	}
	if _, err := exactChartVersion(meta.Version); err != nil {
		return "", err
	}
	if expectedVersion != "" && meta.Version != expectedVersion {
		return "", fmt.Errorf("%s version %s does not match selected release %s", name, meta.Version, expectedVersion)
	}
	if accessor.IsLibraryChart() {
		return "", fmt.Errorf("%s is a library chart and cannot be installed", name)
	}
	if err := action.CheckDependencies(chart, accessor.MetaDependencies()); err != nil {
		return "", fmt.Errorf("%s dependencies: %w", name, err)
	}
	// Unannotated releases predate the contract and use API 1.
	if api := meta.Annotations[installerAPIAnnotation]; api != "" && api != supportedInstallerAPI {
		return "", fmt.Errorf("%s %s requires installer API %s; this installer supports %s. Update the installer", name, meta.Version, api, supportedInstallerAPI)
	}
	if minimum := meta.Annotations[minimumInstallerAnnotation]; minimum != "" {
		min, err := semver.StrictNewVersion(strings.TrimPrefix(minimum, "v"))
		if err != nil {
			return "", fmt.Errorf("%s has invalid minimum installer version %q", name, minimum)
		}
		current, err := semver.StrictNewVersion(strings.TrimPrefix(installerVersion, "v"))
		if err != nil || current.LessThan(min) {
			return "", fmt.Errorf("%s %s requires installer >= %s; current version is %s. Update the installer or build with -ldflags '-X main.version=X.Y.Z'", name, meta.Version, min, installerVersion)
		}
	}
	return meta.Version, nil
}

func validateChartLock(lock chartLock) error {
	if lock.SchemaVersion != 1 {
		return fmt.Errorf("unsupported chart lock schema %d", lock.SchemaVersion)
	}
	if lock.Source != "github" && lock.Source != "bundled" {
		return fmt.Errorf("invalid chart lock source %q", lock.Source)
	}
	if lock.Source == "github" && !commitSHAPattern.MatchString(lock.Commit) {
		return fmt.Errorf("chart lock requires an exact public repository commit SHA")
	}
	if _, err := exactChartVersion(lock.Version); err != nil {
		return fmt.Errorf("invalid chart lock: %w", err)
	}
	for _, name := range patchworksChartNames {
		if _, err := digest.Parse(lock.Digests[name]); err != nil {
			return fmt.Errorf("invalid chart lock digest for %s: %w", name, err)
		}
	}
	return nil
}

func readChartLock(path string) (*chartLock, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var lock chartLock
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&lock); err != nil {
		return nil, fmt.Errorf("read chart lock %s: %w", path, err)
	}
	if err := validateChartLock(lock); err != nil {
		return nil, fmt.Errorf("read chart lock %s: %w", path, err)
	}
	return &lock, nil
}

func writeChartLock(path string, lock chartLock) error {
	if err := validateChartLock(lock); err != nil {
		return err
	}
	data, err := yaml.Marshal(lock)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".charts-lock-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func selectedHelmCommandText(namespace, valuesFile string, installInfrastructure bool) string {
	lockPath := cleanPath(valuesFile) + ".charts.lock.yaml"
	return "patchworks-installer unpack-charts --chart-lock " + shellQuote(lockPath) + " --output ./charts\n" + helmCommandText(namespace, valuesFile, installInfrastructure)
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func runUnpackCharts(opts cliOptions) error {
	// Merging chart versions can leave removed templates behind. Require fresh
	// chart directories while allowing unrelated files in the output directory.
	for _, name := range patchworksChartNames {
		path := filepath.Join(cleanPath(opts.ChartsOutputDir), name)
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("%s already exists; choose a fresh --output directory to avoid mixing chart versions", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	var request chartRequest
	var pinned *chartLock
	var err error
	if opts.ChartLockPath != "" {
		pinned, err = readChartLock(cleanPath(opts.ChartLockPath))
		if err != nil {
			return err
		}
		if pinned == nil {
			return fmt.Errorf("chart lock %s does not exist", opts.ChartLockPath)
		}
		request = chartRequest{Source: pinned.Source, Version: pinned.Version}
	} else {
		// Preserve the existing unpack-charts default, independently of installs.
		if opts.ChartSource == "" && opts.ChartVersion == "" {
			opts.ChartSource = "bundled"
		}
		request, _, err = chooseChartRequest(opts, localConfig{}, nil)
		if err != nil {
			return err
		}
	}
	charts, err := prepareCharts(request, pinned)
	if err != nil {
		return err
	}
	for _, name := range patchworksChartNames {
		if err := chartutil.Expand(cleanPath(opts.ChartsOutputDir), bytes.NewReader(charts.Archives[name])); err != nil {
			return err
		}
	}
	fmt.Printf("Selected release %s (%s)\n", charts.Lock.Version, charts.Lock.Source)
	return writeChartLock(filepath.Join(cleanPath(opts.ChartsOutputDir), "charts.lock.yaml"), charts.Lock)
}
