Patchworks Self-Hosted
===

Self-hosted deployment of Patchworks Core via Helm.

## Documentation

| Document | Description |
|---|---|
| [`docs/chart-overview.svg`](docs/chart-overview.svg) | Every component the chart manages and how they connect |
| [`docs/workers-diagram.svg`](docs/workers-diagram.svg) | The three worker deployment modes in detail |
| [`AGENTS.md`](AGENTS.md) | Conventions for agents and contributors working on this repo |

Diagrams are D2 source + rendered SVG. Re-render with:
```bash
d2 docs/chart-overview.d2  docs/chart-overview.svg
d2 docs/workers-diagram.d2 docs/workers-diagram.svg
```

---

## Contents

- [Requirements](#requirements)
- [Quick start](#quick-start)
- [Configuration](#configuration)
  - [Images](#images)
  - [Application](#application)
  - [Web](#web)
  - [Workers](#workers)
  - [Migrations](#migrations)
  - [Ingress](#ingress)
  - [MySQL](#mysql)
  - [Redis](#redis)
  - [RabbitMQ](#rabbitmq)
  - [Elasticsearch](#elasticsearch)
  - [S3 / MinIO](#s3--minio)
  - [KubeFaaS](#kubefaas)
  - [Existing secrets](#existing-secrets)
- [Example configurations](#example-configurations)
  - [Minimal (all bundled infrastructure)](#minimal-all-bundled-infrastructure)
  - [Production (all external infrastructure)](#production-all-external-infrastructure)
  - [Monocore workers](#monocore-workers)
  - [Microservice workers](#microservice-workers)
  - [Secrets from Kubernetes Secrets](#secrets-from-kubernetes-secrets)

---

## Requirements

- Kubernetes 1.25+
- Helm 3.10+

## Quick start

```bash
helm install patchworks ./charts/patchworks \
  --set app.key="base64:$(openssl rand -base64 32)" \
  --set app.url="https://patchworks.example.com" \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=patchworks.example.com
```

This deploys Patchworks Core with bundled MySQL, Redis, RabbitMQ, Elasticsearch, and MinIO. All data is persisted via PersistentVolumeClaims.

---

## Configuration

### Images

All Patchworks application images share a global registry and tag. Individual services can override either.

| Key | Default | Description |
|-----|---------|-------------|
| `image.registry` | `ghcr.io/patchworks` | Registry prefix applied to all app images |
| `image.tag` | `latest` | Image tag applied to all app images |
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

### Application

Shared configuration injected into every application pod (web, workers, migrations).

| Key | Default | Description |
|-----|---------|-------------|
| `app.key` | `""` | Laravel `APP_KEY` — required. Generate with `php artisan key:generate --show` |
| `app.existingSecret.name` | `""` | Secret to source `APP_KEY` from instead of the inline value |
| `app.existingSecret.key` | `app-key` | Key within the above secret |
| `app.env` | `production` | `APP_ENV` |
| `app.debug` | `"false"` | `APP_DEBUG` |
| `app.url` | `http://localhost` | `APP_URL` — set to your public-facing URL |
| `app.extraEnv` | `[]` | Additional env vars injected into all app pods |
| `app.extraEnvFrom` | `[]` | Additional `secretRef`/`configMapRef` sources for all app pods |

---

### Web

The main Laravel web application. Always deployed.

| Key | Default | Description |
|-----|---------|-------------|
| `web.replicaCount` | `1` | Number of web replicas |
| `web.service.type` | `ClusterIP` | Kubernetes service type |
| `web.service.port` | `80` | Service port |
| `web.resources` | `{}` | Resource requests and limits |
| `web.extraEnv` | `[]` | Additional env vars for web pods only |
| `web.extraEnvFrom` | `[]` | Additional env sources for web pods only |
| `web.podAnnotations` | `{}` | Pod annotations |
| `web.nodeSelector` | `{}` | Node selector |
| `web.tolerations` | `[]` | Tolerations |
| `web.affinity` | `{}` | Affinity rules |

---

### Workers

The `workers.type` field selects the deployment model. See [`docs/workers-diagram.svg`](docs/workers-diagram.svg) for a visual overview.

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
| `workers.replicaCount` | `1` | Replica count (standalone only) |
| `workers.processes` | `15` | Worker concurrency — `numprocs` in supervisord.conf (standalone/microservice) |
| `workers.queue.connection` | `rabbitmq` | Laravel queue connection |
| `workers.queue.name` | `default` | Hub queue name (standalone/mono) |
| `workers.resources` | `{}` | Resource requests and limits |
| `workers.extraEnv` | `[]` | Additional env vars for all worker pods |
| `workers.extraEnvFrom` | `[]` | Additional env sources for all worker pods |

**`type: microservice` — per-service configuration**

Each key in `workers.microservices` (except `_default`) creates one Deployment. `_default` provides fallback values.

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
| `workers.mono.processes` | `15` | Worker goroutine count |
| `workers.mono.otel.enabled` | `false` | Enable OpenTelemetry tracing |
| `workers.mono.otel.endpoint` | `""` | OTLP collector endpoint |
| `workers.mono.otel.serviceName` | `monocore` | Service name reported to the collector |

**Multi-company workers**

`workers.companies[]` is supported by all three types. Each entry adds a Deployment consuming `company.queue` (or `company.name`) alongside the hub.

> **Note (standalone/microservice):** RabbitMQ queues for company workers must be created manually — topology automation only applies to `type: mono` via the `topology.yaml` startup assertion.

---

### Migrations

Runs `php artisan migrate --force` as a pre-install/pre-upgrade hook.

| Key | Default | Description |
|-----|---------|-------------|
| `migrations.backoffLimit` | `3` | Job retry limit |
| `migrations.resources` | `{}` | Resource requests and limits |

---

### Ingress

| Key | Default | Description |
|-----|---------|-------------|
| `ingress.enabled` | `false` | Create an Ingress for Core Web |
| `ingress.className` | `""` | `ingressClassName` |
| `ingress.annotations` | `{}` | Ingress annotations |
| `ingress.hosts` | `[]` | Host and path rules |
| `ingress.tls` | `[]` | TLS configuration |

---

### MySQL

| Key | Default | Description |
|-----|---------|-------------|
| `mysql.enabled` | `true` | Deploy MySQL in-cluster. Set `false` to use an external instance |
| `mysql.external.host` | `""` | External MySQL hostname |
| `mysql.external.port` | `3306` | External MySQL port |
| `mysql.external.database` | `patchworks` | Database name |
| `mysql.external.username` | `patchworks` | Username |
| `mysql.external.password` | `""` | Password (or use `existingSecret`) |
| `mysql.external.existingSecret.name` | `""` | Secret name for external credentials |
| `mysql.external.existingSecret.passwordKey` | `password` | Key for the password |
| `mysql.auth.rootPassword` | `root` | Root password for bundled MySQL |
| `mysql.auth.database` | `patchworks` | Database created on first run |
| `mysql.auth.username` | `patchworks` | Application user |
| `mysql.auth.password` | `patchworks` | Application user password |
| `mysql.auth.existingSecret.name` | `""` | Secret name for in-cluster credentials |
| `mysql.auth.existingSecret.rootPasswordKey` | `root-password` | Key for the root password |
| `mysql.auth.existingSecret.passwordKey` | `password` | Key for the app user password |
| `mysql.persistence.size` | `10Gi` | PVC size |
| `mysql.persistence.storageClass` | `""` | Storage class (uses cluster default if empty) |
| `mysql.persistence.existingClaim` | `""` | Use a pre-existing PVC |

---

### Redis

| Key | Default | Description |
|-----|---------|-------------|
| `redis.enabled` | `true` | Deploy Valkey in-cluster. Set `false` to use an external instance |
| `redis.external.host` | `""` | External Redis hostname |
| `redis.external.port` | `6379` | External Redis port |
| `redis.external.password` | `""` | Password (or use `existingSecret`) |
| `redis.external.existingSecret.name` | `""` | Secret name for external password |
| `redis.external.existingSecret.passwordKey` | `password` | Key for the password |
| `redis.persistence.size` | `1Gi` | PVC size |
| `redis.persistence.existingClaim` | `""` | Use a pre-existing PVC |

---

### RabbitMQ

| Key | Default | Description |
|-----|---------|-------------|
| `rabbitmq.enabled` | `true` | Deploy RabbitMQ in-cluster. Set `false` to use an external instance |
| `rabbitmq.external.host` | `""` | External RabbitMQ hostname |
| `rabbitmq.external.port` | `5672` | External RabbitMQ AMQP port |
| `rabbitmq.external.username` | `patchworks` | Username |
| `rabbitmq.external.vhost` | `/` | Virtual host |
| `rabbitmq.external.password` | `""` | Password (or use `existingSecret`) |
| `rabbitmq.external.existingSecret.name` | `""` | Secret name for external credentials |
| `rabbitmq.external.existingSecret.passwordKey` | `password` | Key for the password |
| `rabbitmq.auth.username` | `patchworks` | Username for bundled RabbitMQ |
| `rabbitmq.auth.password` | `patchworks` | Password for bundled RabbitMQ |
| `rabbitmq.auth.vhost` | `/` | Virtual host |
| `rabbitmq.auth.existingSecret.name` | `""` | Secret name for in-cluster credentials |
| `rabbitmq.auth.existingSecret.passwordKey` | `password` | Key for the password |
| `rabbitmq.persistence.size` | `5Gi` | PVC size |
| `rabbitmq.persistence.existingClaim` | `""` | Use a pre-existing PVC |

---

### Elasticsearch

| Key | Default | Description |
|-----|---------|-------------|
| `elasticsearch.enabled` | `true` | Deploy Elasticsearch in-cluster. Set `false` to use an external cluster |
| `elasticsearch.external.host` | `""` | External hostname |
| `elasticsearch.external.port` | `9200` | External port |
| `elasticsearch.external.scheme` | `http` | `http` or `https` |
| `elasticsearch.external.username` | `""` | Username (if auth enabled) |
| `elasticsearch.external.password` | `""` | Password (or use `existingSecret`) |
| `elasticsearch.external.existingSecret.name` | `""` | Secret name for credentials |
| `elasticsearch.external.existingSecret.passwordKey` | `password` | Key for the password |
| `elasticsearch.persistence.size` | `15Gi` | PVC size |
| `elasticsearch.persistence.existingClaim` | `""` | Use a pre-existing PVC |
| `elasticsearch.javaOpts` | `-Xms512m -Xmx512m` | JVM heap settings |

---

### S3 / MinIO

When `s3.enabled` is `true`, a MinIO instance is deployed and a post-install/upgrade Job creates the configured bucket.

| Key | Default | Description |
|-----|---------|-------------|
| `s3.enabled` | `true` | Deploy MinIO in-cluster. Set `false` to use external S3 |
| `s3.bucket` | `patchworks` | Bucket name |
| `s3.region` | `us-east-1` | Region |
| `s3.auth.rootUser` | `minioadmin` | MinIO root user |
| `s3.auth.rootPassword` | `minioadmin` | MinIO root password |
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
| `s3.persistence.size` | `10Gi` | PVC size |
| `s3.persistence.existingClaim` | `""` | Use a pre-existing PVC |

---

### KubeFaaS

Optional serverless function execution layer. Disabled by default.

| Key | Default | Description |
|-----|---------|-------------|
| `kubefaas.enabled` | `false` | Deploy KubeFaaS in-cluster |
| `kubefaas.namespace` | `kubefaas` | Namespace for KubeFaaS components (separate from app namespace) |
| `kubefaas.functions.namespaceCount` | `5` | Number of function execution namespaces created |
| `kubefaas.builder.tls.mode` | `helm` | TLS mode: `helm`, `certManager`, or `existingSecret` |
| `kubefaas.auth.username` | `""` | Auth username shared by controller and builder |
| `kubefaas.auth.password` | `""` | Auth password |
| `kubefaas.auth.existingSecret.name` | `""` | Secret name for auth credentials |
| `kubefaas.registry.name` | `""` | Container registry for built function images |

---

### Existing secrets

Every credential has a companion `existingSecret` block with named key fields. When `name` is set, the chart renders a `valueFrom.secretKeyRef` instead of an inline value. You can mix inline values and secret references freely.

```yaml
# kubectl create secret generic patchworks-secrets \
#   --from-literal=app-key="base64:..." \
#   --from-literal=db-password="s3cr3t" \
#   --from-literal=minio-password="s3cr3t"

app:
  existingSecret:
    name: patchworks-secrets
    key: app-key            # field name is "key" for the app secret (single-credential)

mysql:
  auth:
    existingSecret:
      name: patchworks-secrets
      passwordKey: db-password   # field names end in "Key"

s3:
  auth:
    existingSecret:
      name: patchworks-secrets
      rootPasswordKey: minio-password
```

---

## Example configurations

### Minimal (all bundled infrastructure)

Suitable for evaluation or single-node deployments.

```yaml
# values-minimal.yaml
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

```bash
helm install patchworks ./charts/patchworks -f values-minimal.yaml
```

---

### Production (all external infrastructure)

No stateful workloads in the cluster.

```yaml
# values-production.yaml
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

---

### Monocore workers

Use the Go-based Monocore worker instead of PHP queue workers.

```yaml
workers:
  type: mono
  mono:
    processes: 20
    otel:
      enabled: true
      endpoint: "http://otel-collector:4317"
```

---

### Microservice workers

One PHP worker Deployment per service, each consuming its own RabbitMQ queue.

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
      processes: 20    # override for high-throughput service
```

---

### Secrets from Kubernetes Secrets

Store all credentials in a pre-existing Secret.

```bash
kubectl create secret generic patchworks-secrets \
  --from-literal=app-key="base64:$(openssl rand -base64 32)" \
  --from-literal=db-password="$(openssl rand -base64 24)" \
  --from-literal=minio-root-password="$(openssl rand -base64 24)"
```

```yaml
app:
  existingSecret:
    name: patchworks-secrets
    key: app-key

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
