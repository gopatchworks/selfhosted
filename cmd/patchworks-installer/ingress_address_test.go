package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/huh"
	yaml "go.yaml.in/yaml/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// envoyService is the Envoy Service as the Contour chart renders it with its
// defaults, with the node ports a cluster allocates.
func envoyService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      contourEnvoyService,
			Namespace: contourNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "contour",
				"app.kubernetes.io/instance":  "contour",
				"app.kubernetes.io/component": "envoy",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeLoadBalancer,
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, NodePort: 30868},
				{Name: "https", Port: 443, NodePort: 31618},
			},
		},
	}
}

func withAddress(svc *corev1.Service, ingress corev1.LoadBalancerIngress) *corev1.Service {
	svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{ingress}
	return svc
}

func TestLoadBalancerWarningNamesNodePortsWhenPending(t *testing.T) {
	lines := loadBalancerWarning(*envoyService())
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	text := strings.Join(lines, "\n")
	for _, want := range []string{
		"No LoadBalancer address was assigned to Service projectcontour/contour-envoy",
		"ports 80 and 443",
		"node ports 30868 for port 80 and 31618 for port 443",
		"on a node that runs the ingress controller",
		"MetalLB in L2 mode",
		"NodePort or host ports",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("warning omits %q:\n%s", want, text)
		}
	}
	if notice := loadBalancerNotice(*envoyService()); !strings.Contains(notice, "projectcontour/contour-envoy") || !strings.Contains(notice, "30868 for port 80") {
		t.Errorf("summary notice = %q", notice)
	}
}

func TestLoadBalancerWarningSaysAnyNodeForClusterTrafficPolicy(t *testing.T) {
	svc := envoyService()
	svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyCluster
	text := strings.Join(loadBalancerWarning(*svc), "\n")
	if !strings.Contains(text, "31618 for port 443 on any node.") {
		t.Errorf("warning does not say any node:\n%s", text)
	}
}

func TestLoadBalancerWarningWithoutNodePorts(t *testing.T) {
	svc := envoyService()
	for i := range svc.Spec.Ports {
		svc.Spec.Ports[i].NodePort = 0
	}
	lines := loadBalancerWarning(*svc)
	text := strings.Join(lines, "\n")
	if len(lines) != 2 || strings.Contains(text, "node port") || strings.Contains(text, " 0 ") {
		t.Errorf("warning without node ports:\n%s", text)
	}
	if notice := loadBalancerNotice(*svc); strings.Contains(notice, "meanwhile") {
		t.Errorf("summary notice names node ports it does not have: %q", notice)
	}
}

func TestLoadBalancerWarningSilentWithAddress(t *testing.T) {
	cases := map[string]*corev1.Service{
		"ip":       withAddress(envoyService(), corev1.LoadBalancerIngress{IP: "203.0.113.10"}),
		"hostname": withAddress(envoyService(), corev1.LoadBalancerIngress{Hostname: "envoy.elb.example.com"}),
		"external IP": func() *corev1.Service {
			svc := envoyService()
			svc.Spec.ExternalIPs = []string{"192.0.2.5"}
			return svc
		}(),
	}
	for name, svc := range cases {
		if lines := loadBalancerWarning(*svc); len(lines) != 0 {
			t.Errorf("%s: unexpected warning %q", name, lines)
		}
		if notice := loadBalancerNotice(*svc); notice != "" {
			t.Errorf("%s: unexpected notice %q", name, notice)
		}
	}
	if lines := loadBalancerWarning(*withAddress(envoyService(), corev1.LoadBalancerIngress{})); len(lines) == 0 {
		t.Error("an ingress entry without an IP or hostname counts as an address")
	}
}

func TestLoadBalancerWarningSilentForNodePortAndClusterIP(t *testing.T) {
	for _, serviceType := range []corev1.ServiceType{corev1.ServiceTypeNodePort, corev1.ServiceTypeClusterIP} {
		svc := envoyService()
		svc.Spec.Type = serviceType
		if lines := loadBalancerWarning(*svc); len(lines) != 0 {
			t.Errorf("%s: unexpected warning %q", serviceType, lines)
		}
	}
}

func TestWaitForLoadBalancerAddressReturnsAddressedService(t *testing.T) {
	client := fake.NewClientset(withAddress(envoyService(), corev1.LoadBalancerIngress{IP: "203.0.113.10"}))
	start := time.Now()
	svc, err := waitForLoadBalancerAddress(context.Background(), client, contourNamespace, contourEnvoyService, time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("waited %s for a Service that already had an address", elapsed)
	}
	if got := ingressAddressDone(svc, "projectcontour/contour-envoy"); got != "Envoy address 203.0.113.10 · Service projectcontour/contour-envoy" {
		t.Errorf("trail line = %q", got)
	}
}

func TestWaitForLoadBalancerAddressGivesUpWhenPending(t *testing.T) {
	client := fake.NewClientset(envoyService())
	svc, err := waitForLoadBalancerAddress(context.Background(), client, contourNamespace, contourEnvoyService, 50*time.Millisecond, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("running out of time is not an error: %v", err)
	}
	if svc == nil || !loadBalancerPending(*svc) {
		t.Fatalf("got %+v, want the pending Service", svc)
	}
	if lines := loadBalancerWarning(*svc); len(lines) == 0 || !strings.Contains(strings.Join(lines, "\n"), "30868") {
		t.Errorf("warning = %q", lines)
	}
}

func TestWaitForLoadBalancerAddressSeesLateAddress(t *testing.T) {
	client := fake.NewClientset(envoyService())
	go func() {
		time.Sleep(30 * time.Millisecond)
		svc := withAddress(envoyService(), corev1.LoadBalancerIngress{IP: "203.0.113.10"})
		_, _ = client.CoreV1().Services(contourNamespace).UpdateStatus(context.Background(), svc, metav1.UpdateOptions{})
	}()
	svc, err := waitForLoadBalancerAddress(context.Background(), client, contourNamespace, contourEnvoyService, 10*time.Second, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if svc == nil || loadBalancerPending(*svc) {
		t.Fatalf("got %+v, want the Service with its address", svc)
	}
}

func TestWaitForLoadBalancerAddressStopsWithContext(t *testing.T) {
	client := fake.NewClientset(envoyService())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := waitForLoadBalancerAddress(ctx, client, contourNamespace, contourEnvoyService, time.Minute, time.Second)
	if err == nil {
		t.Fatal("a cancelled wait returned no error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("a cancelled wait took %s", elapsed)
	}
}

func TestWaitForLoadBalancerAddressMissingService(t *testing.T) {
	client := fake.NewClientset()
	svc, err := waitForLoadBalancerAddress(context.Background(), client, contourNamespace, contourEnvoyService, 30*time.Millisecond, 10*time.Millisecond)
	if err != nil || svc != nil {
		t.Fatalf("got %+v, %v; want nil, nil", svc, err)
	}
	if got := ingressAddressDone(nil, "projectcontour/contour-envoy"); !strings.Contains(got, "could not read") {
		t.Errorf("trail line = %q", got)
	}
}

func TestContourAddressStepReportsTimeoutAsChecked(t *testing.T) {
	client := fake.NewClientset(envoyService())
	var svc *corev1.Service
	step, run := contourAddressStep(client, &svc, 30*time.Millisecond, 10*time.Millisecond)
	model, ctx := newStepModel(step)
	done, _ := send(model, progressResult{err: run(ctx)})
	if err := done.result(); err != nil {
		t.Fatalf("running out of time should not fail the step: %v", err)
	}
	if summary := done.summary(); !strings.HasPrefix(summary, "✓ Checked Envoy address · Service projectcontour/contour-envoy") {
		t.Fatalf("timeout summary: %q", summary)
	}
	if svc == nil || !loadBalancerPending(*svc) {
		t.Fatalf("step stored %+v, want the pending Service", svc)
	}
}

func TestContourAddressStepReportsStop(t *testing.T) {
	client := fake.NewClientset(envoyService())
	var svc *corev1.Service
	step, run := contourAddressStep(client, &svc, time.Minute, 10*time.Millisecond)
	model, ctx := newStepModel(step)
	result := make(chan error, 1)
	go func() { result <- run(ctx) }()

	asking, _ := send(model, keyMsg("esc"))
	if view := asking.View(); !strings.Contains(view, "Stop waiting for Envoy's LoadBalancer address?") || !strings.Contains(view, "Contour stays installed") {
		t.Fatalf("stop question:\n%s", view)
	}
	stopping, _ := send(asking, keyMsg("y"))
	var err error
	select {
	case err = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("the wait did not end when the step was stopped")
	}
	stopped, _ := send(stopping, progressResult{err: err})
	if !errors.Is(stopped.result(), huh.ErrUserAborted) {
		t.Fatalf("stopped wait should return huh.ErrUserAborted, got %v", stopped.result())
	}
	summary := stopped.summary()
	if !strings.Contains(summary, "✗ Waiting for Envoy's LoadBalancer address stopped") || !strings.Contains(summary, "Contour stays installed") {
		t.Fatalf("stopped summary:\n%s", summary)
	}
}

func TestFindIngressServiceForExistingControllers(t *testing.T) {
	quickstartEnvoy := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "envoy", Namespace: contourNamespace},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer, Ports: []corev1.ServicePort{{Port: 80, NodePort: 30080}}},
	}
	contourXDS := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "contour", Namespace: contourNamespace, Labels: map[string]string{
			"app.kubernetes.io/name": "contour", "app.kubernetes.io/component": "contour",
		}},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
	}
	nginxLabels := map[string]string{"app.kubernetes.io/name": "ingress-nginx", "app.kubernetes.io/component": "controller"}
	nginxAdmission := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "ingress-nginx-controller-admission", Namespace: "ingress-nginx", Labels: nginxLabels},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
	}
	nginxController := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "ingress-nginx-controller", Namespace: "ingress-nginx", Labels: nginxLabels},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer, Ports: []corev1.ServicePort{
			{Port: 80, NodePort: 32080}, {Port: 443, NodePort: 32443},
		}},
	}
	nginxNodePort := nginxController.DeepCopy()
	nginxNodePort.Spec.Type = corev1.ServiceTypeNodePort

	cases := []struct {
		name     string
		provider string
		objects  []*corev1.Service
		want     string
		warns    bool
	}{
		{"chart envoy pending", "contour", []*corev1.Service{contourXDS, envoyService()}, "projectcontour/contour-envoy", true},
		{"chart envoy with address", "contour", []*corev1.Service{withAddress(envoyService(), corev1.LoadBalancerIngress{IP: "203.0.113.10"})}, "projectcontour/contour-envoy", false},
		{"quickstart envoy pending", "contour", []*corev1.Service{contourXDS, quickstartEnvoy}, "projectcontour/envoy", true},
		{"nginx prefers the LoadBalancer over the admission webhook", "nginx", []*corev1.Service{nginxAdmission, nginxController}, "ingress-nginx/ingress-nginx-controller", true},
		{"nginx as NodePort", "nginx", []*corev1.Service{nginxAdmission, nginxNodePort}, "ingress-nginx/ingress-nginx-controller", false},
		{"no controller Service", "contour", []*corev1.Service{contourXDS}, "", false},
		{"other provider", "other", []*corev1.Service{envoyService(), nginxController}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewClientset()
			for _, svc := range tc.objects {
				if _, err := client.CoreV1().Services(svc.Namespace).Create(context.Background(), svc.DeepCopy(), metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			svc := findIngressService(context.Background(), client, tc.provider)
			if tc.want == "" {
				if svc != nil {
					t.Fatalf("found %s, want none", serviceRef(*svc))
				}
				return
			}
			if svc == nil || serviceRef(*svc) != tc.want {
				t.Fatalf("found %v, want %s", svc, tc.want)
			}
			if warns := len(loadBalancerWarning(*svc)) > 0; warns != tc.warns {
				t.Errorf("warning shown = %t, want %t", warns, tc.warns)
			}
		})
	}
}

func TestIngressAddressNoticeStaysOutOfWrittenFiles(t *testing.T) {
	notice := loadBalancerNotice(*envoyService())
	values := map[string]string{"namespace": "patchworks", "ingressEnabled": "true", "ingressProvider": "contour", "ingressClass": "contour", "ingressAddressNotice": notice}
	if summary := installSummary(installConfig{Values: values}); !strings.Contains(summary, notice) {
		t.Fatalf("install summary omits the LoadBalancer notice:\n%s", summary)
	}
	if summary := installSummary(installConfig{Values: map[string]string{"ingressEnabled": "true"}}); strings.Contains(summary, "LoadBalancer") {
		t.Fatalf("install summary shows a notice without one:\n%s", summary)
	}
	path := filepath.Join(t.TempDir(), "values.yaml")
	if err := writeValues(path, values); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := yaml.Marshal(localConfigFromInstallConfig(installConfig{Values: values}, true))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"values file": written, "saved config": saved} {
		if bytes.Contains(data, []byte("LoadBalancer")) || bytes.Contains(data, []byte("ingressAddressNotice")) {
			t.Fatalf("%s contains the LoadBalancer notice:\n%s", name, data)
		}
	}
}
