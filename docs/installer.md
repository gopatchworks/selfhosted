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
- Node architectures and container runtimes, for KubeFaaS

Cluster discovery is performed through the Kubernetes API from inside the Go
application. The installer does not shell out to `kubectl`. The interface uses
Charm's `huh` package for confirmations, text inputs, and select lists, and small
Bubble Tea screens for cluster inspection and each long-running step.

Only the step in progress is drawn live. When a step finishes, its live view is
replaced by one line, so the terminal keeps a short record of the run:

```text
Patchworks Self-Hosted installer 1.4.0

✓ Cluster talos · /Users/me/.kube/config
  storage local-path · ingress contour · cert-manager not installed
✓ Prepared charts 0.2.14 (github) · 1s
✓ Wrote patchworks.values.yaml
  namespace patchworks · https://selfhosted.example.com · infrastructure bundled, install/upgrade infra
  chart release pinned in patchworks.values.yaml.charts.lock.yaml
✓ Created pull secret quay-credentials in patchworks
✓ Installed infrastructure · 2m13s
  ! would violate PodSecurity "restricted:latest" (6 times)
✓ Installed application · 4m2s
✓ Workloads in patchworks · 14/14 replicas ready · pods: 14 running, 9 succeeded
```

Warnings from Helm and the Kubernetes API, such as PodSecurity warnings, are
collected while a step runs and printed under it, grouped and at most once per
run, so they cannot garble the live view.

Press Esc or Ctrl+C during a step to stop the installer. It asks first and says
what stopping leaves behind; press Y or Enter (or Ctrl+C again) to stop. A
stopped Helm install or upgrade is recorded as failed, not left pending, so the
next run can upgrade it; resources already created stay in the cluster. While
the step winds down, Ctrl+C quits at once.

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
a changed bundle; see [Bundled locks from another installer](#bundled-locks-from-another-installer)
for a lock written by a different installer binary. Selection precedence is
explicit CLI options, then the adjacent lock, then `installer.chartSource` /
`installer.chartVersion` in `config.yaml`, then `github` / `latest`.

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

When no chart option is given and the run selects a GitHub release, the
installer compares it with the charts bundled in its binary. It warns, under
the **Prepared charts** line and in the install summary, when the bundled
charts are a newer release, or the same release with different content (a
development build). A newer binary therefore cannot quietly install an older
pinned release:

```text
✓ Prepared charts 0.2.14 (github) · 1s
  ! This installer's bundled charts differ from release 0.2.14
  Release 0.2.14 is pinned in patchworks.values.yaml.charts.lock.yaml; run with --chart-source bundled to use the bundled charts
```

The warning never changes the selection or stops the run. A binary built from a
release's `Update image tags for vX.Y.Z` commit bundles exactly that release's
charts, so it stays silent unless a lock or `config.yaml` pins an older
release. Chart options suppress the check.

### Bundled locks from another installer

A bundled lock records the charts embedded in the installer binary that wrote
it, and no other binary has them. This usually happens when you download a newer
installer and re-run it beside an existing lock. With no chart option, the
installer compares the lock with its own bundled charts before the first prompt,
and again for the values file chosen in the settings form.

When this installer bundles a newer release, it moves to those charts, like a
moving selection. It warns under the **Prepared charts** line and repeats the
offer in the install summary:

```text
✓ Prepared charts 0.2.20 (bundled) · 1s
  ! Upgrading from bundled charts 0.2.19, pinned in patchworks.values.yaml.charts.lock.yaml, to this installer's 0.2.20; to stay on 0.2.19, quit and run with --chart-version 0.2.19 for release 0.2.19 from GitHub
```

The lock moves to the new release once the summary is accepted, including with
**Only write values**. Quit at the summary to keep the old lock. `--chart-version`
downloads the public release, which can differ from a development build's
bundle. Only the installer that wrote the lock has exactly the locked charts.

The other mismatches stop the run before the first prompt and name the same
options: a lock from a newer installer (the run never moves to older charts),
and a lock for the same release with different contents (a development build,
or a changed bundle):

```text
patchworks.values.yaml.charts.lock.yaml pins bundled charts 0.2.21, and this installer bundles older charts (0.2.20); only the installer that wrote the lock has them, so use that installer, run with --chart-source bundled for this installer's charts, or run with --chart-version 0.2.21 for release 0.2.21 from GitHub
```

`--chart-source bundled` or `--chart-version` skips the comparison.

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

`--chart-lock` exports exactly the locked charts and never moves to newer ones.
A bundled lock written by another installer binary is an error, even when this
binary bundles a newer release. The error names the options: drop `--chart-lock`
and pass `--chart-source bundled` for this binary's charts, or
`--chart-version X` for release X from GitHub.

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
- Installation size: standard, or lightweight for small installations
- Bundled infrastructure or external services, including their connection and credential settings
- License key
- Ingress provider and class
- Dashboard routing mode
- Cookie/session domain
- With mono workers, whether the Monocore hub serves Core's webhooks, callbacks
  and start API, and, with ingress on, whether the ingress routes their paths
  to it (see [Monocore inbound triggers](../charts/patchworks-app/README.md#monocore-inbound-triggers))
- With mono workers, whether to install the sample flows (**Samples**)
- With bundled infrastructure, whether to install **KubeFaaS**, which runs
  Script steps, and with it the **Bundled image registry**
- Image pull secret mode: existing secret, create from Quay credentials, or none
- Initial company and admin details, including an optional admin password
- Output values file path
- Whether to run the Helm install immediately

The license server URL defaults to `https://license.wearepatchworks.com` and is
not prompted for interactively. Set `application.license.serverUrl` in
`config.yaml` when it needs to be overridden.

`--save-config` keeps the inbound trigger answers as `workers.inbound.enabled`
and `workers.inbound.routeIngress`. Answer No to the routes when adding inbound
triggers to a running installation, and Yes on a later run once the Monocore
hub has restarted; a new installation can answer Yes at once. When the selected
chart release cannot serve them (its chart predates the setting, or its
Monocore image is `v0.2.12` or older), the installer leaves them off, says why in
the install summary, and keeps the saved answers for a run with a newer release.

With mono workers the installer also writes
`workers.mono.allCompanies.enabled=true` without asking, since a mono
installation deploys no Core flow workers: Core turns Monocore and its scheduler
on for the seeded tenant and every company after it, and leaves out the Core
processes those companies don't need (see
[Running every company on Monocore](../charts/patchworks-app/README.md#running-every-company-on-monocore)).
A release whose chart predates the setting, or whose Core image is `v0.2.16` or
older, leaves it off and says so in the install summary; turn Monocore on for
each company from its feature settings instead.

**Samples**, offered with mono workers, writes `samples.enabled: true`: a hook
Job creates sample flows in a Samples folder of the initial company, one per
shape and one that uses every shape, each ending in an Assert on its exact output
(see [Samples](../charts/patchworks-app/README.md#samples)). The installer also
turns on Monocore's optional shapes the samples use: `workers.mono.enable.*` when
the selected chart has those keys, otherwise the `WORKER_ENABLE_*` variables in
`workers.mono.extraEnv`. `--save-config` keeps the choice as `samples`. The
installer leaves samples off, and says why in the install summary, when the
selected release has no samples, cannot run every company on Monocore, or the run
creates no initial company/admin. The progress screen shows the Job as
"Creating sample flows".

**KubeFaaS**, offered with bundled infrastructure and off by default, writes
`kubefaas.enabled: true`. **Bundled image registry**, asked next and on by
default, adds `kubefaas.registry.bundled.enabled: true`: a registry in the
cluster that the KubeFaaS builder pushes function images to and nodes pull them
from as `localhost:<port>/...`, behind a generated login (see
[Bundled image registry](../charts/patchworks-infra/README.md#bundled-image-registry)).
It needs nodes that run containerd and route NodePorts on localhost, as k3s
does; the prompt notes nodes that don't run containerd, and the installer then
leaves the registry off and says so in the install summary. Answer No to use
your own registry, and set `kubefaas.registry.config` in the values file. When
every node is arm64, the installer also writes `kubefaas.builder.platform:
linux/arm64`, since the builder otherwise builds for `linux/amd64`; nodes of
mixed architectures keep `linux/amd64`, and the install summary says so.
`--save-config` keeps the answers as `infrastructure.kubefaas` and
`infrastructure.kubefaasRegistry`. With your own infrastructure, KubeFaaS stays as `config.yaml`
has it, without the bundled registry.

With KubeFaaS on, the installer applies the infra chart's KubeFaaS CRDs before
every infra install or upgrade, since Helm applies `crds/` only on a first
install. The printed manual commands run the same `kubectl apply` before the
infra upgrade.

With KubeFaaS and an image pull secret, the installer creates the KubeFaaS
namespace before the infra install, labelled for `patchworks-infra` to adopt
(a namespace that exists is left as it is), and copies the pull secret into it, since the controller and builder images come
from the same registry as the application. The infra install then waits up to
60 minutes instead of 15: the builder is ready only once it has built the base
image of every function template, one at a time. A release whose chart has no
bundled registry or builder platform setting leaves those out and says so in
the install summary.

**Installation size** chooses the chart's `profile`. Lightweight runs Core in one
web pod and one worker pod instead of separate gateway and start pods, a pod per
processor queue and a schedule CronJob per processor (see
[Installation profile](../charts/patchworks-app/README.md#installation-profile)).
Its Core web pod, Core worker pod and Monocore request 512Mi, 384Mi and 128Mi
of memory instead of 900Mi each. The profile reaches the infra chart through
the same values file. There, bundled Elasticsearch requests 512Mi with a 256m
heap instead of 1Gi with 512m, RabbitMQ requests 256Mi and Soketi 128Mi (see
the infra chart's
[Installation profile](../charts/patchworks-infra/README.md#installation-profile)).
With mono workers and KubeFaaS off, the two charts request about 1.9 GiB, so
the release and Contour can be scheduled on a single 4 GB node. A Raspberry
Pi 5 with a system reservation had 3021Mi allocatable. KubeFaaS, with the
bundled registry, runs one controller and requests 240Mi instead of about
1.6 GiB, plus 35M for each script Core deploys. The install summary's KubeFaaS
line states that cost. It also says when the selected release predates the
change and keeps KubeFaaS's standard requests. The gateway, Core worker and
Monocore roll without a surge: the old pod is stopped as its replacement is
created. An upgrade then needs no spare memory, at the cost of a short gap in
each. Standalone and microservice workers keep their 900Mi request. These are
requests, not limits: a small node can still run short of memory once the pods
are busy, and KubeFaaS's image builds can use up to dind's 2Gi limit. The
installer writes no resource or rollout settings. To change them, add the
`lightweight.*` blocks (such as `lightweight.elasticsearch.javaOpts`,
`lightweight.kubefaas.builder.dind.resources` or `lightweight.rollingUpdate`)
to the values file and apply it with Helm (see
[Install From Generated Values](#install-from-generated-values)). The
installer writes the file afresh on its next run.
`--save-config` keeps the choice as `profile`. When the selected chart release
has no lightweight profile, the installer writes the standard profile and says
so in the install summary. With a Core image `v0.2.16` or older it still runs
one worker pod but keeps separate gateway and start pods
(`lightweight.web.combine: false`), since that Core serves one domain per
process.

When complete, it shows an install summary and writes the selected values to
disk. Contour, if requested, is installed earlier during cluster setup.

Contour is installed with its chart defaults, so Envoy receives traffic through
the `LoadBalancer` Service `projectcontour/contour-envoy`. After installing it,
the installer waits up to 40 seconds for the Service to be given an address
(Esc or Ctrl+C stops the wait like any other step). When ingress is enabled with
an existing Contour or ingress-nginx, it reads that controller's Service once
instead. If a `LoadBalancer` Service has no address, the installer warns that no
LoadBalancer address was assigned, lists the node ports that work meanwhile,
and suggests installing a LoadBalancer implementation (MetalLB in L2 mode on
bare metal) or using NodePort or host ports. The warning also appears in the
install summary and, if the Service still has no address, under the access
details at the end. It never stops the install. A `NodePort` or `ClusterIP`
Service gets no warning.

```text
✓ Installed Contour · ingress class contour · 48s
✓ Checked Envoy address · Service projectcontour/contour-envoy · 40s
  ! No LoadBalancer address was assigned to Service projectcontour/contour-envoy, so the Patchworks hosts will not answer on ports 80 and 443.
  Until it has one, use node ports 30868 for port 80 and 31618 for port 443 on a node that runs the ingress controller.
  The cluster may have no LoadBalancer implementation. On bare metal, install one (MetalLB in L2 mode is the usual choice), or expose the ingress controller with NodePort or host ports.
```

If you choose **Only write values**, it prints the Helm commands to install the infra
and app charts with that shared values file. If installation is enabled, it can
create or update a `kubernetes.io/dockerconfigjson` Quay pull secret in the
selected namespace, runs the install through the Helm Go SDK using the prepared
charts, then checks workload status through the Kubernetes API. The Helm CLI is
only needed if you choose to copy and run the manual commands yourself.

The installer and the printed `helm upgrade` commands can take turns on the same
releases. The installer applies under the Helm CLI's field manager, `helm`, and,
on a release Helm applies server-side, forces conflicts as
`helm upgrade --force-conflicts` does. Re-running the installer to change a
setting, such as the ingress class, therefore overwrites values that the Helm
CLI, `kubectl edit` or an earlier installer last set. Earlier installers applied
under their binary's name, `patchworks-installer`. That manager stays in an
object's `managedFields` and keeps co-owning the fields it set that have not
changed since, so if a later chart stops setting such a field, server-side
apply leaves it on the object instead of removing it.

During the infra and app Helm installs, the progress screen polls Kubernetes
Jobs and readiness state so it can show phases such as credential generation,
RabbitMQ queue setup, Fabric migrations, Fabric seeders, Core migrations, and
Core seeders, alongside each Deployment's readiness. When the list is taller
than the terminal, it shows the components in progress or failed and counts the
rest. A hook Job that failed an attempt and is retrying is shown as in progress
until Kubernetes gives up on it.

If the infra or application install fails, the installer keeps the unfinished
components on screen, then prints failed Job status, pod container state, pod
events, recent container logs, and the manual install commands before exiting.
Obvious secret values in logs are redacted before display. Hook Jobs
use `restartPolicy: Never` by default so failed Pods are kept long enough to
inspect. Repeatable hook resources are deleted before the next hook run and
after successful completion. Seed hook Jobs are kept after success so later
syncs do not seed the installation again.

## Install From Generated Values

For a manual install, use the printed Helm commands. They unpack the exact
selection from the adjacent chart lock, then install the local charts. No
repository checkout, GitHub login, or registry login is needed. Dependencies
are already included in the snapshot. For bundled selections, use the same
installer binary that generated the lock; another binary refuses it. To move a
bundled lock to a newer installer's charts, re-run that installer's `install`
(choose **Only write values** to skip the installation), which rewrites the lock.

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
when the installer performs the installation. With KubeFaaS and a pull secret,
the printed commands first create the KubeFaaS namespace for `patchworks-infra`
to adopt, unless it exists, and copy the pull secret into it with `kubectl`, and
give the infra install `--timeout 60m`.

The installer prints the dashboard URL and initial admin email address once:
after a successful install, or after writing values when you selected **Only
write values**. User-provided passwords are not displayed. If the admin password
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
