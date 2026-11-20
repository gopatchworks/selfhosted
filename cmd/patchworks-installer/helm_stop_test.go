package main

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/kube"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	rcommon "helm.sh/helm/v4/pkg/release/common"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
)

// slowKubeClient makes Helm's readiness wait take a while, standing in for
// an install that waits on a cluster.
type slowKubeClient struct {
	*kubefake.FailingKubeClient
	delay *time.Duration
}

func (c slowKubeClient) GetWaiter(strategy kube.WaitStrategy) (kube.Waiter, error) {
	waiter, err := c.FailingKubeClient.GetWaiter(strategy)
	return slowWaiter{Waiter: waiter, delay: *c.delay}, err
}

func (c slowKubeClient) GetWaiterWithOptions(strategy kube.WaitStrategy, opts ...kube.WaitOption) (kube.Waiter, error) {
	waiter, err := c.FailingKubeClient.GetWaiterWithOptions(strategy, opts...)
	return slowWaiter{Waiter: waiter, delay: *c.delay}, err
}

type slowWaiter struct {
	kube.Waiter
	delay time.Duration
}

func (w slowWaiter) Wait(resources kube.ResourceList, timeout time.Duration) error {
	time.Sleep(w.delay)
	return w.Waiter.Wait(resources, timeout)
}

func (w slowWaiter) WaitWithJobs(resources kube.ResourceList, timeout time.Duration) error {
	time.Sleep(w.delay)
	return w.Waiter.WaitWithJobs(resources, timeout)
}

func testHelmConfig(delay *time.Duration) *action.Configuration {
	return &action.Configuration{
		Releases:     storage.Init(driver.NewMemory()),
		KubeClient:   slowKubeClient{FailingKubeClient: &kubefake.FailingKubeClient{PrintingKubeClient: kubefake.PrintingKubeClient{Out: io.Discard}}, delay: delay},
		Capabilities: common.DefaultCapabilities,
	}
}

func testHelmChart() *chartv2.Chart {
	return &chartv2.Chart{
		Metadata: &chartv2.Metadata{APIVersion: "v2", Name: "demo", Version: "0.1.0"},
		Templates: []*common.File{{
			Name:    "templates/configmap.yaml",
			ModTime: time.Now(),
			Data:    []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"),
		}},
	}
}

func lastReleaseStatus(t *testing.T, cfg *action.Configuration, name string) rcommon.Status {
	t.Helper()
	last, err := cfg.Releases.Last(name)
	if err != nil {
		t.Fatalf("read release %s: %v", name, err)
	}
	release, ok := last.(*releasev1.Release)
	if !ok {
		t.Fatalf("unexpected release type %T", last)
	}
	return release.Info.Status
}

// Stopping a step cancels its context. Helm then records the release as
// failed rather than leaving it pending, so the next run can upgrade it.
func TestStoppedHelmInstallAndUpgradeAreRecordedAsFailed(t *testing.T) {
	delay := 5 * time.Second
	cfg := testHelmConfig(&delay)
	chart := testHelmChart()

	stop := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 200*time.Millisecond)
	}

	ctx, cancel := stop()
	started := time.Now()
	err := helmUpgradeOrInstall(ctx, cfg, "demo", "default", chart, map[string]any{}, false, time.Minute, nil)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stopped install error = %v", err)
	}
	if waited := time.Since(started); waited > 2*time.Second {
		t.Fatalf("stopped install took %s to return", waited)
	}
	if status := lastReleaseStatus(t, cfg, "demo"); status != rcommon.StatusFailed {
		t.Fatalf("stopped install left release %s", status)
	}

	ctx, cancel = stop()
	err = helmUpgradeOrInstall(ctx, cfg, "demo", "default", chart, map[string]any{}, false, time.Minute, nil)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stopped upgrade error = %v", err)
	}
	if status := lastReleaseStatus(t, cfg, "demo"); status != rcommon.StatusFailed {
		t.Fatalf("stopped upgrade left release %s", status)
	}

	delay = 0
	if err := helmUpgradeOrInstall(context.Background(), cfg, "demo", "default", chart, map[string]any{}, false, time.Minute, nil); err != nil {
		t.Fatalf("rerun after a stop failed: %v", err)
	}
	if status := lastReleaseStatus(t, cfg, "demo"); status != rcommon.StatusDeployed {
		t.Fatalf("rerun left release %s", status)
	}
}
