package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
)

// installerBundle is the lock this installer writes for its bundled charts.
func installerBundle(t *testing.T) chartLock {
	t.Helper()
	bundled, err := prepareChartsFromRepository(chartRequest{Source: "bundled"}, nil, chartRepositoryClient{})
	if err != nil {
		t.Fatal(err)
	}
	return bundled.Lock
}

// otherBundleDigests stand for the charts bundled in another installer binary.
func otherBundleDigests() map[string]string {
	return map[string]string{
		"patchworks-infra": digest.FromString("other infra").String(),
		"patchworks-app":   digest.FromString("other app").String(),
	}
}

// writeLockBesideValues writes lock where an install using values.yaml in a
// fresh directory finds it, and returns that path.
func writeLockBesideValues(t *testing.T, lock chartLock) string {
	t.Helper()
	path := chartLockPath(filepath.Join(t.TempDir(), "values.yaml"))
	if err := writeChartLock(path, lock); err != nil {
		t.Fatal(err)
	}
	return path
}

func bundledLock(version string, digests map[string]string) chartLock {
	return chartLock{SchemaVersion: 1, Source: "bundled", Version: version, Digests: digests}
}

// A newer installer re-run with no chart flags over a lock that an older
// installer wrote for its bundled charts moves to its own bundled charts and
// says so, instead of failing on the digests.
func TestOlderBundledLockMovesToBundledCharts(t *testing.T) {
	bundled := installerBundle(t)
	const older = "0.0.1"
	path := writeLockBesideValues(t, bundledLock(older, otherBundleDigests()))

	// --save-config records the bundled source and the old release; the lock
	// still wins, so that config must not bring the old release back.
	for _, defaults := range []localConfig{{}, {ChartSource: "bundled", ChartVersion: older}} {
		request, pinned, notice, err := selectInstallCharts(cliOptions{}, defaults, path)
		if err != nil {
			t.Fatalf("config %+v: %v", defaults, err)
		}
		if request != (chartRequest{Source: "bundled"}) || pinned != nil {
			t.Fatalf("config %+v: got request %+v, pinned %+v; want this installer's bundled charts", defaults, request, pinned)
		}
		for _, want := range []string{
			"Upgrading from bundled charts " + older + ", pinned in " + path + ", to this installer's " + bundled.Version,
			"quit and run with --chart-version " + older + " for release " + older + " from GitHub",
		} {
			if !strings.Contains(notice, want) {
				t.Fatalf("config %+v: notice %q lacks %q", defaults, notice, want)
			}
		}
	}

	request, pinned, notice, err := selectInstallCharts(cliOptions{}, localConfig{}, path)
	if err != nil {
		t.Fatal(err)
	}
	summary := installSummary(installConfig{Values: map[string]string{"chartSource": "bundled", "chartVersion": bundled.Version, "chartNotice": notice}})
	if !strings.Contains(summary, bundled.Version+" (bundled)") || !strings.Contains(summary, notice) {
		t.Fatalf("install summary does not offer the upgrade:\n%s", summary)
	}

	// Accepting the summary writes the new lock; the next run is pinned to it
	// and quiet.
	charts, err := prepareCharts(request, pinned)
	if err != nil {
		t.Fatal(err)
	}
	if charts.Lock.Version != bundled.Version {
		t.Fatalf("prepared %s, want %s", charts.Lock.Version, bundled.Version)
	}
	if err := writeChartLock(path, charts.Lock); err != nil {
		t.Fatal(err)
	}
	request, pinned, notice, err = selectInstallCharts(cliOptions{}, localConfig{}, path)
	if err != nil || pinned == nil || notice != "" || request != (chartRequest{"bundled", bundled.Version}) {
		t.Fatalf("repeat run: request %+v, pinned %v, notice %q, err %v", request, pinned != nil, notice, err)
	}
	if _, err := prepareCharts(request, pinned); err != nil {
		t.Fatal(err)
	}
}

// A bundled lock this installer can't satisfy and won't upgrade from fails
// with the flags that get past it, and the flags do get past it.
func TestBundledLockMismatchNamesTheFlags(t *testing.T) {
	bundled := installerBundle(t)
	for _, tc := range []struct {
		name, version, differs string
	}{
		{"same release with different contents", bundled.Version, "this installer's bundled " + bundled.Version + " charts have different contents"},
		{"lock from a newer installer", "999.0.0", "this installer bundles older charts (" + bundled.Version + ")"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeLockBesideValues(t, bundledLock(tc.version, otherBundleDigests()))
			_, _, _, err := selectInstallCharts(cliOptions{}, localConfig{}, path)
			if err == nil {
				t.Fatal("accepted a bundled lock from another installer")
			}
			for _, want := range []string{
				path + " pins bundled charts " + tc.version + ", and " + tc.differs,
				"run with --chart-source bundled for this installer's charts",
				"run with --chart-version " + tc.version + " for release " + tc.version + " from GitHub",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q lacks %q", err, want)
				}
			}

			request, pinned, notice, err := selectInstallCharts(cliOptions{ChartSource: "bundled"}, localConfig{}, path)
			if err != nil || pinned != nil || notice != "" || request != (chartRequest{Source: "bundled"}) {
				t.Fatalf("--chart-source bundled: request %+v, pinned %v, notice %q, err %v", request, pinned != nil, notice, err)
			}
			request, pinned, notice, err = selectInstallCharts(cliOptions{ChartVersion: tc.version}, localConfig{}, path)
			if err != nil || pinned != nil || notice != "" || request != (chartRequest{"github", tc.version}) {
				t.Fatalf("--chart-version: request %+v, pinned %v, notice %q, err %v", request, pinned != nil, notice, err)
			}
		})
	}
}

func TestLocksThisInstallerCanUseAreUnchanged(t *testing.T) {
	bundled := installerBundle(t)
	for _, tc := range []struct {
		name string
		lock chartLock
		want chartRequest
	}{
		{"this installer's bundled charts", bundled, chartRequest{"bundled", bundled.Version}},
		{"a GitHub release", testChartLock("github", "0.0.1"), chartRequest{"github", "0.0.1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeLockBesideValues(t, tc.lock)
			request, pinned, notice, err := selectInstallCharts(cliOptions{}, localConfig{}, path)
			if err != nil || pinned == nil || notice != "" || request != tc.want {
				t.Fatalf("request %+v, pinned %v, notice %q, err %v; want %+v pinned", request, pinned != nil, notice, err, tc.want)
			}
		})
	}
}

// unpack-charts --chart-lock exports exactly the locked charts, so it never
// moves to newer ones; its hint drops --chart-lock, which can't be combined
// with the chart flags.
func TestUnpackChartLockFromAnotherInstaller(t *testing.T) {
	bundled := installerBundle(t)
	for _, tc := range []struct {
		name, version, differs string
	}{
		{"lock from an older installer", "0.0.1", "this installer bundles newer charts (" + bundled.Version + ")"},
		{"same release with different contents", bundled.Version, "have different contents"},
		{"lock from a newer installer", "999.0.0", "this installer bundles older charts (" + bundled.Version + ")"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeLockBesideValues(t, bundledLock(tc.version, otherBundleDigests()))
			output := filepath.Join(t.TempDir(), "charts")
			err := runUnpackCharts(cliOptions{ChartsOutputDir: output, ChartLockPath: path})
			if err == nil {
				t.Fatal("exported charts other than the locked ones")
			}
			for _, want := range []string{
				path + " pins bundled charts " + tc.version,
				tc.differs,
				"drop --chart-lock and pass --chart-source bundled for this installer's charts or --chart-version " + tc.version + " for release " + tc.version + " from GitHub",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q lacks %q", err, want)
				}
			}
			if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("wrote %s after refusing the lock: %v", output, err)
			}
		})
	}

	output := filepath.Join(t.TempDir(), "charts")
	if err := runUnpackCharts(cliOptions{ChartsOutputDir: output, ChartLockPath: writeLockBesideValues(t, bundled)}); err != nil {
		t.Fatalf("this installer's own lock: %v", err)
	}
}
