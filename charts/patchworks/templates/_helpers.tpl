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
Common labels applied to every resource.
*/}}
{{- define "patchworks.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Selector labels for a given component.
Usage: include "patchworks.selectorLabels" (dict "component" "core-web" "Release" .Release)
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

{{- define "patchworks.workers.namespace" -}}
{{- include "patchworks.componentNamespace" (dict "ns" .Values.workers.namespace "root" .) -}}
{{- end }}

{{- define "patchworks.migrations.namespace" -}}
{{- include "patchworks.componentNamespace" (dict "ns" .Values.migrations.namespace "root" .) -}}
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
{{- $tag := .svc.image.tag | default .root.Values.image.tag -}}
{{- printf "%s/%s:%s" $registry .svc.image.repository $tag -}}
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

{{/* RABBITMQ_URL — constructed from components; password injected via $(RABBITMQ_PASSWORD). */}}
{{- define "patchworks.env.rabbitmqUrl" -}}
{{- $vhost := trimPrefix "/" (include "patchworks.rabbitmq.vhost" .) -}}
- name: RABBITMQ_URL
  value: {{ printf "amqp://%s:$(RABBITMQ_PASSWORD)@%s:%s/%s" (include "patchworks.rabbitmq.username" .) (include "patchworks.rabbitmq.host" .) (include "patchworks.rabbitmq.port" .) $vhost | quote }}
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
{{- end -}}

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
