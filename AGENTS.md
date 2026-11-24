# Patchworks Self-Hosted — Agent Reference

Conventions and patterns for this repository. Read this before making changes.

Chart source lives in `charts/patchworks/`. All paths below are relative to the repository root.

---

## Diagrams

Two D2 diagrams live in `docs/`. Re-render both after any change that affects them:

```bash
make -C docs all       # rebuild everything
make -C docs overview  # chart-overview.svg only
make -C docs workers   # workers-diagram.svg only
```

### Root `README.md` — getting started guide + diagrams

The root README contains the getting started guide, both embedded diagrams, and the link to the chart configuration reference. **Update whenever:**

| Change | What to update |
|---|---|
| New prerequisite tool required | Add to the Prerequisites table |
| Helm values API changes affecting the install command | Update `--set` flags in the Getting Started section |
| New mandatory init step (e.g. new chart dependency) | Add a numbered step |
| `docs/kind/values.yaml` changes | Update the file and any prose references in README |
| Startup order changes (new wait dependency) | Update the "Watch the rollout" expected pod list |

`docs/kind/cluster.yaml` and `docs/kind/values.yaml` are companion files — keep them in sync with the README prose.

### `charts/patchworks/README.md` — configuration reference

All `values.yaml` keys, their defaults, and example configurations. **Update whenever:**

| Change | What to update |
|---|---|
| New top-level values key added | Add a row to the relevant table |
| Key renamed or removed | Update or remove the row |
| New infrastructure component added | Add a new section |
| `existingSecret` field names change | Update the Existing secrets section |
| New example configuration worth documenting | Add under Example configurations |

---

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
| `kubefaas-registry.yaml` | Bundled image registry: login and `registries.yaml` Secrets, pull Secrets, PVC, Deployment, NodePort Service — gated on `kubefaas.registry.bundled.enabled` |

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

When a connection string (DSN, URL) embeds a password, **never** put the password inline as plaintext. Use Kubernetes env var substitution:

1. Emit the password as its own env var first (`DB_PASSWORD`, etc.).
2. Reference it with `$(VAR_NAME)` in the URL value.

```yaml
- name: DB_PASSWORD
  value: "secret"            # or secretKeyRef
- name: DB_DSN
  value: "user:$(DB_PASSWORD)@tcp(mysql:3306)/database"
```

RabbitMQ is intentionally configured with component env vars (`RABBITMQ_HOST`, `RABBITMQ_PORT`, `RABBITMQ_USER`, `RABBITMQ_PASSWORD`, `RABBITMQ_VHOST`) rather than a full DSN so vhosts and credentials do not need URL encoding in the chart.

---

## Image resolution

Use `patchworks.appImage` / `patchworks.appPullPolicy` for all Patchworks application images (web, workers, migrations, kubefaas services). These helpers resolve `registry`, `tag`, and `pullPolicy` with a global → component override cascade.

Infrastructure images (MySQL, Redis, RabbitMQ, etc.) use their own fully-qualified `image.repository:image.tag` blocks directly — they don't use these helpers.

## Runtime resolution

Use `patchworks.usesFrankenphp`, `patchworks.artisanShellCommand`, and `patchworks.phpCliCommandArgs` for PHP runtime decisions. Do not hard-code new Artisan template commands without passing them through the runtime helpers.

`runtime.frankenphp.enabled` defaults to `false`. Service-level `frankenphp.enabled` values override it. When enabled, web pods run `frankenphp php-server` and Artisan commands render as `/usr/local/bin/frankenphp php-cli artisan ...`. When disabled, the helpers normalize back to the standard PHP command shape.

---

## `patchworks.appEnv` — shared app env

`patchworks.appEnv` is injected into **every** app pod (PHP Core web, migrations, standalone workers). It contains:

- `APP_KEY`, `APP_ENV`, `APP_DEBUG`, `APP_URL`, `APP_NAME`
- `DB_HOST/PORT/DATABASE/USERNAME/PASSWORD`
- `REDIS_HOST/PORT/PASSWORD`
- `RABBITMQ_HOST/PORT/USER/PASSWORD/VHOST`
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

Both types generate a **ConfigMap** per Deployment containing a `supervisord.conf`, then run `supervisord -c /etc/supervisor/conf.d/supervisord.conf` as the container command. The `processes` field maps to `numprocs` in the conf. The standard PHP artisan command embedded in each conf is:

```
/usr/local/bin/php /var/www/html/artisan queue:work <connection> --queue=<queue> \
  --backoff=0 --max-jobs=0 --memory=256 --sleep=3 --timeout=21600 --tries=1 --rest=0
```

Because standard PHP is the default runtime, the rendered default uses the standard PHP command above. When a service opts into FrankenPHP, the helper renders `/usr/local/bin/frankenphp php-cli artisan`.

ConfigMap naming: `{fullname}-{slug}-supervisord` (same slug as the Deployment).

**Do not use `HORIZON_MAX_PROCESSES`** — it is deprecated. Use `processes` which controls `numprocs` in the supervisord.conf.

### type: microservice

Each key in `workers.microservices` (except `_default`) produces a Deployment named `{fullname}-workers-{key}`. The `_default` block provides fallback values for every per-service field.

Required per-service fields:
- `name` → `APP_NAME` env var
- `domain` → `APP_DOMAIN` env var and the default hub queue name

Optional per-service overrides (fall back to `_default` then `workers.*`):
`queue`, `enabled`, `replicas`, `processes`, `namespace`, `resources`, `extraEnv`, `extraEnvFrom`, `podAnnotations`, `nodeSelector`, `tolerations`, `affinity`. `queue` overrides the hub queue (`--queue=<queue>`) while leaving `APP_DOMAIN` unchanged.

`processes` maps to `HORIZON_MAX_PROCESSES` (worker concurrency within the container).

The full list of microservices is derived from the haberdashery `apps/core/overlays/dev2-aws` directories. When adding a new microservice, add a key to `workers.microservices` with at minimum `name` and `domain`. Setting `enabled: false` suppresses the Deployment entirely without removing the key.

### Company deployments (all types)

`workers.companies[]` is shared across all three worker types. Each entry adds one extra Deployment per service (for `microservice`) or one extra Deployment overall (for `standalone`/`mono`).

- Hub queue: the service's `queue | default domain` (microservice), `workers.queue.name` (standalone), or `workers.mono.queue` (mono)
- Company queue: `company.queue | default company.name`

Company deployments set the same `APP_NAME`/`APP_DOMAIN` as their hub (for `microservice`). The queue name is the company's queue — all microservice types for a given company share one queue.

**RabbitMQ note for `standalone` and `microservice`:** The chart cannot assert RabbitMQ topology. Queues and bindings for company workers must be created manually. Only `type: mono` automates this via monocore's `topology.yaml` startup assertion.

### Monocore workers

Monocore workers get their config from a generated `config.yaml` ConfigMap (via `patchworks.mono.configYaml`) and a generated `topology.yaml` (via `patchworks.mono.topologyYaml`). The config sets `rabbitmq.queue` and `rabbitmq.flow.exchange`, and the Monocore process also receives the resolved flow exchange via `--rabbitmq-flow-exchange`. If `workers.mono.rabbitmq.flowExchange` is empty, it resolves to `workers.mono.queue` when `companyFlows.enabled` is false, or `customer-flows` when enabled. Company deployments bind to that exchange only when `workers.mono.rabbitmq.companyFlows.enabled` is true.

The Monocore image is licensed. Every Monocore worker, hub and company, gets `LICENSE_KEY` and `LICENSE_SERVER_URL` from `app.license` through `patchworks.mono.licenseEnv`, not `patchworks.licenseEnv`, which adds Core's `LICENSE_ACTIVE` and Fabric's `LICENSE_URL`. Their readiness probe is `/readyz`, which waits for the licence server's first answer; liveness stays on `/healthz`.

Monocore's optional shapes (Merge, Data Transformation, Cross-Reference, and waiting Run Flow with Flow Return) have no step runner unless enabled, and a run that reaches one fails. `workers.mono.enable.*` mirrors Monocore's own chart (`monocore.worker.enable.*` in `helm/monocore/values.yaml`, the authoritative contract), except `dataProcessing`, and renders `WORKER_ENABLE_*` as `"true"`/`"false"` on the hub and every company, through `patchworks.mono.enableEnv`. A shape left `null` follows `workers.mono.allCompanies.enabled`; `catchAtRunEnd` defaults to `false` and never follows it. A `WORKER_ENABLE_*` name in the worker's `extraEnv` (`workers.extraEnv`, `workers.mono.extraEnv`, the company's) skips the chart's entry rather than duplicating it. `enable` is unset from the config checksum, like the other env-only keys. Core has every one of these shapes, but the Dashboard pinned for `v0.2.17` has no Data Transformation node: its canvas can't add that shape and shows an existing step as broken. `dataTransform` still follows `allCompanies`, because the runner is what lets such a step run; turning it off doesn't fix the canvas. `dataProcessing` is left out on purpose, so Monocore's Data Processing runner stays off: Core and the Dashboard are dropping that shape for Data Transformation, and an install that still needs it sets `WORKER_ENABLE_DATA_PROCESSING` in `extraEnv`. Don't add it back. When Monocore adds a flag to that block, add it to the helper's list, `values.yaml`, the chart README and `tests/mono_optional_shapes_test.rb`.

---

## KubeFaaS

### Component split

- **Controller**: manages function lifecycle; needs ClusterRole (pods, deployments, services, namespaces, HPAs).
- **Builder**: builds function images; runs a Docker-in-Docker sidecar with mutual TLS. Its readiness probe is `/v1/readyz`, which fails until Docker answers and every template's base image is in the registry. Startup and liveness stay on `/v1/healthz`. Keep the probes in step with the KubeFaaS chart (`charts/kubefaas/templates/builder/deployment.yaml` in the kubefaas repo). With readiness on `/v1/healthz` it took builds before their base images existed, and they failed with `failed to resolve source metadata ... not found`. `tests/kubefaas_builder_readiness_test.rb` checks this.
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

### Lightweight profile

Under `profile: lightweight` KubeFaaS runs one controller and requests 240Mi in place of about 1.6 GiB. The function profile requests 16M, but Core's scripts send their own 35M request, which wins, and keep their pod until 24 hours after their last call. The templates take replicas, resources and the function profile from `patchworks.kubefaas.sizing`, which lays `lightweight.kubefaas.<path>` over `kubefaas.<path>`. Read sizes from it, never `$kf.*.resources` directly, and leave `$kf` as given: `checksum/config` hashes it.

The app chart's gateway, start, processor and Monocore roll with `maxSurge: 0` under the profile (`patchworks.lightweight.strategy`): the old pod is stopped as its replacement is created, so their upgrades need no spare memory. Helm's legacy wait returns early on those one-replica Deployments; the installer uses the status watcher. `tests/lightweight_profile_test.rb` checks the budget. A 3021Mi node runs k3s's and Contour's pods, both charts, KubeFaaS and one 35M script pod. It must still have room for the largest pod that does surge. The installer's summary repeats the 240Mi figure, and `TestLightweightProfileRequestsLessKubeFaaSMemory` ties the two together.

### Auth and registry

The auth secret (`kubefaas.auth.*`) is shared between controller, builder, and monocore workers. The registry secret holds the raw `registries.yaml` content consumed by the builder.

Both support `existingSecret` for bringing pre-created Secrets.

The chart renders the KubeFaaS auth Secret itself, in the KubeFaaS and release namespaces, not the credential generator Job: that pre-install hook runs before the KubeFaaS namespace exists, and Helm decides which objects to adopt before hooks run, so a hook can't create it either. Logins that several templates embed go through `patchworks.memoLogin`: inline values, else the existing Secret's (`lookup`), else generated, cached on `.Values._memo` so every template in one render gets the same value. `patchworks.secretRenderable` skips a Secret this release doesn't own, such as one an older chart's generator wrote, and its login is reused. Under `helm template` and Argo CD `lookup` returns nothing, so generated logins change on every render; the values comments say to set them there.

The builder's S3 login (image cache) follows the same rule. `patchworks.kubefaas.s3Login` copies it into `<fullname>-kubefaas-s3` in the KubeFaaS namespace: from the S3 Secret via `lookup`, or, before the generator has created it, generated at render and handed to the generator Job as its seed. Never point a KubeFaaS pod at a Secret in another namespace. `tests/kubefaas_s3_credentials_test.rb` checks that every Secret a KubeFaaS pod reads is rendered in its namespace.

### Bundled image registry

`kubefaas.registry.bundled.enabled` runs `registry:2` in the KubeFaaS namespace for installations without a registry. Images are named `localhost:<nodePort>/...`: nodes pull through the NodePort on localhost (containerd uses plain HTTP for localhost; kube-proxy in iptables mode routes it), and the builder pushes through its `registry-proxy` socat sidecar on `127.0.0.1:<nodePort>` (Docker treats localhost as insecure). No node or daemon configuration is needed. One login (`patchworks.kubefaas.registry.login`) goes into the registry's htpasswd, `registries.yaml` and a dockerconfigjson pull Secret in every function namespace (`patchworks.kubefaas.functionNamespaces`); `tests/kubefaas_registry_test.rb` checks they match. `kubefaas.builder.platform` sets `BASE_IMAGE_PLATFORM`; the builder defaults to `linux/amd64`, so arm64 clusters need `linux/arm64`. The registry login has no `existingSecret`, a deliberate exception to the existingSecret pattern: `registries.yaml` and the pull Secrets embed the password, so the chart needs the value itself. The installer offers both with bundled infrastructure (`cmd/patchworks-installer/kubefaas.go`) and copies the image pull Secret into the KubeFaaS namespace, which it creates labelled for `patchworks-infra` to adopt.

### dind MTU

Image builds run on dind's Docker bridge, so its MTU must fit the pod network. dind starts with `--mtu`: `kubefaas.builder.dind.mtu` when set, else the pod's `eth0` MTU, read at startup. Docker's default 1500 stalled build downloads (GitHub) under k3s flannel's 1450. Keep the entrypoint call taking only options, so it still adds its host and TLS flags. `tests/kubefaas_dind_mtu_test.rb` checks this.

---

## RabbitMQ

Follows the standard infrastructure pattern. Key details:

- Image: `rabbitmq:3-management-alpine` (exposes AMQP on 5672, management UI on 15672).
- `RABBITMQ_DEFAULT_VHOST` is only set when the configured vhost is not `/` — RabbitMQ's default is already `/`.
- Readiness/liveness probes use `rabbitmq-diagnostics -q ping` (not TCP socket) because the broker takes time to initialise its vhost even after the port opens.
- `workers.mono` and app pods wait for RabbitMQ via a `busybox` initContainer running `nc -zv`.
- The image names its node `rabbit@<hostname>` and keeps its data in `/var/lib/rabbitmq/mnesia/rabbit@<hostname>`. The Deployment therefore pins `spec.template.spec.hostname`: `rabbitmq.hostname`, default `<fullname>-rabbitmq`. With the pod name as hostname, every new pod started an empty broker. Don't switch to `RABBITMQ_NODENAME`: its host part must resolve inside the pod, and the pod resolves only its own hostname. The broker fails to start, and so do the probes.
- Only one broker may open that directory at a time.
  - **Rollouts:** the Deployment uses `Recreate`.
  - **Deleted or evicted pods:** `Recreate` doesn't cover these. The ReplicaSet starts the replacement while the old pod is still stopping, and on one node both mount the volume; on kind it happened on every delete. The container therefore runs `sh -c` with a `flock` on `/var/lib/rabbitmq/.broker.lock`, then `exec docker-entrypoint.sh rabbitmq-server`. The broker inherits the lock fd, so the lock is held until the old container exits. Keep the entrypoint and command in step with the official image.
  - **Tests:** `tests/rabbitmq_node_name_test.rb` checks the hostname, the strategy and the lock.
- Moving a live Deployment to `Recreate` needs a transition under server-side apply, which is Helm 4's default and the installer's. Kubernetes defaulted that Deployment's `rollingUpdate`, and no field manager owns it, so the apply keeps it and fails with `spec.strategy.rollingUpdate: Forbidden`. `rollingUpdate: null` in the template doesn't help. `rabbitmq-recreate.yaml` is a `pre-upgrade` hook that merge-patches the type and drops the block. `lookup` skips it once the Deployment uses `Recreate`. Do the same for any other Deployment that moves to `Recreate`.

---

## Samples

`samples.enabled` runs `templates/samples.yaml`: a post-install/post-upgrade hook Job that runs `files/samples/install.php` in the Core image to create sample flows in the seeded company. The PHP lives in `files/samples/` and is mounted from a ConfigMap with `.Files.Get`:

| File | What it contains |
|---|---|
| `install.php` | Boots Core's Laravel app (no tinker) and runs the samples |
| `lib.php` | `Samples`: create-if-missing flows built with Core's `app/Ai/Tools` classes, route naming, layout, deploy with Core's connector allowance check |
| `layout.php` | Port of the dashboard's `lib/flowGraphLayout.ts` (`applyDfsLayout`, 15px snap) |
| `flows.php` | The sample flows and their supporting records |

Rules for changing samples:

- Never pass the PHP through `tpl`: flows contain Patchworks `{{flow.variables.*}}` placeholders.
- Never destructive: a sample that exists is left alone. Only the half-built flow of a failed build is deleted.
- Look records up by key or name (transformation functions, system and authentication types, entity types), never by id.
- Route and Branch steps come with two routes: rename them with `Samples::routes()` instead of adding more.
- Every sample ends in an Assert on its exact output. Check new ones on a cluster, run twice, before shipping.
- Keep `layout.php` in step with the dashboard's `flowGraphLayout.ts`; `tests/samples_test.rb` compares it with positions the TypeScript gave (`tests/fixtures/samples_layout.json`).
- Samples need mono workers with `allCompanies` and Monocore's optional shapes (`patchworks.samples.validate`).

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
