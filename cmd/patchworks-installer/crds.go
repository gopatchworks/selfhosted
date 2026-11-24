package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"

	"helm.sh/helm/v4/pkg/action"
	helmchart "helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/common"
	"helm.sh/helm/v4/pkg/kube"
)

// The infra chart ships KubeFaaS's CustomResourceDefinitions (Function and
// Profile) in crds/, and renders the Profiles. Helm applies a chart's crds/
// only when it first installs a release, so an install that turns KubeFaaS on
// later never gets them, nor does one whose chart has newer CRDs. The
// controller then can't list or watch its resources, and Helm can't build the
// release's Profiles. With KubeFaaS on, the installer applies them itself
// before every infra install or upgrade.

// crdEstablishTimeout is how long the installer waits for the API server to
// serve applied CRDs, as Helm does for crds/.
const crdEstablishTimeout = 60 * time.Second

// chartCRDFiles are the manifests in the chart's own crds/ directory, not its
// subcharts'.
func chartCRDFiles(chart helmchart.Charter) ([]*common.File, error) {
	accessor, err := helmchart.NewAccessor(chart)
	if err != nil {
		return nil, err
	}
	var files []*common.File
	for _, file := range accessor.Files() {
		switch filepath.Ext(file.Name) {
		case ".yaml", ".yml", ".json":
			if strings.HasPrefix(file.Name, "crds/") {
				files = append(files, file)
			}
		}
	}
	return files, nil
}

// applyChartCRDs server-side applies the chart's CRDs as Helm's field manager,
// taking over fields another manager set, and waits until they are served. It
// then clears cfg's cached discovery so the release that follows can build
// resources of their kinds.
func applyChartCRDs(cfg *action.Configuration, chart helmchart.Charter) error {
	files, err := chartCRDFiles(chart)
	if err != nil || len(files) == 0 {
		return err
	}
	var applied kube.ResourceList
	for _, file := range files {
		resources, err := cfg.KubeClient.Build(bytes.NewReader(file.Data), false)
		if err != nil {
			return fmt.Errorf("reading CRD %s: %w", file.Name, err)
		}
		if _, err := cfg.KubeClient.Create(resources, kube.ClientCreateOptionServerSideApply(true, true)); err != nil {
			return fmt.Errorf("applying CRD %s: %w", file.Name, err)
		}
		applied = append(applied, resources...)
	}
	waiter, err := cfg.KubeClient.GetWaiter(kube.StatusWatcherStrategy)
	if err != nil {
		return err
	}
	if err := waiter.Wait(applied, crdEstablishTimeout); err != nil {
		return fmt.Errorf("waiting for the CRDs in crds/ to be served: %w", err)
	}
	return resetDiscovery(cfg)
}

// resetDiscovery drops the API groups and REST mappings cfg has cached.
func resetDiscovery(cfg *action.Configuration) error {
	if cfg.RESTClientGetter == nil {
		return nil
	}
	discoveryClient, err := cfg.RESTClientGetter.ToDiscoveryClient()
	if err != nil {
		return err
	}
	discoveryClient.Invalidate()
	_, _ = discoveryClient.ServerGroups()
	restMapper, err := cfg.RESTClientGetter.ToRESTMapper()
	if err != nil {
		return err
	}
	if resettable, ok := restMapper.(meta.ResettableRESTMapper); ok {
		resettable.Reset()
	}
	return nil
}

// kubefaasCRDCommand applies the infra chart's CRDs by hand, as applyChartCRDs
// does, before a manual infra install with KubeFaaS on and a chart that has them.
const kubefaasCRDCommand = "kubectl apply --server-side --field-manager=helm --force-conflicts -f ./charts/patchworks-infra/crds/\n"
