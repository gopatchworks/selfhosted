#!/usr/bin/env bash
# Installs Contour via Helm with host-port binding so that ports 80/443 on
# localhost route through Envoy without a LoadBalancer.
#
# The kind cluster config already maps host ports 80/443 to the node,
# so no further kind configuration is needed.
#
# Usage:
#   ./docs/kind/setup-contour.sh

set -euo pipefail

CONTOUR_VERSION="${CONTOUR_VERSION:-22.1.2}"
NAMESPACE="projectcontour"

echo "Adding projectcontour Helm repo..."
helm repo add projectcontour https://charts.projectcontour.io
helm repo update projectcontour

echo "Installing Contour ${CONTOUR_VERSION}..."
helm upgrade --install contour projectcontour/contour \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  --version "${CONTOUR_VERSION}" \
  --set envoy.hostPorts.enabled=true \
  --set envoy.hostPorts.http=80 \
  --set envoy.hostPorts.https=443 \
  --set envoy.service.type=ClusterIP \
  --wait

echo ""
echo "Contour ready. IngressClass: contour"
echo ""
echo "Add these entries to /etc/hosts:"
echo "  127.0.0.1 patchworks.local core.local start.local fabric.local"
