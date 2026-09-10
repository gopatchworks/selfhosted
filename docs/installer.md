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

## Install and run

Install the released binary on macOS with Homebrew:

```bash
brew install --cask gopatchworks/tap/patchworks-installer
patchworks-installer
```

The binary uses the Helm SDK and Kubernetes API, so no repository checkout,
Go installation, Helm CLI, or kubectl is required to run it. It downloads
charts from the public `gopatchworks/selfhosted` repository by default and includes
bundled charts for offline use. No GitHub account or registry login is required.
See the [README quick start](../README.md#getting-started) for local
kind cluster and ingress setup, or download another platform's binary from
[GitHub Releases](https://github.com/gopatchworks/selfhosted/releases).

To run from a source checkout with Go installed:

```bash
go run ./cmd/patchworks-installer
```

Use `--save-config` to write the selected prompt values back to the local
`config.yaml` file:

```bash
patchworks-installer --save-config
```

## Chart versions

The private release pipeline publishes each application release into this public
repository by updating both charts' `values.yaml` image tags. For example, the
commit `Update image tags for v0.1.39` updates the image defaults to `v0.1.39`;
the Helm `Chart.yaml` metadata version may still be `0.1.0`.

`--chart-version` selects that **application release's chart snapshot**, not the
Helm metadata version or installer binary version. It uses the public commit
history and downloads both charts from one exact commit SHA. The installer does
not contact the private releases repository or require published Helm packages.

```bash
# Select the newest stable values-update commit on main.
patchworks-installer --chart-version latest

# Select the snapshot committed for this release (v prefix is also accepted).
patchworks-installer --chart-version 0.1.39

# Use the charts included in this binary without downloading a snapshot.
patchworks-installer --chart-source bundled
```

`--chart-source` accepts `github` or `bundled`; `--chart-version` implies `github`
and cannot be combined with `--chart-source bundled`. New installations default
to `github` / `latest`. `latest` means the most recent stable release-update
commit on `main`, excluding prereleases. An explicit version can select a
prerelease. Unknown releases fail instead of inventing image tags.

Release discovery follows the existing commit subject convention
`Update image tags for vX.Y.Z` and paginates the history of
`charts/patchworks-app/values.yaml` (up to 10,000 commits). The downloaded
`image.tag` in **both** charts must match the selected release. Chart names,
matching Helm metadata versions, packaged dependencies, and installer
compatibility are also checked before either Patchworks release is applied.
The install summary displays the resolved application release and chart source.

All requests are anonymous HTTPS requests to GitHub's public API and archive
host. No local GitHub, Docker, or Helm credentials are read. GitHub's anonymous
API rate limits still apply to version discovery; errors explain when to retry.
Saved selections download their commit directly without querying release history.
Network and validation failures stop the run; bundled mode is always explicit.
Offline chart mode still requires the cluster to pull its container images.

After the summary is accepted, the installer writes
`<values-file>.charts.lock.yaml` (normally
`patchworks.values.yaml.charts.lock.yaml`). It records the application release,
public repository commit SHA, and chart content digests. This happens even with
**Only write values** and without `--save-config`. The lock records the selected
release, not installation success. Keep it beside the generated values.

Repeat runs using that values path download the saved commit and verify the
chart digests. Bundled selections verify the embedded chart contents and reject
a changed bundle. Selection precedence is explicit CLI options, then the
adjacent lock, then `installer.chartSource` / `installer.chartVersion` in
`config.yaml`, then `github` / `latest`.

Use `--chart-version latest` explicitly to refresh a saved selection.
`--save-config` also writes the resolved source and application release:

```yaml
installer:
  enabled: true
  chartSource: github
  chartVersion: 0.1.39
```

An explicit `--chart-source bundled` selects the current binary's bundle anew.
Existing installations without a lock do not infer a version from the cluster;
use an explicit release on the first run if you want to retain a particular one.

### Unpack charts

`unpack-charts` defaults to the current binary's bundle. Use an explicit release
or `latest` to export a public snapshot. For a manual installation, use
`--chart-lock` to export exactly the saved selection:

```bash
patchworks-installer unpack-charts --output ./charts
patchworks-installer unpack-charts --chart-version 0.1.39 --output ./charts
patchworks-installer unpack-charts \
  --chart-lock patchworks.values.yaml.charts.lock.yaml --output ./charts
```

Choose a fresh output directory to avoid leaving old templates behind when
switching versions. The default is `./patchworks-charts`. Each export includes
`charts.lock.yaml`. `--chart-lock` is only supported by `unpack-charts` and cannot
be combined with `--chart-version` or `--chart-source`. Without that flag, exports
do not read installation locks or config defaults.

### Chart compatibility metadata

Both charts declare `selfhosted.patchworks.io/installer-api-version: "1"` in
`Chart.yaml` annotations. This installer accepts API 1; older unannotated charts
are treated as API 1. Chart authors must change the API version when the generated
values contract becomes incompatible. They may additionally require a minimum
installer release:

```yaml
annotations:
  selfhosted.patchworks.io/installer-api-version: "1"
  selfhosted.patchworks.io/min-installer-version: "1.2.0"
```

The minimum is optional for existing releases. When present, it must be an exact
semantic version, and the running installer's build version must meet it.
Development builds with an unknown version (`dev` or `ci`) cannot satisfy a
minimum; set the actual installer version using `-ldflags '-X main.version=X.Y.Z'`
when testing such charts from source.

## Installer prompts

The TUI asks for:

- Confirmation that the detected kubeconfig and context are correct
- Kubeconfig path and a selectable Kubernetes context, when the detected values are rejected
- Whether to install Contour, when no ingress controller is detected
- Namespace
- Public domain and URL scheme
- Worker mode: standalone, microservice, or mono
- Bundled infrastructure or external services, including their connection and credential settings
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
shows an install summary before applying the Patchworks releases. Contour, if
requested, is installed earlier during cluster setup. If installation is enabled,
it can create or update a `kubernetes.io/dockerconfigjson` Quay pull secret in
the selected namespace, runs the install through the Helm Go SDK using the
prepared charts, then checks workload status through the Kubernetes API. The Helm
CLI is only needed if you choose to copy and run the manual commands yourself.

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

For a manual install, use the printed Helm commands. They unpack the exact
selection from the adjacent chart lock, then install the local charts. No
repository checkout, GitHub login, or registry login is needed. Dependencies
are already included in the snapshot. For bundled selections, use the same
installer binary that generated the lock.

```bash
patchworks-installer unpack-charts \
  --chart-lock patchworks.values.yaml.charts.lock.yaml --output ./charts

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

Use the namespace and values path you selected in the installer, and select the
same Kubernetes context for these Helm commands. If you chose external
infrastructure, follow the installer-generated commands, which skip the infra
release. If you selected **Only write values**, create any requested Quay pull
Secret separately before the manual install; it is created automatically only
when the installer performs the installation.

After writing values, the installer prints the dashboard URL and initial admin
email address. User-provided passwords are not displayed. If the admin password
is left blank, the chart generates it into the `patchworks-tenant-admin` Secret
under the `adminPassword` key, and the installer displays the generated value
after a successful install when it can read the Secret.

## Uninstall

```bash
patchworks-installer uninstall
```

The uninstall command confirms the kubeconfig/context, asks for the Patchworks
namespace, shows a deletion summary, and requires confirmation before removing
anything. It removes the `patchworks-app` release first, then
`patchworks-infra`. Contour is labelled on the Helm release and chart resources
when installed by this tool, and the uninstaller prompts before removing it.
Quay pull secrets are only deleted when they carry the installer label.
