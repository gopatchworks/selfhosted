package main

import (
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	yaml "go.yaml.in/yaml/v3"

	"helm.sh/helm/v4/pkg/action"
	helmloader "helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/kube"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
)

// crdRecordingKubeClient records the CRDs applyChartCRDs builds, applies and
// waits for.
type crdRecordingKubeClient struct {
	kubefake.PrintingKubeClient
	built   []string
	created []string
	waited  []string
	timeout time.Duration
}

func (c *crdRecordingKubeClient) Build(reader io.Reader, _ bool) (kube.ResourceList, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var crd struct {
		Kind     string
		Metadata struct{ Name string }
	}
	if err := yaml.Unmarshal(data, &crd); err != nil {
		return nil, err
	}
	c.built = append(c.built, crd.Kind+"/"+crd.Metadata.Name)
	return kube.ResourceList{{Name: crd.Metadata.Name}}, nil
}

func (c *crdRecordingKubeClient) Create(resources kube.ResourceList, _ ...kube.ClientCreateOption) (*kube.Result, error) {
	for _, info := range resources {
		c.created = append(c.created, info.Name)
	}
	return &kube.Result{Created: resources}, nil
}

func (c *crdRecordingKubeClient) GetWaiter(kube.WaitStrategy) (kube.Waiter, error) {
	return crdRecordingWaiter{PrintingKubeWaiter: &kubefake.PrintingKubeWaiter{Out: io.Discard}, client: c}, nil
}

type crdRecordingWaiter struct {
	*kubefake.PrintingKubeWaiter
	client *crdRecordingKubeClient
}

func (w crdRecordingWaiter) Wait(resources kube.ResourceList, timeout time.Duration) error {
	for _, info := range resources {
		w.client.waited = append(w.client.waited, info.Name)
	}
	w.client.timeout = timeout
	return nil
}

var kubefaasCRDs = []string{"functions.kubefaas.patchworks.io", "profiles.kubefaas.patchworks.io"}

// The bundled infra chart carries KubeFaaS's CRDs: the embed list names
// crds/, and Helm reads them from the chart's files.
func TestBundledInfraChartCarriesKubeFaaSCRDs(t *testing.T) {
	bundled, cleanup, err := embeddedChartPath("patchworks-infra")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for name, path := range map[string]string{"bundled": bundled, "repository": filepath.Join("..", "..", "charts", "patchworks-infra")} {
		chart, err := helmloader.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		files, err := chartCRDFiles(chart)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, file := range files {
			names = append(names, file.Name)
		}
		if want := []string{"crds/kubefaas.io_functions.yaml", "crds/kubefaas.io_profiles.yaml"}; !reflect.DeepEqual(names, want) {
			t.Fatalf("%s chart CRD files = %v, want %v", name, names, want)
		}
		if got := chart.(*chartv2.Chart).CRDObjects(); len(got) != 2 {
			t.Fatalf("%s chart: Helm sees %d CRDs, want 2", name, len(got))
		}
	}
}

// With KubeFaaS on, the installer applies every CRD before the infra release
// and waits for the API server to serve them, as Helm does only on a first
// install.
func TestApplyChartCRDsAppliesAndWaitsForEveryCRD(t *testing.T) {
	chart, err := helmloader.Load(filepath.Join("..", "..", "charts", "patchworks-infra"))
	if err != nil {
		t.Fatal(err)
	}
	client := &crdRecordingKubeClient{PrintingKubeClient: kubefake.PrintingKubeClient{Out: io.Discard}}
	if err := applyChartCRDs(&action.Configuration{KubeClient: client}, chart); err != nil {
		t.Fatal(err)
	}
	if want := []string{"CustomResourceDefinition/" + kubefaasCRDs[0], "CustomResourceDefinition/" + kubefaasCRDs[1]}; !reflect.DeepEqual(client.built, want) {
		t.Fatalf("built %v, want %v", client.built, want)
	}
	if !reflect.DeepEqual(client.created, kubefaasCRDs) || !reflect.DeepEqual(client.waited, kubefaasCRDs) {
		t.Fatalf("applied %v and waited for %v, want %v", client.created, client.waited, kubefaasCRDs)
	}
	if client.timeout != crdEstablishTimeout {
		t.Fatalf("waited %v, want %v", client.timeout, crdEstablishTimeout)
	}

	// A chart without crds/, such as an older infra chart, applies nothing.
	client = &crdRecordingKubeClient{PrintingKubeClient: kubefake.PrintingKubeClient{Out: io.Discard}}
	if err := applyChartCRDs(&action.Configuration{KubeClient: client}, testHelmChart()); err != nil {
		t.Fatal(err)
	}
	if len(client.built)+len(client.created)+len(client.waited) != 0 {
		t.Fatalf("without crds/: built %v, applied %v, waited for %v", client.built, client.created, client.waited)
	}
}

// The printed commands apply the CRDs only when the selected infra chart has
// them.
func TestCheckKubeFaaSRecordsWhetherTheChartHasCRDs(t *testing.T) {
	chart, err := helmloader.Load(filepath.Join("..", "..", "charts", "patchworks-infra"))
	if err != nil {
		t.Fatal(err)
	}
	values := kubefaasValues(false, "")
	checkKubeFaaS(values, chart)
	if !manualKubeFaaSFor(values, true).CRDs {
		t.Fatalf("kubefaasCRDs = %q with this chart", values["kubefaasCRDs"])
	}
	values = kubefaasValues(false, "")
	checkKubeFaaS(values, testHelmChart())
	if manualKubeFaaSFor(values, true).CRDs {
		t.Fatalf("kubefaasCRDs = %q with a chart without crds/", values["kubefaasCRDs"])
	}
}

func TestKubeFaaSValuesEnabled(t *testing.T) {
	for _, tc := range []struct {
		values map[string]any
		want   bool
	}{
		{map[string]any{"kubefaas": map[string]any{"enabled": true}}, true},
		{map[string]any{"kubefaas": map[string]any{"enabled": false}}, false},
		{map[string]any{"kubefaas": map[string]any{}}, false},
		{map[string]any{}, false},
	} {
		if got := kubefaasValuesEnabled(tc.values); got != tc.want {
			t.Errorf("kubefaasValuesEnabled(%v) = %v, want %v", tc.values, got, tc.want)
		}
	}
	if !strings.HasSuffix(kubefaasCRDCommand, "./charts/patchworks-infra/crds/\n") {
		t.Fatalf("CRD command %q doesn't apply the unpacked chart's crds/", kubefaasCRDCommand)
	}
}
