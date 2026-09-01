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
a stable `patchworks-app-key` Secret with an `APP_KEY` value equivalent to:

```bash
echo "base64:$(openssl rand -base64 32)"
```

Migrations run as `pre-install,pre-upgrade` hooks. Seeders run as `pre-install`
hooks only. The first-install order is: Fabric migrations, Fabric seeders,
Fabric company seeder, Core migrations, Core seeders, then application startup.
The hook jobs render their required environment directly instead of depending
on app ConfigMaps/Secrets that Helm has not created yet.
Successful seed Jobs are kept in the cluster so GitOps syncs and installer
reruns do not create a fresh seed Job after the initial install.

When Fabric seeds are enabled and no `seeds.tenant.adminPassword` or
`seeds.tenant.existingSecret.name` is provided, the app chart creates a stable
`patchworks-tenant-admin` Secret before the Fabric seed job runs.

← [Back to repo README](../../README.md)

---

## Contents

- [Global](#global)
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
- [Ingress](#ingress)
- [MySQL](#mysql)
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

## Global

| Key | Default | Description |
|-----|---------|-------------|
| `revisionHistoryLimit` | `3` | Number of old ReplicaSets retained for chart-managed Deployments |

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
| `app.existingSecret.name` | `""` | Secret to source `APP_KEY` from instead of the inline value |
| `app.existingSecret.key` | `APP_KEY` | Key within the above secret |
| `app.env` | `production` | `APP_ENV` |
| `app.debug` | `"false"` | `APP_DEBUG` |
| `app.url` | `http://localhost` | `APP_URL` — set to your public-facing URL |
| `app.license.active` | `"true"` | `LICENSE_ACTIVE` for encoded application images |
| `app.license.key` | `""` | Inline `LICENSE_KEY`. Prefer `app.license.existingSecret.name` for production |
| `app.license.serverUrl` | `https://license.wearepatchworks.com` | License server URL rendered as `LICENSE_SERVER_URL` for Core and `LICENSE_URL` for Fabric |
| `app.license.existingSecret.name` | `""` | Existing Secret containing license values |
| `app.license.existingSecret.keyKey` | `LICENSE_KEY` | Key containing `LICENSE_KEY` in the existing Secret |
| `app.license.existingSecret.serverUrlKey` | `""` | Optional key containing the license server URL in the existing Secret |
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
| `fabric.session.driver` | `redis` | Fabric web `SESSION_DRIVER`; applied only to the Fabric PHP-FPM container |
| `fabric.session.lifetime` | `10080` | Fabric web `SESSION_LIFETIME` in minutes |
| `fabric.mysql.maxConnections` | `1000` | `max_connections` for dedicated bundled Fabric MySQL when `fabric.mysql.enabled=true` |

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
| `dashboard.broadcasting.host` | `""` | Browser `broadcasting.host`; defaults to the shared `pusher` host |
| `dashboard.broadcasting.port` | `""` | Browser `broadcasting.port`; defaults to the shared `pusher` port |
| `dashboard.broadcasting.scheme` | `""` | Browser `broadcasting.scheme`; defaults to the shared `pusher` scheme |
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

The generated Secret lives in the Helm release namespace (where Core runs). Fabric pods load it from that namespace. If you override `fabric.namespace` to place Fabric in a different namespace, copy the Secret there manually or use `passport.existingSecret` to point at a copy you manage.

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

---

## Web

The main Laravel web application. Always deployed.

| Key | Default | Description |
|-----|---------|-------------|
| `web.replicaCount` | `1` | Number of web replicas |
| `web.frankenphp.enabled` | unset | Override global FrankenPHP runtime for both Core web services |
| `web.service.type` | `ClusterIP` | Kubernetes service type |
| `web.service.port` | `80` | Service port |
| `web.sessionDomain` | `""` | `SESSION_DOMAIN`; controls the cookie domain for authentication |
| `web.resources` | requests memory `900Mi` | Resource requests and limits |
| `web.extraEnv` | `[]` | Additional env vars for web pods only |
| `web.extraEnvFrom` | `[]` | Additional env sources for web pods only |
| `web.podAnnotations` | `{}` | Pod annotations |
| `web.nodeSelector` | `{}` | Node selector |
| `web.tolerations` | `[]` | Tolerations |
| `web.affinity` | `{}` | Affinity rules |

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

`processors[]` defines the PHP Core background processor queues. These are
separate from `workers.type`: even when `workers.type=mono`, processor queues
are still handled by PHP Core worker Deployments because Monocore does not run
these jobs.

The default list creates `start`, `gateway`, `short-processor`,
`medium-processor`, `long-processor`, and `logging` workers. Scheduler CronJobs
are created for processors unless `processors[].scheduler.enabled=false`; the
default `logging` processor disables its scheduler because it only consumes
application broadcast/logging work. A
pre-install/pre-upgrade hook also asserts these RabbitMQ queues, plus the active
PHP worker hub queues: `workers.queue.name` for `workers.type=standalone`, or
each enabled `workers.microservices[*].domain` for
`workers.type=microservice`. Queue creation works with either the bundled broker
or a user-provided external RabbitMQ instance.

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
| `workers.type` | `standalone` | Worker deployment type |
| `workers.namespace` | `""` | Namespace for worker resources (defaults to release namespace) |
| `workers.frankenphp.enabled` | unset | Override FrankenPHP runtime for PHP workers |
| `workers.replicaCount` | `1` | Replica count (standalone only) |
| `workers.processes` | `15` | Worker concurrency — `numprocs` in supervisord.conf (standalone/microservice) |
| `workers.queue.connection` | `rabbitmq` | Laravel queue connection |
| `workers.queue.name` | `default` | Hub queue name for standalone workers |
| `workers.resources` | requests memory `900Mi` | Resource requests and limits |
| `workers.extraEnv` | `[]` | Additional env vars for all worker pods |
| `workers.extraEnvFrom` | `[]` | Additional env sources for all worker pods |

**`type: microservice` — per-service configuration**

Each key in `workers.microservices` (except `_default`) creates one Deployment. `_default` provides fallback values. Gateway, start, logging, and the short/medium/long processor queues are configured through `processors[]`, not `workers.microservices`.

| Field | Description |
|-------|-------------|
| `name` | `APP_NAME` env var |
| `domain` | `APP_DOMAIN` env var and the RabbitMQ queue name |
| `processes` | Worker concurrency — overrides `_default.processes` |
| `replicas` | Replica count — overrides `_default.replicas` |
| `enabled` | Set `false` to suppress the Deployment without removing the key |

**`type: mono` keys**

| Key | Default | Description |
|-----|---------|-------------|
| `workers.mono.image.repository` | `monocore` | Image repository |
| `workers.mono.queue` | `flows` | Hub queue consumed by Monocore and used for the generated `flows` topology |
| `workers.mono.processes` | `15` | Worker goroutine count |
| `workers.mono.store.existingSecret.name` | `""` | Existing Secret containing monocore's `store.yaml`; skips the generated store Secret hook |
| `workers.mono.store.existingSecret.key` | `store.yaml` | Secret key to mount as `/etc/monocore/store.yaml` |
| `workers.mono.rabbitmq.flowExchange` | `""` | Flow publish exchange. Empty defaults to `workers.mono.queue` when `companyFlows.enabled=false`, or `customer-flows` when enabled |
| `workers.mono.rabbitmq.companyFlows.enabled` | `false` | Create the configured flow exchange, company queues, bindings, and fallback policy |
| `workers.mono.otel.enabled` | `false` | Enable OpenTelemetry tracing |
| `workers.mono.otel.endpoint` | `""` | OTLP collector endpoint |
| `workers.mono.otel.serviceName` | `monocore` | Service name reported to the collector |

When `workers.type=mono`, a pre-install/pre-upgrade hook creates a
`<fullname>-workers-store` Secret containing monocore's `store.yaml`, unless
`workers.mono.store.existingSecret.name` is set. The generated file includes the
resolved S3 endpoint, bucket names, region, path-style setting, and access
credentials for both the `default` and `customer_cache` stores. Existing store
Secrets must exist in every namespace where monocore worker pods run.

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
| `monocore.url` | `""` | Override the Monocore API URL. Defaults to `http://<fullname>-workers.<workers namespace>.svc.cluster.local:8080` |
| `monocore.timeout` | `120` | Monocore request timeout in seconds |

**Multi-company workers**

`workers.companies[]` is supported by all three types. Each entry adds a Deployment consuming `company.queue` (or `company.name`) alongside the hub.

> **Note (standalone/microservice):** RabbitMQ queues for company workers must be created manually. The app-chart topology hook creates processor queues and hub standalone/microservice queues only.

---

## Migrations

Fabric migrations run before Core migrations as pre-install/pre-upgrade hooks.
Core migrations default to `php artisan migrate --force`.

| Key | Default | Description |
|-----|---------|-------------|
| `fabric.migrations.enabled` | `true` | Run Fabric migrations before Core migrations |
| `fabric.migrations.frankenphp.enabled` | unset | Override FrankenPHP runtime for Fabric migrations |
| `fabric.migrations.command` | `php artisan migrate --force` | Fabric migration command |
| `migrations.frankenphp.enabled` | unset | Override FrankenPHP runtime for Core migrations |
| `migrations.command` | `php artisan migrate --force` | Core migration command; include extra flags here |
| `migrations.restartPolicy` | `Never` | Job pod restart policy. `Never` preserves failed Pods for diagnostics |
| `migrations.backoffLimit` | `3` | Job retry limit |
| `migrations.resources` | `{}` | Resource requests and limits |

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
| `seeds.tenant.createDatabase` | `true` | Create the initial tenant database before Core tenant migrations run |
| `seeds.tenant.tier` | `Professional` | Initial tenant tier passed to `app:create-tenant` |
| `seeds.tenant.adminName` | `""` | Initial admin user name |
| `seeds.tenant.adminEmail` | `""` | Initial admin user email |
| `seeds.tenant.userRole` | `patchworks admin` | Initial admin user role passed as `--user-role` |
| `seeds.tenant.adminPassword` | `""` | Initial admin user password. Generated when empty and Fabric seeds are enabled |
| `seeds.tenant.existingSecret.name` | `""` | Secret name for the initial admin password |
| `seeds.tenant.existingSecret.passwordKey` | `adminPassword` | Secret key for the initial admin password |

---

## Ingress

| Key | Default | Description |
|-----|---------|-------------|
| `ingress.enabled` | `false` | Create public ingress resources |
| `ingress.scheme` | `http` | Public URL scheme used for dashboard/browser-facing generated URLs. Set to `https` when TLS is terminated outside this chart |
| `ingress.className` | `""` | `ingressClassName` for standard Ingress resources |
| `ingress.annotations` | `{}` | Annotations applied to every generated Ingress or HTTPProxy resource |
| `ingress.tls.<service>.secretName` | `""` | TLS Secret for `gateway`, `start`, `webhook`, `callback`, `fabric`, or `dashboard` |
| `ingress.hosts.gateway` | `""` | Hostname for the Core gateway service (`/`) |
| `ingress.hosts.start` | `""` | Hostname for the Core start service (`/`) |
| `ingress.hosts.webhook` | `""` | Hostname for webhook traffic; routes to the Core start service (`/`) |
| `ingress.hosts.callback` | `""` | Hostname for callback traffic; routes to the Core start service (`/`) |
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
| `mysql.auth.database` | `core` | Database created on first run |
| `mysql.auth.username` | `patchworks` | Application user |
| `mysql.auth.password` | `""` | Application user password. Generated by default when empty |
| `mysql.auth.existingSecret.name` | `""` | Secret name for in-cluster credentials |
| `mysql.auth.existingSecret.rootPasswordKey` | `root-password` | Key for the root password |
| `mysql.auth.existingSecret.passwordKey` | `password` | Key for the app user password |
| `mysql.persistence.size` | `10Gi` | PVC size |
| `mysql.persistence.storageClass` | `""` | Storage class (uses cluster default if empty) |
| `mysql.persistence.existingClaim` | `""` | Use a pre-existing PVC |

---

## Redis

| Key | Default | Description |
|-----|---------|-------------|
| `redis.enabled` | `true` | Deploy Valkey in-cluster. Set `false` to use an external instance |
| `redis.external.host` | `""` | External Redis hostname |
| `redis.external.port` | `6379` | External Redis port |
| `redis.external.password` | `""` | Password (or use `existingSecret`) |
| `redis.external.existingSecret.name` | `""` | Secret name for external password |
| `redis.external.existingSecret.passwordKey` | `password` | Key for the password |
| `redis.prefix` | `core` | Redis key prefix injected as `REDIS_PREFIX` for Core web and workers |
| `redis.persistence.size` | `1Gi` | PVC size |
| `redis.persistence.existingClaim` | `""` | Use a pre-existing PVC |

---

## RabbitMQ

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

When `s3.enabled` is `true`, a MinIO instance is deployed by the infra chart and its post-install/upgrade Job creates the configured buckets. Payload, tenant-cache, and file-download buckets default to the main bucket so self-hosted installs do not require a per-tenant bucket creation service.

| Key | Default | Description |
|-----|---------|-------------|
| `s3.enabled` | `true` | Deploy MinIO in-cluster. Set `false` to use external S3 |
| `s3.bucket` | `patchworks` | Bucket name |
| `s3.payloadsBucket` | `""` | Default bucket for Core payloads when tenant-specific buckets are not available; defaults to `s3.bucket` / `s3.external.bucket` |
| `s3.tenantCacheBucket` | `""` | Bucket for tenant cache payloads; defaults to `s3.companyCacheBucket`, then `s3.bucket` / `s3.external.bucket` |
| `s3.fileDownloadsBucket` | `""` | Bucket for file downloads; defaults to `s3.bucket` / `s3.external.bucket` |
| `s3.bucketCreationEndpoint` | `""` | Explicit bucket creation endpoint; defaults to `s3Manager.external.endpoint`, then the in-cluster S3 Manager service, then the resolved S3 endpoint |
| `s3.region` | `us-east-1` | Region |
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
`S3_BUCKET_CREATION_ENDPOINT` at it for Core web pods and non-mono workers.
It uses the same S3 endpoint and credentials as Core.

| Key | Default | Description |
|-----|---------|-------------|
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
continue using the in-cluster Soketi service.

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
| `pusher.ingress.tlsSecretName` | `""` | Optional TLS secret for the Soketi ingress. Also flips dashboard defaults to `https:443` |
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
| `kubefaas.auth.enabled` | `true` | Enable KubeFaaS basic auth env wiring |
| `kubefaas.auth.username` | `""` | Auth username shared by controller and builder; generated when omitted and eligible |
| `kubefaas.auth.password` | `""` | Auth password; generated when omitted and eligible |
| `kubefaas.auth.existingSecret.name` | `""` | Source auth credentials from this Secret instead of inline values |
| `kubefaas.auth.existingSecret.usernameKey` | `username` | Key for the username |
| `kubefaas.auth.existingSecret.passwordKey` | `password` | Key for the password |
| `kubefaas.registry.name` | `""` | Container registry for built function images |

---

## Existing secrets

Every credential has a companion `existingSecret` block with named key fields. When `name` is set the chart renders a `valueFrom.secretKeyRef` instead of an inline value. You can mix inline values and secret references freely.

```yaml
# kubectl create secret generic patchworks-secrets \
#   --from-literal=APP_KEY="base64:..." \
#   --from-literal=db-password="s3cr3t" \
#   --from-literal=minio-password="s3cr3t"

app:
  existingSecret:
    name: patchworks-secrets
    key: APP_KEY            # single-credential secrets use "key"

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
