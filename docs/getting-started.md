# Patchworks Self-Hosted Getting Started

This guide covers installing Patchworks into an existing Kubernetes cluster
using the split Helm charts:

- `charts/patchworks-infra` installs baseline infrastructure.
- `charts/patchworks-app` installs Core, Fabric, dashboard, workers, migrations,
  seeders, S3 Manager, and ingress.

Use one shared values file for both charts.

## Prerequisites

Before installing the charts, make sure the cluster has:

- Kubernetes access through `kubectl`.
- Helm 3.
- A default `StorageClass` if using the chart-managed MySQL, Redis,
  RabbitMQ, Elasticsearch, and S3/MinIO services.
- An ingress controller if the application will be exposed publicly. Contour is
  the reference setup in this repo; see the
  [Contour installation guide](https://projectcontour.io/getting-started/) for
  cluster installs, or use [`docs/kind/setup-contour.sh`](kind/setup-contour.sh)
  for the local kind setup. Another controller can be used if it supports the
  configured ingress resources.
- DNS records pointing at the ingress/load balancer for the dashboard, gateway,
  start, Fabric, webhook, callback, and websocket hosts you plan to use.
- TLS certificates for HTTPS, usually through cert-manager, manually created
  TLS secrets, or ingress-controller automation.
- Image pull credentials if the cluster cannot pull from
  `quay.io/patchworks/*`.

If Argo CD or another GitOps system already owns cluster-level services such as
the ingress controller, cert-manager, Kyverno, or image-pull-secret replication,
do not install those again manually. Let GitOps manage them and install only the
Patchworks charts.

## 1. Create a Values File

Create a shared `values.yaml` for both charts. This example deploys the
chart-managed infrastructure and exposes the app with dedicated hostnames.

```yaml
namespace: patchworks

image:
  pullSecrets:
    - name: quay-credentials

app:
  url: https://selfhosted.example.com

ingress:
  enabled: true
  className: contour
  scheme: https
  hosts:
    dashboard: selfhosted.example.com
    gateway: gateway.selfhosted.example.com
    start: start.selfhosted.example.com
    fabric: fabric.selfhosted.example.com
    webhook: webhooks.selfhosted.example.com
    callback: callbacks.selfhosted.example.com

dashboard:
  enabled: true
  routingMode: host
  authCookieDomain: .selfhosted.example.com

web:
  sessionDomain: .selfhosted.example.com

seeds:
  fabric:
    enabled: true
  core:
    enabled: true
  tenant:
    companyName: "Example Company"
    tier: "Professional"
    adminName: "Admin User"
    adminEmail: "admin@example.com"
    userRole: "superadmin"
```

The charts generate stable Secrets for empty managed credentials by default.
That includes the app key and in-cluster infrastructure passwords. For
production installs, you can provide your own values or existing Secrets
instead.

## 2. Prepare the Namespace and Pull Secret

Create the application namespace:

```bash
kubectl create namespace patchworks
```

If your cluster needs credentials for Quay, create an image pull secret in the
same namespace:

```bash
kubectl create secret docker-registry quay-credentials \
  -n patchworks \
  --docker-server=quay.io \
  --docker-username='patchworks+robot' \
  --docker-password='...' \
  --docker-email='unused@example.com'
```

If your cluster uses a pull-secret replication system, create the source secret
in the namespace that system expects instead.

## 3. Install Chart Dependencies

Run this once before installing or whenever `charts/patchworks-infra/Chart.yaml`
changes:

```bash
helm dependency update charts/patchworks-infra
```

## 4. Install Infrastructure

Install the infrastructure chart first:

```bash
helm upgrade --install patchworks-infra ./charts/patchworks-infra \
  -n patchworks \
  -f values.yaml \
  --timeout 15m \
  --wait
```

The infra chart can deploy:

- MySQL
- Redis
- RabbitMQ
- Elasticsearch
- S3/MinIO
- Soketi
- KubeFaaS baseline services
- Generated infrastructure credentials

You can disable any managed component and point the shared values file at an
external service instead.

## 5. Install the Application

Install the application chart using the same values file:

```bash
helm upgrade --install patchworks-app ./charts/patchworks-app \
  -n patchworks \
  -f values.yaml \
  --timeout 15m \
  --wait
```

The app chart runs install hooks before the app starts. The expected order is:

1. Fabric migrations
2. Fabric seeders
3. Fabric company seeder
4. Core migrations
5. Core seeders and tenant setup
6. Application startup

## 6. Watch the Rollout

```bash
kubectl get pods -n patchworks --watch
```

Useful checks:

```bash
kubectl get pods,svc,ingress -n patchworks
kubectl logs -n patchworks job/patchworks-fabric-company-seed
kubectl logs -n patchworks job/patchworks-core-seeds
kubectl logs -n patchworks deploy/patchworks-gateway
kubectl logs -n patchworks deploy/patchworks-dashboard
```

If your cluster uses Contour HTTPProxy resources, check those too:

```bash
kubectl get httpproxy -n patchworks
```

## 7. Upgrade

Apply the same values file to both charts when upgrading:

```bash
helm upgrade patchworks-infra ./charts/patchworks-infra \
  -n patchworks \
  -f values.yaml \
  --timeout 15m \
  --wait

helm upgrade patchworks-app ./charts/patchworks-app \
  -n patchworks \
  -f values.yaml \
  --timeout 15m \
  --wait
```

## GitOps Notes

For Argo CD, create two applications or two sources:

1. `patchworks-infra`
2. `patchworks-app`

Point both at the same values file and sync infra before app. If Argo already
manages ingress, Kyverno, cert-manager, pull-secret replication, or monitoring,
keep those outside the Patchworks bootstrap scripts.

## Local Kind Install

For a local kind-based install, use the root README guide:

[Repository README getting started](../README.md#getting-started)
