Patchworks Self-Hosted
===

Deploy Patchworks Core on Kubernetes with the interactive Patchworks installer.
The installer includes the infrastructure and application Helm charts, guides
configuration, and installs them in the right order. **You do not need to clone
this repository, install Go, or install the Helm CLI to run the installer.**

For manual Helm installation, use the [advanced install guide](docs/advanced-install.md).
For an existing-cluster Helm or GitOps deployment, see the
[existing-cluster guide](docs/getting-started.md).

## Getting started

### Prerequisites

| Requirement | Details |
|---|---|
| Kubernetes cluster | A working kubeconfig and permissions to install releases, Secrets, and any required cluster resources. Follow step 2 for a local kind cluster. |
| Persistent storage | A default StorageClass when using bundled infrastructure. kind supplies one. |
| Patchworks access | A license key and credentials that can pull the Patchworks images from Quay, or an existing image pull Secret in the target namespace. |
| Public access | An ingress controller, DNS for the chosen domain and service subdomains, and certificates when using HTTPS. The local example below uses HTTP. |
| [Homebrew](https://brew.sh) | Used below to install the installer on macOS. |
| [Docker](https://docs.docker.com/get-docker/) and [kind](https://kind.sigs.k8s.io/docs/user/quick-start/) | Local installs only: start Docker, then `brew install kind`. |
| [Helm](https://helm.sh/docs/intro/install/) | Local Contour setup or manual Helm installs only: `brew install helm`. |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | Optional for inspecting workloads: `brew install kubectl`. |

### 1. Install the installer

```bash
brew install --cask gopatchworks/tap/patchworks-installer
```

Binaries for other platforms are available from
[GitHub Releases](https://github.com/gopatchworks/selfhosted/releases).

### 2. Prepare a Kubernetes cluster

**Existing cluster:** use its kubeconfig and continue to step 3. The installer
confirms the detected context before connecting, inspects storage and ingress,
and offers to install Contour if no ingress controller is detected. Configure
DNS and, for HTTPS, TLS certificates for your chosen hosts.

**Local kind cluster:** with Docker running, install the local setup tools:

```bash
brew install kind helm
```

Create the cluster with this single command. The configuration is passed on
stdin, so no checkout or downloaded configuration file is needed. It matches
[docs/kind/cluster.yaml](docs/kind/cluster.yaml), reserving host ports 80 and 443
for ingress; those ports must be available.

```bash
kind create cluster --wait 120s --config - <<'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: patchworks
nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |
        kind: InitConfiguration
        nodeRegistration:
          kubeletExtraArgs:
            node-labels: "ingress-ready=true"
    extraPortMappings:
      - containerPort: 80
        hostPort: 80
        protocol: TCP
      - containerPort: 443
        hostPort: 443
        protocol: TCP
EOF
```

Install Contour with host ports so ingress is reachable from your laptop.
Contour's [default chart configuration](https://github.com/projectcontour/helm-charts/blob/main/charts/contour/values.yaml)
uses a LoadBalancer and disables host ports; the installer uses those defaults.
This local setup supplies the kind-specific settings before running the installer:

```bash
helm upgrade --install contour contour \
  --repo https://projectcontour.github.io/helm-charts/ \
  --kube-context kind-patchworks \
  --namespace projectcontour \
  --create-namespace \
  --set 'commonLabels.selfhosted\.patchworks\.io/installed-by=patchworks-installer' \
  --set envoy.useHostPort.http=true \
  --set envoy.useHostPort.https=true \
  --set envoy.hostPorts.http=80 \
  --set envoy.hostPorts.https=443 \
  --set envoy.service.type=ClusterIP \
  --set-string 'envoy.service.externalTrafficPolicy=' \
  --timeout 10m \
  --wait
```

The empty `externalTrafficPolicy` override removes Contour's default `Local`
policy, which Kubernetes does not allow on a ClusterIP Service.

### 3. Run the installer

Run from a directory where you want to keep the generated configuration:

```bash
patchworks-installer --save-config
```

For the local cluster, confirm that the selected context is `kind-patchworks`.
If another context is detected, choose **No** and select the correct one.

The installer walks through the namespace, domain, worker mode, bundled or
external infrastructure, image pull credentials, ingress, license, and initial
company/admin. For a local install, use:

| Prompt | Local value |
|---|---|
| Namespace | `patchworks` |
| Base domain | `patchworks.local` |
| Public URL scheme | `http` |
| Enable dashboard / ingress | Yes |
| Dashboard routing mode | `host` |
| Worker mode | `standalone` |
| Infrastructure | Install bundled infrastructure |
| Ingress provider / class | `contour` |
| Cookie/session domain | `.patchworks.local` |
| Image pull secret | Create from your Quay credentials, or select an existing Secret |
| Create initial company/admin | Yes; enter your company and admin details |

Provide your license key. Leave the admin password blank to generate one, or
enter your own. Review the install summary and choose **Install**.

New installations download both charts from the latest stable release-update
commit in this public repository. No GitHub account or login is required.
The summary shows the resolved version, and an adjacent `.charts.lock.yaml` file
pins it for repeat runs. Use `--chart-version latest` to refresh the selection,
`--chart-version X.Y.Z` to select a release, or `--chart-source bundled` to use
the charts included in the binary without downloading a snapshot.


The installer writes `patchworks.values.yaml` by default, creates the Quay pull
Secret if requested, installs infrastructure followed by the app, and displays
migration, seeding, and rollout progress. `--save-config` also saves prompt
choices to `config.yaml` for future runs. These files can contain credentials;
keep them private.

Choose **Only write values** to prepare configuration for a later manual install.
See the [installer reference](docs/installer.md) for chart selection, unpacking charts,
and using the generated values without a checkout.

For a separate landlord database and multiple tenant databases, configure
[`database.landlord` and `database.tenant`](charts/patchworks-app/README.md#landlord-and-tenant-databases)
in the shared values file. The chart maps these settings to the different Core
and Monocore variables. For bundled MySQL, list additional schema names in
`mysql.databases`, then install or upgrade infrastructure before the app.
Tenant records and their database/server assignments are managed by the application.

### 4. Open Patchworks

For the local example, add the generated hostnames to `/etc/hosts`:

```bash
echo "127.0.0.1 patchworks.local gateway.patchworks.local start.patchworks.local fabric.patchworks.local webhooks.patchworks.local callbacks.patchworks.local wss.patchworks.local" | sudo tee -a /etc/hosts
```

Open [http://patchworks.local](http://patchworks.local). On an existing cluster,
open the dashboard URL printed by the installer after DNS and TLS are ready.
Sign in with the admin email you provided. After a successful install, the
installer displays the generated admin password when it can read the Secret;
a password you supplied is not printed.

## Uninstall

```bash
patchworks-installer uninstall
```

The uninstaller confirms the cluster and namespace, shows a deletion summary,
and removes the app release before infrastructure. It asks separately about
removing Contour.

To delete the entire local test cluster and its data:

```bash
kind delete cluster --name patchworks
```

## Overview

![Chart Overview](docs/chart-overview.svg)

## Worker modes

![Workers Diagram](docs/workers-diagram.svg)

## Documentation

| Guide | Contents |
|---|---|
| [Installer reference](docs/installer.md) | Installer options, generated values, chart versions, and uninstall |
| [Advanced install](docs/advanced-install.md) | Manual local Helm installation, useful commands, ingress, and troubleshooting |
| [Existing-cluster / GitOps guide](docs/getting-started.md) | Shared values, external prerequisites, upgrades, and Argo CD |
| [Infra chart configuration](charts/patchworks-infra/README.md) | Infrastructure values and generated credentials |
| [App chart configuration](charts/patchworks-app/README.md) | Application values, migrations, seeders, workers, and ingress |
| [Chart overview diagram](docs/chart-overview.svg) | Managed components and their connections |
| [Worker modes diagram](docs/workers-diagram.svg) | Standalone, microservice, and mono worker types |
