package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// The installer installs Contour with its chart defaults, so Envoy receives
// traffic through a LoadBalancer Service. A cluster with no LoadBalancer
// implementation, such as bare metal without MetalLB, kube-vip or Cilium LB
// IPAM, never gives that Service an address, and the ingress hosts never
// answer on ports 80 and 443. These checks only report it: they never change
// the Service and never fail the install.

const (
	contourNamespace    = "projectcontour"
	contourEnvoyService = "contour-envoy"

	// ingressAddressWait bounds the wait for a new Envoy Service's address.
	ingressAddressWait = 40 * time.Second
	ingressAddressPoll = 2 * time.Second
	// ingressReadTimeout bounds a single read of an ingress Service.
	ingressReadTimeout = 10 * time.Second
)

// loadBalancerPending reports whether svc is a LoadBalancer Service that has
// no address yet. An external IP set on the Service counts as an address,
// as it does for Helm, because traffic to it already reaches the Service.
func loadBalancerPending(svc corev1.Service) bool {
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return false
	}
	return len(loadBalancerAddresses(svc)) == 0
}

func loadBalancerAddresses(svc corev1.Service) []string {
	var addresses []string
	for _, ingress := range svc.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			addresses = append(addresses, ingress.IP)
		}
		if ingress.Hostname != "" {
			addresses = append(addresses, ingress.Hostname)
		}
	}
	return append(addresses, svc.Spec.ExternalIPs...)
}

func serviceRef(svc corev1.Service) string {
	return svc.Namespace + "/" + svc.Name
}

// loadBalancerWarning is what the installer prints when the ingress
// controller's Service is waiting for a LoadBalancer address: a warning line,
// then notes. It is empty when the Service has an address or is not a
// LoadBalancer.
func loadBalancerWarning(svc corev1.Service) []string {
	if !loadBalancerPending(svc) {
		return nil
	}
	lines := []string{fmt.Sprintf("No LoadBalancer address was assigned to Service %s, so the Patchworks hosts will not answer on ports 80 and 443.", serviceRef(svc))}
	if ports := nodePortsText(svc); ports != "" {
		where := "on any node"
		if svc.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyLocal {
			where = "on a node that runs the ingress controller"
		}
		lines = append(lines, fmt.Sprintf("Until it has one, use %s %s.", ports, where))
	}
	lines = append(lines, "The cluster may have no LoadBalancer implementation. On bare metal, install one (MetalLB in L2 mode is the usual choice), or expose the ingress controller with NodePort or host ports.")
	return lines
}

// loadBalancerNotice is the one-line form of loadBalancerWarning for the
// install summary.
func loadBalancerNotice(svc corev1.Service) string {
	if !loadBalancerPending(svc) {
		return ""
	}
	notice := fmt.Sprintf("No LoadBalancer address for Service %s; ports 80 and 443 will not answer until it has one", serviceRef(svc))
	if ports := nodePortsText(svc); ports != "" {
		notice += " (meanwhile: " + ports + ")"
	}
	return notice
}

// nodePortsText lists the Service's node ports, such as "node port 30868 for
// port 80 and 31618 for port 443". Ports without a node port are left out.
func nodePortsText(svc corev1.Service) string {
	var parts []string
	for _, port := range svc.Spec.Ports {
		if port.NodePort == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d for port %d", port.NodePort, port.Port))
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return "node port " + parts[0]
	}
	return "node ports " + strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

func printLoadBalancerWarning(svc *corev1.Service) {
	if svc == nil {
		return
	}
	for i, line := range loadBalancerWarning(*svc) {
		if i == 0 {
			fmt.Println(warningLine(line))
			continue
		}
		fmt.Println(noteLine(line))
	}
}

// waitForLoadBalancerAddress reads the Service until it has an address, is
// not a LoadBalancer, or wait runs out, and returns the last Service it read.
// Running out of time is not an error; the Service is returned as it was. It
// returns nil when the Service could not be read at all, and the context's
// error when the context ends first.
func waitForLoadBalancerAddress(ctx context.Context, client kubernetes.Interface, namespace, name string, wait, poll time.Duration) (*corev1.Service, error) {
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	var last *corev1.Service
	for {
		svc, err := client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			last = svc
			if !loadBalancerPending(*svc) {
				return svc, nil
			}
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-deadline.C:
			return last, nil
		case <-ticker.C:
		}
	}
}

// waitForContourAddress gives the Envoy Service of the Contour release the
// installer just installed a short time to be given an address. Esc or
// Ctrl+C stops the wait like any other step, and only then does it return an
// error, huh.ErrUserAborted. It returns the Service as last read, or nil.
func waitForContourAddress(access kubeAccess) (*corev1.Service, error) {
	client, err := kubernetesClient(access)
	if err != nil {
		return nil, nil
	}
	ref := contourNamespace + "/" + contourEnvoyService
	var svc *corev1.Service
	err = runProgress(progressStep{
		Title:  "Waiting for Envoy's LoadBalancer address",
		Detail: fmt.Sprintf("Reading Service %s for up to %s", ref, ingressAddressWait),
		DoneFunc: func() string {
			return ingressAddressDone(svc, ref)
		},
		StopNote: "Contour stays installed. Run the installer again to continue.",
	}, func(ctx context.Context) error {
		svc, _ = waitForLoadBalancerAddress(ctx, client, contourNamespace, contourEnvoyService, ingressAddressWait, ingressAddressPoll)
		return nil
	})
	if errors.Is(err, huh.ErrUserAborted) {
		return nil, err
	}
	return svc, nil
}

// ingressAddressDone is the trail line of the Envoy address wait. A missing
// address is reported by the warning printed under it.
func ingressAddressDone(svc *corev1.Service, ref string) string {
	switch {
	case svc == nil:
		return "Skipped Envoy address check · could not read Service " + ref
	case svc.Spec.Type != corev1.ServiceTypeLoadBalancer:
		return fmt.Sprintf("Envoy Service %s is type %s", ref, svc.Spec.Type)
	case loadBalancerPending(*svc):
		return "Checked Envoy address · Service " + ref
	}
	return fmt.Sprintf("Envoy address %s · Service %s", strings.Join(loadBalancerAddresses(*svc), ", "), ref)
}

// ingressServiceMatches reports whether svc is where the provider's ingress
// controller receives traffic. The names and labels are those the Contour
// Helm chart, Contour's quickstart manifests and the ingress-nginx Helm chart
// give their Services.
func ingressServiceMatches(provider string, svc corev1.Service) bool {
	labels := svc.Labels
	switch provider {
	case "contour":
		if labels["app.kubernetes.io/name"] == "contour" && labels["app.kubernetes.io/component"] == "envoy" {
			return true
		}
		return svc.Namespace == contourNamespace && (svc.Name == "envoy" || svc.Name == contourEnvoyService)
	case "nginx":
		return labels["app.kubernetes.io/name"] == "ingress-nginx" && labels["app.kubernetes.io/component"] == "controller"
	}
	return false
}

// findIngressService returns the Service through which the provider's
// ingress controller receives traffic, or nil when none is found or Services
// cannot be listed. A LoadBalancer Service is preferred because
// ingress-nginx's admission webhook Service carries the controller's labels
// too.
func findIngressService(ctx context.Context, client kubernetes.Interface, provider string) *corev1.Service {
	if provider != "contour" && provider != "nginx" {
		return nil
	}
	list, err := client.CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	var matches []corev1.Service
	for _, svc := range list.Items {
		if ingressServiceMatches(provider, svc) {
			matches = append(matches, svc)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.Slice(matches, func(i, j int) bool { return serviceRef(matches[i]) < serviceRef(matches[j]) })
	for i := range matches {
		if matches[i].Spec.Type == corev1.ServiceTypeLoadBalancer {
			return &matches[i]
		}
	}
	return &matches[0]
}

// existingIngressService reads, once and without waiting, the Service of an
// ingress controller that was already on the cluster.
func existingIngressService(access kubeAccess, provider string) *corev1.Service {
	client, err := kubernetesClient(access)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), ingressReadTimeout)
	defer cancel()
	return findIngressService(ctx, client, provider)
}

// stillPendingIngressService reads svc again for the final output and
// returns it when it still has no LoadBalancer address.
func stillPendingIngressService(access kubeAccess, svc *corev1.Service) *corev1.Service {
	if svc == nil || !loadBalancerPending(*svc) {
		return nil
	}
	client, err := kubernetesClient(access)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), ingressReadTimeout)
	defer cancel()
	current, err := client.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
	if err != nil || !loadBalancerPending(*current) {
		return nil
	}
	return current
}
