# Patchworks Installer TUI

The installer TUI is a small Bubble Tea application that generates a shared
values file for the split Helm charts. It starts from the user's existing
`KUBECONFIG`, `KUBE_CONTEXT`, or `CONTEXT` environment variables, then prompts
with a yes/no confirmation before connecting to the cluster. Choosing no lets
the user override the kubeconfig path and context first.

It checks for:

- Selected kubeconfig and context
- StorageClasses
- IngressClasses
- Contour HTTPProxy support
- cert-manager CRDs
- Existing `quay-credentials` pull secret in the selected namespace

Cluster discovery is performed through the Kubernetes API from inside the Go
application. The installer does not shell out to `kubectl`. The interface uses
Charm's `huh` package for confirmations, text inputs, and select lists, with a
small Bubble Tea screen for cluster inspection progress.

## Run

```bash
go run ./cmd/patchworks-installer
```

Use `--save-config` to write the selected prompt values back to the ignored
`config.yaml` file:

```bash
go run ./cmd/patchworks-installer --save-config
```

The released binary includes the Patchworks Helm charts. To inspect exactly
what the installer will apply, unpack them with:

```bash
patchworks-installer unpack-charts
```

By default this writes to `./patchworks-charts`. Use `--output` to choose a
different directory:

```bash
patchworks-installer unpack-charts --output ./charts
```

The TUI asks for:

- Confirmation that the detected kubeconfig and context are correct
- Kubeconfig path and a selectable Kubernetes context, when the detected values are rejected
- Whether to install Contour, when no ingress controller is detected
- Namespace
- Public domain and URL scheme
- Worker mode: standalone, microservice, or mono
- License key
- Ingress provider and class
- Dashboard routing mode
- Cookie/session domain
- Image pull secret mode: existing secret, create from Quay credentials, or none
- Initial company and admin details, including an optional admin password
- Output values file path
- Whether to run the Helm install immediately

The license server URL defaults to `https://license.wearepatchworks.com` and is
not prompted for interactively. Set `application.license.serverUrl` in
`config.yaml` when it needs to be overridden.

When complete, it writes the selected values to disk and prints the Helm
commands to install the infra and app charts with that shared values file. It
shows an install summary before applying anything. If installation is enabled,
it can create or update a `kubernetes.io/dockerconfigjson` Quay pull secret in
the selected namespace, runs the install through the embedded Helm Go SDK and
embedded Patchworks charts, then checks workload status through the Kubernetes
API. The Helm CLI is only needed if you choose to copy and run the manual
commands yourself.

During the infra and app Helm installs, the progress screen polls Kubernetes
Jobs and readiness state so it can show phases such as credential generation,
RabbitMQ queue setup, Fabric migrations, Fabric seeders, Core migrations, and
Core seeders. When multiple hook Jobs or deployments are active at the same
time, the progress screen lists all of them.

If the application install fails during a hook, the installer prints failed Job
status, pod container state, pod events, and recent container logs before
exiting. Obvious secret values in logs are redacted before display. Hook Jobs
use `restartPolicy: Never` by default so failed Pods are kept long enough to
inspect. Repeatable hook resources are deleted before the next hook run and
after successful completion. Seed hook Jobs are kept after success so later
syncs do not seed the installation again.

## Install From Generated Values

```bash
helm dependency update charts/patchworks-infra
helm dependency update charts/patchworks-app

helm upgrade --install patchworks-infra ./charts/patchworks-infra \
  -n patchworks \
  --create-namespace \
  -f patchworks.values.yaml \
  --timeout 15m \
  --wait

helm upgrade --install patchworks-app ./charts/patchworks-app \
  -n patchworks \
  -f patchworks.values.yaml \
  --timeout 15m \
  --wait
```

Review the generated file before applying it in production. The installer is
intended to create a sensible starting point, not to replace environment-specific
review.

After writing values, the installer prints the dashboard URL and initial admin
email address. User-provided passwords are not displayed. If the admin password
is left blank, the chart generates it into the `patchworks-tenant-admin` Secret
under the `adminPassword` key, and the installer displays the generated value
after a successful install when it can read the Secret.

## Uninstall

```bash
go run ./cmd/patchworks-installer uninstall
```

The uninstall command confirms the kubeconfig/context, asks for the Patchworks
namespace, shows a deletion summary, and requires confirmation before removing
anything. It removes the `patchworks-app` release first, then
`patchworks-infra`. Contour is labelled on the Helm release and chart resources
when installed by this tool, and the uninstaller prompts before removing it.
Quay pull secrets are only deleted when they carry the installer label.
