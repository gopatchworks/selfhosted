{{/*
Expand the name of the chart.
*/}}
{{- define "patchworks.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "patchworks.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Name of the Secret containing PASSPORT_PRIVATE_KEY / PASSPORT_PUBLIC_KEY.
Shared between Core and Fabric. Returns the existingSecret name if configured,
otherwise the auto-generated secret name.
*/}}
{{- define "patchworks.passportSecretName" -}}
{{- if .Values.passport.existingSecret.name }}
{{- .Values.passport.existingSecret.name }}
{{- else }}
{{- printf "%s-passport-keys" (include "patchworks.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Name of the app ServiceAccount.
*/}}
{{- define "patchworks.serviceAccountName" -}}
{{- if .Values.serviceAccount.name }}
{{- .Values.serviceAccount.name }}
{{- else }}
{{- include "patchworks.fullname" . }}
{{- end }}
{{- end }}

{{/*
Common labels applied to every resource.
*/}}
{{- define "patchworks.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Selector labels for a given component.
Usage: include "patchworks.selectorLabels" (dict "component" "gateway" "Release" .Release)
*/}}
{{- define "patchworks.selectorLabels" -}}
app.kubernetes.io/name: {{ .component }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* ── Namespace helpers ─────────────────────────────────────────────────────── */}}

{{/*
Resolve a component's deployment namespace.
Returns the first non-empty value of: component ns → global ns → release namespace.
Usage: include "patchworks.componentNamespace" (dict "ns" .Values.web.namespace "root" .)
*/}}
{{- define "patchworks.componentNamespace" -}}
{{- .ns | default .root.Values.namespace | default .root.Release.Namespace -}}
{{- end }}

{{- define "patchworks.web.namespace" -}}
{{- include "patchworks.componentNamespace" (dict "ns" .Values.web.namespace "root" .) -}}
{{- end }}

{{- define "patchworks.gateway.namespace" -}}
{{- $ns := .Values.web.gateway.namespace | default .Values.web.namespace -}}
{{- include "patchworks.componentNamespace" (dict "ns" $ns "root" .) -}}
{{- end }}

{{- define "patchworks.start.namespace" -}}
{{- $ns := .Values.web.start.namespace | default .Values.web.namespace -}}
{{- include "patchworks.componentNamespace" (dict "ns" $ns "root" .) -}}
{{- end }}

{{- define "patchworks.fabric.namespace" -}}
{{- include "patchworks.componentNamespace" (dict "ns" .Values.fabric.namespace "root" .) -}}
{{- end }}

{{- define "patchworks.dashboard.namespace" -}}
{{- include "patchworks.componentNamespace" (dict "ns" .Values.dashboard.namespace "root" .) -}}
{{- end }}

{{- define "patchworks.workers.namespace" -}}
{{- include "patchworks.componentNamespace" (dict "ns" .Values.workers.namespace "root" .) -}}
{{- end }}

{{- define "patchworks.migrations.namespace" -}}
{{- include "patchworks.componentNamespace" (dict "ns" .Values.migrations.namespace "root" .) -}}
{{- end }}

{{- define "patchworks.seeds.namespace" -}}
{{- $ns := .Values.seeds.namespace | default .Values.migrations.namespace -}}
{{- include "patchworks.componentNamespace" (dict "ns" $ns "root" .) -}}
{{- end }}

{{- define "patchworks.mysql.namespace" -}}
{{- include "patchworks.componentNamespace" (dict "ns" .Values.mysql.namespace "root" .) -}}
{{- end }}

{{- define "patchworks.redis.namespace" -}}
{{- include "patchworks.componentNamespace" (dict "ns" .Values.redis.namespace "root" .) -}}
{{- end }}

{{- define "patchworks.rabbitmq.namespace" -}}
{{- include "patchworks.componentNamespace" (dict "ns" .Values.rabbitmq.namespace "root" .) -}}
{{- end }}

{{- define "patchworks.elasticsearch.namespace" -}}
{{- include "patchworks.componentNamespace" (dict "ns" .Values.elasticsearch.namespace "root" .) -}}
{{- end }}

{{- define "patchworks.s3.namespace" -}}
{{- include "patchworks.componentNamespace" (dict "ns" .Values.s3.namespace "root" .) -}}
{{- end }}

{{/* KubeFaaS control-plane namespace — defaults to "kubefaas", not the release namespace. */}}
{{- define "patchworks.kubefaas.namespace" -}}
{{- .Values.kubefaas.namespace | default "kubefaas" -}}
{{- end }}

{{/* ── KubeFaaS connection helpers ─────────────────────────────────────────────── */}}

{{- define "patchworks.kubefaas.host" -}}
{{- if .Values.kubefaas.enabled -}}
{{- printf "http://%s-kubefaas-controller.%s.svc.cluster.local:8080" (include "patchworks.fullname" .) (include "patchworks.kubefaas.namespace" .) -}}
{{- else -}}
{{- .Values.kubefaas.host -}}
{{- end -}}
{{- end }}

{{- define "patchworks.kubefaas.builderHost" -}}
{{- if .Values.kubefaas.enabled -}}
{{- printf "http://%s-kubefaas-builder.%s.svc.cluster.local:8080" (include "patchworks.fullname" .) (include "patchworks.kubefaas.namespace" .) -}}
{{- else -}}
{{- .Values.kubefaas.builderHost -}}
{{- end -}}
{{- end }}

{{- define "patchworks.kubefaas.redisHost" -}}
{{- if .Values.kubefaas.redis.enabled -}}
{{- printf "%s-kubefaas-redis.%s.svc.cluster.local" (include "patchworks.fullname" .) (include "patchworks.kubefaas.namespace" .) -}}
{{- else -}}
{{- .Values.kubefaas.redis.external.host -}}
{{- end -}}
{{- end }}

{{- define "patchworks.kubefaas.redisPort" -}}
{{- if .Values.kubefaas.redis.enabled -}}6379{{- else -}}{{ .Values.kubefaas.redis.external.port }}{{- end -}}
{{- end }}

{{/* Auth secret name and keys for KubeFaaS components. */}}
{{- define "patchworks.kubefaas.authSecretName" -}}
{{- if .Values.kubefaas.auth.existingSecret.name -}}
{{- .Values.kubefaas.auth.existingSecret.name -}}
{{- else -}}
{{- printf "%s-kubefaas-auth" (include "patchworks.fullname" .) -}}
{{- end -}}
{{- end }}

{{- define "patchworks.kubefaas.authUsernameKey" -}}
{{- if .Values.kubefaas.auth.existingSecret.name -}}
{{- .Values.kubefaas.auth.existingSecret.usernameKey | default "username" -}}
{{- else -}}
username
{{- end -}}
{{- end }}

{{- define "patchworks.kubefaas.authPasswordKey" -}}
{{- if .Values.kubefaas.auth.existingSecret.name -}}
{{- .Values.kubefaas.auth.existingSecret.passwordKey | default "password" -}}
{{- else -}}
password
{{- end -}}
{{- end }}

{{/* Registry secret name and key. */}}
{{- define "patchworks.kubefaas.registrySecretName" -}}
{{- if .Values.kubefaas.registry.existingSecret.name -}}
{{- .Values.kubefaas.registry.existingSecret.name -}}
{{- else -}}
{{- printf "%s-kubefaas-registry" (include "patchworks.fullname" .) -}}
{{- end -}}
{{- end }}

{{- define "patchworks.kubefaas.registrySecretKey" -}}
{{- if .Values.kubefaas.registry.existingSecret.name -}}
{{- .Values.kubefaas.registry.existingSecret.key | default "registries.yaml" -}}
{{- else -}}
registries.yaml
{{- end -}}
{{- end }}

{{/* ── Pusher helpers ─────────────────────────────────────────────────────────── */}}

{{/* Returns non-empty when any pusher configuration is present. */}}
{{- define "patchworks.pusher.isConfigured" -}}
{{- if or .Values.pusher.enabled .Values.pusher.external.host .Values.pusher.appKey .Values.pusher.existingSecret.name -}}
true
{{- end -}}
{{- end }}

{{/* PUSHER_HOST — in-cluster Soketi service FQDN, or external host. */}}
{{- define "patchworks.pusher.host" -}}
{{- if .Values.pusher.enabled -}}
{{- printf "%s-soketi.%s.svc.cluster.local" .Release.Name .Release.Namespace -}}
{{- else -}}
{{- .Values.pusher.external.host -}}
{{- end -}}
{{- end }}

{{/* PUSHER_PORT — 6001 for in-cluster Soketi, or external port. */}}
{{- define "patchworks.pusher.port" -}}
{{- if .Values.pusher.enabled -}}6001{{- else -}}{{ .Values.pusher.external.port }}{{- end -}}
{{- end }}

{{/* PUSHER_SCHEME — http for in-cluster Soketi, or external scheme. */}}
{{- define "patchworks.pusher.scheme" -}}
{{- if .Values.pusher.enabled -}}http{{- else -}}{{ .Values.pusher.external.scheme }}{{- end -}}
{{- end }}

{{/* ── Image helpers ──────────────────────────────────────────────────────────── */}}

{{/*
Resolve the full image reference for a Patchworks application service.
Global image.registry and image.tag are used unless the service overrides them.

Usage:
  image: {{ include "patchworks.appImage" (dict "svc" .Values.web "root" .) }}
*/}}
{{- define "patchworks.appImage" -}}
{{- $registry := .svc.image.registry | default .root.Values.image.registry -}}
{{- $repository := .svc.image.repository | default .root.Values.image.repository -}}
{{- $tag := .svc.image.tag | default .root.Values.image.tag -}}
{{- printf "%s/%s:%s" $registry $repository $tag -}}
{{- end }}

{{/*
Resolve the imagePullPolicy for a Patchworks application service.
Falls back to global image.pullPolicy.
*/}}
{{- define "patchworks.appPullPolicy" -}}
{{- .svc.image.pullPolicy | default .root.Values.image.pullPolicy -}}
{{- end }}

{{/* ── MySQL helpers ──────────────────────────────────────────────────────────── */}}

{{- define "patchworks.mysql.host" -}}
{{- if .Values.mysql.enabled -}}
{{- printf "%s-mysql.%s.svc.cluster.local" (include "patchworks.fullname" .) (include "patchworks.mysql.namespace" .) -}}
{{- else -}}
{{- .Values.mysql.external.host -}}
{{- end -}}
{{- end }}

{{- define "patchworks.mysql.port" -}}
{{- if .Values.mysql.enabled -}}3306{{- else -}}{{ .Values.mysql.external.port }}{{- end -}}
{{- end }}

{{- define "patchworks.mysql.database" -}}
{{- if .Values.mysql.enabled -}}{{ .Values.mysql.auth.database }}{{- else -}}{{ .Values.mysql.external.database }}{{- end -}}
{{- end }}

{{- define "patchworks.mysql.username" -}}
{{- if .Values.mysql.enabled -}}{{ .Values.mysql.auth.username }}{{- else -}}{{ .Values.mysql.external.username }}{{- end -}}
{{- end }}

{{- define "patchworks.mysql.password" -}}
{{- if .Values.mysql.enabled -}}{{ .Values.mysql.auth.password }}{{- else -}}{{ .Values.mysql.external.password }}{{- end -}}
{{- end }}

{{/* ── Fabric MySQL helpers ────────────────────────────────────────────────────── */}}
{{/*
Resolve Fabric's MySQL connection details with three fallback modes:
  1. fabric.mysql.enabled  → dedicated in-cluster MySQL for Fabric
  2. fabric.mysql.external.host set → external MySQL for Fabric
  3. (default) shared mode → main MySQL, separate database
*/}}

{{- define "patchworks.fabric.mysql.host" -}}
{{- if .Values.fabric.mysql.enabled -}}
{{- printf "%s-fabric-mysql.%s.svc.cluster.local" (include "patchworks.fullname" .) (include "patchworks.fabric.namespace" .) -}}
{{- else if .Values.fabric.mysql.external.host -}}
{{- .Values.fabric.mysql.external.host -}}
{{- else -}}
{{- include "patchworks.mysql.host" . -}}
{{- end -}}
{{- end }}

{{- define "patchworks.fabric.mysql.port" -}}
{{- if .Values.fabric.mysql.enabled -}}3306
{{- else if .Values.fabric.mysql.external.host -}}{{ .Values.fabric.mysql.external.port }}
{{- else -}}{{ include "patchworks.mysql.port" . }}
{{- end -}}
{{- end }}

{{- define "patchworks.fabric.mysql.database" -}}
{{- if .Values.fabric.mysql.enabled -}}{{ .Values.fabric.mysql.auth.database }}
{{- else if .Values.fabric.mysql.external.host -}}{{ .Values.fabric.mysql.external.database }}
{{- else -}}{{ .Values.fabric.mysql.database }}
{{- end -}}
{{- end }}

{{- define "patchworks.fabric.mysql.username" -}}
{{- if .Values.fabric.mysql.enabled -}}{{ .Values.fabric.mysql.auth.username }}
{{- else if .Values.fabric.mysql.external.host -}}{{ .Values.fabric.mysql.external.username }}
{{- else -}}{{ include "patchworks.mysql.username" . }}
{{- end -}}
{{- end }}

{{- define "patchworks.fabric.mysql.password" -}}
{{- if .Values.fabric.mysql.enabled -}}{{ .Values.fabric.mysql.auth.password }}
{{- else if .Values.fabric.mysql.external.host -}}{{ .Values.fabric.mysql.external.password }}
{{- else -}}{{ include "patchworks.mysql.password" . }}
{{- end -}}
{{- end }}

{{/* Resolve the existingSecret dict for Fabric's MySQL password. */}}
{{- define "patchworks.fabric.mysql.existingSecret" -}}
{{- if .Values.fabric.mysql.enabled -}}
{{- .Values.fabric.mysql.auth.existingSecret | toJson -}}
{{- else if .Values.fabric.mysql.external.host -}}
{{- .Values.fabric.mysql.external.existingSecret | toJson -}}
{{- else -}}
{{- (ternary .Values.mysql.auth.existingSecret .Values.mysql.external.existingSecret .Values.mysql.enabled) | toJson -}}
{{- end -}}
{{- end }}

{{/* ── Fabric Redis helpers ────────────────────────────────────────────────────── */}}
{{/*
Resolve Fabric's Redis connection details with three fallback modes:
  1. fabric.redis.enabled  → dedicated in-cluster Redis for Fabric
  2. fabric.redis.external.host set → external Redis for Fabric
  3. (default) shared mode → main Redis instance
*/}}

{{- define "patchworks.fabric.redis.host" -}}
{{- if .Values.fabric.redis.enabled -}}
{{- printf "%s-fabric-redis.%s.svc.cluster.local" (include "patchworks.fullname" .) (include "patchworks.fabric.namespace" .) -}}
{{- else if .Values.fabric.redis.external.host -}}
{{- .Values.fabric.redis.external.host -}}
{{- else -}}
{{- include "patchworks.redis.host" . -}}
{{- end -}}
{{- end }}

{{- define "patchworks.fabric.redis.port" -}}
{{- if .Values.fabric.redis.enabled -}}6379
{{- else if .Values.fabric.redis.external.host -}}{{ .Values.fabric.redis.external.port }}
{{- else -}}{{ include "patchworks.redis.port" . }}
{{- end -}}
{{- end }}

{{- define "patchworks.fabric.redis.password" -}}
{{- if .Values.fabric.redis.enabled -}}
{{- else if .Values.fabric.redis.external.host -}}{{ .Values.fabric.redis.external.password }}
{{- else -}}{{ include "patchworks.redis.password" . }}
{{- end -}}
{{- end }}

{{/* ── Redis helpers ───────────────────────────────────────────────────────────── */}}

{{- define "patchworks.redis.host" -}}
{{- if .Values.redis.enabled -}}
{{- printf "%s-redis.%s.svc.cluster.local" (include "patchworks.fullname" .) (include "patchworks.redis.namespace" .) -}}
{{- else -}}
{{- .Values.redis.external.host -}}
{{- end -}}
{{- end }}

{{- define "patchworks.redis.port" -}}
{{- if .Values.redis.enabled -}}6379{{- else -}}{{ .Values.redis.external.port }}{{- end -}}
{{- end }}

{{- define "patchworks.redis.password" -}}
{{- if .Values.redis.enabled -}}{{- else -}}{{ .Values.redis.external.password }}{{- end -}}
{{- end }}

{{/* ── RabbitMQ helpers ───────────────────────────────────────────────────────── */}}

{{- define "patchworks.rabbitmq.host" -}}
{{- if .Values.rabbitmq.enabled -}}
{{- printf "%s-rabbitmq.%s.svc.cluster.local" (include "patchworks.fullname" .) (include "patchworks.rabbitmq.namespace" .) -}}
{{- else -}}
{{- .Values.rabbitmq.external.host -}}
{{- end -}}
{{- end }}

{{- define "patchworks.rabbitmq.port" -}}
{{- if .Values.rabbitmq.enabled -}}5672{{- else -}}{{ .Values.rabbitmq.external.port }}{{- end -}}
{{- end }}

{{- define "patchworks.rabbitmq.username" -}}
{{- if .Values.rabbitmq.enabled -}}{{ .Values.rabbitmq.auth.username }}{{- else -}}{{ .Values.rabbitmq.external.username }}{{- end -}}
{{- end }}

{{- define "patchworks.rabbitmq.password" -}}
{{- if .Values.rabbitmq.enabled -}}{{ .Values.rabbitmq.auth.password }}{{- else -}}{{ .Values.rabbitmq.external.password }}{{- end -}}
{{- end }}

{{- define "patchworks.rabbitmq.passwordSecret" -}}
{{- $es := ternary .Values.rabbitmq.auth.existingSecret .Values.rabbitmq.external.existingSecret .Values.rabbitmq.enabled -}}
{{- toJson (dict "name" $es.name "key" $es.passwordKey) -}}
{{- end }}

{{- define "patchworks.rabbitmq.vhost" -}}
{{- if .Values.rabbitmq.enabled -}}{{ .Values.rabbitmq.auth.vhost }}{{- else -}}{{ .Values.rabbitmq.external.vhost }}{{- end -}}
{{- end }}

{{/* ── Elasticsearch helpers ──────────────────────────────────────────────────── */}}

{{- define "patchworks.elasticsearch.host" -}}
{{- if .Values.elasticsearch.enabled -}}
{{- printf "%s-elasticsearch.%s.svc.cluster.local" (include "patchworks.fullname" .) (include "patchworks.elasticsearch.namespace" .) -}}
{{- else -}}
{{- .Values.elasticsearch.external.host -}}
{{- end -}}
{{- end }}

{{- define "patchworks.elasticsearch.port" -}}
{{- if .Values.elasticsearch.enabled -}}9200{{- else -}}{{ .Values.elasticsearch.external.port }}{{- end -}}
{{- end }}

{{- define "patchworks.elasticsearch.url" -}}
{{- if .Values.elasticsearch.enabled -}}
http://{{ include "patchworks.elasticsearch.host" . }}:9200
{{- else -}}
{{ .Values.elasticsearch.external.scheme }}://{{ include "patchworks.elasticsearch.host" . }}:{{ include "patchworks.elasticsearch.port" . }}
{{- end -}}
{{- end }}

{{/* ── S3 helpers ─────────────────────────────────────────────────────────────── */}}

{{- define "patchworks.s3.host" -}}
{{- printf "%s-s3.%s.svc.cluster.local" (include "patchworks.fullname" .) (include "patchworks.s3.namespace" .) -}}
{{- end }}

{{- define "patchworks.s3.endpoint" -}}
{{- if .Values.s3.enabled -}}
{{- printf "http://%s:9000" (include "patchworks.s3.host" .) -}}
{{- else -}}
{{- .Values.s3.external.endpoint -}}
{{- end -}}
{{- end }}

{{- define "patchworks.s3.accessKey" -}}
{{- if .Values.s3.enabled -}}{{ .Values.s3.auth.rootUser }}{{- else -}}{{ .Values.s3.external.accessKey }}{{- end -}}
{{- end }}

{{- define "patchworks.s3.secretKey" -}}
{{- if .Values.s3.enabled -}}{{ .Values.s3.auth.rootPassword }}{{- else -}}{{ .Values.s3.external.secretKey }}{{- end -}}
{{- end }}

{{- define "patchworks.s3.bucket" -}}
{{- if .Values.s3.enabled -}}{{ .Values.s3.bucket }}{{- else -}}{{ .Values.s3.external.bucket }}{{- end -}}
{{- end }}

{{- define "patchworks.s3.region" -}}
{{- if .Values.s3.enabled -}}{{ .Values.s3.region }}{{- else -}}{{ .Values.s3.external.region }}{{- end -}}
{{- end }}

{{/* ── Supervisord helper ─────────────────────────────────────────────────────── */}}

{{/*
Render a supervisord.conf for a PHP queue worker.
Usage: {{ include "patchworks.supervisordConf" (dict "connection" "rabbitmq" "queue" "default" "processes" 15) }}
Mount the result at /etc/supervisor/conf.d/supervisord.conf and run:
  command: [supervisord, -c, /etc/supervisor/conf.d/supervisord.conf]
*/}}
{{- define "patchworks.supervisordConf" -}}
[supervisord]
nodaemon=true
loglevel=info
logfile=/dev/null
pidfile=/tmp/supervisord.pid

[program:worker]
command=/usr/local/bin/php /var/www/html/artisan queue:work {{ .connection }} --queue={{ .queue }} --backoff=0 --max-jobs=0 --memory=256 --sleep=3 --timeout=21600 --tries=1 --rest=0
process_name=%(program_name)s_%(process_num)02d
numprocs={{ .processes }}
stopwaitsecs=21600
autostart=true
autorestart=true
stdout_logfile=/dev/stdout
stdout_logfile_maxbytes=0
stderr_logfile=/dev/stderr
stderr_logfile_maxbytes=0
{{- end }}

{{/* ── PVC helper ──────────────────────────────────────────────────────────────── */}}

{{/*
Render a PVC spec body from a persistence block.
If persistence.spec is non-empty it is used verbatim (full override).
Otherwise accessModes, storageClass, and size are used.

Usage (call inside the PVC manifest at spec: indent level):
  spec:
    {{- include "patchworks.pvcSpec" .Values.mysql.persistence | nindent 4 }}

Callers must gate PVC creation on `not persistence.existingClaim` and
reference the claim as:
  claimName: {{ persistence.existingClaim | default (printf "%s-<component>" $fullname) }}
*/}}
{{- define "patchworks.pvcSpec" -}}
{{- $p := . -}}
{{- if $p.spec -}}
{{ toYaml $p.spec | trim }}
{{- else -}}
accessModes:
{{- range ($p.accessModes | default (list "ReadWriteOnce")) }}
- {{ . }}
{{- end }}
{{- with $p.storageClass }}
storageClassName: {{ . | quote }}
{{- end }}
resources:
  requests:
    storage: {{ $p.size }}
{{- end -}}
{{- end }}

{{/* ── Secret env helper ──────────────────────────────────────────────────────── */}}

{{/*
Render a single env var, sourcing its value from an existing Secret when
existingSecret.name and existingSecret.key are both non-empty, falling back
to a plain inline value otherwise.

Usage (call at the right indent level via nindent):
  {{- include "patchworks.secretEnv" (dict "name" "MY_VAR" "value" "plaintext" "secret" .Values.foo.existingSecret) | nindent 12 }}
*/}}
{{- define "patchworks.secretEnv" -}}
- name: {{ .name }}
{{- if and .secret .secret.name .secret.key }}
  valueFrom:
    secretKeyRef:
      name: {{ .secret.name }}
      key: {{ .secret.key }}
{{- else }}
  value: {{ .value | quote }}
{{- end }}
{{- end }}

{{/* ── Resolved env var helpers ───────────────────────────────────────────────── */}}

{{/* DB_PASSWORD — selects the in-cluster or external password secret automatically. */}}
{{- define "patchworks.env.dbPassword" -}}
{{- $es := ternary .Values.mysql.auth.existingSecret .Values.mysql.external.existingSecret .Values.mysql.enabled -}}
{{- $s := dict "name" $es.name "key" $es.passwordKey -}}
{{- include "patchworks.secretEnv" (dict "name" "DB_PASSWORD" "value" (include "patchworks.mysql.password" .) "secret" $s) -}}
{{- end }}

{{/* FABRIC_DB_PASSWORD — sourced from the fabric MySQL existingSecret or inline password. */}}
{{- define "patchworks.env.fabricDbPassword" -}}
{{- $es := fromJson (include "patchworks.fabric.mysql.existingSecret" .) -}}
{{- $s := dict "name" $es.name "key" $es.passwordKey -}}
{{- include "patchworks.secretEnv" (dict "name" "FABRIC_DB_PASSWORD" "value" (include "patchworks.fabric.mysql.password" .) "secret" $s) -}}
{{- end }}

{{/* AWS_ACCESS_KEY_ID — selects MinIO root user or external access key secret automatically. */}}
{{- define "patchworks.env.s3AccessKey" -}}
{{- if .Values.s3.enabled -}}
{{- $s := dict "name" .Values.s3.auth.existingSecret.name "key" .Values.s3.auth.existingSecret.rootUserKey -}}
{{- include "patchworks.secretEnv" (dict "name" "AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s) -}}
{{- else -}}
{{- $s := dict "name" .Values.s3.external.existingSecret.name "key" .Values.s3.external.existingSecret.accessKeyKey -}}
{{- include "patchworks.secretEnv" (dict "name" "AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s) -}}
{{- end -}}
{{- end }}

{{/* AWS_SECRET_ACCESS_KEY — selects MinIO root password or external secret key secret automatically. */}}
{{- define "patchworks.env.s3SecretKey" -}}
{{- if .Values.s3.enabled -}}
{{- $s := dict "name" .Values.s3.auth.existingSecret.name "key" .Values.s3.auth.existingSecret.rootPasswordKey -}}
{{- include "patchworks.secretEnv" (dict "name" "AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s) -}}
{{- else -}}
{{- $s := dict "name" .Values.s3.external.existingSecret.name "key" .Values.s3.external.existingSecret.secretKeyKey -}}
{{- include "patchworks.secretEnv" (dict "name" "AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s) -}}
{{- end -}}
{{- end }}

{{/*
LARAVEL_APP_KEY — mirrors app.key / app.existingSecret under the monocore env
var name. Returns empty string (renders nothing) when no key is configured.
*/}}
{{- define "patchworks.env.laravelAppKey" -}}
{{- if or .Values.app.key .Values.app.existingSecret.name -}}
{{- include "patchworks.secretEnv" (dict "name" "LARAVEL_APP_KEY" "value" .Values.app.key "secret" .Values.app.existingSecret) -}}
{{- end -}}
{{- end }}

{{/* RABBITMQ_PASSWORD — sourced from auth.passwordSecret or external.passwordSecret. */}}
{{- define "patchworks.env.rabbitmqPassword" -}}
{{- $secret := fromJson (include "patchworks.rabbitmq.passwordSecret" .) -}}
{{- include "patchworks.secretEnv" (dict "name" "RABBITMQ_PASSWORD" "value" (include "patchworks.rabbitmq.password" .) "secret" $secret) -}}
{{- end }}

{{/* RABBITMQ_URL — built entirely from $(VAR) substitution; all components must be explicit env entries before this. */}}
{{- define "patchworks.env.rabbitmqUrl" -}}
- name: RABBITMQ_URL
  value: "amqp://$(RABBITMQ_USER):$(RABBITMQ_PASSWORD)@$(RABBITMQ_HOST):$(RABBITMQ_PORT)/$(RABBITMQ_VHOST)"
{{- end }}

{{/* PUSHER_APP_ID */}}
{{- define "patchworks.env.pusherAppId" -}}
{{- $s := dict "name" .Values.pusher.existingSecret.name "key" .Values.pusher.existingSecret.appIdKey -}}
{{- include "patchworks.secretEnv" (dict "name" "PUSHER_APP_ID" "value" .Values.pusher.appId "secret" $s) -}}
{{- end }}

{{/* PUSHER_APP_KEY */}}
{{- define "patchworks.env.pusherAppKey" -}}
{{- $s := dict "name" .Values.pusher.existingSecret.name "key" .Values.pusher.existingSecret.appKeyKey -}}
{{- include "patchworks.secretEnv" (dict "name" "PUSHER_APP_KEY" "value" .Values.pusher.appKey "secret" $s) -}}
{{- end }}

{{/* PUSHER_APP_SECRET */}}
{{- define "patchworks.env.pusherAppSecret" -}}
{{- $s := dict "name" .Values.pusher.existingSecret.name "key" .Values.pusher.existingSecret.appSecretKey -}}
{{- include "patchworks.secretEnv" (dict "name" "PUSHER_APP_SECRET" "value" .Values.pusher.appSecret "secret" $s) -}}
{{- end }}

{{/* PUSHER_APP_CLUSTER */}}
{{- define "patchworks.env.pusherAppCluster" -}}
{{- $s := dict "name" .Values.pusher.existingSecret.name "key" .Values.pusher.existingSecret.appClusterKey -}}
{{- include "patchworks.secretEnv" (dict "name" "PUSHER_APP_CLUSTER" "value" .Values.pusher.appCluster "secret" $s) -}}
{{- end }}

{{/* KUBEFAAS_FUNCTIONS_USERNAME */}}
{{- define "patchworks.env.kubefaasUsername" -}}
{{- $auth := .Values.kubefaas.auth -}}
{{- $s := dict "name" $auth.existingSecret.name "key" ($auth.existingSecret.usernameKey | default "username") -}}
{{- include "patchworks.secretEnv" (dict "name" "KUBEFAAS_FUNCTIONS_USERNAME" "value" $auth.username "secret" $s) -}}
{{- end }}

{{/* KUBEFAAS_FUNCTIONS_PASSWORD */}}
{{- define "patchworks.env.kubefaasPassword" -}}
{{- $auth := .Values.kubefaas.auth -}}
{{- $s := dict "name" $auth.existingSecret.name "key" ($auth.existingSecret.passwordKey | default "password") -}}
{{- include "patchworks.secretEnv" (dict "name" "KUBEFAAS_FUNCTIONS_PASSWORD" "value" $auth.password "secret" $s) -}}
{{- end }}


{{/*
Render the body of monocore's config.yaml.
Usage: {{ include "patchworks.mono.configYaml" (dict "root" . "queueName" "flows" "processes" 15 "otelServiceName" "monocore") | indent 4 }}
All keys are required; callers compute defaults before calling.
*/}}
{{- define "patchworks.mono.configYaml" -}}
{{- $root := .root -}}
{{- $mono := $root.Values.workers.mono -}}
logging:
  format: {{ $mono.logging.format | quote }}
  addSource: {{ $mono.logging.addSource }}
  verbose: {{ $mono.logging.verbose }}

rabbitmq:
  queue: {{ .queueName | quote }}
  topology:
    file: "/etc/monocore/topology.yaml"

redis:
  mode: {{ $root.Values.redis.mode | quote }}
  addrs:
    {{- if $root.Values.redis.addrs }}
    {{- range $root.Values.redis.addrs }}
    - {{ . | quote }}
    {{- end }}
    {{- else }}
    - {{ printf "%s:%s" (include "patchworks.redis.host" $root) (include "patchworks.redis.port" $root) | quote }}
    {{- end }}
  {{- if eq $root.Values.redis.mode "sentinel" }}
  sentinel:
    master: {{ $root.Values.redis.sentinel.master | quote }}
  {{- end }}
  db: {{ $root.Values.redis.db }}

worker:
  processes: {{ .processes }}

db:
  landlord:
    name: {{ include "patchworks.mysql.database" $root | quote }}

store:
  config: "/etc/monocore/store.yaml"

otel:
  enabled: {{ $mono.otel.enabled }}
  service:
    name: {{ .otelServiceName | quote }}
{{- if $mono.otel.endpoint }}
  endpoint: {{ $mono.otel.endpoint | quote }}
{{- end }}
{{- $kfHost := include "patchworks.kubefaas.host" $root -}}
{{- $kfBuilderHost := include "patchworks.kubefaas.builderHost" $root -}}
{{- $kfRegistry := $root.Values.kubefaas.registry.name -}}
{{- if or $kfHost $kfBuilderHost $kfRegistry }}

kubefaas:
  functions:
    {{- if $kfHost }}
    host: {{ $kfHost | quote }}
    {{- end }}
    {{- if $kfBuilderHost }}
    builder:
      host: {{ $kfBuilderHost | quote }}
    {{- end }}
    {{- if $kfRegistry }}
    registry: {{ $kfRegistry | quote }}
    {{- end }}
{{- end }}
{{- $pusherHost := include "patchworks.pusher.host" $root -}}
{{- $pusherPort := include "patchworks.pusher.port" $root -}}
{{- $pusherScheme := include "patchworks.pusher.scheme" $root -}}
{{- if or $pusherHost $pusherPort $pusherScheme }}

pusher:
  {{- if $pusherScheme }}
  scheme: {{ $pusherScheme | quote }}
  {{- end }}
  {{- if $pusherHost }}
  host: {{ $pusherHost | quote }}
  {{- end }}
  {{- if $pusherPort }}
  port: {{ $pusherPort }}
  {{- end }}
{{- end }}
{{- with $root.Values.workers.notify }}
{{- if or .email.from .ses.region .ses.endpoint .aws.role.arn .aws.external.id }}

notify:
  {{- if .email.from }}
  email:
    from: {{ .email.from | quote }}
  {{- end }}
  {{- if or .ses.region .ses.endpoint }}
  ses:
    {{- if .ses.region }}
    region: {{ .ses.region | quote }}
    {{- end }}
    {{- if .ses.endpoint }}
    endpoint: {{ .ses.endpoint | quote }}
    {{- end }}
  {{- end }}
  {{- if or .aws.role.arn .aws.external.id }}
  aws:
    {{- if .aws.role.arn }}
    role:
      arn: {{ .aws.role.arn | quote }}
    {{- end }}
    {{- if .aws.external.id }}
    external:
      id: {{ .aws.external.id | quote }}
    {{- end }}
  {{- end }}
{{- end }}
{{- end }}
{{- end }}

{{/* ── Mono worker helpers ─────────────────────────────────────────────────────── */}}

{{/*
Render the RabbitMQ topology.yaml content (unindented).
Usage: {{ include "patchworks.mono.topologyYaml" (dict "root" . "queueName" $queueName) | indent 4 }}
*/}}
{{- define "patchworks.mono.topologyYaml" -}}
{{- $root := .root -}}
{{- $mono := $root.Values.workers.mono -}}
{{- $queueName := .queueName -}}
{{- $cf := $mono.rabbitmq.companyFlows -}}
{{- if $mono.rabbitmq.topology -}}
{{ $mono.rabbitmq.topology | toYaml }}
{{- else -}}
vhost: {{ $mono.rabbitmq.vhost | quote }}
queues:
  - name: {{ $queueName | quote }}
    type: quorum
    durable: true
  {{- if $cf.enabled }}
  {{- range $root.Values.workers.companies }}
  - name: {{ (.queue | default .name) | quote }}
    type: quorum
    durable: true
  {{- end }}
  {{- end }}
exchanges:
  - name: {{ $queueName | quote }}
    type: topic
    durable: true
  {{- if $cf.enabled }}
  - name: {{ printf "company-%s" $queueName | quote }}
    type: direct
    durable: true
  {{- end }}
bindings:
  - exchange: {{ $queueName | quote }}
    queue: {{ $queueName | quote }}
    routing_keys:
      - ""
      - "*"
  {{- if $cf.enabled }}
  {{- range $root.Values.workers.companies }}
  {{- $cq := .queue | default .name }}
  - exchange: {{ printf "company-%s" $queueName | quote }}
    queue: {{ $cq | quote }}
    routing_keys:
      - {{ $cq | quote }}
  {{- end }}
  {{- end }}
{{- if $cf.enabled }}
policies:
  - name: {{ printf "company-%s-fallback" $queueName | quote }}
    pattern: {{ printf "company-%s" $queueName | quote }}
    apply_to: exchanges
    priority: 0
    definition:
      alternate-exchange: {{ $queueName | quote }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Render the store.yaml content (unindented).
Usage: {{ include "patchworks.mono.storeYaml" (dict "root" .) | indent 4 }}
*/}}
{{- define "patchworks.mono.storeYaml" -}}
{{- $root := .root -}}
stores:
  default:
    type: s3
    s3:
      bucket: {{ include "patchworks.s3.bucket" $root | quote }}
      region: {{ include "patchworks.s3.region" $root | quote }}
  company_cache:
    type: s3
    s3:
      bucket: {{ $root.Values.s3.companyCacheBucket | default (include "patchworks.s3.bucket" $root) | quote }}
      region: {{ include "patchworks.s3.region" $root | quote }}
{{- end }}

{{/* ── Validation ─────────────────────────────────────────────────────────────── */}}

{{/*
Validate workers.type is a known value and not yet-unavailable.
Called from web.yaml so the error surfaces at render time for any install.
*/}}
{{- define "patchworks.validate" -}}
{{- $valid := list "standalone" "mono" "microservice" -}}
{{- if not (has .Values.workers.type $valid) -}}
{{- fail (printf "workers.type must be one of: standalone, mono, microservice. Got: %q" .Values.workers.type) -}}
{{- end -}}
{{- if and (empty .Values.app.existingSecret.name) (not .Values.app.key) -}}
{{- fail "app.key is required. Generate one with: echo \"base64:$(openssl rand -base64 32)\"" -}}
{{- end -}}
{{- if and (not .Values.mysql.enabled) (not .Values.fabric.mysql.enabled) (empty .Values.fabric.mysql.external.host) -}}
{{- fail "Fabric has no MySQL source. Enable mysql, enable fabric.mysql, or set fabric.mysql.external.host." -}}
{{- end -}}
{{- end -}}

{{/*
Non-sensitive app env vars as a YAML map for ConfigMap data:.
*/}}
{{- define "patchworks.appConfigData" -}}
APP_ENV: {{ .Values.app.env | quote }}
APP_DEBUG: {{ .Values.app.debug | quote }}
APP_URL: {{ .Values.app.url | quote }}
DB_CONNECTION: "mysql"
DB_HOST: {{ include "patchworks.mysql.host" . | quote }}
DB_PORT: {{ include "patchworks.mysql.port" . | quote }}
DB_DATABASE: {{ include "patchworks.mysql.database" . | quote }}
DB_USERNAME: {{ include "patchworks.mysql.username" . | quote }}
LANDLORD_DB_CONNECTION: "mysql"
LANDLORD_DB_HOST: {{ include "patchworks.mysql.host" . | quote }}
LANDLORD_DB_PORT: {{ include "patchworks.mysql.port" . | quote }}
LANDLORD_DB_DATABASE: {{ include "patchworks.mysql.database" . | quote }}
LANDLORD_DB_USERNAME: {{ include "patchworks.mysql.username" . | quote }}
REDIS_HOST: {{ include "patchworks.redis.host" . | quote }}
REDIS_PORT: {{ include "patchworks.redis.port" . | quote }}
RABBITMQ_HOST: {{ include "patchworks.rabbitmq.host" . | quote }}
RABBITMQ_PORT: {{ include "patchworks.rabbitmq.port" . | quote }}
RABBITMQ_USER: {{ include "patchworks.rabbitmq.username" . | quote }}
RABBITMQ_VHOST: {{ trimPrefix "/" (include "patchworks.rabbitmq.vhost" .) | quote }}
ELASTICSEARCH_HOST: {{ include "patchworks.elasticsearch.url" . | quote }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.username }}
ELASTICSEARCH_USER: {{ .Values.elasticsearch.external.username | quote }}
{{- end }}
AWS_ENDPOINT_URL: {{ include "patchworks.s3.endpoint" . | quote }}
AWS_DEFAULT_REGION: {{ include "patchworks.s3.region" . | quote }}
AWS_BUCKET: {{ include "patchworks.s3.bucket" . | quote }}
TENANT_DB_CONNECTION: "tenant"
TENANT_DB_HOST: {{ include "patchworks.mysql.host" . | quote }}
TENANT_DB_PORT: {{ include "patchworks.mysql.port" . | quote }}
TENANT_DB_USERNAME: {{ include "patchworks.mysql.username" . | quote }}
TENANT_REDIS_HOST: {{ include "patchworks.redis.host" . | quote }}
FABRIC_DB_CONNECTION: "fabric"
FABRIC_DB_HOST: {{ include "patchworks.fabric.mysql.host" . | quote }}
FABRIC_DB_PORT: {{ include "patchworks.fabric.mysql.port" . | quote }}
FABRIC_DB_DATABASE: {{ include "patchworks.fabric.mysql.database" . | quote }}
FABRIC_DB_USERNAME: {{ include "patchworks.fabric.mysql.username" . | quote }}
{{- if .Values.s3.enabled }}
TENANT_CACHE_AWS_ENDPOINT_URL: {{ include "patchworks.s3.endpoint" . | quote }}
TENANT_CACHE_AWS_USE_PATH_STYLE_ENDPOINT: "true"
{{- else }}
TENANT_CACHE_AWS_USE_PATH_STYLE_ENDPOINT: "false"
{{- end }}
TENANT_CACHE_AWS_BUCKET: {{ include "patchworks.s3.bucket" . | quote }}
TENANT_CACHE_AWS_DEFAULT_REGION: {{ include "patchworks.s3.region" . | quote }}
{{- if include "patchworks.pusher.isConfigured" . }}
BROADCAST_CONNECTION: "pusher"
{{- with include "patchworks.pusher.host" . }}
PUSHER_HOST: {{ . | quote }}
{{- end }}
{{- with include "patchworks.pusher.scheme" . }}
PUSHER_SCHEME: {{ . | quote }}
{{- end }}
{{- with include "patchworks.pusher.port" . }}
PUSHER_PORT: {{ . | quote }}
{{- end }}
{{- end }}
{{- with include "patchworks.kubefaas.host" . }}
KUBEFAAS_FUNCTIONS_HOST: {{ . | quote }}
{{- end }}
{{- with include "patchworks.kubefaas.builderHost" . }}
KUBEFAAS_FUNCTION_BUILDER_HOST: {{ . | quote }}
{{- end }}
KUBEFAAS_FUNCTION_REGISTRY: {{ .Values.kubefaas.registry.name | quote }}
{{- end }}

{{/*
Sensitive app env vars as a YAML list for pod spec env:.
Includes RABBITMQ_URL (uses $(RABBITMQ_PASSWORD) substitution — must stay in pod env).
*/}}
{{- define "patchworks.appSecretEnv" -}}
{{ include "patchworks.secretEnv" (dict "name" "APP_KEY" "value" .Values.app.key "secret" .Values.app.existingSecret) }}
{{ include "patchworks.env.dbPassword" . }}
{{- $landlordDbSecret := ternary .Values.mysql.auth.existingSecret .Values.mysql.external.existingSecret .Values.mysql.enabled }}
{{ include "patchworks.secretEnv" (dict "name" "LANDLORD_DB_PASSWORD" "value" (include "patchworks.mysql.password" .) "secret" (dict "name" $landlordDbSecret.name "key" $landlordDbSecret.passwordKey)) }}
{{- $tenantDbSecret := ternary .Values.mysql.auth.existingSecret .Values.mysql.external.existingSecret .Values.mysql.enabled }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_DB_PASSWORD" "value" (include "patchworks.mysql.password" .) "secret" (dict "name" $tenantDbSecret.name "key" $tenantDbSecret.passwordKey)) }}
{{- if or (include "patchworks.redis.password" .) .Values.redis.external.existingSecret.name }}
{{- $s := dict "name" .Values.redis.external.existingSecret.name "key" .Values.redis.external.existingSecret.passwordKey }}
{{ include "patchworks.secretEnv" (dict "name" "REDIS_PASSWORD" "value" (include "patchworks.redis.password" .) "secret" $s) }}
{{- end }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.username }}
{{- $esSecret := dict "name" .Values.elasticsearch.external.existingSecret.name "key" .Values.elasticsearch.external.existingSecret.passwordKey }}
{{ include "patchworks.secretEnv" (dict "name" "ELASTICSEARCH_PASSWORD" "value" .Values.elasticsearch.external.password "secret" $esSecret) }}
{{- end }}
{{ include "patchworks.env.s3AccessKey" . }}
{{ include "patchworks.env.s3SecretKey" . }}
{{- if .Values.s3.enabled }}
{{- $s3AccessSecret := dict "name" .Values.s3.auth.existingSecret.name "key" .Values.s3.auth.existingSecret.rootUserKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s3AccessSecret) }}
{{- $s3SecretSecret := dict "name" .Values.s3.auth.existingSecret.name "key" .Values.s3.auth.existingSecret.rootPasswordKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s3SecretSecret) }}
{{- else }}
{{- $s3AccessSecret := dict "name" .Values.s3.external.existingSecret.name "key" .Values.s3.external.existingSecret.accessKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s3AccessSecret) }}
{{- $s3SecretSecret := dict "name" .Values.s3.external.existingSecret.name "key" .Values.s3.external.existingSecret.secretKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s3SecretSecret) }}
{{- end }}
{{ include "patchworks.env.rabbitmqPassword" . }}
{{ include "patchworks.env.rabbitmqUrl" . }}
{{- if include "patchworks.pusher.isConfigured" . }}
{{ include "patchworks.env.pusherAppId" . }}
{{ include "patchworks.env.pusherAppKey" . }}
{{ include "patchworks.env.pusherAppSecret" . }}
{{ include "patchworks.env.pusherAppCluster" . }}
{{- end }}
{{- end }}

{{/*
Non-sensitive Fabric env vars as a YAML map for ConfigMap data:.
Like appConfigData but uses fabric MySQL/Redis helpers.
*/}}
{{- define "patchworks.fabricConfigData" -}}
APP_ENV: {{ .Values.app.env | quote }}
APP_DEBUG: {{ .Values.app.debug | quote }}
APP_URL: {{ .Values.app.url | quote }}
DB_CONNECTION: "mysql"
DB_HOST: {{ include "patchworks.fabric.mysql.host" . | quote }}
DB_PORT: {{ include "patchworks.fabric.mysql.port" . | quote }}
DB_DATABASE: {{ include "patchworks.fabric.mysql.database" . | quote }}
DB_USERNAME: {{ include "patchworks.fabric.mysql.username" . | quote }}
LANDLORD_DB_CONNECTION: "mysql"
LANDLORD_DB_HOST: {{ include "patchworks.fabric.mysql.host" . | quote }}
LANDLORD_DB_PORT: {{ include "patchworks.fabric.mysql.port" . | quote }}
LANDLORD_DB_DATABASE: {{ include "patchworks.fabric.mysql.database" . | quote }}
LANDLORD_DB_USERNAME: {{ include "patchworks.fabric.mysql.username" . | quote }}
REDIS_HOST: {{ include "patchworks.fabric.redis.host" . | quote }}
REDIS_PORT: {{ include "patchworks.fabric.redis.port" . | quote }}
ELASTICSEARCH_HOST: {{ include "patchworks.elasticsearch.url" . | quote }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.username }}
ELASTICSEARCH_USER: {{ .Values.elasticsearch.external.username | quote }}
{{- end }}
AWS_ENDPOINT_URL: {{ include "patchworks.s3.endpoint" . | quote }}
AWS_DEFAULT_REGION: {{ include "patchworks.s3.region" . | quote }}
AWS_BUCKET: {{ include "patchworks.s3.bucket" . | quote }}
TENANT_DB_CONNECTION: "tenant"
TENANT_DB_HOST: {{ include "patchworks.fabric.mysql.host" . | quote }}
TENANT_DB_PORT: {{ include "patchworks.fabric.mysql.port" . | quote }}
TENANT_DB_USERNAME: {{ include "patchworks.fabric.mysql.username" . | quote }}
TENANT_REDIS_HOST: {{ include "patchworks.fabric.redis.host" . | quote }}
{{- if .Values.s3.enabled }}
TENANT_CACHE_AWS_ENDPOINT_URL: {{ include "patchworks.s3.endpoint" . | quote }}
TENANT_CACHE_AWS_USE_PATH_STYLE_ENDPOINT: "true"
{{- else }}
TENANT_CACHE_AWS_USE_PATH_STYLE_ENDPOINT: "false"
{{- end }}
TENANT_CACHE_AWS_BUCKET: {{ include "patchworks.s3.bucket" . | quote }}
TENANT_CACHE_AWS_DEFAULT_REGION: {{ include "patchworks.s3.region" . | quote }}
{{- if include "patchworks.pusher.isConfigured" . }}
BROADCAST_CONNECTION: "pusher"
{{- with include "patchworks.pusher.host" . }}
PUSHER_HOST: {{ . | quote }}
{{- end }}
{{- with include "patchworks.pusher.scheme" . }}
PUSHER_SCHEME: {{ . | quote }}
{{- end }}
{{- with include "patchworks.pusher.port" . }}
PUSHER_PORT: {{ . | quote }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Sensitive Fabric env vars as a YAML list for pod spec env:.
Like appSecretEnv but uses fabric MySQL/Redis password helpers.
*/}}
{{- define "patchworks.fabricSecretEnv" -}}
{{ include "patchworks.secretEnv" (dict "name" "APP_KEY" "value" .Values.app.key "secret" .Values.app.existingSecret) }}
{{- $fabricDbSecret := fromJson (include "patchworks.fabric.mysql.existingSecret" .) }}
{{ include "patchworks.secretEnv" (dict "name" "DB_PASSWORD" "value" (include "patchworks.fabric.mysql.password" .) "secret" (dict "name" $fabricDbSecret.name "key" $fabricDbSecret.passwordKey)) }}
{{ include "patchworks.secretEnv" (dict "name" "LANDLORD_DB_PASSWORD" "value" (include "patchworks.fabric.mysql.password" .) "secret" (dict "name" $fabricDbSecret.name "key" $fabricDbSecret.passwordKey)) }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_DB_PASSWORD" "value" (include "patchworks.fabric.mysql.password" .) "secret" (dict "name" $fabricDbSecret.name "key" $fabricDbSecret.passwordKey)) }}
{{- $fabricRedisExistingSecretName := ternary .Values.fabric.redis.external.existingSecret.name .Values.redis.external.existingSecret.name (ne .Values.fabric.redis.external.host "") }}
{{- $fabricRedisExistingSecretKey := ternary .Values.fabric.redis.external.existingSecret.passwordKey .Values.redis.external.existingSecret.passwordKey (ne .Values.fabric.redis.external.host "") }}
{{- if or (include "patchworks.fabric.redis.password" .) $fabricRedisExistingSecretName }}
{{- $s := dict "name" $fabricRedisExistingSecretName "key" $fabricRedisExistingSecretKey }}
{{ include "patchworks.secretEnv" (dict "name" "REDIS_PASSWORD" "value" (include "patchworks.fabric.redis.password" .) "secret" $s) }}
{{- end }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.username }}
{{- $esSecret := dict "name" .Values.elasticsearch.external.existingSecret.name "key" .Values.elasticsearch.external.existingSecret.passwordKey }}
{{ include "patchworks.secretEnv" (dict "name" "ELASTICSEARCH_PASSWORD" "value" .Values.elasticsearch.external.password "secret" $esSecret) }}
{{- end }}
{{ include "patchworks.env.s3AccessKey" . }}
{{ include "patchworks.env.s3SecretKey" . }}
{{- if .Values.s3.enabled }}
{{- $s3AccessSecret := dict "name" .Values.s3.auth.existingSecret.name "key" .Values.s3.auth.existingSecret.rootUserKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s3AccessSecret) }}
{{- $s3SecretSecret := dict "name" .Values.s3.auth.existingSecret.name "key" .Values.s3.auth.existingSecret.rootPasswordKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s3SecretSecret) }}
{{- else }}
{{- $s3AccessSecret := dict "name" .Values.s3.external.existingSecret.name "key" .Values.s3.external.existingSecret.accessKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s3AccessSecret) }}
{{- $s3SecretSecret := dict "name" .Values.s3.external.existingSecret.name "key" .Values.s3.external.existingSecret.secretKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s3SecretSecret) }}
{{- end }}
{{ include "patchworks.env.rabbitmqPassword" . }}
{{ include "patchworks.env.rabbitmqUrl" . }}
{{- if include "patchworks.pusher.isConfigured" . }}
{{ include "patchworks.env.pusherAppId" . }}
{{ include "patchworks.env.pusherAppKey" . }}
{{ include "patchworks.env.pusherAppSecret" . }}
{{ include "patchworks.env.pusherAppCluster" . }}
{{- end }}
{{- end }}

{{/*
Dashboard-specific non-sensitive env vars as a YAML map for ConfigMap data:.
*/}}
{{- define "patchworks.dashboardConfigData" -}}
{{- $fullname := include "patchworks.fullname" . }}
{{- $scheme := ternary "https" "http" (gt (len .Values.ingress.tls) 0) }}
{{- $ingressGateway := .Values.ingress.hosts.gateway | default "" }}
{{- $ingressStart   := .Values.ingress.hosts.start   | default "" }}
{{- $ingressFabric  := .Values.ingress.hosts.fabric  | default "" }}
{{- $coreUrl   := .Values.dashboard.coreUrl   | default (ternary (printf "%s://%s" $scheme $ingressGateway) (printf "http://%s-gateway.%s.svc.cluster.local" $fullname (include "patchworks.gateway.namespace" .)) (ne $ingressGateway "")) }}
{{- $startUrl  := .Values.dashboard.startUrl  | default (ternary (printf "%s://%s" $scheme $ingressStart)   (printf "http://%s-start.%s.svc.cluster.local"   $fullname (include "patchworks.start.namespace" .))   (ne $ingressStart "")) }}
{{- $fabricUrl := .Values.dashboard.fabricUrl | default (ternary (printf "%s://%s" $scheme $ingressFabric)  (printf "http://%s-fabric.%s.svc.cluster.local"  $fullname (include "patchworks.fabric.namespace" .))  (ne $ingressFabric "")) }}
{{- $mcpUrl    := .Values.dashboard.mcpUrl    | default (ternary (printf "%s://%s/api/v1/mcp" $scheme $ingressGateway) (printf "http://%s-gateway.%s.svc.cluster.local/api/v1/mcp" $fullname (include "patchworks.gateway.namespace" .)) (ne $ingressGateway "")) }}
CORE_URL: {{ $coreUrl | quote }}
START_URL: {{ $startUrl | quote }}
FABRIC_URL: {{ $fabricUrl | quote }}
MCP_URL: {{ $mcpUrl | quote }}
INBOUND_URL: {{ .Values.dashboard.inboundUrl | quote }}
GA4_TAG: {{ .Values.dashboard.ga4Tag | quote }}
ZENDESK_URL: {{ .Values.dashboard.zendeskUrl | quote }}
FORCE_REGISTRATION_REQUEST: {{ .Values.dashboard.forceRegistrationRequest | quote }}
{{- end }}

{{/* ── Managed Secret helpers ─────────────────────────────────────────────────── */}}

{{/*
stringData content for the managed patchworks-secret.
Each key is only included when NOT backed by an existingSecret.
*/}}
{{- define "patchworks.appSecretData" -}}
{{- if not .Values.app.existingSecret.name }}
APP_KEY: {{ .Values.app.key | quote }}
{{- end }}
{{- $dbSecret := ternary .Values.mysql.auth.existingSecret .Values.mysql.external.existingSecret .Values.mysql.enabled }}
{{- if not $dbSecret.name }}
DB_PASSWORD: {{ include "patchworks.mysql.password" . | quote }}
LANDLORD_DB_PASSWORD: {{ include "patchworks.mysql.password" . | quote }}
TENANT_DB_PASSWORD: {{ include "patchworks.mysql.password" . | quote }}
{{- end }}
{{- $fabricDbSecret := fromJson (include "patchworks.fabric.mysql.existingSecret" .) }}
{{- if not $fabricDbSecret.name }}
FABRIC_DB_PASSWORD: {{ include "patchworks.fabric.mysql.password" . | quote }}
{{- end }}
{{- if and (not .Values.redis.enabled) (not .Values.redis.external.existingSecret.name) (include "patchworks.redis.password" .) }}
REDIS_PASSWORD: {{ include "patchworks.redis.password" . | quote }}
{{- end }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.username (not .Values.elasticsearch.external.existingSecret.name) }}
ELASTICSEARCH_PASSWORD: {{ .Values.elasticsearch.external.password | quote }}
{{- end }}
{{- $s3ExSecret := ternary .Values.s3.auth.existingSecret .Values.s3.external.existingSecret .Values.s3.enabled }}
{{- if not $s3ExSecret.name }}
AWS_ACCESS_KEY_ID: {{ include "patchworks.s3.accessKey" . | quote }}
AWS_SECRET_ACCESS_KEY: {{ include "patchworks.s3.secretKey" . | quote }}
TENANT_CACHE_AWS_ACCESS_KEY_ID: {{ include "patchworks.s3.accessKey" . | quote }}
TENANT_CACHE_AWS_SECRET_ACCESS_KEY: {{ include "patchworks.s3.secretKey" . | quote }}
{{- end }}
{{- $rmqSecret := fromJson (include "patchworks.rabbitmq.passwordSecret" .) }}
{{- if not $rmqSecret.name }}
RABBITMQ_PASSWORD: {{ include "patchworks.rabbitmq.password" . | quote }}
{{- end }}
{{- if include "patchworks.pusher.isConfigured" . }}
{{- if not .Values.pusher.existingSecret.name }}
PUSHER_APP_ID: {{ .Values.pusher.appId | quote }}
PUSHER_APP_KEY: {{ .Values.pusher.appKey | quote }}
PUSHER_APP_SECRET: {{ .Values.pusher.appSecret | quote }}
PUSHER_APP_CLUSTER: {{ .Values.pusher.appCluster | quote }}
{{- end }}
{{- end }}
{{- if and (or .Values.kubefaas.enabled .Values.kubefaas.host) (not .Values.kubefaas.auth.existingSecret.name) }}
KUBEFAAS_FUNCTIONS_USERNAME: {{ .Values.kubefaas.auth.username | quote }}
KUBEFAAS_FUNCTIONS_PASSWORD: {{ .Values.kubefaas.auth.password | quote }}
{{- end }}
{{- end }}

{{/*
env list items for managed app Secret: RABBITMQ component vars (from ConfigMap, for $(VAR)
substitution), RABBITMQ_PASSWORD (secretKeyRef), RABBITMQ_URL (full substitution), and
existingSecret overrides.
*/}}
{{- define "patchworks.appSecretEnvRefs" -}}
{{- $fullname := include "patchworks.fullname" . -}}
{{- $rmqSecret := fromJson (include "patchworks.rabbitmq.passwordSecret" .) -}}
- name: RABBITMQ_HOST
  valueFrom:
    configMapKeyRef:
      name: {{ $fullname }}-config
      key: RABBITMQ_HOST
- name: RABBITMQ_PORT
  valueFrom:
    configMapKeyRef:
      name: {{ $fullname }}-config
      key: RABBITMQ_PORT
- name: RABBITMQ_USER
  valueFrom:
    configMapKeyRef:
      name: {{ $fullname }}-config
      key: RABBITMQ_USER
- name: RABBITMQ_VHOST
  valueFrom:
    configMapKeyRef:
      name: {{ $fullname }}-config
      key: RABBITMQ_VHOST
- name: RABBITMQ_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ if $rmqSecret.name }}{{ $rmqSecret.name }}{{ else }}{{ $fullname }}-secret{{ end }}
      key: {{ if $rmqSecret.name }}{{ $rmqSecret.key }}{{ else }}RABBITMQ_PASSWORD{{ end }}
{{ include "patchworks.env.rabbitmqUrl" . }}
{{- if .Values.app.existingSecret.name }}
- name: APP_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.app.existingSecret.name }}
      key: {{ .Values.app.existingSecret.key }}
{{- end }}
{{- $dbSecret := ternary .Values.mysql.auth.existingSecret .Values.mysql.external.existingSecret .Values.mysql.enabled }}
{{- if $dbSecret.name }}
- name: DB_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ $dbSecret.name }}
      key: {{ $dbSecret.passwordKey }}
- name: LANDLORD_DB_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ $dbSecret.name }}
      key: {{ $dbSecret.passwordKey }}
- name: TENANT_DB_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ $dbSecret.name }}
      key: {{ $dbSecret.passwordKey }}
{{- end }}
{{- $fabricDbSecret := fromJson (include "patchworks.fabric.mysql.existingSecret" .) }}
{{- if $fabricDbSecret.name }}
- name: FABRIC_DB_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ $fabricDbSecret.name }}
      key: {{ $fabricDbSecret.passwordKey }}
{{- end }}
{{- if .Values.redis.external.existingSecret.name }}
- name: REDIS_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ .Values.redis.external.existingSecret.name }}
      key: {{ .Values.redis.external.existingSecret.passwordKey }}
{{- end }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.username .Values.elasticsearch.external.existingSecret.name }}
- name: ELASTICSEARCH_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ .Values.elasticsearch.external.existingSecret.name }}
      key: {{ .Values.elasticsearch.external.existingSecret.passwordKey }}
{{- end }}
{{- if .Values.s3.enabled }}
{{- if .Values.s3.auth.existingSecret.name }}
- name: AWS_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.auth.existingSecret.name }}
      key: {{ .Values.s3.auth.existingSecret.rootUserKey }}
- name: AWS_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.auth.existingSecret.name }}
      key: {{ .Values.s3.auth.existingSecret.rootPasswordKey }}
- name: TENANT_CACHE_AWS_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.auth.existingSecret.name }}
      key: {{ .Values.s3.auth.existingSecret.rootUserKey }}
- name: TENANT_CACHE_AWS_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.auth.existingSecret.name }}
      key: {{ .Values.s3.auth.existingSecret.rootPasswordKey }}
{{- end }}
{{- else }}
{{- if .Values.s3.external.existingSecret.name }}
- name: AWS_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.external.existingSecret.name }}
      key: {{ .Values.s3.external.existingSecret.accessKeyKey }}
- name: AWS_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.external.existingSecret.name }}
      key: {{ .Values.s3.external.existingSecret.secretKeyKey }}
- name: TENANT_CACHE_AWS_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.external.existingSecret.name }}
      key: {{ .Values.s3.external.existingSecret.accessKeyKey }}
- name: TENANT_CACHE_AWS_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.external.existingSecret.name }}
      key: {{ .Values.s3.external.existingSecret.secretKeyKey }}
{{- end }}
{{- end }}
{{- if and (include "patchworks.pusher.isConfigured" .) .Values.pusher.existingSecret.name }}
- name: PUSHER_APP_ID
  valueFrom:
    secretKeyRef:
      name: {{ .Values.pusher.existingSecret.name }}
      key: {{ .Values.pusher.existingSecret.appIdKey }}
- name: PUSHER_APP_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.pusher.existingSecret.name }}
      key: {{ .Values.pusher.existingSecret.appKeyKey }}
- name: PUSHER_APP_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ .Values.pusher.existingSecret.name }}
      key: {{ .Values.pusher.existingSecret.appSecretKey }}
- name: PUSHER_APP_CLUSTER
  valueFrom:
    secretKeyRef:
      name: {{ .Values.pusher.existingSecret.name }}
      key: {{ .Values.pusher.existingSecret.appClusterKey }}
{{- end }}
{{- if .Values.kubefaas.auth.existingSecret.name }}
- name: KUBEFAAS_FUNCTIONS_USERNAME
  valueFrom:
    secretKeyRef:
      name: {{ .Values.kubefaas.auth.existingSecret.name }}
      key: {{ .Values.kubefaas.auth.existingSecret.usernameKey }}
- name: KUBEFAAS_FUNCTIONS_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ .Values.kubefaas.auth.existingSecret.name }}
      key: {{ .Values.kubefaas.auth.existingSecret.passwordKey }}
{{- end }}
{{- end }}

{{/*
stringData content for the managed patchworks-fabric-secret.
Same as appSecretData but uses Fabric MySQL/Redis password helpers.
*/}}
{{- define "patchworks.fabricSecretData" -}}
{{- if not .Values.app.existingSecret.name }}
APP_KEY: {{ .Values.app.key | quote }}
{{- end }}
{{- $dbSecret := fromJson (include "patchworks.fabric.mysql.existingSecret" .) }}
{{- if not $dbSecret.name }}
DB_PASSWORD: {{ include "patchworks.fabric.mysql.password" . | quote }}
LANDLORD_DB_PASSWORD: {{ include "patchworks.fabric.mysql.password" . | quote }}
TENANT_DB_PASSWORD: {{ include "patchworks.fabric.mysql.password" . | quote }}
{{- end }}
{{- $fabricRedisExistingSecretName := ternary .Values.fabric.redis.external.existingSecret.name .Values.redis.external.existingSecret.name (ne .Values.fabric.redis.external.host "") }}
{{- if and (not $fabricRedisExistingSecretName) (include "patchworks.fabric.redis.password" .) }}
REDIS_PASSWORD: {{ include "patchworks.fabric.redis.password" . | quote }}
{{- end }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.username (not .Values.elasticsearch.external.existingSecret.name) }}
ELASTICSEARCH_PASSWORD: {{ .Values.elasticsearch.external.password | quote }}
{{- end }}
{{- $s3ExSecret := ternary .Values.s3.auth.existingSecret .Values.s3.external.existingSecret .Values.s3.enabled }}
{{- if not $s3ExSecret.name }}
AWS_ACCESS_KEY_ID: {{ include "patchworks.s3.accessKey" . | quote }}
AWS_SECRET_ACCESS_KEY: {{ include "patchworks.s3.secretKey" . | quote }}
TENANT_CACHE_AWS_ACCESS_KEY_ID: {{ include "patchworks.s3.accessKey" . | quote }}
TENANT_CACHE_AWS_SECRET_ACCESS_KEY: {{ include "patchworks.s3.secretKey" . | quote }}
{{- end }}
{{- if include "patchworks.pusher.isConfigured" . }}
{{- if not .Values.pusher.existingSecret.name }}
PUSHER_APP_ID: {{ .Values.pusher.appId | quote }}
PUSHER_APP_KEY: {{ .Values.pusher.appKey | quote }}
PUSHER_APP_SECRET: {{ .Values.pusher.appSecret | quote }}
PUSHER_APP_CLUSTER: {{ .Values.pusher.appCluster | quote }}
{{- end }}
{{- end }}
{{- end }}

{{/*
env list items for managed fabric Secret: existingSecret overrides only.
Fabric does not connect to RabbitMQ.
*/}}
{{- define "patchworks.fabricSecretEnvRefs" -}}
{{- if .Values.app.existingSecret.name }}
- name: APP_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.app.existingSecret.name }}
      key: {{ .Values.app.existingSecret.key }}
{{- end }}
{{- $dbSecret := fromJson (include "patchworks.fabric.mysql.existingSecret" .) }}
{{- if $dbSecret.name }}
- name: DB_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ $dbSecret.name }}
      key: {{ $dbSecret.passwordKey }}
- name: LANDLORD_DB_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ $dbSecret.name }}
      key: {{ $dbSecret.passwordKey }}
- name: TENANT_DB_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ $dbSecret.name }}
      key: {{ $dbSecret.passwordKey }}
{{- end }}
{{- $fabricRedisExistingSecret := ternary .Values.fabric.redis.external.existingSecret .Values.redis.external.existingSecret (ne .Values.fabric.redis.external.host "") }}
{{- if $fabricRedisExistingSecret.name }}
- name: REDIS_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ $fabricRedisExistingSecret.name }}
      key: {{ $fabricRedisExistingSecret.passwordKey }}
{{- end }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.username .Values.elasticsearch.external.existingSecret.name }}
- name: ELASTICSEARCH_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ .Values.elasticsearch.external.existingSecret.name }}
      key: {{ .Values.elasticsearch.external.existingSecret.passwordKey }}
{{- end }}
{{- if .Values.s3.enabled }}
{{- if .Values.s3.auth.existingSecret.name }}
- name: AWS_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.auth.existingSecret.name }}
      key: {{ .Values.s3.auth.existingSecret.rootUserKey }}
- name: AWS_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.auth.existingSecret.name }}
      key: {{ .Values.s3.auth.existingSecret.rootPasswordKey }}
- name: TENANT_CACHE_AWS_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.auth.existingSecret.name }}
      key: {{ .Values.s3.auth.existingSecret.rootUserKey }}
- name: TENANT_CACHE_AWS_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.auth.existingSecret.name }}
      key: {{ .Values.s3.auth.existingSecret.rootPasswordKey }}
{{- end }}
{{- else }}
{{- if .Values.s3.external.existingSecret.name }}
- name: AWS_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.external.existingSecret.name }}
      key: {{ .Values.s3.external.existingSecret.accessKeyKey }}
- name: AWS_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.external.existingSecret.name }}
      key: {{ .Values.s3.external.existingSecret.secretKeyKey }}
- name: TENANT_CACHE_AWS_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.external.existingSecret.name }}
      key: {{ .Values.s3.external.existingSecret.accessKeyKey }}
- name: TENANT_CACHE_AWS_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.s3.external.existingSecret.name }}
      key: {{ .Values.s3.external.existingSecret.secretKeyKey }}
{{- end }}
{{- end }}
{{- if and (include "patchworks.pusher.isConfigured" .) .Values.pusher.existingSecret.name }}
- name: PUSHER_APP_ID
  valueFrom:
    secretKeyRef:
      name: {{ .Values.pusher.existingSecret.name }}
      key: {{ .Values.pusher.existingSecret.appIdKey }}
- name: PUSHER_APP_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.pusher.existingSecret.name }}
      key: {{ .Values.pusher.existingSecret.appKeyKey }}
- name: PUSHER_APP_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ .Values.pusher.existingSecret.name }}
      key: {{ .Values.pusher.existingSecret.appSecretKey }}
- name: PUSHER_APP_CLUSTER
  valueFrom:
    secretKeyRef:
      name: {{ .Values.pusher.existingSecret.name }}
      key: {{ .Values.pusher.existingSecret.appClusterKey }}
{{- end }}
{{- end }}

{{/*
Common env vars injected into every Patchworks app pod (web, workers,
migrations). Infrastructure connection details are derived from the
in-cluster service names or external.* overrides. Secret fields use
patchworks.secretEnv so they can be sourced from an existing Secret.
*/}}
{{- define "patchworks.appEnv" -}}
{{ include "patchworks.secretEnv" (dict "name" "APP_KEY" "value" .Values.app.key "secret" .Values.app.existingSecret) }}
- name: APP_ENV
  value: {{ .Values.app.env | quote }}
- name: APP_DEBUG
  value: {{ .Values.app.debug | quote }}
- name: APP_URL
  value: {{ .Values.app.url | quote }}
- name: DB_CONNECTION
  value: mysql
- name: DB_HOST
  value: {{ include "patchworks.mysql.host" . | quote }}
- name: DB_PORT
  value: {{ include "patchworks.mysql.port" . | quote }}
- name: DB_DATABASE
  value: {{ include "patchworks.mysql.database" . | quote }}
- name: DB_USERNAME
  value: {{ include "patchworks.mysql.username" . | quote }}
{{ include "patchworks.env.dbPassword" . }}
- name: LANDLORD_DB_CONNECTION
  value: mysql
- name: LANDLORD_DB_HOST
  value: {{ include "patchworks.mysql.host" . | quote }}
- name: LANDLORD_DB_PORT
  value: {{ include "patchworks.mysql.port" . | quote }}
- name: LANDLORD_DB_DATABASE
  value: {{ include "patchworks.mysql.database" . | quote }}
- name: LANDLORD_DB_USERNAME
  value: {{ include "patchworks.mysql.username" . | quote }}
{{- $landlordDbSecret := ternary .Values.mysql.auth.existingSecret .Values.mysql.external.existingSecret .Values.mysql.enabled }}
{{ include "patchworks.secretEnv" (dict "name" "LANDLORD_DB_PASSWORD" "value" (include "patchworks.mysql.password" .) "secret" (dict "name" $landlordDbSecret.name "key" $landlordDbSecret.passwordKey)) }}
- name: REDIS_HOST
  value: {{ include "patchworks.redis.host" . | quote }}
- name: REDIS_PORT
  value: {{ include "patchworks.redis.port" . | quote }}
{{- if or (include "patchworks.redis.password" .) .Values.redis.external.existingSecret.name }}
{{- $s := dict "name" .Values.redis.external.existingSecret.name "key" .Values.redis.external.existingSecret.passwordKey }}
{{ include "patchworks.secretEnv" (dict "name" "REDIS_PASSWORD" "value" (include "patchworks.redis.password" .) "secret" $s) }}
{{- end }}
{{ include "patchworks.env.rabbitmqPassword" . }}
{{ include "patchworks.env.rabbitmqUrl" . }}
- name: ELASTICSEARCH_HOST
  value: {{ include "patchworks.elasticsearch.url" . | quote }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.username }}
- name: ELASTICSEARCH_USER
  value: {{ .Values.elasticsearch.external.username | quote }}
{{- $esSecret := dict "name" .Values.elasticsearch.external.existingSecret.name "key" .Values.elasticsearch.external.existingSecret.passwordKey }}
{{ include "patchworks.secretEnv" (dict "name" "ELASTICSEARCH_PASSWORD" "value" .Values.elasticsearch.external.password "secret" $esSecret) }}
{{- end }}
- name: AWS_ENDPOINT_URL
  value: {{ include "patchworks.s3.endpoint" . | quote }}
{{ include "patchworks.env.s3AccessKey" . }}
{{ include "patchworks.env.s3SecretKey" . }}
- name: AWS_DEFAULT_REGION
  value: {{ include "patchworks.s3.region" . | quote }}
- name: AWS_BUCKET
  value: {{ include "patchworks.s3.bucket" . | quote }}
- name: TENANT_DB_CONNECTION
  value: tenant
- name: TENANT_DB_HOST
  value: {{ include "patchworks.mysql.host" . | quote }}
- name: TENANT_DB_PORT
  value: {{ include "patchworks.mysql.port" . | quote }}
- name: TENANT_DB_USERNAME
  value: {{ include "patchworks.mysql.username" . | quote }}
{{- $tenantDbSecret := ternary .Values.mysql.auth.existingSecret .Values.mysql.external.existingSecret .Values.mysql.enabled }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_DB_PASSWORD" "value" (include "patchworks.mysql.password" .) "secret" (dict "name" $tenantDbSecret.name "key" $tenantDbSecret.passwordKey)) }}
- name: TENANT_REDIS_HOST
  value: {{ include "patchworks.redis.host" . | quote }}
{{- if .Values.s3.enabled }}
- name: TENANT_CACHE_AWS_ENDPOINT_URL
  value: {{ include "patchworks.s3.endpoint" . | quote }}
- name: TENANT_CACHE_AWS_USE_PATH_STYLE_ENDPOINT
  value: "true"
{{- else }}
- name: TENANT_CACHE_AWS_USE_PATH_STYLE_ENDPOINT
  value: "false"
{{- end }}
{{- if .Values.s3.enabled }}
{{- $s3AccessSecret := dict "name" .Values.s3.auth.existingSecret.name "key" .Values.s3.auth.existingSecret.rootUserKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s3AccessSecret) }}
{{- $s3SecretSecret := dict "name" .Values.s3.auth.existingSecret.name "key" .Values.s3.auth.existingSecret.rootPasswordKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s3SecretSecret) }}
{{- else }}
{{- $s3AccessSecret := dict "name" .Values.s3.external.existingSecret.name "key" .Values.s3.external.existingSecret.accessKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s3AccessSecret) }}
{{- $s3SecretSecret := dict "name" .Values.s3.external.existingSecret.name "key" .Values.s3.external.existingSecret.secretKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s3SecretSecret) }}
{{- end }}
- name: TENANT_CACHE_AWS_BUCKET
  value: {{ include "patchworks.s3.bucket" . | quote }}
- name: TENANT_CACHE_AWS_DEFAULT_REGION
  value: {{ include "patchworks.s3.region" . | quote }}
{{- if include "patchworks.pusher.isConfigured" . }}
- name: BROADCAST_CONNECTION
  value: pusher
{{ include "patchworks.env.pusherAppId" . }}
{{ include "patchworks.env.pusherAppKey" . }}
{{ include "patchworks.env.pusherAppSecret" . }}
{{ include "patchworks.env.pusherAppCluster" . }}
{{- with include "patchworks.pusher.host" . }}
- name: PUSHER_HOST
  value: {{ . | quote }}
{{- end }}
{{- with include "patchworks.pusher.scheme" . }}
- name: PUSHER_SCHEME
  value: {{ . | quote }}
{{- end }}
{{- with include "patchworks.pusher.port" . }}
- name: PUSHER_PORT
  value: {{ . | quote }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Env vars for the Fabric deployment. Identical to patchworks.appEnv except
all DB_*, LANDLORD_DB_*, and TENANT_DB_* vars point at Fabric's own MySQL
(dedicated, external, or the shared MySQL with a separate database).
*/}}
{{- define "patchworks.fabricEnv" -}}
{{ include "patchworks.secretEnv" (dict "name" "APP_KEY" "value" .Values.app.key "secret" .Values.app.existingSecret) }}
- name: APP_ENV
  value: {{ .Values.app.env | quote }}
- name: APP_DEBUG
  value: {{ .Values.app.debug | quote }}
- name: APP_URL
  value: {{ .Values.app.url | quote }}
- name: DB_CONNECTION
  value: mysql
- name: DB_HOST
  value: {{ include "patchworks.fabric.mysql.host" . | quote }}
- name: DB_PORT
  value: {{ include "patchworks.fabric.mysql.port" . | quote }}
- name: DB_DATABASE
  value: {{ include "patchworks.fabric.mysql.database" . | quote }}
- name: DB_USERNAME
  value: {{ include "patchworks.fabric.mysql.username" . | quote }}
{{- $fabricDbSecret := fromJson (include "patchworks.fabric.mysql.existingSecret" .) }}
{{ include "patchworks.secretEnv" (dict "name" "DB_PASSWORD" "value" (include "patchworks.fabric.mysql.password" .) "secret" (dict "name" $fabricDbSecret.name "key" $fabricDbSecret.passwordKey)) }}
- name: LANDLORD_DB_CONNECTION
  value: mysql
- name: LANDLORD_DB_HOST
  value: {{ include "patchworks.fabric.mysql.host" . | quote }}
- name: LANDLORD_DB_PORT
  value: {{ include "patchworks.fabric.mysql.port" . | quote }}
- name: LANDLORD_DB_DATABASE
  value: {{ include "patchworks.fabric.mysql.database" . | quote }}
- name: LANDLORD_DB_USERNAME
  value: {{ include "patchworks.fabric.mysql.username" . | quote }}
{{ include "patchworks.secretEnv" (dict "name" "LANDLORD_DB_PASSWORD" "value" (include "patchworks.fabric.mysql.password" .) "secret" (dict "name" $fabricDbSecret.name "key" $fabricDbSecret.passwordKey)) }}
- name: REDIS_HOST
  value: {{ include "patchworks.fabric.redis.host" . | quote }}
- name: REDIS_PORT
  value: {{ include "patchworks.fabric.redis.port" . | quote }}
{{- $fabricRedisExistingSecretName := ternary .Values.fabric.redis.external.existingSecret.name .Values.redis.external.existingSecret.name (ne .Values.fabric.redis.external.host "") }}
{{- $fabricRedisExistingSecretKey := ternary .Values.fabric.redis.external.existingSecret.passwordKey .Values.redis.external.existingSecret.passwordKey (ne .Values.fabric.redis.external.host "") }}
{{- if or (include "patchworks.fabric.redis.password" .) $fabricRedisExistingSecretName }}
{{- $s := dict "name" $fabricRedisExistingSecretName "key" $fabricRedisExistingSecretKey }}
{{ include "patchworks.secretEnv" (dict "name" "REDIS_PASSWORD" "value" (include "patchworks.fabric.redis.password" .) "secret" $s) }}
{{- end }}
{{ include "patchworks.env.rabbitmqPassword" . }}
{{ include "patchworks.env.rabbitmqUrl" . }}
- name: ELASTICSEARCH_HOST
  value: {{ include "patchworks.elasticsearch.url" . | quote }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.username }}
- name: ELASTICSEARCH_USER
  value: {{ .Values.elasticsearch.external.username | quote }}
{{- $esSecret := dict "name" .Values.elasticsearch.external.existingSecret.name "key" .Values.elasticsearch.external.existingSecret.passwordKey }}
{{ include "patchworks.secretEnv" (dict "name" "ELASTICSEARCH_PASSWORD" "value" .Values.elasticsearch.external.password "secret" $esSecret) }}
{{- end }}
- name: AWS_ENDPOINT_URL
  value: {{ include "patchworks.s3.endpoint" . | quote }}
{{ include "patchworks.env.s3AccessKey" . }}
{{ include "patchworks.env.s3SecretKey" . }}
- name: AWS_DEFAULT_REGION
  value: {{ include "patchworks.s3.region" . | quote }}
- name: AWS_BUCKET
  value: {{ include "patchworks.s3.bucket" . | quote }}
- name: TENANT_DB_CONNECTION
  value: tenant
- name: TENANT_DB_HOST
  value: {{ include "patchworks.fabric.mysql.host" . | quote }}
- name: TENANT_DB_PORT
  value: {{ include "patchworks.fabric.mysql.port" . | quote }}
- name: TENANT_DB_USERNAME
  value: {{ include "patchworks.fabric.mysql.username" . | quote }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_DB_PASSWORD" "value" (include "patchworks.fabric.mysql.password" .) "secret" (dict "name" $fabricDbSecret.name "key" $fabricDbSecret.passwordKey)) }}
- name: TENANT_REDIS_HOST
  value: {{ include "patchworks.fabric.redis.host" . | quote }}
{{- if .Values.s3.enabled }}
- name: TENANT_CACHE_AWS_ENDPOINT_URL
  value: {{ include "patchworks.s3.endpoint" . | quote }}
- name: TENANT_CACHE_AWS_USE_PATH_STYLE_ENDPOINT
  value: "true"
{{- else }}
- name: TENANT_CACHE_AWS_USE_PATH_STYLE_ENDPOINT
  value: "false"
{{- end }}
{{- if .Values.s3.enabled }}
{{- $s3AccessSecret := dict "name" .Values.s3.auth.existingSecret.name "key" .Values.s3.auth.existingSecret.rootUserKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s3AccessSecret) }}
{{- $s3SecretSecret := dict "name" .Values.s3.auth.existingSecret.name "key" .Values.s3.auth.existingSecret.rootPasswordKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s3SecretSecret) }}
{{- else }}
{{- $s3AccessSecret := dict "name" .Values.s3.external.existingSecret.name "key" .Values.s3.external.existingSecret.accessKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s3AccessSecret) }}
{{- $s3SecretSecret := dict "name" .Values.s3.external.existingSecret.name "key" .Values.s3.external.existingSecret.secretKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s3SecretSecret) }}
{{- end }}
- name: TENANT_CACHE_AWS_BUCKET
  value: {{ include "patchworks.s3.bucket" . | quote }}
- name: TENANT_CACHE_AWS_DEFAULT_REGION
  value: {{ include "patchworks.s3.region" . | quote }}
{{- if include "patchworks.pusher.isConfigured" . }}
- name: BROADCAST_CONNECTION
  value: pusher
{{ include "patchworks.env.pusherAppId" . }}
{{ include "patchworks.env.pusherAppKey" . }}
{{ include "patchworks.env.pusherAppSecret" . }}
{{ include "patchworks.env.pusherAppCluster" . }}
{{- with include "patchworks.pusher.host" . }}
- name: PUSHER_HOST
  value: {{ . | quote }}
{{- end }}
{{- with include "patchworks.pusher.scheme" . }}
- name: PUSHER_SCHEME
  value: {{ . | quote }}
{{- end }}
{{- with include "patchworks.pusher.port" . }}
- name: PUSHER_PORT
  value: {{ . | quote }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Init containers that wait for required infrastructure to be reachable
before starting application pods.
*/}}
{{- define "patchworks.waitForDeps" -}}
- name: wait-for-deps
  image: busybox:1.37
  command:
    - sh
    - -c
    - |
      echo "Waiting for MySQL..."
      until nc -z {{ include "patchworks.mysql.host" . }} {{ include "patchworks.mysql.port" . }}; do sleep 2; done
      echo "Waiting for Redis..."
      until nc -z {{ include "patchworks.redis.host" . }} {{ include "patchworks.redis.port" . }}; do sleep 2; done
      echo "Waiting for RabbitMQ..."
      until nc -z {{ include "patchworks.rabbitmq.host" . }} {{ include "patchworks.rabbitmq.port" . }}; do sleep 2; done
      echo "Waiting for Elasticsearch..."
      until nc -z {{ include "patchworks.elasticsearch.host" . }} {{ include "patchworks.elasticsearch.port" . }}; do sleep 2; done
      echo "Waiting for S3..."
      until nc -z {{ include "patchworks.s3.host" . }} 9000; do sleep 2; done
      echo "All dependencies ready."
{{- end }}

{{/*
Annotation map for dedicated per-service Ingresses (gateway, start, fabric).
Merges ingress.annotations with provider-specific timeout / body-size annotations.
Renders an "annotations:" YAML block (with 2-space indent) or nothing if empty.
*/}}
{{- define "patchworks.ingress.serviceAnnotations" -}}
{{- $ing := .Values.ingress -}}
{{- $anns := deepCopy ($ing.annotations | default dict) -}}
{{- if $ing.timeout -}}
  {{- if eq $ing.provider "contour" -}}
    {{- $_ := set $anns "projectcontour.io/response-timeout" (printf "%vs" $ing.timeout) -}}
  {{- else if eq $ing.provider "nginx" -}}
    {{- $_ := set $anns "nginx.ingress.kubernetes.io/proxy-read-timeout" (toString $ing.timeout) -}}
    {{- $_ =  set $anns "nginx.ingress.kubernetes.io/proxy-send-timeout" (toString $ing.timeout) -}}
  {{- end -}}
{{- end -}}
{{- if and (eq $ing.provider "nginx") $ing.maxBodySize -}}
  {{- $_ := set $anns "nginx.ingress.kubernetes.io/proxy-body-size" $ing.maxBodySize -}}
{{- end -}}
{{- if $anns -}}
annotations:
  {{- toYaml $anns | nindent 2 }}
{{- end -}}
{{- end }}
