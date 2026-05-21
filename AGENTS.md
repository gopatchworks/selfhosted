# Patchworks Self-Hosted — Agent Reference

Conventions and patterns for this repository. Read this before making changes.

Chart source lives in `charts/patchworks/`. All paths below are relative to the repository root.

---

## Diagrams

Two D2 diagrams live in `docs/`. Re-render both after any change that affects them:

```bash
d2 docs/chart-overview.d2   docs/chart-overview.svg
d2 docs/workers-diagram.d2  docs/workers-diagram.svg
```

### `docs/chart-overview.d2` — full system overview

Shows every component the chart can manage, which are optional, and how the application connects to each. **Update whenever:**

| Change | What to update |
|---|---|
| New infrastructure component added (new template file) | Add a node in the Infrastructure or Optional Integrations container |
| Component removed or renamed | Remove/rename the corresponding node and any edges to it |
| New connection between app and infrastructure | Add an edge with the env var prefix as the label |
| Chart dependency added (`Chart.yaml`) | Add to the Optional Integrations container |

### `docs/workers-diagram.d2` — worker modes

Shows the three worker types (standalone, microservice, mono) and the Kubernetes resources each creates. **Update whenever:**

| Change | What to update |
|---|---|
| New `workers.type` value | Add a new top-level container |
| New ConfigMap mounted by a worker Deployment | Add node + edge inside that mode's container |
| Renamed Kubernetes resource | Update the label on the corresponding node |
| New per-company behaviour in any mode | Update the company sub-container for that mode |
| `workers.microservices` field semantics changed | Update the service container description |

---

## File organisation

One logical component per template file. KubeFaaS is the canonical example:

| File | What it contains |
|---|---|
| `kubefaas-controller.yaml` | ServiceAccount, ClusterRole/Binding, ConfigMap, Deployment, Service |
| `kubefaas-builder.yaml` | Registry Secret, TLS resources, ServiceAccount, ConfigMap, Deployment, Service |
| `kubefaas-redis.yaml` | PVC, Deployment, Service — gated on `kubefaas.redis.enabled` |
| `kubefaas-functions.yaml` | Profile ConfigMap, function ServiceAccounts, Namespaces, optional NetworkPolicies |

Don't bundle unrelated resources into one file just because they share a feature flag.

---

## Namespace resolution

Three-level cascade via `patchworks.componentNamespace`:

```
component.namespace → .Values.namespace → .Release.Namespace
```

Every component has a dedicated helper (`patchworks.web.namespace`, `patchworks.mysql.namespace`, etc.) that calls the cascade.

**Exception:** KubeFaaS lives in its own dedicated namespace (`patchworks.kubefaas.namespace` defaults to `"kubefaas"`, not the release namespace). Use this pattern for any component that must be cluster-scoped or lives outside the app namespace.

---

## Infrastructure component pattern

Every infrastructure dependency (MySQL, Redis, RabbitMQ, Elasticsearch, S3, KubeFaaS Redis) follows the same shape in `values.yaml`:

```yaml
component:
  namespace: ""
  enabled: true          # deploy in-cluster
  external:              # used when enabled: false
    host: ""
    port: 1234
    # ... auth fields
  image: { repository, tag, pullPolicy }
  auth: { ... }          # in-cluster credentials
  persistence: { size, storageClass }
  resources: {}
```

Helpers select between in-cluster and external automatically:

```
patchworks.mysql.host     → fullname-mysql.<ns>.svc.cluster.local  OR  external.host
patchworks.mysql.password → auth.password                           OR  external.password
```

Always add a `patchworks.<component>.namespace` helper using `patchworks.componentNamespace`.

---

## existingSecret pattern — one secret, multiple named keys

Every component that has credentials uses a single `existingSecret` block with per-credential key names. **Never** use separate per-credential secret structs (e.g. `passwordSecret`, `rootPasswordSecret`, `accessKeySecret`).

```yaml
# Correct — one secret, multiple key names
auth:
  username: patchworks
  password: patchworks
  existingSecret:
    name: ""
    passwordKey: password

# Correct — multi-credential case (mysql auth)
auth:
  rootPassword: root
  password: patchworks
  existingSecret:
    name: ""
    rootPasswordKey: root-password
    passwordKey: password

# Wrong — do not add per-credential secrets
auth:
  rootPasswordSecret:    # ← never do this
    name: ""
    key: ""
  passwordSecret:        # ← never do this
    name: ""
    key: ""
```

The convention is:
- The container is always `existingSecret` (never `passwordSecret`, `rootUserSecret`, etc.)
- Key field names are descriptive and end in `Key` (e.g. `passwordKey`, `rootPasswordKey`, `accessKeyKey`)
- Default values in `values.yaml` should reflect realistic key names (e.g. `passwordKey: password`, `rootPasswordKey: root-password`)

In templates, construct the `{ name, key }` dict that `patchworks.secretEnv` expects:

```yaml
{{- $es := .Values.mysql.auth.existingSecret }}
{{- include "patchworks.secretEnv" (dict "name" "MYSQL_ROOT_PASSWORD" "value" .Values.mysql.auth.rootPassword "secret" (dict "name" $es.name "key" $es.rootPasswordKey)) | nindent 12 }}
{{- include "patchworks.secretEnv" (dict "name" "MYSQL_PASSWORD" "value" .Values.mysql.auth.password "secret" (dict "name" $es.name "key" $es.passwordKey)) | nindent 12 }}
```

The `patchworks.secretEnv` helper itself takes `secret: { name, key }` and emits a plain `value:` when name/key are empty, or `valueFrom.secretKeyRef` when set.

For cases where the right `existingSecret` depends on `enabled` vs `external` (e.g. rabbitmq), select the struct before constructing the key dict:

```yaml
{{- $es := ternary .Values.rabbitmq.auth.existingSecret .Values.rabbitmq.external.existingSecret .Values.rabbitmq.enabled -}}
{{- $s := dict "name" $es.name "key" $es.passwordKey -}}
{{- include "patchworks.secretEnv" (dict "name" "RABBITMQ_PASSWORD" ... "secret" $s) -}}
```

---

## Passwords in connection strings — `$(VAR)` substitution

When a connection string (DSN, AMQP URL) embeds a password, **never** put the password inline as plaintext. Use Kubernetes env var substitution:

1. Emit the password as its own env var first (`DB_PASSWORD`, `RABBITMQ_PASSWORD`).
2. Reference it with `$(VAR_NAME)` in the URL value.

```yaml
- name: RABBITMQ_PASSWORD
  value: "secret"            # or secretKeyRef
- name: RABBITMQ_URL
  value: "amqp://user:$(RABBITMQ_PASSWORD)@host:5672/vhost"
```

The template helper `patchworks.env.rabbitmqUrl` relies on `patchworks.env.rabbitmqPassword` being emitted in the same `env:` block first.

---

## Image resolution

Use `patchworks.appImage` / `patchworks.appPullPolicy` for all Patchworks application images (web, workers, migrations, kubefaas services). These helpers resolve `registry`, `tag`, and `pullPolicy` with a global → component override cascade.

Infrastructure images (MySQL, Redis, RabbitMQ, etc.) use their own fully-qualified `image.repository:image.tag` blocks directly — they don't use these helpers.

---

## `patchworks.appEnv` — shared app env

`patchworks.appEnv` is injected into **every** app pod (PHP Core web, migrations, standalone workers). It contains:

- `APP_KEY`, `APP_ENV`, `APP_DEBUG`, `APP_URL`, `APP_NAME`
- `DB_HOST/PORT/DATABASE/USERNAME/PASSWORD`
- `REDIS_HOST/PORT/PASSWORD`
- `RABBITMQ_PASSWORD`, `RABBITMQ_URL`
- `AWS_*` (S3)
- `ELASTICSEARCH_HOST/PORT`
- Pusher vars (when configured)
- KubeFaaS vars (when configured)
- `extraEnv` / `extraEnvFrom` pass-through

If a new service needs its credentials in app pods, add helpers here rather than duplicating in each template.

---

## Worker types

`workers.type` controls what gets deployed:

| Value | What it is |
|---|---|
| `standalone` | PHP worker via supervisord — one Deployment (+ one per company) |
| `mono` | Monocore Go-based worker Deployment |
| `microservice` | PHP worker via supervisord — one Deployment per microservice key (+ one per company per microservice) |

### supervisord.conf pattern (standalone and microservice)

Both types generate a **ConfigMap** per Deployment containing a `supervisord.conf`, then run `supervisord -c /etc/supervisor/conf.d/supervisord.conf` as the container command. The `processes` field maps to `numprocs` in the conf. The artisan command embedded in each conf is:

```
/usr/local/bin/php /var/www/html/artisan queue:work <connection> --queue=<queue> \
  --backoff=0 --max-jobs=0 --memory=256 --sleep=3 --timeout=21600 --tries=1 --rest=0
```

ConfigMap naming: `{fullname}-{slug}-supervisord` (same slug as the Deployment).

**Do not use `HORIZON_MAX_PROCESSES`** — it is deprecated. Use `processes` which controls `numprocs` in the supervisord.conf.

### type: microservice

Each key in `workers.microservices` (except `_default`) produces a Deployment named `{fullname}-workers-{key}`. The `_default` block provides fallback values for every per-service field.

Required per-service fields:
- `name` → `APP_NAME` env var
- `domain` → `APP_DOMAIN` env var and the hub queue name (`--queue=<domain>`)

Optional per-service overrides (fall back to `_default` then `workers.*`):
`enabled`, `replicas`, `processes`, `namespace`, `resources`, `extraEnv`, `extraEnvFrom`, `podAnnotations`, `nodeSelector`, `tolerations`, `affinity`

`processes` maps to `HORIZON_MAX_PROCESSES` (worker concurrency within the container).

The full list of microservices is derived from the haberdashery `apps/core/overlays/dev2-aws` directories. When adding a new microservice, add a key to `workers.microservices` with at minimum `name` and `domain`. Setting `enabled: false` suppresses the Deployment entirely without removing the key.

### Company deployments (all types)

`workers.companies[]` is shared across all three worker types. Each entry adds one extra Deployment per service (for `microservice`) or one extra Deployment overall (for `standalone`/`mono`).

- Hub queue: the service's `domain` (microservice) or `workers.queue.name` (standalone/mono)
- Company queue: `company.queue | default company.name`

Company deployments set the same `APP_NAME`/`APP_DOMAIN` as their hub (for `microservice`). The queue name is the company's queue — all microservice types for a given company share one queue.

**RabbitMQ note for `standalone` and `microservice`:** The chart cannot assert RabbitMQ topology. Queues and bindings for company workers must be created manually. Only `type: mono` automates this via monocore's `topology.yaml` startup assertion.

### Monocore workers

Monocore workers get their config from a generated `config.yaml` ConfigMap (via `patchworks.mono.configYaml`) and a generated `topology.yaml` (via `patchworks.mono.topologyYaml`). Company deployments bind to the company-flows exchange and require `workers.mono.rabbitmq.companyFlows.enabled: true` on the hub.

---

## KubeFaaS

### Component split

- **Controller**: manages function lifecycle; needs ClusterRole (pods, deployments, services, namespaces, HPAs).
- **Builder**: builds function images; runs a Docker-in-Docker sidecar with mutual TLS.
- **Redis**: shared state between controller and builder; separate from app Redis.
- **Functions**: one Namespace + ServiceAccount per execution slot, optional NetworkPolicy.

### Namespace count

The chart creates `kubefaas.functions.namespaceCount` namespaces named `<namespacePrefix>-01`, `<namespacePrefix>-02`, etc. The controller is told the count via `FUNCTION_NAMESPACE_MAX`.

### TLS modes for the builder's dind sidecar

Set `kubefaas.builder.tls.mode`:

| Mode | Behaviour |
|---|---|
| `helm` | Helm generates self-signed certs. CA Secret is preserved across upgrades via `lookup` + `helm.sh/resource-policy: keep`. Server/client certs are re-signed on every render using the stored CA. |
| `certManager` | cert-manager issues certs. Leave `certManager.issuerName` empty to auto-create a self-signed CA chain (Issuer → Certificate → CA Issuer). Set `issuerName` to use an existing Issuer/ClusterIssuer. |
| `existingSecret` | BYO Secrets. Set `existingSecret.serverTls` and `existingSecret.clientTls` to names of pre-created Secrets with `ca.crt`, `tls.crt`, `tls.key`. |

The `setup-docker-certs` initContainer renames keys from `ca.crt/tls.crt/tls.key` → `ca.pem/cert.pem/key.pem` so dind and the builder binary can consume them directly.

### Auth and registry

The auth secret (`kubefaas.auth.*`) is shared between controller, builder, and monocore workers. The registry secret holds the raw `registries.yaml` content consumed by the builder.

Both support `existingSecret` for bringing pre-created Secrets.

---

## RabbitMQ

Follows the standard infrastructure pattern. Key details:

- Image: `rabbitmq:3-management-alpine` (exposes AMQP on 5672, management UI on 15672).
- `RABBITMQ_DEFAULT_VHOST` is only set when the configured vhost is not `/` — RabbitMQ's default is already `/`.
- Readiness/liveness probes use `rabbitmq-diagnostics -q ping` (not TCP socket) because the broker takes time to initialise its vhost even after the port opens.
- `workers.mono` and app pods wait for RabbitMQ via a `busybox` initContainer running `nc -zv`.

---

## Reference implementations

Look at **Haberdashery** (internal) for:
- cert-manager CA chain patterns
- Advanced monocore topology examples
- ClusterRole patterns for controller-type services

---

## Persistent volumes — `patchworks.pvcSpec`

Every component with a PVC exposes a `persistence` block with three levels of control:

```yaml
persistence:
  existingClaim: ""        # BYO — use a pre-existing PVC; chart creates nothing
  accessModes: [ReadWriteOnce]
  size: 10Gi
  storageClass: ""
  spec: {}                 # full PVC spec override; replaces all other fields when non-empty
```

The `patchworks.pvcSpec` helper in `_helpers.tpl` renders the PVC `spec:` body:
- If `spec` is non-empty → emits it verbatim via `toYaml`.
- Otherwise → builds from `accessModes`, `storageClass`, and `size`.

Usage in a template:

```yaml
{{- if not $p.existingClaim }}
---
apiVersion: v1
kind: PersistentVolumeClaim
...
spec:
  {{- include "patchworks.pvcSpec" $p | nindent 2 }}
{{- end }}
...
volumes:
  - name: data
    persistentVolumeClaim:
      claimName: {{ $p.existingClaim | default (printf "%s-<component>" $fullname) }}
```

**This pattern is mandatory for every component that creates a PVC.** Never hardcode `accessModes`, `storageClass`, or `storage` directly in a template — route everything through the helper. Never add a new PVC-bearing component without all five persistence fields in `values.yaml`.

---

## What NOT to do

- Don't add `workers.elasticsearch` or any component-specific Elastic config — both PHP Core and monocore use the shared `elasticsearch.*` config.
- Don't fall back to the release namespace for KubeFaaS resources — it has its own dedicated namespace.
- Don't use `HORIZON_MAX_PROCESSES` — it is deprecated. Worker concurrency is controlled via `processes` → `numprocs` in the supervisord.conf ConfigMap.
- Don't put sensitive values (passwords, tokens) inline in connection string env vars — use `$(VAR)` substitution.
- Don't bundle multiple unrelated components into a single template file.
- Don't add NetworkPolicies as mandatory — they should be opt-in (`enabled: false` by default).
