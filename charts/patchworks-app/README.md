# Patchworks Application Chart — Configuration Reference

The `patchworks-app` chart owns Patchworks application resources: Core/Fabric
deployments, dashboard, workers, ingress, Passport keys, migrations, and
first-install seeders.

Use it with the `patchworks-infra` chart and pass the same values file to both:

```bash
helm upgrade --install patchworks-infra ./charts/patchworks-infra -f values.yaml
helm upgrade --install patchworks-app ./charts/patchworks-app -f values.yaml
```

Resource names default to the stable `patchworks` prefix, not the Helm release
name, so the app chart can reference services and generated Secrets created by
the infra chart.

If `app.key` and `app.existingSecret.name` are both empty, the app chart creates
a stable `patchworks-app-key` Secret for enabled APP_KEY consumers, with a value equivalent to:

```bash
echo "base64:$(openssl rand -base64 32)"
```

For graceful Laravel key rotation, keep the replacement in `app.key`/`APP_KEY`
and provide older keys through `app.previousKeys` or the optional
`app.existingSecret.previousKeysKey`. New values use the replacement key while
Laravel can still decrypt ciphertext written with any previous key.

Migrations run as `pre-install,pre-upgrade` hooks. Seeders run as `pre-install`
hooks only. The first-install order is: Fabric migrations, Fabric seeders,
Fabric company seeder, Core migrations, Core seeders, then application startup.
The hook jobs render their required environment directly instead of depending
on app ConfigMaps/Secrets that Helm has not created yet.
Successful seed Jobs are kept in the cluster so GitOps syncs and installer
reruns do not create a fresh seed Job after the initial install.

Argo CD applies persistent ConfigMaps, Secrets, ServiceAccounts and Services in
Sync wave `-30`, then runs migration and optional seed Jobs as `Sync` hooks.
Fabric migration/seed waves are `-20`, `-19`, and `-18`; optional tenant database
creation is `-15`; Core migrations and seeds are `-10` and `-5`. Application
workloads remain in the default wave `0`, so failed migrations block their
rollout. These prerequisites are ordinary managed resources and are not deleted
as hooks. Existing external Secrets must already be materialized before sync.
Use a full Application sync; selective resource sync skips hooks.

When Fabric seeds are enabled and no `seeds.tenant.adminPassword` or
`seeds.tenant.existingSecret.name` is provided, the app chart creates a stable
`patchworks-tenant-admin` Secret before the Fabric seed job runs.

← [Back to repo README](../../README.md)

---

## Contents

- [Component selection](#component-selection)
- [Global](#global)
- [Pod DNS](#pod-dns)
- [Images](#images)
- [Shared values and generated credentials](#shared-values-and-generated-credentials)
- [Application](#application)
- [Dashboard](#dashboard)
- [Passport OAuth keys](#passport-oauth-keys)
- [Web](#web)
- [Scheduler](#scheduler)
- [Processors](#processors)
- [Workers](#workers)
- [Mapping documents](#mapping-documents)
- [Migrations](#migrations)
- [Samples](#samples)
- [Ingress](#ingress)
- [MySQL](#mysql)
- [Landlord and tenant databases](#landlord-and-tenant-databases)
- [Redis](#redis)
- [RabbitMQ](#rabbitmq)
- [Elasticsearch](#elasticsearch)
- [S3 / MinIO](#s3--minio)
- [Pusher / Soketi](#pusher--soketi)
- [KubeFaaS](#kubefaas)
- [Existing secrets](#existing-secrets)
- [Example configurations](#example-configurations)

---

## Shared values and generated credentials

Keep `credentials.autoGenerate` and the infrastructure auth values identical
between the infra and app chart releases. With the default
`credentials.autoGenerate=true`, empty in-cluster passwords are read from the
Secrets created by the infra chart:

| Component | Secret |
|-----------|--------|
| MySQL | `patchworks-mysql-auth` |
| Fabric MySQL | `patchworks-fabric-mysql-auth` |
| RabbitMQ | `patchworks-rabbitmq-auth` |
| Elasticsearch | `patchworks-elasticsearch-auth` |
| S3 / MinIO | `patchworks-s3-auth` |
| Soketi / Pusher | `patchworks-soketi-auth` |

If you use component namespace overrides, remember Kubernetes Secrets are
namespace-scoped. Keep app and generated infra secrets in the same namespace or
provide copied Secrets via the relevant `existingSecret` values.

## Component selection

The default installation is unchanged: Gateway, Start, Fabric, processor
Deployments, scheduler CronJobs, workers and migrations are enabled. Each can
also be installed in its own Helm release or Argo CD Application.

| Key | Default | Resources controlled |
|-----|---------|----------------------|
| `web.gateway.enabled` | `true` | Gateway Deployment, Service and ingress routes |
| `web.start.enabled` | `true` | Start Deployment, Service and webhook/callback ingress routes |
| `fabric.enabled` | `true` | Fabric Deployment, Service, runtime configuration and ingress routes |
| `dashboard.enabled` | `false` | Dashboard Deployment, Service and root ingress route |
| `processorDeployments.enabled` | `true` | Processor Deployments and supervisord ConfigMaps; preserves `processors[]` for schedulers/topology |
| `scheduler.enabled` | `true` | CronJobs for enabled entries in `processors[]` |
| `workers.enabled` | `true` | The selected `workers.type` hub, companies, configuration and mono store generator |
| `workers.hub.enabled` | `true` | Hub worker Deployments; set `false` for company-only releases |
| `migrations.enabled` | `true` | Core migration Job |
| `fabric.migrations.enabled` | `true` | Fabric migration Job; independent of `fabric.enabled` |
| `seeds.core.enabled` / `seeds.fabric.enabled` | `false` | First-install seed Jobs |
| `rabbitmq.topology.enabled` | `true` | Explicit RabbitMQ topology Job; independent of workload switches |
| `s3Manager.enabled` | `true` | S3 Manager, unless an external endpoint is provided |

For separate releases, keep a shared values file with all the switches above
turned off, then enable only the required component in that release's override.
A Gateway override is `web.gateway.enabled: true`; a migration release enables
`migrations.enabled` and `fabric.migrations.enabled`. Disable both seed switches
and `rabbitmq.topology.enabled` in ordinary workload releases. Assign database
migrations, initial seeding and RabbitMQ topology to explicit owners.

The app chart deploys no bundled database, Redis, Elasticsearch, Soketi or
KubeFaaS resources. For externally owned infrastructure, set the corresponding
`enabled` flags to `false` and configure `external` endpoints/authentication.
Use `kubefaas.host`, `kubefaas.builderHost` and `pusher.external.host` for those external
services. See their configuration sections for the complete credential fields.

Shared Core ConfigMaps, inline Secrets and the application ServiceAccount are
created only in namespaces with active consumers. Fabric runtime configuration
is created only for the Fabric Deployment; migration and seed Jobs render their
environment directly. APP_KEY, Passport and Pusher generators are omitted when
unused. When generation is needed, it happens in the sole consumer namespace.
If a credential is consumed in several namespaces, set its `existingSecret`
and replicate **the same credential** to every consumer namespace; the chart
rejects multi-namespace generation instead of issuing different keys.

The `fullnameOverride` prefix is independent of Helm's release name. Use a
unique prefix for releases sharing a namespace. In different namespaces the
same prefix is safe, provided no cluster-scoped resources collide (the mono
store generator owns cluster RBAC; supplying its existing store Secret avoids
that generator). Existing Secrets must exist in each destination namespace.

PHP Core workers and processors use `<fullname>-<component>` unless the full name
already equals the component, in which case it is emitted once. For example,
`fullnameOverride: workers-manual-payload` with microservice `manual-payload`
creates Deployment `workers-manual-payload`, ConfigMap
`workers-manual-payload-supervisord`, ScaledObject
`workers-manual-payload-autoscaler`, and TriggerAuthentication
`workers-manual-payload-autoscaler-rabbitmq`. Names longer than 63 characters are
shortened with a hash suffix; references use the same resolved name.

Default service URLs are release-local. Set `dashboard.coreUrl`,
`dashboard.startUrl`, `dashboard.fabricUrl` and `dashboard.mcpUrl` to the actual
public endpoints; set `monocore.url` for Core releases using a separately
installed mono worker. Use `dashboard.routingMode: host` for independent public
hosts. Shared-host path routes are emitted only for enabled services in the
Dashboard namespace. Cross-namespace/shared-host routing must be supplied by
the ingress configuration that owns that hostname.

## Global

| Key | Default | Description |
|-----|---------|-------------|
| `revisionHistoryLimit` | `3` | Number of old ReplicaSets retained for chart-managed Deployments |
| `ndots` | unset | Optional DNS `ndots` for chart-managed pods; sections and services can override it |
| `deploymentAnnotations` | `{}` | Annotations on application Deployment metadata, inherited by every Deployment |

For Reloader, place its opt-in and pause window on Deployment metadata:

```yaml
deploymentAnnotations:
  reloader.stakater.com/auto: "true"
  deployment.reloader.stakater.com/pause-period: "60s"
```

Install and configure the reload controller separately. Component
`deploymentAnnotations` override global keys: `web` (then `web.gateway` or
`web.start`), `fabric`, `dashboard`, `workers`, `processors[]`, and `s3Manager`.
Workers also support per-microservice/default, mono and company overrides.
`podAnnotations` retains its existing meaning and legacy behavior. CronJobs
and lifecycle Jobs do not inherit Deployment annotations.

The chart retains its checksum annotations for configuration rollouts. Reloader
can also react to changes in referenced external Secrets and ConfigMaps. Its
pause window does not guarantee a single rollout when chart checksums change
at the same time. Direct pod-spec changes also initiate normal rollouts.
Dashboard's generated config.js is embedded in its init-container command, so
changing Dashboard values changes the pod spec directly. Configure Argo CD to
ignore only the reload controller's documented fields for each managed
Deployment; this chart does not configure Argo CD itself.


## Pod DNS

Set top-level `ndots` in the shared values file to configure pod DNS in both
charts. Every chart-managed Deployment, Job and CronJob inherits it. The most
specific non-null, non-empty setting wins; numeric `0` is supported. With no
setting at any level, the chart omits `dnsConfig` entirely and leaves Kubernetes
DNS behaviour unchanged. No numeric default is supplied.

All keys below are optional and unset by default. Each path supports `.ndots`;
chains run from less to more specific, after the top-level setting.

| Workload | Override chain |
|----------|----------------|
| Gateway / Start | `web` → `web.gateway` / `web.start` |
| Fabric, Dashboard, S3 Manager | `fabric`, `dashboard`, `s3Manager` respectively |
| Standalone workers | `workers` → `workers.hub` or `workers.companies[]` |
| Microservice workers | `workers` → `workers.microservices._default` → `workers.microservices.<service>` → `workers.hub` or `workers.companies[]` → `workers.companies[].microservices.<service>` |
| Mono workers | `workers` → `workers.mono` → `workers.hub` or `workers.companies[]` |
| Processor Deployments | `workers` → `processorDeployments` → `processors[]` |
| Scheduler CronJobs | `workers` → `scheduler` → `processors[]` → `processors[].scheduler` |
| Core / Fabric migrations | `migrations`; Fabric also inherits `fabric` before `migrations`, then `fabric.migrations` |
| Seed Jobs | `seeds` → `seeds.fabric`, `seeds.tenant` or `seeds.core`; Fabric seed Jobs also inherit `fabric` before `seeds` |
| Samples Job | `samples` |
| MySQL, Redis, RabbitMQ, Elasticsearch, MinIO | `mysql`, `redis`, `rabbitmq`, `elasticsearch`, `s3` respectively; setup Jobs inherit their component |
| Fabric MySQL / Redis | `fabric` → `fabric.mysql` / `fabric.redis` |
| KubeFaaS controller / builder / Redis | `kubefaas` → `kubefaas.controller` / `kubefaas.builder` / `kubefaas.redis` |
| Native Soketi | `pusher` → `soketi` |
| RabbitMQ topology Job | `rabbitmq` → `rabbitmq.topology` |
| Credential generators | `credentials` → `credentials.generator`; app key, Passport and Pusher generators then apply `app`, `passport` or `pusher`; tenant password generation then applies `seeds` → `seeds.tenant` |
| Mono store generator | `workers` → `workers.mono` → `credentials` → `credentials.generator` |

For example:

```yaml
ndots: 2
web:
  ndots: 1
  gateway:
    ndots: 0
workers:
  ndots: 1
  microservices:
    batch:
      ndots: 3
```

This sets Gateway to `0`, Start and workers to `1`, the batch microservice to
`3` when microservice mode is enabled, and other chart-managed pods to `2`.

The optional upstream Soketi subchart and function pods created dynamically by
the KubeFaaS controller are managed outside these pod templates and do not
inherit this setting.

## Images

All Patchworks application images share a global registry and tag. Individual services can override either.

| Key | Default | Description |
|-----|---------|-------------|
| `image.registry` | `quay.io/patchworks` | Registry prefix applied to all app images |
| `image.tag` | `v0.0.3` | Image tag applied to all app images |
| `image.pullPolicy` | `IfNotPresent` | Pull policy applied to all app images |
| `image.pullSecrets` | `[]` | Image pull secret names for all app pods |

Per-service overrides (all optional, fall back to global):

```yaml
web:
  image:
    registry: ""      # overrides image.registry
    tag: ""           # overrides image.tag
    pullPolicy: ""    # overrides image.pullPolicy
```

---

## Application

Shared configuration injected into every application pod (web, workers, migrations).

| Key | Default | Description |
|-----|---------|-------------|
| `app.key` | `""` | Laravel `APP_KEY`. Leave empty to auto-generate a stable key Secret |
| `app.previousKeys` | `[]` | Older Laravel keys rendered as comma-separated `APP_PREVIOUS_KEYS` for graceful rotation |
| `app.existingSecret.name` | `""` | Secret to source `APP_KEY` from instead of the inline value |
| `app.existingSecret.key` | `APP_KEY` | Key within the above secret |
| `app.existingSecret.previousKeysKey` | `""` | Optional key containing comma-separated `APP_PREVIOUS_KEYS`; empty omits the variable |
| `app.env` | `production` | `APP_ENV` |
| `app.debug` | `"false"` | `APP_DEBUG` |
| `app.url` | `http://localhost` | `APP_URL` — set to your public-facing URL |
| `app.license.active` | `"true"` | `LICENSE_ACTIVE` for encoded Core and Fabric images. Monocore workers do not read it |
| `app.license.key` | `""` | Inline `LICENSE_KEY` for Core, Fabric and Monocore workers. Prefer `app.license.existingSecret.name` for production |
| `app.license.serverUrl` | `https://license.wearepatchworks.com` | License server URL rendered as `LICENSE_SERVER_URL` for Core and Monocore workers, and as `LICENSE_URL` for Fabric |
| `app.license.existingSecret.name` | `""` | Existing Secret containing license values. It must exist in every namespace that runs Core, Fabric or Monocore workers |
| `app.license.existingSecret.keyKey` | `LICENSE_KEY` | Key containing `LICENSE_KEY` in the existing Secret |
| `app.license.existingSecret.serverUrlKey` | `""` | Optional key containing the license server URL in the existing Secret |
| `app.payloadMetaMaxBytes` | `""` | `PAYLOAD_META_MAX_BYTES`, the largest payload meta in bytes, for Core and for Monocore workers, so both enforce the same limit. Empty omits it and both keep their default of `10240`. A Monocore worker whose `extraEnv` sets the variable keeps that value |
| `app.extraEnv` | `[]` | Additional env vars injected into all app pods |
| `app.extraEnvFrom` | `[]` | Additional `secretRef`/`configMapRef` sources for all app pods |

Core web, Gateway, Start, scheduler, and worker pods receive
`QUEUE_CONNECTION` from `workers.queue.connection`. When `workers.type` is
`standalone`, they also receive `REDIS_QUEUE` from `workers.queue.name` so jobs
dispatch to the standalone hub queue instead of each pod's `APP_DOMAIN`.

---

## Fabric

| Key | Default | Description |
|-----|---------|-------------|
| `fabric.enabled` | `true` | Deploy Fabric web resources; does not control migrations/seeds |
| `fabric.deploymentAnnotations` | `{}` | Deployment metadata annotations |
| `fabric.preStopSleepSeconds` | `20` | PHP-FPM: Nginx and PHP-FPM both wait this long before their stop signal so routing updates stop new requests first; `0` renders no hook |
| `fabric.terminationGracePeriodSeconds` | `90` | PHP-FPM: pod termination allowance; must exceed `preStopSleepSeconds` plus `fpm.drainTimeoutSeconds` |
| `fabric.fpm.drainTimeoutSeconds` | `60` | PHP-FPM: longest in-flight request drain; match the image's `process_control_timeout` (60s in the Fabric image) |
| `fabric.nginx.fastcgiReadTimeoutSeconds` | `0` | PHP-FPM: seconds Nginx waits for PHP-FPM before returning a 504, while PHP-FPM keeps running the request; `0` keeps Nginx's 60s default |
| `fabric.podDisruptionBudget.enabled` | `true` | Create a PodDisruptionBudget for Fabric pods |
| `fabric.podDisruptionBudget.maxUnavailable` | `1` | Fabric pods that node drains and consolidation may evict at once; an integer or percentage |
| `fabric.core.initialiseDatabases` | `true` | Set `CORE_INITIALISE_DATABASES` so Fabric asks Core to create and migrate databases for new companies |
| `fabric.core.createSubscription` | `true` | Set `CORE_CREATE_SUBSCRIPTION` so Fabric asks Core to create a subscription for new companies |
| `fabric.core.gatewayUrl` | `""` | Set `CORE_GATEWAY_URL`; empty resolves the Gateway Service name, namespace and port |
| `fabric.session.driver` | `redis` | Fabric web `SESSION_DRIVER`; applied only to the Fabric PHP-FPM container |
| `fabric.session.lifetime` | `10080` | Fabric web `SESSION_LIFETIME` in minutes |
| `fabric.mysql.maxConnections` | `1000` | `max_connections` for dedicated bundled Fabric MySQL when `fabric.mysql.enabled=true` |
| `fabric.mysql.external.readHost` | `""` | Optional external Fabric read host for Fabric, Core, and Monocore; empty uses the write host |

Fabric's Core settings reach its web and init containers, migration Jobs and
seed Jobs. Changes also update the Fabric Deployment's configuration checksum.
The default Gateway URL follows `fullnameOverride`/`nameOverride`, the Gateway
namespace cascade and `web.gateway.service.port` (falling back to
`web.service.port`). Port 80 is omitted. When Gateway is managed separately,
set `fabric.core.gatewayUrl` to its reachable base URL, even if
`web.gateway.enabled=false` in this release. Set
`fabric.core.initialiseDatabases=false` or `fabric.core.createSubscription=false`
to leave provisioning to an operator.

Enabling automatic provisioning applies to new company creation requests; it
does not backfill databases for companies already recorded in Fabric. The
first-install `app:create-tenant` seeder still uses the chart's separate tenant
database and Core migration Jobs before application startup.

When a PHP-FPM Fabric pod stops, both containers first sleep for
`fabric.preStopSleepSeconds` so the Service and ingress stop routing to the pod.
Kubernetes then sends each image's stop signal, SIGQUIT: Nginx stops accepting
connections and waits for its in-flight requests, and PHP-FPM lets busy workers
finish for up to its `process_control_timeout`. Fabric images before that
setting was added kill busy workers at once, so in-flight requests receive 502
responses whatever the grace period. The PodDisruptionBudget limits how many
Fabric pods node drains and Karpenter consolidation evict at once; rollouts
follow the Deployment's update strategy instead. FrankenPHP pods do not receive
the hook or the termination allowance, but keep the PodDisruptionBudget.

---

## Dashboard

Dashboard values are written to `/usr/share/nginx/html/config.js` before the
static nginx container starts. The config writer uses the same inline,
existingSecret, or generated Soketi/Pusher auth values as the application pods,
so dashboard broadcasting works with generated credentials.

| Key | Default | Description |
|-----|---------|-------------|
| `dashboard.enabled` | `false` | Deploy the dashboard |
| `dashboard.routingMode` | `path` | Browser URL defaulting mode: `path` uses dashboard `/core-main`, `/core-start`, and `/fabric`; `host` uses dedicated service hostnames |
| `dashboard.coreUrl` | `""` | Browser `coreMainUrl`; default depends on `dashboard.routingMode`, then falls back to the in-cluster gateway service |
| `dashboard.startUrl` | `""` | Browser `coreStartUrl`; default depends on `dashboard.routingMode`, then falls back to the in-cluster start service |
| `dashboard.fabricUrl` | `""` | Browser `fabricUrl`; default depends on `dashboard.routingMode`, then falls back to the in-cluster Fabric service |
| `dashboard.mcpUrl` | `""` | Browser `mcpUrl`; default depends on `dashboard.routingMode`, then falls back to the in-cluster gateway service |
| `dashboard.authCookieDomain` | `""` | Browser `authCookieDomain`; defaults to `web.sessionDomain` |
| `dashboard.inboundUrl` | `""` | Browser `inboundUrl` |
| `dashboard.webhookHandlerUrl` | `https://webhook-handler.pwks.co` | Browser `webhookHandlerUrl` |
| `dashboard.broadcasting.host` | `""` | Browser `broadcasting.host`; defaults to `pusher.ingress.host` when the Soketi ingress is enabled, otherwise the shared `pusher` host |
| `dashboard.broadcasting.port` | `""` | Browser `broadcasting.port`; with the Soketi ingress enabled, defaults to `443` when `pusher.ingress.tlsSecretName` is set or `ingress.scheme=https`, otherwise `80`; without it, the shared `pusher` port |
| `dashboard.broadcasting.scheme` | `""` | Browser `broadcasting.scheme`; with the Soketi ingress enabled, defaults to `https` when `pusher.ingress.tlsSecretName` is set or `ingress.scheme=https`, otherwise `http`; without it, the shared `pusher` scheme |
| `dashboard.ga4Tag` | `none` | Browser `ga4Tag` |
| `dashboard.zendeskUrl` | `none` | Browser `zendeskUrl` |
| `dashboard.forceRegistrationRequest` | `false` | Browser `forceRegistrationRequests` |
| `dashboard.postmanImporter.enabled` | `true` | Browser `postmanImporter.enabled` |
| `dashboard.postmanImporter.maxFileUploadSize` | `50` | Browser `postmanImporter.maxFileUploadSize` |
| `dashboard.links.docs.allowances` | `""` | Browser `links.docs.allowances` |
| `dashboard.extraEnv` | `[]` | Additional env vars injected into the dashboard pod |

---

## Passport OAuth keys

Both Core and Fabric use [Laravel Passport](https://laravel.com/docs/passport) for OAuth token signing and verification. They share a single RSA key pair:

- **`PASSPORT_PRIVATE_KEY`** — used by Fabric to sign JWT tokens.
- **`PASSPORT_PUBLIC_KEY`** — used by both Core and Fabric to verify them.

### Auto-generation (default)

On `helm install`, a `pre-install` hook Job runs `openssl genrsa` inside an `alpine` container, creates the key pair, and stores it in a Kubernetes Secret called `<release-name>-passport-keys`. The Secret is annotated with `helm.sh/resource-policy: keep` so it is **never deleted** by Helm — not on `helm upgrade`, not on `helm uninstall`. The Job is idempotent: on subsequent upgrades it checks whether the Secret already exists and exits immediately if so.

No configuration is required for the default behaviour.

### Bringing your own keys

If you want to supply pre-generated keys (recommended for production), generate an RSA key pair and create the Secret before installing the chart:

```bash
# Generate a 4096-bit RSA private key and derive the public key
openssl genrsa -out passport.key 4096
openssl rsa -in passport.key -pubout -out passport.pub

# Create the Secret in the release namespace
kubectl create secret generic my-passport-keys \
  --from-file=PASSPORT_PRIVATE_KEY=passport.key \
  --from-file=PASSPORT_PUBLIC_KEY=passport.pub

# Remove the local key files
rm passport.key passport.pub
```

Then reference it in your values:

```yaml
passport:
  existingSecret:
    name: my-passport-keys
    privateKeyKey: PASSPORT_PRIVATE_KEY   # default, change if your keys differ
    publicKeyKey: PASSPORT_PUBLIC_KEY     # default, change if your keys differ
```

When `passport.existingSecret.name` is set the auto-generation Job, ServiceAccount, and RBAC are not created.

### Multi-namespace deployments

The generator uses the sole namespace containing enabled Passport consumers. If Core, Fabric or lifecycle Jobs consume Passport in different namespaces, supply `passport.existingSecret` and replicate the same keypair to each namespace. Kubernetes cannot mount a Secret from another namespace; multi-namespace auto-generation is rejected.

| Key | Default | Description |
|-----|---------|-------------|
| `passport.existingSecret.name` | `""` | Existing Secret name. Leave empty to auto-generate |
| `passport.existingSecret.privateKeyKey` | `PASSPORT_PRIVATE_KEY` | Key within the Secret for the private key |
| `passport.existingSecret.publicKeyKey` | `PASSPORT_PUBLIC_KEY` | Key within the Secret for the public key |

---

## Runtime

By default the chart renders the standard PHP runtime command shape:

```yaml
runtime:
  frankenphp:
    enabled: false
```

Service-level `frankenphp.enabled` values override the global default. For
example, this opts Fabric into the FrankenPHP runtime:

```yaml
fabric:
  frankenphp:
    enabled: true
```

When enabled, web pods start `frankenphp php-server`, and rendered Artisan
commands use `/usr/local/bin/frankenphp php-cli artisan ...`.

| Key | Default | Description |
|-----|---------|-------------|
| `runtime.frankenphp.enabled` | `false` | Global default for PHP app runtime mode |
| `web.frankenphp.enabled` | unset | Override runtime mode for Core web defaults |
| `web.gateway.frankenphp.enabled` | unset | Override runtime mode for gateway only |
| `web.start.frankenphp.enabled` | unset | Override runtime mode for start only |
| `fabric.frankenphp.enabled` | unset | Override runtime mode for Fabric web, init, and migrations |
| `workers.frankenphp.enabled` | unset | Override runtime mode for PHP worker/scheduler images |
| `migrations.frankenphp.enabled` | unset | Override runtime mode for Core migrations |
| `seeds.fabric.frankenphp.enabled` | unset | Override runtime mode for Fabric seed jobs |
| `seeds.core.frankenphp.enabled` | unset | Override runtime mode for Core seed jobs |
| `samples.frankenphp.enabled` | unset | Override runtime mode for the samples Job |

---

## Installation profile

`profile: standard` (the default) runs each Core domain in its own Deployment:
gateway and start web pods, one Deployment per processor queue, each with 15
queue workers by default, and a schedule CronJob per processor.
`profile: lightweight` runs Core in two pods, for small installations: on a
mono installation measured on 2026-11-23, the seven processor pods held about
7.2 GiB between them, almost all of it idle PHP queue workers.

| Key | Default | Description |
|---|---|---|
| `profile` | `standard` | `standard` or `lightweight` |
| `lightweight.web.combine` | `true` | The gateway Deployment also serves the start routes (`APP_DOMAINS=start`) and the start Service selects its pods. Needs a Core image later than `v0.2.16`, and `web.gateway` and `web.start` in one namespace. Off keeps separate gateway and start Deployments |
| `lightweight.web.resources` | requests memory `512Mi` | Goes over `web.resources` for the gateway pods, and the start pods with `combine` off. `web.gateway.resources` and `web.start.resources` still go over it |
| `lightweight.worker.replicaCount` | `1` | Replicas of `<fullname>-processor` |
| `lightweight.worker.processes` | `2` | Queue workers in the main pool, which consumes every enabled processor queue not in `slowQueues`, in `processors[]` order |
| `lightweight.worker.slowQueues` | `[medium-processor, long-processor]` | Queues whose jobs can run for hours, given a pool of their own so they cannot hold up scheduling and the API's background jobs |
| `lightweight.worker.slowProcesses` | `1` | Queue workers in the slow pool |
| `lightweight.worker.schedules` | `true` | Run each processor's schedule in the pod instead of its CronJob. Off renders the schedule CronJobs as in the standard profile |
| `lightweight.worker.resources` | requests memory `384Mi` | Goes over `workers.resources` for `<fullname>-processor` |
| `lightweight.worker.nodeSelector`, `tolerations`, `affinity`, `podAnnotations` | empty | Default to the matching `workers.*` settings |
| `lightweight.mono.resources` | requests memory `128Mi` | Goes over `workers.resources` for the Monocore hub and company workers (`workers.type: mono`). A company's own `resources` replace it |

What changes under `profile: lightweight`:

- **Web.** The gateway Deployment gets `APP_DOMAINS=start`, so one process
  loads both the gateway and start routes, and the start Deployment is not
  rendered. The start Service keeps its name and selects the gateway pods, so
  ingress routes, Monocore inbound, the dashboard and Fabric reach it
  unchanged. `web.gateway`'s settings apply; `web.start`'s `extraEnv` and
  `extraEnvFrom` are added to them, after the gateway's. The start routes are
  scoped to their own hosts where they overlap the gateway's, so each host
  still reaches its own route.
- **Worker.** `<fullname>-processor` replaces the processor Deployments and
  schedule CronJobs. It runs supervisord with:
  - a main queue pool and a slow pool, each a `queue:work` over several queues.
    Each pool's `APP_DOMAIN` is one of its queues (`gateway` for the main pool
    when it has it), so follow-up jobs return to that pool;
  - one `schedule-<queue>` loop per processor schedule, running `schedule:run`
    with that `APP_DOMAIN` at the start of every minute, stopped after the
    processor's `scheduler.activeDeadlineSeconds`. A processor's
    `scheduler.enabled: false` or `suspend: true` leaves its loop out; other
    CronJob settings (`schedule`, history limits) do not apply.

  No program sets a per-processor `APP_NAME`, so every Core process shares one
  cache prefix and the unique-job and schedule locks one process takes are the
  ones another releases. Per-processor `replicas`, `processes`, `resources`
  and `namespace` do not apply; `processors[].extraEnv` and `extraEnvFrom`
  apply to the shared pod. Per-queue autoscaling is refused. The RabbitMQ
  topology hook declares the same queues as before.
- **Memory requests.** The web pods, `<fullname>-processor` and Monocore
  request `512Mi`, `384Mi` and `128Mi` instead of `900Mi` each, so the
  profile can be scheduled on a small single node. On a 4 GB Raspberry Pi 5
  with the bundled infrastructure, the three `900Mi` requests left the
  processor Pending (`Insufficient memory`). Idle after an install with
  samples there, they used about 360Mi, 250Mi and 30Mi. Each
  `lightweight.*.resources` block goes over `web.resources` or
  `workers.resources`: its requests replace the shared ones and the shared
  block's other settings, such as `cpu` and `limits`, stay, so Monocore's
  `GOMEMLIMIT` still follows `workers.resources.limits.memory`. Set the block
  to `null` to use the shared one alone. Requests only reserve memory: a node
  that schedules every pod can still run short when they get busy. With mono
  workers the app chart requests about 1.1 GB in all; the infra chart's
  requests (Elasticsearch `1Gi`, RabbitMQ `512Mi`, Soketi `256Mi`) are
  unchanged.
- **Unchanged.** Monocore's other settings, standalone and microservice
  workers (`workers.resources` and `workers.processes` still apply to them),
  Fabric, the dashboard, S3 Manager and the infra chart. With
  `workers.mono.allCompanies` its left-out processors and schedules stay out
  of the worker too.

A standard installation can switch in an upgrade. The processor pods stop and
`<fullname>-processor` starts, with queued jobs waiting in RabbitMQ in between.
The start Service selects the gateway pods as soon as it is updated, so the
start, webhook and callback routes can answer 404 until the first gateway pod
with `APP_DOMAINS` is ready; switch outside busy hours.

A lightweight installation that set `web.resources` or `workers.resources`
requests to size these pods gets the profile's requests from the release that
added `lightweight.*.resources`. Set those blocks instead.

---

## Web

Gateway and Start are independently selectable Laravel web services.

| Key | Default | Description |
|-----|---------|-------------|
| `web.gateway.enabled` / `web.start.enabled` | `true` | Select each Core web Deployment independently |
| `web.deploymentAnnotations` | `{}` | Deployment metadata annotations; each web service can override keys |
| `web.replicaCount` | `1` | Number of web replicas |
| `web.frankenphp.enabled` | unset | Override global FrankenPHP runtime for both Core web services |
| `web.preStopSleepSeconds` | `20` | RoadRunner: wait for routing updates before starting request drain; `0` skips only this delay |
| `web.terminationGracePeriodSeconds` | `200` | RoadRunner: pod termination allowance; must exceed the routing delay plus drain timeout |
| `web.roadrunner.drainTimeoutSeconds` | `160` | Maximum wait for RoadRunner to exit after SIGTERM; keep above the image's `endure.grace_period` |
| `web.roadrunner.stateFile` | `/var/www/html/storage/logs/octane-server-state.json` | Octane state file containing RoadRunner's master PID; match any `octane.state_file` override |
| `web.service.type` | `ClusterIP` | Kubernetes service type |
| `web.service.port` | `80` | Service port |
| `web.sessionDomain` | `""` | `SESSION_DOMAIN`; controls the cookie domain for authentication |
| `web.resources` | requests memory `900Mi` | Resource requests and limits. Under `profile: lightweight`, `lightweight.web.resources` goes over it |
| `web.extraEnv` | `[]` | Additional env vars for web pods only |
| `web.extraEnvFrom` | `[]` | Additional env sources for web pods only |
| `web.podAnnotations` | `{}` | Pod annotations |
| `web.nodeSelector` | `{}` | Node selector |
| `web.tolerations` | `[]` | Tolerations |
| `web.affinity` | `{}` | Affinity rules |

RoadRunner Gateway and Start pods use a `preStop` hook that waits for routing
updates, signals RoadRunner directly, and waits for it to exit before Kubernetes
signals Octane. The hook runs PHP without booting Laravel or contacting a database.
It is embedded in the chart, so it does not require a separate drain script in the
image. The entire hook consumes the pod termination allowance. A missing state
file or an already-exited process is a no-op; invalid PIDs and drain timeouts are
reported as hook failures, after which normal Kubernetes termination continues.

These values can be overridden under `web.gateway` or `web.start`. FrankenPHP
pods do not receive the RoadRunner hook or these termination settings. Use a Core
image with `endure.grace_period: 150s` and `http.pool.destroy_timeout: 120s` to allow
the intended request drain; the chart does not override the image's RoadRunner
configuration. If a service mesh is injected, its proxy must also remain available
while requests drain.

---

## Scheduler

Shared defaults for processor scheduler CronJobs. Each enabled entry in
`processors[]` creates one scheduler CronJob which runs
`/usr/local/bin/php artisan schedule:run` with `APP_DOMAIN` and
`RABBITMQ_QUEUE` set to the processor queue.

| Key | Default | Description |
|-----|---------|-------------|
| `scheduler.enabled` | `true` | Create processor scheduler CronJobs |
| `scheduler.frankenphp.enabled` | unset | Override FrankenPHP runtime for scheduler CronJobs |
| `scheduler.schedule` | `*/1 * * * *` | Default cron schedule for scheduler jobs |
| `scheduler.suspend` | `false` | Suspend scheduler CronJobs |
| `scheduler.concurrencyPolicy` | `Forbid` | CronJob concurrency policy |
| `scheduler.successfulJobsHistoryLimit` | `2` | Number of successful scheduler Jobs retained |
| `scheduler.failedJobsHistoryLimit` | `2` | Number of failed scheduler Jobs retained |
| `scheduler.backoffLimit` | `2` | Job retry backoff limit |
| `scheduler.activeDeadlineSeconds` | `300` | Maximum runtime for a scheduler Job |
| `scheduler.restartPolicy` | `Never` | Scheduler pod restart policy |
| `scheduler.command` | `[/usr/local/bin/php]` | Container command |
| `scheduler.args` | `[artisan, schedule:run]` | Container args |
| `scheduler.resources` | requests `150m` / `300M` | Default scheduler resources |
| `scheduler.extraEnv` | `[]` | Additional env vars injected into scheduler pods |
| `scheduler.extraEnvFrom` | `[]` | Additional envFrom sources injected into scheduler pods |

---

## Processors

`processorDeployments.enabled` selects processor Deployments independently of
`scheduler.enabled`. `processors[]` defines the PHP Core background processor queues. These are
separate from `workers.type`: even when `workers.type=mono`, processor queues
are still handled by PHP Core worker Deployments because Monocore does not run
these jobs.

| Key | Default | Description |
|-----|---------|-------------|
| `processors[].deploymentName` | derived from fullname and queue | Optional Deployment name, also used for its supervisord ConfigMap and autoscaler names. Does not change queue names, pod selectors, or scheduler CronJob identities. Must be unique within the namespace. |

The default list creates `scheduler`, `start`, `gateway`, `short-processor`,
`medium-processor`, `long-processor`, and `logging` workers. Scheduler CronJobs
are created for processors unless `processors[].scheduler.enabled=false`; the
default `logging` processor disables its scheduler because it only consumes
application broadcast/logging work. A
pre-install/pre-upgrade hook also asserts these RabbitMQ queues, plus the active
PHP worker hub queues: `workers.queue.name` for `workers.type=standalone`, or
each enabled `workers.microservices[*].queue` (falling back to `domain`) for
`workers.type=microservice`. Queue creation works with either the bundled broker
or a user-provided external RabbitMQ instance.

The `scheduler` processor isolates flow scheduling and scheduled flow initialisation
(CPT-6321). It runs one CronJob and a PHP worker pool on the `scheduler` queue.
Its `extraEnv` sets `REDIS_QUEUE=scheduler` so nested jobs use this queue even
when the shared standalone-worker configuration selects `default`.
`FLOW_SCHEDULER_SEGMENTS` defaults to `1` in that same processor's `extraEnv`;
change it to `4`, for example, to dispatch four groups of companies onto the
same queue. It does not create four CronJobs or reserve four pods. Keep both
environment entries when replacing `extraEnv`, because Helm replaces lists.
When replacing the entire `processors` list, include the scheduler entry and
retain start/medium/long cron runners for their other domain tasks.
Provision the queue and compatible consumers before releasing Core's scheduler
domain gates. On rollback, retain compatible consumers until queued segment
jobs and their descendants drain. Monocore scheduler shards are configured
separately and are unaffected by this PHP setting.

For external RabbitMQ, the AMQP endpoint must be reachable from the cluster and
the configured user must be allowed to declare queues in the configured vhost.
The topology hook runs Monocore's `apply-rabbitmq-topology` command using the
`workers.mono.image` image, not the Core PHP image. The command receives
`RABBITMQ_*` environment variables and a mounted
`/etc/patchworks/rabbitmq/topology.yaml`.

| Field | Description |
|-------|-------------|
| `name` | Display name and `APP_NAME` value for the processor |
| `queue` | Queue name; also used for `APP_DOMAIN`, `RABBITMQ_QUEUE`, resource slugs, and RabbitMQ queue creation |
| `enabled` | Set `false` to suppress the processor worker, scheduler, and RabbitMQ queue |
| `frankenphp.enabled` | Runtime override for this processor's worker and scheduler |
| `namespace` | Namespace override for the processor worker and scheduler |
| `replicas` | Worker Deployment replicas; defaults to `workers.replicaCount` |
| `processes` | Worker concurrency; defaults to `workers.processes` |
| `resources` | Worker resources; defaults to `workers.resources` |
| `extraEnv` / `extraEnvFrom` | Additional env/envFrom for processor worker and scheduler pods |
| `scheduler.*` | Per-processor overrides for any `scheduler.*` field, such as `schedule`, `activeDeadlineSeconds`, `resources`, `extraEnv`, `nodeSelector`, `tolerations`, or `affinity` |

`scheduler.enabled=false` is still a global kill switch. If it is `false`, no
processor scheduler CronJobs are rendered, even if an individual processor sets
`scheduler.enabled=true`.

---

## Workers

`workers.type` selects the deployment model. See the [worker modes diagram](../../docs/workers-diagram.svg) for a visual overview.

| Type | Description |
|------|-------------|
| `standalone` | PHP queue workers via supervisord — one Deployment (+ one per company) |
| `microservice` | PHP queue workers via supervisord — one Deployment per service key (+ one per company per service) |
| `mono` | Monocore Go-based worker |

**Common keys (all types)**

| Key | Default | Description |
|-----|---------|-------------|
| `workers.enabled` | `true` | Deploy worker hub/company resources for the selected type |
| `workers.hub.enabled` | `true` | Include the hub worker for each selected service; independent of `workers.companies` |
| `workers.deploymentAnnotations` | `{}` | Deployment metadata defaults for workers and processors |
| `workers.type` | `standalone` | Worker deployment type |
| `workers.namespace` | `""` | Namespace for worker resources (defaults to release namespace) |
| `workers.frankenphp.enabled` | unset | Override FrankenPHP runtime for PHP workers |
| `workers.replicaCount` | `1` | Replica count (standalone only) |
| `workers.processes` | `15` | Worker concurrency — `numprocs` in supervisord.conf (standalone/microservice) |
| `workers.queue.connection` | `rabbitmq` | Laravel queue connection |
| `workers.queue.name` | `default` | Hub queue name for standalone workers |
| `workers.resources` | requests memory `900Mi` | Resource requests and limits. Under `profile: lightweight`, `lightweight.worker.resources` and `lightweight.mono.resources` go over it for the processor and Monocore workers |
| `workers.extraEnv` | `[]` | Additional env vars for all worker pods |
| `workers.extraEnvFrom` | `[]` | Additional env sources for all worker pods |

**`type: microservice` — per-service configuration**

Each key in `workers.microservices` (except `_default`) creates one Deployment. `_default` provides fallback values. Gateway, start, logging, and the short/medium/long processor queues are configured through `processors[]`, not `workers.microservices`.

| Field | Description |
|-------|-------------|
| `name` | `APP_NAME` env var |
| `domain` | `APP_DOMAIN` env var and, when `queue` is unset, the RabbitMQ queue name |
| `queue` | Optional RabbitMQ queue override; defaults to `domain` |
| `processes` | Worker concurrency — overrides `_default.processes` |
| `replicas` | Replica count — overrides `_default.replicas` |
| `enabled` | Set `false` to suppress the Deployment without removing the key |

**`type: mono` keys**

| Key | Default | Description |
|-----|---------|-------------|
| `workers.mono.nameSuffix` | `workers` | Hub component slug. Its Deployment, Service, ServiceMonitor, config and topology ConfigMaps are `<fullname>-<nameSuffix>`, companies are `<fullname>-<nameSuffix>-<company>`, and the scheduler selects its own pods by it. Changing it renames live objects; see below |
| `workers.mono.image.repository` | `monocore` | Image repository |
| `workers.mono.queue` | `flows` | Hub queue consumed by Monocore and used for the generated `flows` topology |
| `workers.mono.processes` | `15` | Worker goroutine count |
| `workers.mono.goMemLimit` | `""` | `GOMEMLIMIT` for the hub and companies. Empty derives 90% of each worker's `resources.limits.memory` in whole MiB, and leaves it unset without a memory limit. Use the Go runtime's units (`1700MiB`, not `1700Mi`) or `off`. A company's `goMemLimit` overrides it. Set it here rather than through `extraEnv` |
| `workers.mono.tmp.sizeLimit` | `10Gi` | Size limit of the `emptyDir` each worker mounts at `/tmp`, where Monocore keeps payload bundles and page spools. A pod past it is evicted instead of filling the node's disk. The worker gets it in bytes as `WORKER_TEMP_DIR_LIMIT_BYTES` and hands runs to other worker pods before `/tmp` fills (set that variable in `extraEnv` to override it). A company's `tmp.sizeLimit` overrides it. Required |
| `workers.mono.preStopSleepSeconds` | `0` | Seconds to sleep before SIGTERM, letting endpoint and scaler removal propagate first. `0` renders no hook; the sleep counts against `terminationGracePeriodSeconds` |
| `workers.mono.terminationGracePeriodSeconds` | `3660` | Pod shutdown grace; must exceed `preStopSleepSeconds` plus the scheduler drain by more than 10 seconds |
| `workers.mono.platformApi.fabricUrl` | `""` | Fabric API base URL for operator-run tinker commands; empty derives the in-cluster Fabric service `/api/v2` URL |
| `workers.mono.platformApi.coreUrl` | `""` | Core API base URL for operator-run tinker commands; empty derives the in-cluster Gateway `/api/v1/patchworks` URL |
| `workers.mono.operatorKey.enabled` | `false` | Grant the hub worker service account permission to get the named retained operator-key Secret and create it when absent |
| `workers.mono.operatorKey.secretName` | `monocore-operator-api-key` | Retained Secret containing `PATCHWORKS_API_KEY`; the chart does not create it |
| `workers.mono.scheduler.mode` | `kubernetes` | Hub scheduling mode: kubernetes, standalone or disabled; company pods remain execution-only |
| `workers.mono.scheduler.shards` | `3` | Shared desired shard count, updated without rolling worker pods |
| `workers.mono.scheduler.interval` | `5s` | Scheduling and coordination poll interval |
| `workers.mono.scheduler.drainTimeoutSeconds` | `30` | Active scheduling pass shutdown deadline |
| `workers.mono.scheduler.timezone` | `UTC` | Must match Core's application timezone |
| `workers.mono.scheduler.estate` | fullname | Stable scheduling estate identity |
| `workers.mono.scheduler.rbac.create` | `true` | Bind scheduler permissions to the shared service account in the hub namespace |
| `workers.mono.scheduler.admitAllTriggers` | `false` | `SCHEDULER_ADMIT_ALL_TRIGGERS` on the hub: admit every queued `scheduled_flows` row (API, webhook, Shape and other triggers) of a `monocore-scheduler` company, not only cron rows. Turn on only once Core leaves those rows to Monocore, or a row both engines admit runs twice. `workers.mono.allCompanies.scheduler` turns it on. See [Monocore inbound triggers](#monocore-inbound-triggers) |
| `workers.mono.allCompanies.enabled` | `false` | Run every company on Monocore: Core turns the `monocore` feature on as it provisions each company, the Core seed job does so for companies with no stored value, and the Core processors in the two lists below are left out. Needs a release later than `v0.2.16`. See [Running every company on Monocore](#running-every-company-on-monocore) |
| `workers.mono.allCompanies.scheduler` | `true` | Also turn `monocore-scheduler` on for those companies and set `admitAllTriggers`. Needs the hub's scheduler: rendering fails with `workers.mono.scheduler.mode=disabled` or `workers.hub.enabled=false` unless this is `false` |
| `workers.mono.allCompanies.disableProcessors` | `[short-processor]` | Processor queues whose Deployment and schedule CronJob are left out, and dropped from the RabbitMQ topology |
| `workers.mono.allCompanies.disableSchedulers` | `[gateway]` | Processor queues whose schedule CronJob alone is left out |
| `workers.mono.enable.merge` | `null` | `WORKER_ENABLE_MERGE` on the hub and every company: run the Merge shape. `null` follows `workers.mono.allCompanies.enabled`; `true` or `false` overrides it. See [Monocore's optional shapes](#monocores-optional-shapes) |
| `workers.mono.enable.dataTransform` | `null` | `WORKER_ENABLE_DATA_TRANSFORM`: the Data Transformation shape, whose functions run natively in the worker. `null` follows `allCompanies.enabled`. The `v0.2.17` Dashboard has no node for this shape, so it can't add one and shows an existing one as broken; Monocore still runs the step |
| `workers.mono.enable.crossReference` | `null` | `WORKER_ENABLE_CROSS_REFERENCE`: the Cross-Reference shape. `null` follows `allCompanies.enabled` |
| `workers.mono.enable.syncSubprocess` | `null` | `WORKER_ENABLE_SYNC_SUBPROCESS`: waiting Run Flow and Flow Return, where a parent holds its worker slot while its children run. `null` follows `allCompanies.enabled` |
| `workers.mono.enable.catchAtRunEnd` | `false` | `WORKER_ENABLE_CATCH_AT_RUN_END`: run a dropped payload's Try/Catch Catch branch once the run's other branches are done, as Core does. Not a shape, so `allCompanies` leaves it off. Turn it on only once every worker runs a release that reads the deferred Catch branch |
| `workers.mono.inbound.enabled` | `false` | Serve Core's webhook, callback and start API endpoints from the hub's public listener (port 8082) and its `<fullname>-<nameSuffix>-inbound` Service. Needs a mono hub and a Monocore image later than `v0.2.12` |
| `workers.mono.inbound.ingress.enabled` | `false` | Route the paths Monocore serves on the start, webhook and callback hosts to it. Off, routing is left to you. Turn it on in the second of the [two enabling upgrades](#monocore-inbound-triggers) |
| `workers.mono.inbound.coreUrl` | `""` | Where Monocore forwards the requests of companies Core runs (`INBOUND_CORE_URL`). Empty derives Core's in-cluster start Service; required when `web.start.enabled=false`. Never an ingress host |
| `workers.mono.inbound.rateLimitPrefix` | `""` | Cache prefix of Core's start pods, whose rate-limit windows Monocore shares (`INBOUND_RATE_LIMIT_PREFIX`). Empty derives Core's: `CACHE_PREFIX`, else `Str::slug(APP_NAME, '_')` and `_cache_:`, from `app.extraEnv` and the start service's `extraEnv`; `laravel_cache_:` when neither is set. Required when `web.start.enabled=false` |
| `workers.mono.inbound.queueSchedule` | `""` | `INBOUND_QUEUE_SCHEDULE`: queue webhook and `/start` runs in `scheduled_flows`. Empty follows Core's `FLOW_QUEUE_SCHEDULE` from the same `extraEnv` (default `true`); `"true"` or `"false"` overrides it. Required when `web.start.enabled=false` |
| `workers.mono.store.existingSecret.name` | `""` | Existing Secret containing monocore's `store.yaml`; skips the generated store Secret hook |
| `workers.mono.store.existingSecret.key` | `store.yaml` | Secret key to mount as `/etc/monocore/store.yaml` |
| `workers.mono.rabbitmq.flowExchange` | `""` | Flow publish exchange. Empty defaults to `workers.mono.queue` when `companyFlows.enabled=false`, or `customer-flows` when enabled |
| `workers.mono.rabbitmq.companyFlows.enabled` | `false` | Create the configured flow exchange, company queues, bindings, and fallback policy |
| `workers.mono.otel.enabled` | `false` | Enable OpenTelemetry tracing |
| `workers.mono.otel.endpoint` | `""` | OTLP collector endpoint |
| `workers.mono.otel.serviceName` | `monocore` | Service name reported to the collector |

When `workers.type=mono`, a pre-install/pre-upgrade hook creates a
`<fullname>-workers-store` Secret containing monocore's `store.yaml` (this
Secret keeps the `-workers` name whatever `workers.mono.nameSuffix` is), unless
`workers.mono.store.existingSecret.name` is set. The generated file includes the
resolved S3 endpoint, bucket names, region, path-style setting, and access
credentials for both the `default` and `customer_cache` stores. Existing store
Secrets must exist in every namespace where monocore worker pods run.

The Monocore image is licensed, as Core and Fabric are. Every Monocore worker,
the hub and each company, gets `LICENSE_KEY` and `LICENSE_SERVER_URL` from
`app.license` (see [Application](#application)) and checks the licence against
the licence server in the background. While the licence is not valid it starts
no new runs; runs already started finish. Its readiness probe is `/readyz`,
which fails until the licence server has first answered, so a new pod takes
no requests before then, from Core or as an inbound trigger. The liveness probe stays on `/healthz`. An
`app.license.existingSecret` must exist in every worker namespace.

### Renaming the hub

`workers.mono.nameSuffix` defaults to `workers`, which is why the hub is
`<fullname>-workers` — and `workers-workers` when the release is itself named
`workers`. Setting it renames the Deployment, Service, ServiceMonitor,
autoscaler, config and topology ConfigMaps, and any company workers, and changes
the hub pods' `app.kubernetes.io/name` label. The scheduler's pod selector
follows the same value, so sharding keeps finding its own pods.

It is a rename of live objects, not a relabel:

- A Deployment's selector is immutable, so the new name is a new Deployment. The
  old one is **not** deleted unless the installation prunes; scale it to zero or
  delete it as part of the cutover, or two hubs will schedule the same estate
  and consume the same queues.
- Anything referring to the old names by hand — dashboards, VPA or PDB
  `targetRef`s, `kubectl` runbooks, pods mounting the config or topology
  ConfigMaps — has to move with it.
- `monocore.url` is derived from the suffix, so in-cluster callers follow
  automatically. An explicitly set `monocore.url` does not.
- The slug is excluded from the config checksum, so adding the value without
  changing it does not roll existing pods.

The `<fullname>-workers-store` Secret and the storegen hook keep their names.

## Mapping documents

Mapping document storage is used by PHP core and monocore. Monocore defaults to the main Elasticsearch connection, and `mapping.elasticsearch.*` only needs to be set when mappings use a different endpoint or credentials.

| Key | Default | Description |
|-----|---------|-------------|
| `mapping.elasticsearch.addresses` | `[]` | Elasticsearch addresses for monocore mapping storage. Defaults to the main Elasticsearch URL |
| `mapping.elasticsearch.cloudId` | `""` | Elastic Cloud ID for monocore mapping storage |
| `mapping.elasticsearch.apiKey` | `""` | Elasticsearch API key for monocore mapping storage |
| `mapping.elasticsearch.username` | `""` | Username for monocore mapping storage basic auth |
| `mapping.elasticsearch.password` | `""` | Password for monocore mapping storage basic auth |
| `mapping.elasticsearch.index` | `mappings` | Mapping documents index |
| `mapping.elasticsearch.existingSecret.name` | `""` | Secret name for mapping-specific credentials |
| `mapping.elasticsearch.existingSecret.cloudIdKey` | `""` | Secret key for `MAPPING_ELASTICSEARCH_CLOUD_ID` |
| `mapping.elasticsearch.existingSecret.apiKeyKey` | `""` | Secret key for `MAPPING_ELASTICSEARCH_API_KEY` |
| `mapping.elasticsearch.existingSecret.usernameKey` | `""` | Secret key for `MAPPING_ELASTICSEARCH_USERNAME` |
| `mapping.elasticsearch.existingSecret.passwordKey` | `""` | Secret key for `MAPPING_ELASTICSEARCH_PASSWORD` |

## Monocore API

When `workers.type=mono`, Core app pods receive `MONOCORE_URL` and
`MONOCORE_TIMEOUT`. The default URL targets the in-cluster hub monocore Service.

| Key | Default | Description |
|-----|---------|-------------|
| `monocore.url` | `""` | Override the Monocore API URL. Defaults to `http://<fullname>-<workers.mono.nameSuffix>.<workers namespace>.svc.cluster.local:8080` |
| `monocore.timeout` | `120` | Monocore request timeout in seconds |

**Multi-company workers**

`workers.companies[]` is supported by all three types. Each entry adds a Deployment consuming `company.queue` (or `company.name`). Set `workers.hub.enabled: false` to deploy selected companies without another hub. In microservice mode, the existing per-service `enabled` flags select which services to create for those companies.

For a separate company release, use the shared component-off values described above and enable only its workers:

```yaml
workers:
  enabled: true
  hub:
    enabled: false
  type: standalone
  companies:
    - name: customer
      queue: customer-flows
```

With no hub and no companies selected, the chart creates no worker resources. Mono topology ConfigMaps and generated store Secrets are created only in selected worker namespaces. The store-generator Job and its ServiceAccount run in the hub namespace when enabled, otherwise the first company's namespace; its cluster RBAC still permits the required cross-namespace Secret access.

> **Note (standalone/microservice):** RabbitMQ queues for company workers must be created manually. The app-chart topology hook creates processor queues and hub standalone/microservice queues only.

---

## Migrations

Fabric migrations run before Core migrations as pre-install/pre-upgrade hooks.
Core migrations default to `php artisan migrate --force`. When the gateway is
enabled, their image and pull policy follow the gateway; non-empty
`migrations.image.*` fields override that inheritance. With the gateway disabled,
the existing migration/global image fallback applies. Argo CD recreates both
named migration hooks before a new sync and deletes them after success.

| Key | Default | Description |
|-----|---------|-------------|
| `migrations.enabled` | `true` | Run Core migrations independently of Core web Deployments |
| `fabric.migrations.enabled` | `true` | Run Fabric migrations before Core migrations |
| `fabric.migrations.frankenphp.enabled` | unset | Override FrankenPHP runtime for Fabric migrations |
| `fabric.migrations.command` | `php artisan migrate --force` | Fabric migration command |
| `migrations.frankenphp.enabled` | unset | Override FrankenPHP runtime for Core migrations |
| `migrations.command` | `php artisan migrate --force` | Core migration command; include extra flags here |
| `migrations.restartPolicy` | `Never` | Job pod restart policy. `Never` preserves failed Pods for diagnostics |
| `migrations.serviceAccountName` | `""` | Optional ServiceAccount for Fabric/Core migration and seed Jobs. Empty uses the namespace default; for standalone Helm pre-install hooks, a named account must already exist. Argo can create it in the prerequisite Sync wave. |
| `migrations.backoffLimit` | `3` | Job retry limit |
| `migrations.resources` | `{}` | Resource requests and limits |
| `migrations.image.*` | empty | Non-empty registry, repository, tag and pullPolicy override the enabled gateway image; otherwise fall back to global image settings |

---

## Seeds

Fabric seeders run after Fabric migrations and before Core migrations. The
Fabric company seeder runs separately after Fabric seeders. Core seeders run
after Core migrations.

| Key | Default | Description |
|-----|---------|-------------|
| `seeds.fabric.enabled` | `false` | Run the Fabric install seeder Job |
| `seeds.fabric.frankenphp.enabled` | unset | Override FrankenPHP runtime for Fabric seed jobs |
| `seeds.fabric.command` | Faker preflight + `php artisan app:install` | Fabric install seed command. The preflight fails before Passport clients are created if the Fabric image is missing `fakerphp/faker` |
| `seeds.core.enabled` | `false` | Run the Core tenant seeder Job |
| `seeds.core.frankenphp.enabled` | unset | Override FrankenPHP runtime for Core seed jobs |
| `seeds.core.command` | `php artisan db:seed --force && php artisan migrate:tenants --create --no-interaction` | Core first-install seed and tenant migration command |
| `seeds.restartPolicy` | `Never` | Job pod restart policy. `Never` preserves failed Pods for diagnostics |
| `seeds.backoffLimit` | `0` | Job retry limit. Defaults to no retries because seed commands can have side effects before failing |
| `seeds.tenant.companyName` | `""` | Initial tenant company name passed to `app:create-tenant` |
| `seeds.tenant.database` | `""` | Initial tenant database name. Defaults to `companyName` lowercased with non-alphanumeric characters removed |
| `seeds.tenant.createDatabase` | `true` | Create the initial schema on the default tenant endpoint before Core tenant migrations; skipped when `database.tenant.primaryServerId` is set |
| `seeds.tenant.tier` | `Professional` | Initial tenant tier passed to `app:create-tenant` |
| `seeds.tenant.adminName` | `""` | Initial admin user name |
| `seeds.tenant.adminEmail` | `""` | Initial admin user email |
| `seeds.tenant.userRole` | `patchworks admin` | Initial admin user role passed as `--user-role` |
| `seeds.tenant.adminPassword` | `""` | Initial admin user password. Generated when empty and Fabric seeds are enabled |
| `seeds.tenant.existingSecret.name` | `""` | Secret name for the initial admin password |
| `seeds.tenant.existingSecret.passwordKey` | `adminPassword` | Secret key for the initial admin password |

---

## Samples

`samples.enabled` creates sample flows in a **Samples** folder of the seeded
company: one per shape, and **Sample: Every shape**, which uses every shape in
one flow. Each sample starts from a Manual Payload and ends in an Assert on its
exact output, so a successful run shows the shape produced the right data.
Samples are deployed with no schedule; start them from the dashboard.

A `post-install,post-upgrade` hook Job (`<fullname>-samples`, Argo CD
`PostSync`) runs `files/samples/install.php` with the Core image as the seeded
admin. It builds each flow with Core's own flow-builder classes, lays it out as
the dashboard's **Auto layout** does, and deploys it through Core's connector
allowance check. The scripts are mounted from the `<fullname>-samples`
ConfigMap verbatim, never through `tpl`, because the flows contain Patchworks
`{{flow.variables.*}}` placeholders.

The Job never changes a sample that already exists, so edits and run history
survive upgrades. Delete a sample flow to have the next upgrade create it
again. Deleting the folder does not: Core moves its flows out of it, and the
next upgrade would create a second set. Supporting records are created once and reused by name:
"Sample cache" (company scope), "Sample data pool", the "Sample SKU map" lookup,
"Sample system" with "Sample connector" (no authentication, calling the Monocore
hub's `/healthz` inside the cluster), and "Sample script".

| Key | Default | Description |
|-----|---------|-------------|
| `samples.enabled` | `false` | Create the sample flows. Needs `workers.type: mono` with `workers.mono.allCompanies.enabled`, and Monocore's optional shapes on: Merge, Data Transformation, Cross-Reference and waiting Run Flow. On a chart with `workers.mono.enable`, a shape is on unless its key is `false`; otherwise set its `WORKER_ENABLE_*` to `"true"` in `workers.mono.extraEnv` or `workers.extraEnv`. The render fails without them |
| `samples.folder` | `Samples` | Dashboard folder the samples are created in |
| `samples.companyName` | `seeds.tenant.companyName` | Company the samples are created in |
| `samples.userEmail` | `seeds.tenant.adminEmail` | User the samples are created as |
| `samples.frankenphp.enabled` | unset | Override the runtime mode for the samples Job |
| `samples.image.*` | global `image.*` | Core image override for the samples Job |
| `samples.backoffLimit` | `0` | Job retry limit |
| `samples.resources`, `nodeSelector`, `affinity`, `tolerations` | empty | Samples Job scheduling |

What runs where a company's allowances limit it:

- **Connector.** Deploying a connector needs connector allowance. When Core
  refuses it, **Sample: Connector** is left as an undeployed draft, and
  **Sample: Every shape** leaves out its Connector branch so it still deploys.
  The Job does not fail.
- **Run Process Flow** (without waiting) queues its child for the scheduler, so
  the child runs only while the company has flow concurrency allowance.
  **Sample: Callback** answers on its callback URL only while the company has
  callback allowance.
- **Script.** The Script steps stay skipped, passing their payload on, until
  "Sample script" is deployed, which needs KubeFaaS.
- **Try/Catch.** **Sample: Try/Catch** ends in partial success on purpose: an
  Assert fails on the Try route. Assert runs outside Core's per-payload loop, so
  the Catch route receives `{}` with the error in its meta, as in Core.

The Job exits non-zero, failing the install or upgrade, only when a sample
cannot be built; it removes that half-built flow so the next run tries again.

---

## Ingress

| Key | Default | Description |
|-----|---------|-------------|
| `ingress.enabled` | `false` | Create public ingress resources |
| `ingress.scheme` | `http` | Public URL scheme used for dashboard/browser-facing generated URLs, including the dashboard websocket to `pusher.ingress.host`. Set to `https` when TLS is terminated outside this chart |
| `ingress.className` | `""` | `ingressClassName` for standard Ingress resources |
| `ingress.annotations` | `{}` | Annotations applied to every generated Ingress or HTTPProxy resource |
| `ingress.tls.<service>.secretName` | `""` | TLS Secret for `gateway`, `start`, `webhook`, `callback`, `fabric`, or `dashboard` |
| `ingress.hosts.gateway` | `""` | Hostname for the Core gateway service (`/`) |
| `ingress.hosts.start` | `""` | Hostname for the Core start service (`/`); `/api/v1/flows/` goes to Monocore when it serves [inbound triggers](#monocore-inbound-triggers) |
| `ingress.hosts.webhook` | `""` | Hostname for webhook traffic; routes to the Core start service (`/`), or `/api/v1/` to Monocore when it serves inbound triggers |
| `ingress.hosts.callback` | `""` | Hostname for callback traffic; routes to the Core start service (`/`), or `/api/v1/` to Monocore when it serves inbound triggers |
| `ingress.hosts.fabric` | `""` | Hostname for the Fabric service (`/`) |
| `ingress.hosts.dashboard` | `""` | Hostname for the dashboard and path-based routes (see below) |
| `ingress.rewriteAnnotations` | see below | Annotations added only to the path-rewriting Ingress |

### Path-based routing and prefix stripping

When `ingress.provider=contour`, each configured public host is rendered as a
Contour `HTTPProxy`. The dashboard host uses one `HTTPProxy` containing:

- `/` routed to the dashboard service.
- `/fabric`, `/core-main`, and `/core-start` routed to their respective services
  with the prefix stripped before forwarding.

When `ingress.provider=nginx` or `other`, the dashboard host creates two
standard Ingress resources:

- **`patchworks-dashboard`** — routes `/` to the dashboard service.
- **`patchworks-dashboard-routes`** — routes `/fabric`, `/core-main`, and
  `/core-start` to their respective services.

When `dashboard.routingMode=path`, empty dashboard service URLs use these
same-origin routes by default. When `dashboard.routingMode=host`, empty
dashboard service URLs use the dedicated `ingress.hosts.*` hostnames when
configured.

`ingress.hosts.webhook` and `ingress.hosts.callback` create dedicated public
routes to the Core start service. This matches the production Haberdashery
layout where `webhooks.*` and `callbacks.*` are separate public hosts handled by
Core Start. The same host values are also injected into Core application pods as
`WEBHOOK_DOMAIN` and `CALLBACK_DOMAIN`.

> ⚠️ **`ingress.rewriteAnnotations` is only used with standard Ingress providers.** Contour uses `HTTPProxy.pathRewritePolicy` directly. If you use nginx or another Ingress controller, configure rewrite annotations for that controller.

#### Contour (default)

No rewrite annotations are needed. The chart renders `HTTPProxy` resources and
uses `pathRewritePolicy` for the dashboard `/fabric`, `/core-main`, and
`/core-start` routes.

#### nginx-ingress

nginx requires a regex capture group in the path and a different annotation. Set `rewriteAnnotations` and override the path patterns via `annotations`:

```yaml
ingress:
  annotations:
    nginx.ingress.kubernetes.io/use-regex: "true"
  rewriteAnnotations:
    nginx.ingress.kubernetes.io/rewrite-target: /$2
```

> **Note:** nginx also requires the path patterns to include a capture group (e.g. `/fabric(/|$)(.*)`). Because Kubernetes Ingress path patterns are not configurable per-path in Helm without a full template override, nginx users may prefer to set `ingress.enabled=false` and manage Ingress resources manually or via a separate chart.

#### Traefik

Use a `StripPrefix` middleware and reference it via an annotation:

```yaml
# First create the middleware in the same namespace:
# kubectl create -f - <<EOF
# apiVersion: traefik.io/v1alpha1
# kind: Middleware
# metadata:
#   name: patchworks-strip-prefix
# spec:
#   stripPrefix:
#     prefixes: ["/fabric", "/core-main", "/core-start"]
# EOF

ingress:
  rewriteAnnotations:
    traefik.ingress.kubernetes.io/router.middlewares: "<namespace>-patchworks-strip-prefix@kubernetescrd"
```

---

## MySQL

| Key | Default | Description |
|-----|---------|-------------|
| `mysql.enabled` | `true` | Deploy MySQL in-cluster. Set `false` to use an external instance |
| `mysql.external.host` | `""` | External MySQL hostname |
| `mysql.external.port` | `3306` | External MySQL port |
| `mysql.external.database` | `core` | Database name |
| `mysql.external.username` | `patchworks` | Username |
| `mysql.external.password` | `""` | Password (or use `existingSecret`) |
| `mysql.external.existingSecret.name` | `""` | Secret name for external credentials |
| `mysql.external.existingSecret.passwordKey` | `password` | Key for the password |
| `mysql.maxConnections` | `1000` | `max_connections` for bundled MySQL |
| `mysql.auth.rootPassword` | `""` | Root password for bundled MySQL. Generated by default when empty |
| `mysql.auth.database` | `core` | Database created on MySQL's first start and ensured by the infra setup hook when applicable |
| `mysql.databases` | `[]` | Additional schema names created by the infra MySQL setup hook; use 1–64 letters, digits, underscores or hyphens |
| `mysql.auth.username` | `patchworks` | Application user |
| `mysql.auth.password` | `""` | Application user password. Generated by default when empty |
| `mysql.auth.existingSecret.name` | `""` | Secret name for in-cluster credentials |
| `mysql.auth.existingSecret.rootPasswordKey` | `root-password` | Key for the root password |
| `mysql.auth.existingSecret.passwordKey` | `password` | Key for the app user password |
| `mysql.persistence.size` | `10Gi` | PVC size |
| `mysql.persistence.storageClass` | `""` | Storage class (uses cluster default if empty) |
| `mysql.persistence.existingClaim` | `""` | Use a pre-existing PVC |

---

## Landlord and tenant databases

Core and Monocore can use a separate landlord database and multiple tenant
databases, either on the same MySQL instance or on separate servers. Configure
the connections once under `database.*`; the app chart renders each runtime's
variable names, including migration/seed jobs and all worker modes.

By default, landlord and default tenant connections inherit the resolved
`mysql.*` host, port, username and password/Secret. The landlord schema also
inherits `mysql.auth.database` or `mysql.external.database`. Existing values
therefore keep the shared connection. Core's ordinary `DB_*` variables are
compatibility aliases for the resolved landlord connection, so a landlord
override also updates them.
Fabric's own database remains configured through `fabric.mysql.*`.

| Key | Default | Description |
|-----|---------|-------------|
| `database.landlord.host` | `""` | Landlord MySQL host; empty inherits the resolved `mysql.*` host |
| `database.landlord.port` | `0` | Landlord port; zero inherits the resolved MySQL port |
| `database.landlord.database` | `""` | Landlord schema; empty inherits the resolved MySQL database |
| `database.landlord.username` | `""` | Landlord username; empty inherits the resolved MySQL username |
| `database.landlord.password` | `null` | Landlord password; null/unset inherits the resolved MySQL password/Secret; explicit `""` uses an empty password |
| `database.landlord.existingSecret.name` | `""` | Secret containing the landlord password; overrides inline/inherited password |
| `database.landlord.existingSecret.passwordKey` | `password` | Password key in the landlord Secret |
| `database.landlord.readHost` | `""` | Optional landlord read host for Core and Monocore; empty uses the write host |
| `database.tenant.host` | `""` | Default tenant MySQL host; empty inherits the resolved `mysql.*` host |
| `database.tenant.port` | `0` | Default tenant port; zero inherits the resolved MySQL port |
| `database.tenant.username` | `""` | Default tenant username; empty inherits the resolved MySQL username |
| `database.tenant.password` | `null` | Default tenant password; null/unset inherits the resolved MySQL password/Secret; explicit `""` uses an empty password |
| `database.tenant.existingSecret.name` | `""` | Secret containing the default tenant password; overrides inline/inherited password |
| `database.tenant.existingSecret.passwordKey` | `password` | Password key in the default tenant Secret |
| `database.tenant.readHost` | `""` | Optional default tenant read host for Core and Monocore; empty uses the write host |
| `database.tenant.primaryServerId` | `""` | Optional Fabric database-server record ID for Core new tenant placement; distinct from a credential ID. Skips the default-endpoint seed database Job |
| `database.tenant.pool.maxSizePerServer` | `null` | Optional number of warm tenant databases Core maintains on each eligible Fabric database server. `0` stops replenishment; null leaves the landlord setting in control |
| `database.tenant.servers` | `{}` | Map of additional tenant server connections keyed by the application's database server credential ID |

An explicitly supplied password, including `""`, clears an inherited MySQL
Secret reference. A supplied `existingSecret.name` takes precedence. The
chart does not generate passwords for these overrides. Existing Secrets must
exist in every namespace containing a consuming workload or hook Job.

There is no fixed `database.tenant.database`: the application selects a schema
for each tenant. Fabric's `company_databases` records identify those schemas;
their `database_server_id` links to `database_servers.id`. That server record's
`credential_id` selects a key in `database.tenant.servers`. Merely adding a server or schema to
Helm values does not create these registry records or assign tenants to it.

Each `database.tenant.servers.<id>` entry has the following fields. IDs must
contain only letters, digits and underscores (`A-Za-z0-9_`). Server credentials
are independent of `mysql.*` and the default tenant connection. The chart
iterates this map; adding any valid credential ID such as `db4` or `db100000`
automatically emits both the Core and Monocore environment-variable families
without a template change.

| Key | Default | Description |
|-----|---------|-------------|
| `database.tenant.servers.<id>.host` | required | Server hostname |
| `database.tenant.servers.<id>.port` | `3306` | Server port |
| `database.tenant.servers.<id>.username` | required | Server username |
| `database.tenant.servers.<id>.password` | `""` | Nonempty server password required unless an existing Secret is supplied |
| `database.tenant.servers.<id>.existingSecret.name` | `""` | Secret containing this server's password |
| `database.tenant.servers.<id>.existingSecret.passwordKey` | `password` | Password key in this server's Secret |
| `database.tenant.servers.<id>.readHost` | `""` | Optional Core and Monocore read host; empty uses this server's write host |

### Core and Monocore variables

| Connection | Core (PHP) | Monocore (Go) |
|------------|------------|---------------|
| Compatibility connection | `DB_*` aliases the resolved landlord connection | Not used |
| Landlord host, port, username, password | `LANDLORD_DB_HOST`, `LANDLORD_DB_PORT`, `LANDLORD_DB_USERNAME`, `LANDLORD_DB_PASSWORD` | `DB_LANDLORD_HOST`, `DB_LANDLORD_PORT`, `DB_LANDLORD_USERNAME`, `DB_LANDLORD_PASSWORD` |
| Landlord schema | `LANDLORD_DB_DATABASE` | `DB_LANDLORD_DATABASE` and `db.landlord.name` in generated `config.yaml` |
| Default tenant host, port, username, password | `TENANT_DB_HOST`, `TENANT_DB_PORT`, `TENANT_DB_USERNAME`, `TENANT_DB_PASSWORD` | `DB_TENANT_HOST`, `DB_TENANT_PORT`, `DB_TENANT_USERNAME`, `DB_TENANT_PASSWORD` |
| Assigned server `<id>` | `TENANT_DB_HOST_<id>`, `TENANT_DB_PORT_<id>`, `TENANT_DB_USERNAME_<id>`, `TENANT_DB_PASSWORD_<id>` | `DB_TENANT_<id>_HOST`, `DB_TENANT_<id>_PORT`, `DB_TENANT_<id>_USERNAME`, `DB_TENANT_<id>_PASSWORD` |
| Read host | `LANDLORD_DB_READ_HOST`, `TENANT_DB_READ_HOST`, `TENANT_DB_READ_HOST_<id>` | `DB_LANDLORD_READ_HOST`, `DB_TENANT_READ_HOST`, `DB_TENANT_<id>_READ_HOST` |
| Fabric read host | `FABRIC_DB_READ_HOST` from `fabric.mysql.external.readHost` | `DB_FABRIC_READ_HOST` from `fabric.mysql.external.readHost` |
| New tenant placement | `PRIMARY_TENANT_DATABASE_SERVER_ID` | Not used |

Core receives `LANDLORD_DB_CONNECTION=landlord`, the Laravel connection name.
Monocore receives tenant connection settings through process environment
variables; putting them only in `db.tenant` in `config.yaml` is insufficient.
Monocore also receives the Core `TENANT_DB_*` naming family, including read
hosts, as compatibility aliases for versions before the environment-variable
rename; canonical `DB_TENANT_*` variables take precedence. Empty read hosts are
omitted so each runtime falls back to its write host.

Fabric itself receives the same configured endpoint as `DB_READ_HOST`, matching
its Laravel database configuration. Core receives it as `FABRIC_DB_READ_HOST`,
and Monocore receives it as `DB_FABRIC_READ_HOST`.

Monocore receives `DB_LANDLORD_*` and `DB_FABRIC_*` component variables, plus
the compatibility `DB_LANDLORD_DSN` and `DB_FABRIC_DSN` values. The DSN templates
reference `$(DB_LANDLORD_PASSWORD)` and `$(DB_FABRIC_PASSWORD)` for Kubernetes
environment substitution; they do not embed plaintext passwords.

The configured Monocore image must support independent default tenant
connections and assigned-server credentials. This wiring was verified against
current local Monocore source, not the chart's default `v0.1.43` image.
Compatibility aliases cover variable naming; they cannot add database-routing
features to an older image.

### Separate schemas on bundled MySQL

Apply the same values to the infra and app releases, installing/upgrading
infrastructure first:

```yaml
mysql:
  enabled: true
  auth:
    database: core
  databases:
    - landlord
    - tenant_acme
    - tenant_globex

database:
  landlord:
    database: landlord
  # Default tenant connections inherit bundled MySQL host and credentials.
```

The infra `mysql-setup` hook ensures the default schema, `mysql.databases`,
and the shared Fabric schema when applicable on both install and upgrade.
It grants access to the bundled application user. Its existing broad user
privileges are unchanged; listing schemas does not provide tenant isolation
through separate MySQL users. Use tenant registry schema names that match
`tenant_acme` and `tenant_globex` in this example.

### Separate external landlord and tenant servers

Create the databases, users, grants and referenced Secrets before applying
the values. This example sets an external shared MySQL fallback, a separate
landlord connection, a default tenant server and an additional server with
credential ID `eu_1`:

```yaml
mysql:
  enabled: false
  external:
    host: shared-db.example.com
    database: core
    username: core
    existingSecret:
      name: shared-db
      passwordKey: password

database:
  landlord:
    host: landlord-db.example.com
    port: 3306
    database: landlord
    username: landlord
    existingSecret:
      name: landlord-db
      passwordKey: password
  tenant:
    host: tenants-db.example.com
    port: 3306
    username: tenant_app
    existingSecret:
      name: tenant-db
      passwordKey: password
    servers:
      eu_1:
        host: tenants-eu-db.example.com
        port: 3306
        username: tenant_eu
        existingSecret:
          name: tenant-eu-db
          passwordKey: password
        readHost: tenants-eu-read.example.com
```

An application's server registry entry with `credential_id: eu_1` must refer
to the matching host/user credentials above. Multiple tenant schema records
can share that server entry. `mysql.databases` only provisions bundled MySQL;
external schemas and grants are managed outside the chart. Application
onboarding and tenant migrations still create/update the application records
and schema contents. Changing connection values does not move existing data.

`seeds.tenant.createDatabase` creates the initial schema on the default tenant
endpoint; set it to `false` for a preprovisioned schema. When
`database.tenant.primaryServerId` is set, this bootstrap Job is automatically
skipped because Helm cannot resolve the Fabric server record to credentials.
Application provisioning and `migrate:tenants --create` must follow the registry
assignment to the intended server.

---

## Redis

| Key | Default | Description |
|-----|---------|-------------|
| `redis.enabled` | `true` | Deploy Valkey in-cluster. Set `false` to use an external instance |
| `redis.mode` | `standalone` | PHP/Monocore connection mode: `standalone`, `sentinel`, or `cluster` |
| `redis.scheme` | `tcp` | PHP/Monocore transport: `tcp` or certificate-verified `tls` |
| `fabric.redis.mode` | `standalone` | Fabric connection mode: `standalone` or `cluster` |
| `fabric.redis.scheme` | `tcp` | Fabric Redis transport: `tcp` or `tls` |
| `redis.external.host` | `""` | External Redis hostname |
| `redis.external.port` | `6379` | External Redis port |
| `redis.external.password` | `""` | Password (or use `existingSecret`) |
| `redis.external.existingSecret.name` | `""` | Secret name for external password |
| `redis.external.existingSecret.passwordKey` | `password` | Key for the password |
| `redis.prefix` | `core` | Redis key prefix injected as `REDIS_PREFIX` for Core web and workers |
| `redis.persistence.size` | `1Gi` | PVC size |
| `redis.persistence.existingClaim` | `""` | Use a pre-existing PVC |

For a TLS-only Redis Cluster or ElastiCache cluster endpoint, set both
`redis.mode: cluster` and `redis.scheme: tls`. The chart passes these as
`REDIS_MODE` and `REDIS_SCHEME` to Core web services, processors, schedulers,
PHP workers, migration Jobs and seed Jobs. The settings are included in both
the shared ConfigMap and the inline environment used by Jobs; ConfigMap checksum
changes roll the consuming Deployments. Core's cluster configuration covers its
default, cache, tenant and payload connections and requires database `0`.

Fabric uses `fabric.redis.mode` and `fabric.redis.scheme` independently. Set both
to `cluster` and `tls` when its endpoint requires them, including when Fabric
shares Core's Redis host. Its web/init containers and migration/seed Jobs receive
the same settings. Cluster mode selects `REDIS_CLIENT=phpredis`.

Use Core and Fabric images that support these environment variables. Chart
settings alone cannot add TLS/cluster support to older application images.
Monocore similarly requires an image that supports `redis.scheme` /
`REDIS_SCHEME`; its generated `config.yaml` carries the scheme for hub and
company workers. For an older chart with a compatible Monocore image,
`workers.mono.extraEnv: [{name: REDIS_SCHEME, value: tls}]` remains a workaround.

---

## Metrics collection

`workers.mono.serviceMonitor.enabled` creates a monitor for each enabled Monocore
hub/company worker, scraping its existing internal Service on port `metrics`
(8081). Company workers use their configured namespaces. Standalone and
microservice PHP workers have no HTTP metrics listener.

Core publishes `core_*` business metrics to a Pushgateway from its medium
processor scheduler. Supply `PUSH_GATEWAY_HOST` using `app.extraEnv`, run that
scheduler, and configure a ServiceMonitor on the Pushgateway with
`honorLabels: true`. Gateway, Start, and PHP processors cannot be scraped directly
for those metrics. Kubernetes CPU/memory data comes from kubelet/cAdvisor and
kube-state-metrics in the cluster monitoring stack.

All monitors are opt-in and require the `monitoring.coreos.com/v1` ServiceMonitor
CRD and a collector selecting their labels and namespaces. Each monitor is created
in the target Service namespace. Configure the following keys under each
`serviceMonitor` block:

| Key | Default | Purpose |
|---|---|---|
| `enabled` | `false` | Create the ServiceMonitor |
| `additionalLabels` | `{}` | Labels required by the collector's ServiceMonitor selector |
| `interval` | `30s` | Scrape interval |
| `scrapeTimeout` | `10s` | Scrape timeout; must not exceed the interval |
| `path` | `/metrics` | Metrics endpoint path (RabbitMQ defaults to `/metrics/per-object`) |
| `honorLabels` | `false` | Preserve conflicting labels from scraped metrics when true |
| `relabelings` | `[]` | Target relabeling rules |
| `metricRelabelings` | `[]` | Metric relabeling rules before ingestion |

```yaml
workers:
  type: mono
  mono:
    serviceMonitor:
      enabled: true
      additionalLabels:
        release: kube-prometheus-stack
```

## RabbitMQ

PHP Core web and worker pods default to `RABBITMQ_HEARTBEAT=0`, matching production.
Override it through `app.extraEnv` or the component's `extraEnv` when needed.
This default does not apply to Monocore workers.

| Key | Default | Description |
|-----|---------|-------------|
| `rabbitmq.enabled` | `true` | Deploy RabbitMQ in-cluster. Set `false` to use an external instance |
| `rabbitmq.external.host` | `""` | External RabbitMQ hostname |
| `rabbitmq.external.port` | `5672` | External AMQP port |
| `rabbitmq.external.username` | `patchworks` | Username |
| `rabbitmq.external.vhost` | `/` | Virtual host |
| `rabbitmq.external.password` | `""` | Password (or use `existingSecret`) |
| `rabbitmq.external.existingSecret.name` | `""` | Secret name for external credentials |
| `rabbitmq.external.existingSecret.passwordKey` | `password` | Key for the password |
| `rabbitmq.auth.username` | `patchworks` | Username for bundled RabbitMQ |
| `rabbitmq.auth.password` | `""` | Password for bundled RabbitMQ. Generated by default when empty |
| `rabbitmq.auth.vhost` | `/` | Virtual host |
| `rabbitmq.auth.existingSecret.name` | `""` | Secret name for in-cluster credentials |
| `rabbitmq.auth.existingSecret.passwordKey` | `password` | Key for the password |
| `rabbitmq.persistence.size` | `5Gi` | PVC size |
| `rabbitmq.persistence.existingClaim` | `""` | Use a pre-existing PVC |
| `rabbitmq.resources` | See `values.yaml` | CPU/memory resources for bundled RabbitMQ |
| `rabbitmq.topology.enabled` | `true` | Create processor and PHP worker hub queues through AMQP `queue.declare` |
| `rabbitmq.topology.command` | `[monocore]` | Command for the queue topology hook. Image comes from `workers.mono.image` |
| `rabbitmq.topology.args` | `[apply-rabbitmq-topology, --rabbitmq-topology-file=/etc/patchworks/rabbitmq/topology.yaml]` | Arguments for the queue topology hook |
| `rabbitmq.topology.queueType` | `quorum` | Queue type declared for generated queues |
| `rabbitmq.topology.backoffLimit` | `3` | Retry limit for the topology Job |
| `rabbitmq.topology.activeDeadlineSeconds` | `300` | Maximum runtime for the topology Job |

---

## Elasticsearch

| Key | Default | Description |
|-----|---------|-------------|
| `elasticsearch.enabled` | `true` | Deploy Elasticsearch in-cluster. Set `false` to use an external cluster |
| `elasticsearch.auth.username` | `elastic` | Username for the in-cluster Elasticsearch built-in user |
| `elasticsearch.auth.password` | `""` | Password for the in-cluster Elasticsearch built-in user. Generated by default when empty |
| `elasticsearch.auth.existingSecret.name` | `""` | Secret name for in-cluster Elasticsearch credentials |
| `elasticsearch.auth.existingSecret.usernameKey` | `username` | Key for `ELASTIC_SEARCH_USERNAME` when using an existing Secret |
| `elasticsearch.auth.existingSecret.passwordKey` | `password` | Key for `ELASTIC_PASSWORD` / `ELASTIC_SEARCH_PASSWORD` when using an existing Secret |
| `elasticsearch.external.host` | `""` | External hostname |
| `elasticsearch.external.port` | `9200` | External port |
| `elasticsearch.external.scheme` | `http` | `http` or `https` |
| `elasticsearch.external.cloudId` | `""` | Elastic Cloud ID for PHP core |
| `elasticsearch.external.cloudApiKey` | `""` | Elastic Cloud API key for PHP core |
| `elasticsearch.external.apiKey` | `""` | Elasticsearch API key for PHP core |
| `elasticsearch.external.username` | `""` | Username for PHP core basic auth |
| `elasticsearch.external.password` | `""` | Password for PHP core basic auth |
| `elasticsearch.external.existingSecret.name` | `""` | Secret name for credentials |
| `elasticsearch.external.existingSecret.cloudIdKey` | `""` | Key for `ELASTIC_SEARCH_CLOUD_ID` |
| `elasticsearch.external.existingSecret.cloudApiKeyKey` | `""` | Key for `ELASTIC_SEARCH_CLOUD_API_KEY` |
| `elasticsearch.external.existingSecret.apiKeyKey` | `""` | Key for `ELASTIC_SEARCH_API_KEY` |
| `elasticsearch.external.existingSecret.usernameKey` | `ELASTIC_SEARCH_USERNAME` | Key for `ELASTIC_SEARCH_USERNAME` |
| `elasticsearch.external.existingSecret.passwordKey` | `ELASTIC_SEARCH_PASSWORD` | Key for `ELASTIC_SEARCH_PASSWORD` |
| `elasticsearch.persistence.size` | `15Gi` | PVC size |
| `elasticsearch.persistence.existingClaim` | `""` | Use a pre-existing PVC |
| `elasticsearch.javaOpts` | `-Xms512m -Xmx512m` | JVM heap settings |

---

## S3 / MinIO

When `s3.enabled` is `true`, a MinIO-compatible server ([Silo](https://silo.pgsty.com), a maintained MinIO fork) is deployed by the infra chart and its post-install/upgrade Job creates the configured buckets. Payload, tenant-cache, and file-download buckets default to the main bucket so self-hosted installs do not require a per-tenant bucket creation service.

| Key | Default | Description |
|-----|---------|-------------|
| `s3.enabled` | `true` | Deploy MinIO in-cluster. Set `false` to use external S3 |
| `s3.bucket` | `patchworks` | Bucket name |
| `s3.mediaBucket` | `""` | Core's and Fabric's `AWS_BUCKET`, the media library's `s3` disk; defaults to `s3.bucket` / `s3.external.bucket`. Monocore's default store keeps the main bucket |
| `s3.payloadsBucket` | `""` | Default bucket for Core payloads when tenant-specific buckets are not available; defaults to `s3.bucket` / `s3.external.bucket` |
| `s3.tenantCacheBucket` | `""` | Bucket for tenant cache payloads; defaults to `s3.companyCacheBucket`, then `s3.bucket` / `s3.external.bucket` |
| `s3.fileDownloadsBucket` | `""` | Bucket for file downloads; defaults to `s3.bucket` / `s3.external.bucket` |
| `s3.bucketCreationEndpoint` | `""` | Explicit bucket creation endpoint; defaults to `s3Manager.external.endpoint`, then the in-cluster S3 Manager service, then the resolved S3 endpoint |
| `s3.region` | `us-east-1` | Region |
| `s3.image.repository` | `pgsty/silo` | S3 server image. Silo is a maintained MinIO fork; MinIO no longer publishes images |
| `s3.image.tag` | `RELEASE.2026-11-12T00-00-00Z` | S3 server image tag |
| `s3.image.pullPolicy` | `IfNotPresent` | S3 server image pull policy |
| `s3.setup.image.repository` | `pgsty/mc` | Client image for the bucket setup Job; needs `sh` and `mc` |
| `s3.setup.image.tag` | `RELEASE.2026-11-12T00-00-00Z` | Bucket setup Job image tag |
| `s3.setup.image.pullPolicy` | `IfNotPresent` | Bucket setup Job image pull policy |
| `s3.auth.rootUser` | `minioadmin` | MinIO root user |
| `s3.auth.rootPassword` | `""` | MinIO root password. Generated by default when empty |
| `s3.auth.existingSecret.name` | `""` | Secret name for in-cluster credentials |
| `s3.auth.existingSecret.rootUserKey` | `root-user` | Key for the root user |
| `s3.auth.existingSecret.rootPasswordKey` | `root-password` | Key for the root password |
| `s3.external.endpoint` | `""` | S3 endpoint URL |
| `s3.external.accessKey` | `""` | Access key |
| `s3.external.secretKey` | `""` | Secret key |
| `s3.external.existingSecret.name` | `""` | Secret name for external credentials |
| `s3.external.existingSecret.accessKeyKey` | `access-key` | Key for the access key |
| `s3.external.existingSecret.secretKeyKey` | `secret-key` | Key for the secret key |
| `s3.external.region` | `us-east-1` | Region |
| `s3.external.bucket` | `patchworks` | Bucket name |
| `s3.external.pathStyle` | `false` | Use path-style S3 addressing for external S3-compatible storage |
| `s3.companyCacheBucket` | `""` | Deprecated alias for monocore's `customer_cache` store; prefer `s3.tenantCacheBucket` |
| `s3.persistence.size` | `10Gi` | PVC size |
| `s3.persistence.existingClaim` | `""` | Use a pre-existing PVC |

---

## S3 Manager

The app chart deploys S3 Manager by default and points
`S3_BUCKET_CREATION_ENDPOINT` at it for Core web pods and workers. It uses the
same S3 endpoint and credentials as Core.

Monocore workers use it to create a company's payload bucket the first time
they store a payload for that company, as Core does. They receive
`S3_BUCKET_CREATION_ENDPOINT` only when a bucket creation service is configured
(`s3.bucketCreationEndpoint`, `s3Manager.external.endpoint` or
`s3Manager.enabled`), never the resolved S3 endpoint fallback; without one they
write such payloads to the default bucket.

| Key | Default | Description |
|-----|---------|-------------|
| `s3Manager.fullnameOverride` | `""` | Override the Deployment/Service name and ConfigMap prefix; defaults to `<fullname>-s3-manager`. The in-cluster bucket creation endpoint follows this name. |
| `s3Manager.namespace` | `""` | Namespace override for the S3 Manager service |
| `s3Manager.enabled` | `true` | Deploy the in-cluster S3 Manager service from the app chart |
| `s3Manager.external.endpoint` | `""` | External bucket creation service endpoint. Takes precedence over the in-cluster service |
| `s3Manager.image.repository` | `s3manager` | Image repository, resolved through the global Patchworks image registry |
| `s3Manager.image.registry` | `""` | Optional registry override |
| `s3Manager.image.tag` | `v0.0.5` | Image tag |
| `s3Manager.image.pullPolicy` | `""` | Optional pull policy override |
| `s3Manager.replicaCount` | `1` | Number of replicas |
| `s3Manager.service.type` | `ClusterIP` | Kubernetes Service type |
| `s3Manager.service.port` | `8080` | HTTP service port |
| `s3Manager.config.http.addr` | `""` | HTTP listen address in the generated `config.yaml`; defaults to `:<s3Manager.service.port>` |
| `s3Manager.config.log.format` | `json` | S3 Manager log format |
| `s3Manager.config.s3.provider` | `""` | S3 provider passed to S3 Manager; defaults to `minio` for in-cluster S3 or `aws` for external S3 |
| `s3Manager.config.s3.endpoint` | `""` | S3 endpoint in the generated `config.yaml`; defaults to the resolved chart S3 endpoint |
| `s3Manager.config.s3.region` | `""` | S3 region in the generated `config.yaml`; defaults to the resolved chart S3 region |
| `s3Manager.config.s3.sessionToken` | `""` | Optional S3 session token |
| `s3Manager.config.s3.pathStyle` | `""` | S3 path-style setting; defaults to the resolved chart S3 path-style value |
| `s3Manager.config.buckets.name` | `local-pwks-${company_id}.pwks.co` | Bucket name template used for company bucket creation |
| `s3Manager.config.buckets.region` | `""` | Bucket region; defaults to the resolved chart S3 region |
| `s3Manager.config.buckets.permissions` | `{ objectOwnership: BucketOwnerEnforced }` | Bucket permissions block rendered into S3 Manager `config.yaml` |
| `s3Manager.config.buckets.lifecycle` | payload/cache expiry rules | Bucket lifecycle block rendered into S3 Manager `config.yaml` |
| `s3Manager.config.buckets.metricFilterRules` | `AllObjects`, `Payloads`, `Caches` | Bucket metric filters rendered into S3 Manager `config.yaml` |
| `s3Manager.config.buckets.tags` | cost-center/company/environment tags | Tags applied by S3 Manager when creating company buckets |
| `s3Manager.extraEnv` | `[]` | Additional env vars for the S3 Manager pod |
| `s3Manager.extraEnvFrom` | `[]` | Additional envFrom sources for the S3 Manager pod |
| `s3Manager.podAnnotations` | `{}` | Additional pod annotations |
| `s3Manager.nodeSelector` | `{}` | Node selector |
| `s3Manager.tolerations` | `[]` | Pod tolerations |
| `s3Manager.affinity` | `{}` | Pod affinity |
| `s3Manager.resources` | `{}` | Resource requests/limits |

---

## Pusher / Soketi

The app chart does not deploy Soketi resources. It consumes the same shared
`pusher.*` values as the infra chart and injects the corresponding `PUSHER_*`
env vars into Core web pods, workers, and dashboard broadcasting config. When
`pusher.ingress.enabled=true` and `pusher.ingress.host` is set, the dashboard
defaults its websocket connection to that public host while backend services
continue using the in-cluster Soketi service. The dashboard connects on
`443`/`https` when `pusher.ingress.tlsSecretName` is set or `ingress.scheme` is
`https` (TLS terminated outside the chart, for example by an upstream proxy),
and on `80`/`http` otherwise. `dashboard.broadcasting.port` and `scheme`
override both.

When `pusher.enabled=true`, the app chart assumes the infra chart has deployed
the native `patchworks-soketi` Service. If credentials are omitted and
`credentials.autoGenerate=true`, the app chart also ensures the stable
`patchworks-soketi-auth` Secret exists before Core or dashboard pods start. For
an external Pusher-compatible server, leave `pusher.enabled=false`, set
`pusher.external.host`, `port`, and `scheme`, and provide matching credentials
inline or via `pusher.existingSecret`.

| Key | Default | Description |
|-----|---------|-------------|
| `pusher.enabled` | `true` | Use in-cluster Soketi from the infra chart |
| `pusher.appId` | `""` | Pusher app ID. Generated when `pusher.enabled=true`, omitted, and `credentials.autoGenerate=true` |
| `pusher.appKey` | `""` | Pusher app key. Generated when `pusher.enabled=true`, omitted, and `credentials.autoGenerate=true` |
| `pusher.appSecret` | `""` | Pusher app secret. Generated when `pusher.enabled=true`, omitted, and `credentials.autoGenerate=true` |
| `pusher.appCluster` | `mt1` | Pusher cluster value |
| `pusher.existingSecret.name` | `""` | Secret containing Pusher/Soketi credentials |
| `pusher.existingSecret.appIdKey` | `app-id` | Key for the app ID |
| `pusher.existingSecret.appKeyKey` | `app-key` | Key for the app key |
| `pusher.existingSecret.appSecretKey` | `app-secret` | Key for the app secret |
| `pusher.existingSecret.appClusterKey` | `app-cluster` | Key for the cluster |
| `pusher.external.host` | `""` | External Pusher-compatible host when `pusher.enabled=false` |
| `pusher.external.port` | `443` | External Pusher-compatible port |
| `pusher.external.scheme` | `https` | External Pusher-compatible scheme |
| `pusher.ingress.enabled` | `false` | Expose in-cluster Soketi through an ingress for browser websocket traffic |
| `pusher.ingress.provider` | `contour` | Ingress provider hint; `contour` adds the websocket route annotation |
| `pusher.ingress.className` | `""` | Optional ingress class name for the Soketi ingress |
| `pusher.ingress.annotations` | `{}` | Additional annotations for the Soketi ingress |
| `pusher.ingress.host` | `""` | Hostname for the Soketi ingress. When set, the dashboard defaults `PUSHER_HOST` to this value |
| `pusher.ingress.tlsSecretName` | `""` | Optional TLS secret for the Soketi ingress. Also flips dashboard defaults to `https:443`; `ingress.scheme=https` does the same without a secret |
| `pusher.ingress.timeout` | `3600` | Websocket proxy read/send timeout in seconds for nginx ingress |
| `soketi.fullnameOverride` | `patchworks-soketi` | Stable resource name used when opting into the upstream Soketi subchart |
| `soketi.subchart.enabled` | `false` | Shared with the infra chart; when true, app pods target the upstream Soketi subchart service name |

Example using generated in-cluster Soketi credentials:

```yaml
pusher:
  enabled: true
  ingress:
    enabled: true
    host: wss.selfhosted.patchworks.io
```

Example using an existing Secret:

```yaml
pusher:
  enabled: true
  existingSecret:
    name: patchworks-pusher
    appIdKey: app-id
    appKeyKey: app-key
    appSecretKey: app-secret
    appClusterKey: app-cluster
```

---

## KubeFaaS

Optional serverless function execution layer. Disabled by default.

When `kubefaas.enabled`, `kubefaas.auth.enabled`, and `credentials.autoGenerate` are all true, missing auth credentials are generated by the infra chart into `<release>-kubefaas-auth` in both the KubeFaaS namespace and the app release namespace. The app chart consumes the release namespace copy. For external KubeFaaS, provide both inline auth values or `kubefaas.auth.existingSecret.name`.

| Key | Default | Description |
|-----|---------|-------------|
| `kubefaas.enabled` | `false` | Deploy KubeFaaS in-cluster |
| `kubefaas.namespace` | `kubefaas` | Namespace for KubeFaaS components (separate from app namespace) |
| `kubefaas.functions.namespaceCount` | `5` | Number of function execution namespaces created |
| `kubefaas.builder.tls.mode` | `helm` | TLS mode: `helm`, `certManager`, or `existingSecret` |
| `kubefaas.builder.tls.certManager.caDuration` | `87600h` | Lifetime of the chart-managed cert-manager CA (10 years) |
| `kubefaas.builder.tls.certManager.caRenewBefore` | `8760h` | Renew the chart-managed CA one year before expiry |
| `kubefaas.builder.tls.certManager.duration` | `8760h` | Lifetime of the dind server and client certificates |
| `kubefaas.builder.tls.certManager.renewBefore` | `720h` | Renew dind certificates 30 days before expiry |
| `kubefaas.auth.enabled` | `true` | Enable KubeFaaS basic auth env wiring |
| `kubefaas.auth.username` | `""` | Auth username shared by controller and builder; generated when omitted and eligible |
| `kubefaas.auth.password` | `""` | Auth password; generated when omitted and eligible |
| `kubefaas.auth.existingSecret.name` | `""` | Source auth credentials from this Secret instead of inline values |
| `kubefaas.auth.existingSecret.usernameKey` | `username` | Key for the username |
| `kubefaas.auth.existingSecret.passwordKey` | `password` | Key for the password |
| `kubefaas.registry.name` | `""` | Container registry for built function images |

When upgrading a cluster where the cert-manager CA has already rotated, force-renew the
`<release>-kubefaas-docker-server` and `<release>-kubefaas-docker-client` Certificates once
after applying the updated infra chart, then restart the builder Deployment. Cert-manager does not
automatically replace an unexpired leaf certificate solely because its issuing CA changed.

---

## Existing secrets

Every credential has a companion `existingSecret` block with named key fields. When `name` is set the chart renders a `valueFrom.secretKeyRef` instead of an inline value. You can mix inline values and secret references freely.

```yaml
# kubectl create secret generic patchworks-secrets \
#   --from-literal=APP_KEY="base64:..." \
#   --from-literal=APP_PREVIOUS_KEYS="base64:...,base64:..." \
#   --from-literal=db-password="s3cr3t" \
#   --from-literal=minio-password="s3cr3t"

app:
  existingSecret:
    name: patchworks-secrets
    key: APP_KEY
    previousKeysKey: APP_PREVIOUS_KEYS

mysql:
  auth:
    existingSecret:
      name: patchworks-secrets
      passwordKey: db-password   # multi-credential secrets use descriptive *Key fields

s3:
  auth:
    existingSecret:
      name: patchworks-secrets
      rootPasswordKey: minio-password

pusher:
  existingSecret:
    name: patchworks-secrets
    appIdKey: pusher-app-id
    appKeyKey: pusher-app-key
    appSecretKey: pusher-app-secret
    appClusterKey: pusher-app-cluster
```

---

## Example configurations

### Minimal (all bundled infrastructure)

```yaml
app:
  key: "base64:REPLACE_WITH_GENERATED_KEY"
  url: "http://patchworks.example.com"

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: patchworks.example.com
      paths:
        - path: /
          pathType: Prefix
```

### Production (all external infrastructure)

```yaml
app:
  key: "base64:REPLACE_WITH_GENERATED_KEY"
  url: "https://patchworks.example.com"

ingress:
  enabled: true
  className: nginx
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
  hosts:
    - host: patchworks.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: patchworks-tls
      hosts:
        - patchworks.example.com

web:
  replicaCount: 3

workers:
  replicaCount: 2

mysql:
  enabled: false
  external:
    host: "patchworks.cluster.example.com"
    database: patchworks
    username: patchworks
    existingSecret:
      name: patchworks-db
      passwordKey: password

redis:
  enabled: false
  external:
    host: "patchworks.cache.example.com"
    existingSecret:
      name: patchworks-redis
      passwordKey: password

rabbitmq:
  enabled: false
  external:
    host: "patchworks.mq.example.com"
    username: patchworks
    existingSecret:
      name: patchworks-rabbitmq
      passwordKey: password

elasticsearch:
  enabled: false
  external:
    host: "search.example.com"
    scheme: https
    username: patchworks
    existingSecret:
      name: patchworks-es
      passwordKey: password

s3:
  enabled: false
  external:
    endpoint: "https://s3.amazonaws.com"
    region: eu-west-2
    bucket: my-patchworks-bucket
    existingSecret:
      name: patchworks-s3
      accessKeyKey: access-key
      secretKeyKey: secret-key
```

### Monocore workers

```yaml
workers:
  type: mono
  mono:
    processes: 20
    otel:
      enabled: true
      endpoint: "http://otel-collector:4317"
```

### Microservice workers

```yaml
workers:
  type: microservice
  microservices:
    _default:
      processes: 10
      replicas: 2
    gateway:
      name: Core-Gateway
      domain: gateway
      processes: 20
```

### Secrets from Kubernetes Secrets

```bash
kubectl create secret generic patchworks-secrets \
  --from-literal=APP_KEY="base64:$(openssl rand -base64 32)" \
  --from-literal=db-password="$(openssl rand -base64 24)" \
  --from-literal=minio-root-password="$(openssl rand -base64 24)"
```

```yaml
app:
  existingSecret:
    name: patchworks-secrets
    key: APP_KEY

mysql:
  auth:
    existingSecret:
      name: patchworks-secrets
      passwordKey: db-password
      rootPasswordKey: db-password

s3:
  auth:
    existingSecret:
      name: patchworks-secrets
      rootPasswordKey: minio-root-password
```

### Monocore scheduling rollout

With `workers.type=mono`, hub scheduling defaults to Kubernetes mode. Use a
Monocore build supporting scheduler and durable-execution flags. Deploy compatible
Core ownership guards and the existing runtime tables before enabling company
`monocore-scheduler` flags. Installing this chart does not transfer company ownership.
No scheduler schema migration is added.

The chart renders only desired configuration. Monocore creates and updates one
runtime ConfigMap per database group, named `<prefix>-runtime-<group>` where group
is `default` or `server-<fabric server id>`; do not delete them to recover a stuck
pass. Because groups are discovered from Fabric at runtime, the chart cannot
enumerate those names and Kubernetes RBAC has no resource-name prefix match.
Namespace-scoped RBAC therefore permits Lease get/create/update, pod
get/list/watch, named desired ConfigMap get/watch, and namespace-scoped ConfigMap
get/update/create for the per-group runtime state. Set
`workers.mono.scheduler.rbac.create=false` if those grants are managed externally;
`serviceAccount.name` and `serviceAccount.create` retain their usual behavior.

Dedicated company workers never schedule the whole catalogue. Standalone mode runs
on the single hub pod without Kubernetes access, and disabled/standalone modes
render no scheduler RBAC. Durable execution stays enabled so registered work can
finish after scheduling is disabled. Shard changes update desired configuration
without changing the worker checksum. Keep the same estate and runtime ConfigMap
identity through upgrades.

### Monocore inbound triggers

`workers.mono.inbound.enabled=true` has the hub Monocore worker serve Core's
webhook, callback and start API endpoints (gopatchworks/monocore
`docs/inbound-triggers.md`) on a public listener, port 8082, exposed by the
`<fullname>-<nameSuffix>-inbound` Service alone: the hub's own Service also
reaches `/flows/start` and `/flows/run`, which have no authentication. Company
workers do not serve them. Monocore images up to `v0.2.12` predate the
listener; with one the Service has no listener behind it.

The hub is configured as Core's start pods are: their `WEBHOOK_DOMAIN`,
`CALLBACK_DOMAIN`, `WEBHOOK_RATE_LIMIT`, `CALLBACK_RATE_LIMIT` and
`MANUAL_RUNS_API_RATE_LIMIT` (from `app.extraEnv` and the start service's
`extraEnv`, else the ingress hosts), the Passport public key (only the public
key, from the Passport Secret, which then also has to exist in the workers
namespace), and the cache prefix and queue scheduling derived as described in
the table above. The chart cannot read `extraEnvFrom` or a Secret, so set
`rateLimitPrefix` and `queueSchedule` when Core's start pods get `CACHE_PREFIX`,
`APP_NAME` or `FLOW_QUEUE_SCHEDULE` that way, and set the rate limits in
`workers.mono.extraEnv` too. A wrong prefix fails silently: each engine then
admits a company's full limit. Check Core's with `php artisan tinker
--execute="echo config('cache.prefix');"` in a start pod. When Core's start
pods belong to another release (`web.start.enabled=false`), set `coreUrl`,
`rateLimitPrefix` and `queueSchedule` to theirs; the chart then renders no
start, webhook or callback routes.

Enable it in two upgrades. The first sets `workers.mono.inbound.enabled=true`:
it starts the listener on the new hub pods and changes no route, as
`workers.mono.inbound.ingress.enabled` defaults to `false`. Check that the
inbound Service has endpoints and that the start pods' prefix matches. The
second sets `workers.mono.inbound.ingress.enabled=true`. Done in one upgrade,
the routes move before any hub pod has the public port, so the inbound paths
answer 503 until the first new pod is ready, or until rollback on an image
without the listener. A new installation, which has no traffic yet, can set
both at once.

With `ingress.enabled` and `workers.mono.inbound.ingress.enabled`, the start,
webhook and callback hosts route these paths to Monocore, and everything else
to Core's start Service as before:

| Host | Monocore | Core |
|---|---|---|
| start | `/api/v1/flows/` | everything else |
| webhook, callback | `/api/v1/` | `/api/v1/otc/`, `/api/v1/mcp` and everything else |

Core still serves every inbound route, so a request left on Core is answered
correctly; Monocore forwards callbacks and `/manual` requests of companies
without the `monocore` flag to Core's start Service and queues their webhooks
and `/start` requests for Core. The dashboard's path route (`/core-start`)
stays on Core, which hands Monocore's companies to Monocore itself. An Ingress
cannot reach a Service in another namespace, so the chart refuses to route
when the start service and the workers are in different namespaces: leave
`workers.mono.inbound.ingress.enabled` off and route to the inbound Service
yourself. The hub cannot scale or pause to zero while it serves them.

Before exposing the listener to the internet, check that the Monocore release
bounds request bodies and header timeouts (monocore-rod.7). Turn on
`workers.mono.scheduler.admitAllTriggers` only after Core's
`MonocoreSchedulerOwnership::restrictPendingToCore` excludes every trigger for
`monocore-scheduler` companies (monocore-rod.6). Core does from the release
after `v0.2.16`.

### Running every company on Monocore

With `workers.type=mono` the chart deploys no Core flow workers, so a company
runs only once Core's `monocore` company feature is on: without it, its flows
are queued where nothing consumes them. Both features default to off, and
Monocore reads their stored values from Core's `features` table, counting a
company with none as off, so they have to be stored for each company.
`workers.mono.allCompanies.enabled=true` does that:

- Core pods get `MONOCORE_AUTO_ENABLE_FOR_NEW_COMPANIES=true`, so Core turns
  `monocore` on for every company it provisions through Fabric.
  `MONOCORE_SCHEDULER_AUTO_ENABLE_FOR_NEW_COMPANIES` (from
  `allCompanies.scheduler`) adds `monocore-scheduler`, handing the company's
  scheduling to the hub, and the hub admits every trigger.
- The Core seed job runs `php artisan monocore:auto-enable` after
  `migrate:tenants --create`, which provisions the seeded tenant without the
  event Core's listener uses. The command only changes companies with no stored
  `monocore` value, so it is safe on every Argo CD sync and never reverses a
  setting. A Core image without the command (`v0.2.16` and earlier) skips it
  with a note.
- Companies with a stored value keep it. Switch an existing company from its
  feature settings, with `monocore` first; `monocore-scheduler` needs a
  coordinated handover of its pending cron work.
- Every Monocore worker runs the optional shapes, since every company's runs
  now reach it: Merge, Data Transformation, Cross-Reference and waiting Run
  Flow with Flow Return. See
  [Monocore's optional shapes](#monocores-optional-shapes).

It also leaves out the Core processes Monocore companies don't need:

| Left out | Why |
|---|---|
| `short-processor` Deployment and schedule CronJob | No Core code queues work for it; its schedule only queues the hourly health check every other schedule also runs |
| `gateway` schedule CronJob | It runs only that health check. The gateway processor stays: it handles API jobs, notifications and tenant provisioning |

Everything else stays. The scheduler processor still receives Core's flow
scheduling jobs every minute, which skip `monocore-scheduler` companies but
would pile up unconsumed without it. The start, medium-processor,
long-processor and logging processors, and the scheduler, start,
medium-processor and long-processor schedules, run platform work Monocore does
not: flow-run logs of runs started from the dashboard, usage and alert checks,
pruning, metrics, custom agents, heartbeat clean-up and the licence check.
Change `disableProcessors` or `disableSchedulers` to keep or leave out others.

### Monocore's optional shapes

Monocore registers no step runner for some shapes unless it is told to, and
fails a run that reaches one of their steps (`step "merge" has no registered
runner`) instead of handing it to Core. `workers.mono.enable` sets the variables Monocore reads
for them, the same ones its own chart sets from `monocore.worker.enable.*`, on
the hub and every company:

| Key | Variable | Shape |
|---|---|---|
| `merge` | `WORKER_ENABLE_MERGE` | Merge |
| `dataTransform` | `WORKER_ENABLE_DATA_TRANSFORM` | Data Transformation |
| `crossReference` | `WORKER_ENABLE_CROSS_REFERENCE` | Cross-Reference |
| `syncSubprocess` | `WORKER_ENABLE_SYNC_SUBPROCESS` | Waiting Run Flow and Flow Return |
| `catchAtRunEnd` | `WORKER_ENABLE_CATCH_AT_RUN_END` | None: when a run's deferred Try/Catch Catch branch runs |

Each renders as `"true"` or `"false"`. A shape left `null`, the default,
follows `workers.mono.allCompanies.enabled`: every company then runs on
Monocore, which has to run these shapes. Without `allCompanies` they stay off,
as Monocore's own defaults do, so set them `true` when you turn `monocore` on
for companies by hand; set one `false` to keep it off with `allCompanies`.
`catchAtRunEnd` stays `false` either way. Turn it on only once every worker
runs a release that reads the deferred Catch branch.

Core has every one of these shapes, but the `v0.2.17` Dashboard has no
Data Transformation node: its canvas can't add that shape, and shows a step
made another way, such as through Core's API, as broken. Monocore still runs
the step when `dataTransform` is on, so it follows `allCompanies` like the
others; turning it off would only make such a run fail.

Monocore's own chart also has `dataProcessing` (`WORKER_ENABLE_DATA_PROCESSING`)
for the Data Processing shape. This chart leaves it out, so it stays off: Core
and the Dashboard are dropping that shape for Data Transformation, and only
their `feature/data-and-merge-shapes` builds, such as `v0.2.17`'s, have it. An
install whose flows still have a Data Processing step can set
`WORKER_ENABLE_DATA_PROCESSING` to `"true"` in `workers.mono.extraEnv`.

Each shape needs the Core code of its own release, so keep Core and Monocore on
one release. The Monocore images of `v0.2.15` to `v0.2.17` read all of these. A
`WORKER_ENABLE_*` entry in `workers.extraEnv` or `workers.mono.extraEnv`
replaces the chart's on every worker, and one in a company's `extraEnv` on that
company's worker alone, so each pod gets one entry for that name. The settings
change only env, which rolls the worker pods, and leave the config checksum
alone.

## Worker autoscaling

Autoscaling is opt-in for `standalone`, `microservice`, and `mono` workers.
Each enabled hub/company Deployment gets its own HPA or KEDA ScaledObject in
its resolved namespace. Disabled autoscaling preserves fixed replicas; enabled
autoscaling omits `spec.replicas` so Helm upgrades do not reset the scaler's count.
Processor Deployments use the same API, with independent opt-in through
`processorDeployments.autoscaling.enabled` or `processors[].autoscaling.enabled`.
This setting does not scale web Deployments or scheduler CronJobs.
Remove any separately managed autoscaler on the same target before enabling it.

The chart explicitly sets KEDA's HPA name, retaining `keda-hpa-<scaledobject>`
when it fits and adding a hash suffix when it exceeds 63 characters. ScaledObject,
TriggerAuthentication and native HPA names use the same length protection.

Upgrading from duplicated Core names creates replacement Deployments and
scalers. Verify the new targets become Ready and prune the old ScaledObjects,
TriggerAuthentications, Deployments and supervisord ConfigMaps through their
owning release; two Deployment names can otherwise consume the same queue.
Monocore resource names are unchanged by the Core naming fix.


Install KEDA and its CRDs separately before selecting `provider: keda` (examples
use the KEDA 2.20 API). Native `provider: hpa` requires Metrics Server; CPU/memory
utilization requires the corresponding resource requests. The chart does not
install operators or a monitoring stack. Helm rendering intentionally works
without CRDs for offline validation; cluster installation requires those CRDs.

Examples (merge with your installation values):

- [Direct RabbitMQ estimated consumption time](docs/autoscaling/rabbitmq.yaml)
- [Prometheus queue metrics](docs/autoscaling/prometheus.yaml)
- [Native CPU/memory HPA](docs/autoscaling/hpa.yaml)
- [Monocore concurrency-aware queue scaling](docs/autoscaling/mono.yaml)

### Configuration and inheritance

Fields merge recursively in this order; later values win, including explicit
`false` and `0`. Lists such as `extraTriggers` replace earlier lists.

| Worker | Precedence, lowest to highest |
|---|---|
| Processors | `workers.autoscaling` → `processorDeployments.autoscaling` → `processors[].autoscaling` |
| Standalone | `workers.autoscaling` → `companies[].autoscaling` |
| Monocore | `workers.autoscaling` → `workers.mono.autoscaling` → `companies[].autoscaling` |
| Microservice | `workers.autoscaling` → `microservices._default.autoscaling` → `microservices.<key>.autoscaling` → `companies[].autoscaling` → `companies[].microservices.<key>.autoscaling` |

`processorDeployments.autoscaling.enabled` defaults to `false`, so enabling
worker autoscaling alone leaves processor replicas fixed. Each enabled
processor scaler uses its resolved Deployment name, namespace, queue and
process count; HPA request validation uses that processor's resources.
Set `processors[].autoscaling.enabled: false` to keep an individual pool fixed.
Disabling `processorDeployments.enabled` suppresses both Deployments and their
scalers, while scheduler CronJobs remain independently selectable.

Hub queues come from `workers.queue.name`, the microservice `queue` (falling
back to `domain`), or `workers.mono.queue`; company queues use `company.queue`,
falling back to `company.name`. `keda.rabbitmq.queueName` can explicitly override the scaler's
queue. Company microservices share a company queue: identical queue triggers
scale every service against the same backlog. Use per-service policies or
service-specific Prometheus queries when that is not the intended behaviour.

All fields below live under the resolved `autoscaling` block:

| Field | Default | Purpose |
|---|---|---|
| `enabled` | `false` | Enable autoscaling for this target |
| `provider` | `keda` | `hpa` or `keda`; only one controller is rendered |
| `minReplicas` / `maxReplicas` | `1` / `10` | Bounds; max must be positive; native HPA min must be at least one |
| `behavior` | scale-down stabilization 300 seconds | Native HPA behavior, including scale-up/down policies; also passed to KEDA's HPA |
| `hpa.cpu` / `hpa.memory` | `0` / `0` | Utilization percentages; zero disables that metric |
| `keda.pollingInterval` / `cooldownPeriod` | `1` / `300` | Polling and scale-to-zero cooldown in seconds; consumption-time scaling requires one-second polling |
| `keda.pausedReplicas` | `null` | Optional explicit pause count, including zero |
| `keda.fallback` | `{}` | Native KEDA fallback configuration, e.g. failureThreshold and replicas; observe scaler/metric-type support |
| `keda.extraTriggers` | `[]` | Native KEDA trigger list: CPU, memory, cron, Redis, or other supported scalers |
| `keda.rabbitmq.enabled` | `false` | Direct broker scaling, independent of Prometheus |
| `keda.rabbitmq.host` | `""` | Credential-free URL, or provide host through authentication |
| `keda.rabbitmq.protocol` | `http` | `http`, `amqp`, or `auto`; MessageRate and ExpectedQueueConsumptionTime require `http` |
| `keda.rabbitmq.vhostName` | `/` | Broker vhost |
| `keda.rabbitmq.unsafeSsl` | `false` | Opt-in disabling of server certificate verification |
| `keda.rabbitmq.queueName` | `""` | Empty means resolved worker queue |
| `keda.rabbitmq.queueLength.enabled/value/activationValue` | `false` / `"30"` / `"0"` | QueueLength trigger and independent target/activation thresholds |
| `keda.rabbitmq.messageRate.enabled/value/activationValue` | `false` / `"22"` / `"0"` | MessageRate trigger and independent thresholds |
| `keda.rabbitmq.expectedQueueConsumptionTime.enabled/value/activationValue` | `true` / `"10"` / `"1"` | Default trigger: scale when estimated broker drain time exceeds the target seconds; rendered with `metricType: Value` |
| `keda.rabbitmq.authenticationRef` | `{}` | Existing TriggerAuthentication name, optionally kind ClusterTriggerAuthentication |
| `keda.rabbitmq.existingSecret.name` | `""` | Generate a target-specific TriggerAuthentication referencing this existing Secret |
| `keda.rabbitmq.existingSecret.hostKey/usernameKey/passwordKey` | `""` / `username` / `password` | Secret keys mapped to authentication parameters; empty key omits that parameter |
| `keda.prometheus.enabled` | `false` | Query a Prometheus-compatible endpoint |
| `keda.prometheus.serverAddress/query` | `""` / `""` | Required when enabled |
| `keda.prometheus.threshold/activationThreshold` | `"30"` / `"0"` | Positive scaling target and nonnegative activation threshold |
| `keda.prometheus.metricType` | `AverageValue` | `AverageValue` for total work divided across replicas, or `Value` for a deliberately normalized signal |
| `keda.prometheus.ignoreNullValues` | `false` | Missing series produce scaler errors rather than silently implying no work |
| `keda.prometheus.authenticationRef` | `{}` | Existing authentication resource for the endpoint |
| `keda.prometheus.metadata` | `{}` | Additional native scaler metadata, including TLS, authModes, and headers; explicit fields above take precedence |

RabbitMQ authentication Secrets and namespaced TriggerAuthentications must exist
in each worker namespace. Use a ClusterTriggerAuthentication for a centrally
managed reference. For a Secret containing a complete URL, set `hostKey` and
clear `usernameKey`/`passwordKey` if those keys do not exist. Inline credential
URLs in `host` are rejected. Custom CA/client certificate configuration can be
provided through native authentication resources. External RabbitMQ management
URLs may differ from application AMQP hosts; configure the scaler endpoint
explicitly. ExpectedQueueConsumptionTime uses the management API's publish and
delivery rates plus ready and unacknowledged messages to estimate broker drain
time. It is the default because short queue spikes that existing consumers can
drain quickly should not request replicas that arrive after the spike. QueueLength
over AMQP counts ready messages; HTTP can also count unacknowledged work.
MessageRate requires the management API. When enabling QueueLength or MessageRate
instead, explicitly disable ExpectedQueueConsumptionTime unless both independent
signals are intended; KEDA follows the largest replica recommendation.

### Metrics and scaling behaviour

RabbitMQ and Prometheus triggers can run together; KEDA uses the largest replica
recommendation, not their sum. Direct RabbitMQ scaling does not require a metrics
exporter. For Prometheus, enable `rabbitmq.metrics.enabled` in patchworks-infra,
then configure scraping (optionally `rabbitmq.metrics.serviceMonitor.enabled`).
External brokers need their own exporter/scrape configuration. ServiceMonitor
resources require Prometheus Operator CRDs and matching collector selectors;
plain Prometheus can scrape without ServiceMonitors. KEDA queries the endpoint
directly and does not require a Prometheus Adapter.

PromQL must return a single value. Queries support literal `__QUEUE__`,
`__NAMESPACE__`, `__DEPLOYMENT__`, and `__PROCESSES__` placeholders. String
placeholders include JSON quoting: write `queue=__QUEUE__`, not quoted tokens.
Namespace means the worker namespace, which may differ from the broker namespace.
Scope broker metrics by vhost, job and cluster labels appropriate to your setup
so identically named queues in different brokers are not combined. Avoid summing
duplicate scrape series. `rabbitmq_queue_messages` already includes unacknowledged
messages; do not add them again. The examples divide total backlog by a per-pod
target (or worker concurrency), without dividing by current replicas again.

PHP workers do not expose HTTP metrics directly. Haberdashery-style
`core_queues_processes` queries additionally require Core's scheduler/Pushgateway
pipeline described in Metrics collection above. Monocore's existing ServiceMonitor
can supply application metrics for custom queries, but is not required when the
query uses only RabbitMQ metrics. Select thresholds appropriate to job duration,
concurrency, downstream capacity and broker prefetch; example thresholds are not
production sizing recommendations.

Scale-to-zero is opt-in. CPU/memory-only KEDA triggers cannot wake a zero-replica
worker. A Monocore hub with its scheduler enabled cannot scale/pause to zero;
disable that scheduler only when scheduling is handled elsewhere. Ensure queues
exist before scaling to zero, especially where workers normally assert topology.
Missing metrics are not automatically converted to zero: configure fallback
replicas if appropriate for the selected scaler.

PHP worker `workers.terminationGracePeriodSeconds` defaults to `21630`, matching
the six-hour supervisord job timeout plus a shutdown margin; companies may
override it. Monocore retains `workers.mono.terminationGracePeriodSeconds` and its
existing scheduler drain checks. Terminating replicas may linger while work drains.
`workers.mono.replicaCount` now defaults to `1` and can be overridden by company
`replicaCount` when autoscaling is disabled. Validate drain behaviour with your
actual application image and representative long-running jobs before rollout.

Run `ruby tests/autoscaling_test.rb` for offline rendering/validation coverage.
