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
{{- default "patchworks" .Values.nameOverride | trunc 63 | trimSuffix "-" }}
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
Resolve the APP_KEY source. When app.key and app.existingSecret.name are both
empty, the app chart generates a stable Secret named <fullname>-app-key.
*/}}
{{- define "patchworks.appKeySecret" -}}
{{- if .Values.app.existingSecret.name -}}
{{- dict "name" .Values.app.existingSecret.name "key" (.Values.app.existingSecret.key | default "APP_KEY") | toJson -}}
{{- else if not .Values.app.key -}}
{{- dict "name" (printf "%s-app-key" (include "patchworks.fullname" .)) "key" "APP_KEY" | toJson -}}
{{- else -}}
{{- dict "name" "" "key" "APP_KEY" | toJson -}}
{{- end -}}
{{- end }}

{{/*
Initial tenant database name. Fabric records the tenant, but Core owns the
tenant database itself, so the app chart creates this database before Core
tenant migrations run.
*/}}
{{- define "patchworks.seeds.tenantDatabase" -}}
{{- $db := .Values.seeds.tenant.database | default (regexReplaceAll "[^a-z0-9]" (lower .Values.seeds.tenant.companyName) "") -}}
{{- if not $db -}}
{{- fail "seeds.tenant.database or seeds.tenant.companyName is required when creating the initial tenant database" -}}
{{- end -}}
{{- if not (regexMatch "^[A-Za-z0-9_]+$" $db) -}}
{{- fail "seeds.tenant.database must contain only letters, numbers, and underscores" -}}
{{- end -}}
{{- $db -}}
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
Monocore API URL consumed by Core. Defaults to the hub monocore worker Service
rendered by workers-mono.yaml.
*/}}
{{- define "patchworks.monocore.url" -}}
{{- .Values.monocore.url | default (printf "http://%s-workers.%s.svc.cluster.local:8080" (include "patchworks.fullname" .) (include "patchworks.workers.namespace" .)) -}}
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

{{- define "patchworks.kubefaas.authGenerated" -}}
{{- if and .Values.kubefaas.enabled .Values.kubefaas.auth.enabled (not .Values.kubefaas.auth.existingSecret.name) .Values.credentials.autoGenerate (or (not .Values.kubefaas.auth.username) (not .Values.kubefaas.auth.password)) -}}true{{- end -}}
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

{{- define "patchworks.mysql.existingSecret" -}}
{{- if .Values.mysql.enabled -}}
{{- $es := .Values.mysql.auth.existingSecret -}}
{{- if $es.name -}}
{{- dict "name" $es.name "rootPasswordKey" $es.rootPasswordKey "passwordKey" $es.passwordKey | toJson -}}
{{- else if and .Values.credentials.autoGenerate (or (not .Values.mysql.auth.rootPassword) (not .Values.mysql.auth.password)) -}}
{{- dict "name" (printf "%s-mysql-auth" (include "patchworks.fullname" .)) "rootPasswordKey" $es.rootPasswordKey "passwordKey" $es.passwordKey | toJson -}}
{{- else -}}
{{- dict "name" "" "rootPasswordKey" $es.rootPasswordKey "passwordKey" $es.passwordKey | toJson -}}
{{- end -}}
{{- else -}}
{{- $es := .Values.mysql.external.existingSecret -}}
{{- dict "name" $es.name "rootPasswordKey" "" "passwordKey" $es.passwordKey | toJson -}}
{{- end -}}
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
{{- $es := .Values.fabric.mysql.auth.existingSecret -}}
{{- if $es.name -}}
{{- dict "name" $es.name "rootPasswordKey" $es.rootPasswordKey "passwordKey" $es.passwordKey | toJson -}}
{{- else if and .Values.credentials.autoGenerate (or (not .Values.fabric.mysql.auth.rootPassword) (not .Values.fabric.mysql.auth.password)) -}}
{{- dict "name" (printf "%s-fabric-mysql-auth" (include "patchworks.fullname" .)) "rootPasswordKey" $es.rootPasswordKey "passwordKey" $es.passwordKey | toJson -}}
{{- else -}}
{{- dict "name" "" "rootPasswordKey" $es.rootPasswordKey "passwordKey" $es.passwordKey | toJson -}}
{{- end -}}
{{- else if .Values.fabric.mysql.external.host -}}
{{- .Values.fabric.mysql.external.existingSecret | toJson -}}
{{- else -}}
{{- include "patchworks.mysql.existingSecret" . -}}
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
{{- if .Values.rabbitmq.enabled -}}
{{- $es := .Values.rabbitmq.auth.existingSecret -}}
{{- if $es.name -}}
{{- toJson (dict "name" $es.name "key" $es.passwordKey) -}}
{{- else if and .Values.credentials.autoGenerate (not .Values.rabbitmq.auth.password) -}}
{{- toJson (dict "name" (printf "%s-rabbitmq-auth" (include "patchworks.fullname" .)) "key" $es.passwordKey) -}}
{{- else -}}
{{- toJson (dict "name" "" "key" $es.passwordKey) -}}
{{- end -}}
{{- else -}}
{{- $es := .Values.rabbitmq.external.existingSecret -}}
{{- toJson (dict "name" $es.name "key" $es.passwordKey) -}}
{{- end -}}
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

{{- define "patchworks.elasticsearch.secret" -}}
{{- if .Values.elasticsearch.enabled -}}
{{- $es := .Values.elasticsearch.auth.existingSecret -}}
{{- $name := $es.name -}}
{{- if and .Values.credentials.autoGenerate (not $name) (not .Values.elasticsearch.auth.password) -}}
{{- $name = printf "%s-elasticsearch-auth" (include "patchworks.fullname" .) -}}
{{- end -}}
{{- dict
  "name" $name
  "cloudIdKey" ""
  "cloudApiKeyKey" ""
  "apiKeyKey" ""
  "usernameKey" $es.usernameKey
  "passwordKey" $es.passwordKey
  | toJson -}}
{{- else -}}
{{- $es := .Values.elasticsearch.external.existingSecret -}}
{{- dict "name" $es.name "cloudIdKey" $es.cloudIdKey "cloudApiKeyKey" $es.cloudApiKeyKey "apiKeyKey" $es.apiKeyKey "usernameKey" $es.usernameKey "passwordKey" $es.passwordKey | toJson -}}
{{- end -}}
{{- end }}

{{- define "patchworks.elasticsearch.username" -}}
{{- if .Values.elasticsearch.enabled -}}{{ .Values.elasticsearch.auth.username }}{{- else -}}{{ .Values.elasticsearch.external.username }}{{- end -}}
{{- end }}

{{- define "patchworks.elasticsearch.password" -}}
{{- if .Values.elasticsearch.enabled -}}{{ .Values.elasticsearch.auth.password }}{{- else -}}{{ .Values.elasticsearch.external.password }}{{- end -}}
{{- end }}

{{- define "patchworks.env.elasticSearchCloudId" -}}
{{- if and (not .Values.elasticsearch.enabled) (or .Values.elasticsearch.external.cloudId (and .Values.elasticsearch.external.existingSecret.name .Values.elasticsearch.external.existingSecret.cloudIdKey)) -}}
{{- $s := dict "name" .Values.elasticsearch.external.existingSecret.name "key" .Values.elasticsearch.external.existingSecret.cloudIdKey -}}
{{- include "patchworks.secretEnv" (dict "name" "ELASTIC_SEARCH_CLOUD_ID" "value" .Values.elasticsearch.external.cloudId "secret" $s) -}}
{{- end -}}
{{- end }}

{{- define "patchworks.env.elasticSearchCloudApiKey" -}}
{{- if and (not .Values.elasticsearch.enabled) (or .Values.elasticsearch.external.cloudApiKey (and .Values.elasticsearch.external.existingSecret.name .Values.elasticsearch.external.existingSecret.cloudApiKeyKey)) -}}
{{- $s := dict "name" .Values.elasticsearch.external.existingSecret.name "key" .Values.elasticsearch.external.existingSecret.cloudApiKeyKey -}}
{{- include "patchworks.secretEnv" (dict "name" "ELASTIC_SEARCH_CLOUD_API_KEY" "value" .Values.elasticsearch.external.cloudApiKey "secret" $s) -}}
{{- end -}}
{{- end }}

{{- define "patchworks.env.elasticSearchApiKey" -}}
{{- if and (not .Values.elasticsearch.enabled) (or .Values.elasticsearch.external.apiKey (and .Values.elasticsearch.external.existingSecret.name .Values.elasticsearch.external.existingSecret.apiKeyKey)) -}}
{{- $s := dict "name" .Values.elasticsearch.external.existingSecret.name "key" .Values.elasticsearch.external.existingSecret.apiKeyKey -}}
{{- include "patchworks.secretEnv" (dict "name" "ELASTIC_SEARCH_API_KEY" "value" .Values.elasticsearch.external.apiKey "secret" $s) -}}
{{- end -}}
{{- end }}

{{- define "patchworks.env.elasticSearchUsername" -}}
{{- $es := fromJson (include "patchworks.elasticsearch.secret" .) -}}
{{- $value := include "patchworks.elasticsearch.username" . -}}
{{- if or $value (and $es.name $es.usernameKey) -}}
{{- include "patchworks.secretEnv" (dict "name" "ELASTIC_SEARCH_USERNAME" "value" $value "secret" (dict "name" $es.name "key" $es.usernameKey)) -}}
{{- end -}}
{{- end }}

{{- define "patchworks.env.elasticSearchPassword" -}}
{{- $es := fromJson (include "patchworks.elasticsearch.secret" .) -}}
{{- $value := include "patchworks.elasticsearch.password" . -}}
{{- if or $value (and $es.name $es.passwordKey) -}}
{{- include "patchworks.secretEnv" (dict "name" "ELASTIC_SEARCH_PASSWORD" "value" $value "secret" (dict "name" $es.name "key" $es.passwordKey)) -}}
{{- end -}}
{{- end }}

{{- define "patchworks.mapping.elasticsearchAddresses" -}}
{{- $es := .Values.mapping.elasticsearch -}}
{{- if $es.addresses -}}
{{- join "," $es.addresses -}}
{{- else -}}
{{- include "patchworks.elasticsearch.url" . -}}
{{- end -}}
{{- end }}

{{- define "patchworks.mapping.elasticsearchSecret" -}}
{{- $es := .Values.mapping.elasticsearch.existingSecret -}}
{{- dict "name" $es.name "cloudIdKey" $es.cloudIdKey "apiKeyKey" $es.apiKeyKey "usernameKey" $es.usernameKey "passwordKey" $es.passwordKey | toJson -}}
{{- end }}

{{- define "patchworks.mapping.elasticsearchCloudIdConfigured" -}}
{{- $es := .Values.mapping.elasticsearch -}}
{{- if or $es.cloudId (and $es.existingSecret.name $es.existingSecret.cloudIdKey) (and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.cloudId) (and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.existingSecret.name .Values.elasticsearch.external.existingSecret.cloudIdKey) -}}true{{- end -}}
{{- end }}

{{- define "patchworks.mapping.elasticsearchApiKeyConfigured" -}}
{{- $es := .Values.mapping.elasticsearch -}}
{{- if or $es.apiKey (and $es.existingSecret.name $es.existingSecret.apiKeyKey) (and (not .Values.elasticsearch.enabled) (or .Values.elasticsearch.external.apiKey .Values.elasticsearch.external.cloudApiKey)) (and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.existingSecret.name (or .Values.elasticsearch.external.existingSecret.apiKeyKey .Values.elasticsearch.external.existingSecret.cloudApiKeyKey)) -}}true{{- end -}}
{{- end }}

{{- define "patchworks.mapping.elasticsearchUsernameConfigured" -}}
{{- $es := .Values.mapping.elasticsearch -}}
{{- $mainSecret := fromJson (include "patchworks.elasticsearch.secret" .) -}}
{{- if or $es.username (and $es.existingSecret.name $es.existingSecret.usernameKey) (include "patchworks.elasticsearch.username" .) (and $mainSecret.name $mainSecret.usernameKey) -}}true{{- end -}}
{{- end }}

{{- define "patchworks.mapping.elasticsearchPasswordConfigured" -}}
{{- $es := .Values.mapping.elasticsearch -}}
{{- $mainSecret := fromJson (include "patchworks.elasticsearch.secret" .) -}}
{{- if or $es.password (and $es.existingSecret.name $es.existingSecret.passwordKey) (include "patchworks.elasticsearch.password" .) (and $mainSecret.name $mainSecret.passwordKey) -}}true{{- end -}}
{{- end }}

{{- define "patchworks.mapping.elasticsearchEnabled" -}}
{{- if or (include "patchworks.mapping.elasticsearchCloudIdConfigured" .) (include "patchworks.mapping.elasticsearchApiKeyConfigured" .) (include "patchworks.mapping.elasticsearchUsernameConfigured" .) (include "patchworks.mapping.elasticsearchPasswordConfigured" .) (include "patchworks.mapping.elasticsearchAddresses" .) -}}true{{- end -}}
{{- end }}

{{- define "patchworks.env.mappingElasticsearchAddresses" -}}
- name: MAPPING_ELASTICSEARCH_ADDRESSES
  value: {{ include "patchworks.mapping.elasticsearchAddresses" . | quote }}
{{- end }}

{{- define "patchworks.env.mappingElasticsearchCloudId" -}}
{{- $es := .Values.mapping.elasticsearch -}}
{{- $value := $es.cloudId | default .Values.elasticsearch.external.cloudId -}}
{{- $secretName := $es.existingSecret.name | default .Values.elasticsearch.external.existingSecret.name -}}
{{- $secretKey := $es.existingSecret.cloudIdKey | default .Values.elasticsearch.external.existingSecret.cloudIdKey -}}
{{- if include "patchworks.mapping.elasticsearchCloudIdConfigured" . -}}
{{- include "patchworks.secretEnv" (dict "name" "MAPPING_ELASTICSEARCH_CLOUD_ID" "value" $value "secret" (dict "name" $secretName "key" $secretKey)) -}}
{{- end -}}
{{- end }}

{{- define "patchworks.env.mappingElasticsearchApiKey" -}}
{{- $es := .Values.mapping.elasticsearch -}}
{{- $value := $es.apiKey | default .Values.elasticsearch.external.apiKey | default .Values.elasticsearch.external.cloudApiKey -}}
{{- $secretName := $es.existingSecret.name | default .Values.elasticsearch.external.existingSecret.name -}}
{{- $secretKey := $es.existingSecret.apiKeyKey | default .Values.elasticsearch.external.existingSecret.apiKeyKey | default .Values.elasticsearch.external.existingSecret.cloudApiKeyKey -}}
{{- if include "patchworks.mapping.elasticsearchApiKeyConfigured" . -}}
{{- include "patchworks.secretEnv" (dict "name" "MAPPING_ELASTICSEARCH_API_KEY" "value" $value "secret" (dict "name" $secretName "key" $secretKey)) -}}
{{- end -}}
{{- end }}

{{- define "patchworks.env.mappingElasticsearchUsername" -}}
{{- $es := .Values.mapping.elasticsearch -}}
{{- $mainSecret := fromJson (include "patchworks.elasticsearch.secret" .) -}}
{{- $value := $es.username | default (include "patchworks.elasticsearch.username" .) -}}
{{- $secretName := $es.existingSecret.name | default $mainSecret.name -}}
{{- $secretKey := $es.existingSecret.usernameKey | default $mainSecret.usernameKey -}}
{{- if include "patchworks.mapping.elasticsearchUsernameConfigured" . -}}
{{- include "patchworks.secretEnv" (dict "name" "MAPPING_ELASTICSEARCH_USERNAME" "value" $value "secret" (dict "name" $secretName "key" $secretKey)) -}}
{{- end -}}
{{- end }}

{{- define "patchworks.env.mappingElasticsearchPassword" -}}
{{- $es := .Values.mapping.elasticsearch -}}
{{- $mainSecret := fromJson (include "patchworks.elasticsearch.secret" .) -}}
{{- $value := $es.password | default (include "patchworks.elasticsearch.password" .) -}}
{{- $secretName := $es.existingSecret.name | default $mainSecret.name -}}
{{- $secretKey := $es.existingSecret.passwordKey | default $mainSecret.passwordKey -}}
{{- if include "patchworks.mapping.elasticsearchPasswordConfigured" . -}}
{{- include "patchworks.secretEnv" (dict "name" "MAPPING_ELASTICSEARCH_PASSWORD" "value" $value "secret" (dict "name" $secretName "key" $secretKey)) -}}
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

{{- define "patchworks.s3.existingSecret" -}}
{{- if .Values.s3.enabled -}}
{{- $es := .Values.s3.auth.existingSecret -}}
{{- if $es.name -}}
{{- dict "name" $es.name "accessKeyKey" $es.rootUserKey "secretKeyKey" $es.rootPasswordKey "rootUserKey" $es.rootUserKey "rootPasswordKey" $es.rootPasswordKey | toJson -}}
{{- else if and .Values.credentials.autoGenerate (or (not .Values.s3.auth.rootUser) (not .Values.s3.auth.rootPassword)) -}}
{{- dict "name" (printf "%s-s3-auth" (include "patchworks.fullname" .)) "accessKeyKey" $es.rootUserKey "secretKeyKey" $es.rootPasswordKey "rootUserKey" $es.rootUserKey "rootPasswordKey" $es.rootPasswordKey | toJson -}}
{{- else -}}
{{- dict "name" "" "accessKeyKey" $es.rootUserKey "secretKeyKey" $es.rootPasswordKey "rootUserKey" $es.rootUserKey "rootPasswordKey" $es.rootPasswordKey | toJson -}}
{{- end -}}
{{- else -}}
{{- $es := .Values.s3.external.existingSecret -}}
{{- dict "name" $es.name "accessKeyKey" $es.accessKeyKey "secretKeyKey" $es.secretKeyKey "rootUserKey" $es.accessKeyKey "rootPasswordKey" $es.secretKeyKey | toJson -}}
{{- end -}}
{{- end }}

{{- define "patchworks.s3.bucket" -}}
{{- if .Values.s3.enabled -}}{{ .Values.s3.bucket }}{{- else -}}{{ .Values.s3.external.bucket }}{{- end -}}
{{- end }}

{{- define "patchworks.s3.region" -}}
{{- if .Values.s3.enabled -}}{{ .Values.s3.region }}{{- else -}}{{ .Values.s3.external.region }}{{- end -}}
{{- end }}

{{- define "patchworks.s3.pathStyle" -}}
{{- if .Values.s3.enabled -}}true{{- else -}}{{ .Values.s3.external.pathStyle }}{{- end -}}
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
{{- $es := fromJson (include "patchworks.mysql.existingSecret" .) -}}
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
{{- $es := fromJson (include "patchworks.s3.existingSecret" .) -}}
{{- if .Values.s3.enabled -}}
{{- $s := dict "name" $es.name "key" $es.rootUserKey -}}
{{- include "patchworks.secretEnv" (dict "name" "AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s) -}}
{{- else -}}
{{- $s := dict "name" $es.name "key" $es.accessKeyKey -}}
{{- include "patchworks.secretEnv" (dict "name" "AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s) -}}
{{- end -}}
{{- end }}

{{/* AWS_SECRET_ACCESS_KEY — selects MinIO root password or external secret key secret automatically. */}}
{{- define "patchworks.env.s3SecretKey" -}}
{{- $es := fromJson (include "patchworks.s3.existingSecret" .) -}}
{{- if .Values.s3.enabled -}}
{{- $s := dict "name" $es.name "key" $es.rootPasswordKey -}}
{{- include "patchworks.secretEnv" (dict "name" "AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s) -}}
{{- else -}}
{{- $s := dict "name" $es.name "key" $es.secretKeyKey -}}
{{- include "patchworks.secretEnv" (dict "name" "AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s) -}}
{{- end -}}
{{- end }}

{{/*
LARAVEL_APP_KEY — mirrors APP_KEY under the monocore env var name.
*/}}
{{- define "patchworks.env.laravelAppKey" -}}
{{- $appKeySecret := fromJson (include "patchworks.appKeySecret" .) -}}
{{- include "patchworks.secretEnv" (dict "name" "LARAVEL_APP_KEY" "value" .Values.app.key "secret" $appKeySecret) -}}
{{- end }}

{{/* RABBITMQ_PASSWORD — sourced from auth.passwordSecret or external.passwordSecret. */}}
{{- define "patchworks.env.rabbitmqPassword" -}}
{{- $secret := fromJson (include "patchworks.rabbitmq.passwordSecret" .) -}}
{{- include "patchworks.secretEnv" (dict "name" "RABBITMQ_PASSWORD" "value" (include "patchworks.rabbitmq.password" .) "secret" $secret) -}}
{{- end }}

{{/* RABBITMQ_URL only, for env lists that already define RABBITMQ_* component vars. */}}
{{- define "patchworks.env.rabbitmqUrlOnly" -}}
{{- $rmqSecret := fromJson (include "patchworks.rabbitmq.passwordSecret" .) -}}
{{- if $rmqSecret.name -}}
- name: RABBITMQ_URL
  value: "amqp://$(RABBITMQ_USER):$(RABBITMQ_PASSWORD)@$(RABBITMQ_HOST):$(RABBITMQ_PORT)/$(RABBITMQ_VHOST)"
{{- else -}}
- name: RABBITMQ_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "patchworks.fullname" . }}-secret
      key: RABBITMQ_URL
{{- end -}}
{{- end }}

{{/*
RABBITMQ_URL — self-contained helper usable in any container, with or without appSecretEnvRefs.
  - Literal password: references the pre-built, URL-encoded value from the managed Secret.
  - existingSecret:   emits inline RABBITMQ_HOST/PORT/USER/VHOST entries (required for $(VAR)
                      substitution to work) followed by the $(VAR)-templated URL.
*/}}
{{- define "patchworks.env.rabbitmqUrl" -}}
{{- $rmqSecret := fromJson (include "patchworks.rabbitmq.passwordSecret" .) -}}
{{- if $rmqSecret.name -}}
- name: RABBITMQ_HOST
  value: {{ include "patchworks.rabbitmq.host" . | quote }}
- name: RABBITMQ_PORT
  value: {{ include "patchworks.rabbitmq.port" . | quote }}
- name: RABBITMQ_USER
  value: {{ include "patchworks.rabbitmq.username" . | quote }}
- name: RABBITMQ_VHOST
  value: {{ trimPrefix "/" (include "patchworks.rabbitmq.vhost" .) | quote }}
- name: RABBITMQ_URL
  value: "amqp://$(RABBITMQ_USER):$(RABBITMQ_PASSWORD)@$(RABBITMQ_HOST):$(RABBITMQ_PORT)/$(RABBITMQ_VHOST)"
{{- else -}}
- name: RABBITMQ_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "patchworks.fullname" . }}-secret
      key: RABBITMQ_URL
{{- end -}}
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
{{- $secretName := $auth.existingSecret.name -}}
{{- $secretKey := $auth.existingSecret.usernameKey | default "username" -}}
{{- if and (not $secretName) (include "patchworks.kubefaas.authGenerated" .) -}}
{{- $secretName = include "patchworks.kubefaas.authSecretName" . -}}
{{- $secretKey = include "patchworks.kubefaas.authUsernameKey" . -}}
{{- end -}}
{{- $s := dict "name" $secretName "key" $secretKey -}}
{{- include "patchworks.secretEnv" (dict "name" "KUBEFAAS_FUNCTIONS_USERNAME" "value" $auth.username "secret" $s) -}}
{{- end }}

{{/* KUBEFAAS_FUNCTIONS_PASSWORD */}}
{{- define "patchworks.env.kubefaasPassword" -}}
{{- $auth := .Values.kubefaas.auth -}}
{{- $secretName := $auth.existingSecret.name -}}
{{- $secretKey := $auth.existingSecret.passwordKey | default "password" -}}
{{- if and (not $secretName) (include "patchworks.kubefaas.authGenerated" .) -}}
{{- $secretName = include "patchworks.kubefaas.authSecretName" . -}}
{{- $secretKey = include "patchworks.kubefaas.authPasswordKey" . -}}
{{- end -}}
{{- $s := dict "name" $secretName "key" $secretKey -}}
{{- include "patchworks.secretEnv" (dict "name" "KUBEFAAS_FUNCTIONS_PASSWORD" "value" $auth.password "secret" $s) -}}
{{- end }}


{{/* ── Mono worker helpers ─────────────────────────────────────────────────────── */}}

{{/*
Resolve the RabbitMQ exchange used by monocore for flow publishes.
Usage: {{ include "patchworks.mono.flowExchange" (dict "root" . "queueName" $queueName) }}
*/}}
{{- define "patchworks.mono.flowExchange" -}}
{{- $root := .root -}}
{{- $mono := $root.Values.workers.mono -}}
{{- $queueName := .queueName -}}
{{- $configured := $mono.rabbitmq.flowExchange | default "" -}}
{{- if $configured -}}
{{- $configured -}}
{{- else if $mono.rabbitmq.companyFlows.enabled -}}
customer-flows
{{- else -}}
{{- $queueName -}}
{{- end -}}
{{- end -}}

{{/*
Render the RabbitMQ topology.yaml content (unindented).
Usage: {{ include "patchworks.mono.topologyYaml" (dict "root" . "queueName" $queueName) | indent 4 }}
*/}}
{{- define "patchworks.mono.topologyYaml" -}}
{{- $root := .root -}}
{{- $mono := $root.Values.workers.mono -}}
{{- $queueName := .queueName -}}
{{- $cf := $mono.rabbitmq.companyFlows -}}
{{- $flowExchange := include "patchworks.mono.flowExchange" (dict "root" $root "queueName" $queueName) -}}
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
  - name: {{ $flowExchange | quote }}
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
  - exchange: {{ $flowExchange | quote }}
    queue: {{ $cq | quote }}
    routing_keys:
      - {{ $cq | quote }}
  {{- end }}
  {{- end }}
{{- if $cf.enabled }}
policies:
  - name: {{ printf "%s-fallback" $flowExchange | quote }}
    pattern: {{ $flowExchange | quote }}
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
      {{- with include "patchworks.s3.endpoint" $root }}
      endpoint: {{ . | quote }}
      {{- end }}
      bucket: {{ include "patchworks.s3.bucket" $root | quote }}
      region: {{ include "patchworks.s3.region" $root | quote }}
      access_key_id: {{ include "patchworks.s3.accessKey" $root | quote }}
      secret_access_key: {{ include "patchworks.s3.secretKey" $root | quote }}
      path_style: {{ include "patchworks.s3.pathStyle" $root }}
  customer_cache:
    type: s3
    s3:
      {{- with include "patchworks.s3.endpoint" $root }}
      endpoint: {{ . | quote }}
      {{- end }}
      bucket: {{ $root.Values.s3.companyCacheBucket | default (include "patchworks.s3.bucket" $root) | quote }}
      region: {{ include "patchworks.s3.region" $root | quote }}
      access_key_id: {{ include "patchworks.s3.accessKey" $root | quote }}
      secret_access_key: {{ include "patchworks.s3.secretKey" $root | quote }}
      path_style: {{ include "patchworks.s3.pathStyle" $root }}
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
{{- if and (not .Values.mysql.enabled) (not .Values.fabric.mysql.enabled) (empty .Values.fabric.mysql.external.host) -}}
{{- fail "Fabric has no MySQL source. Enable mysql, enable fabric.mysql, or set fabric.mysql.external.host." -}}
{{- end -}}
{{- if and .Values.kubefaas.auth.enabled (not .Values.kubefaas.auth.existingSecret.name) (or (not .Values.kubefaas.auth.username) (not .Values.kubefaas.auth.password)) (not (include "patchworks.kubefaas.authGenerated" .)) -}}
{{- fail "kubefaas.auth.username and kubefaas.auth.password are required unless kubefaas.auth.existingSecret.name is set, or kubefaas.enabled and credentials.autoGenerate are both true." -}}
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
ELASTIC_SEARCH_HOSTS: {{ include "patchworks.elasticsearch.url" . | quote }}
ELASTICSEARCH_HOST: {{ include "patchworks.elasticsearch.url" . | quote }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.cloudId }}
ELASTIC_SEARCH_CLOUD_ID: {{ .Values.elasticsearch.external.cloudId | quote }}
{{- end }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.cloudApiKey }}
ELASTIC_SEARCH_CLOUD_API_KEY: {{ .Values.elasticsearch.external.cloudApiKey | quote }}
{{- end }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.apiKey }}
ELASTIC_SEARCH_API_KEY: {{ .Values.elasticsearch.external.apiKey | quote }}
{{- end }}
{{- if and (include "patchworks.mapping.elasticsearchEnabled" .) .Values.mapping.elasticsearch.index }}
MAPPING_ELASTICSEARCH_INDEX: {{ .Values.mapping.elasticsearch.index | quote }}
{{- end }}
{{- if include "patchworks.elasticsearch.username" . }}
ELASTIC_SEARCH_USERNAME: {{ include "patchworks.elasticsearch.username" . | quote }}
ELASTICSEARCH_USER: {{ include "patchworks.elasticsearch.username" . | quote }}
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
{{- if eq .Values.workers.type "mono" }}
MONOCORE_URL: {{ include "patchworks.monocore.url" . | quote }}
MONOCORE_TIMEOUT: {{ .Values.monocore.timeout | quote }}
{{- end }}
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
{{- $appKeySecret := fromJson (include "patchworks.appKeySecret" .) }}
{{ include "patchworks.secretEnv" (dict "name" "APP_KEY" "value" .Values.app.key "secret" $appKeySecret) }}
{{ include "patchworks.env.dbPassword" . }}
{{- $landlordDbSecret := fromJson (include "patchworks.mysql.existingSecret" .) }}
{{ include "patchworks.secretEnv" (dict "name" "LANDLORD_DB_PASSWORD" "value" (include "patchworks.mysql.password" .) "secret" (dict "name" $landlordDbSecret.name "key" $landlordDbSecret.passwordKey)) }}
{{- $tenantDbSecret := fromJson (include "patchworks.mysql.existingSecret" .) }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_DB_PASSWORD" "value" (include "patchworks.mysql.password" .) "secret" (dict "name" $tenantDbSecret.name "key" $tenantDbSecret.passwordKey)) }}
{{- if or (include "patchworks.redis.password" .) .Values.redis.external.existingSecret.name }}
{{- $s := dict "name" .Values.redis.external.existingSecret.name "key" .Values.redis.external.existingSecret.passwordKey }}
{{ include "patchworks.secretEnv" (dict "name" "REDIS_PASSWORD" "value" (include "patchworks.redis.password" .) "secret" $s) }}
{{- end }}
{{ include "patchworks.env.elasticSearchCloudId" . }}
{{ include "patchworks.env.elasticSearchCloudApiKey" . }}
{{ include "patchworks.env.elasticSearchApiKey" . }}
{{ include "patchworks.env.elasticSearchUsername" . }}
{{ include "patchworks.env.elasticSearchPassword" . }}
{{ include "patchworks.env.mappingElasticsearchPassword" . }}
{{ include "patchworks.env.s3AccessKey" . }}
{{ include "patchworks.env.s3SecretKey" . }}
{{- $s3Secret := fromJson (include "patchworks.s3.existingSecret" .) }}
{{- $s3AccessSecret := dict "name" $s3Secret.name "key" $s3Secret.accessKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s3AccessSecret) }}
{{- $s3SecretSecret := dict "name" $s3Secret.name "key" $s3Secret.secretKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s3SecretSecret) }}
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
ELASTIC_SEARCH_HOSTS: {{ include "patchworks.elasticsearch.url" . | quote }}
ELASTICSEARCH_HOST: {{ include "patchworks.elasticsearch.url" . | quote }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.cloudId }}
ELASTIC_SEARCH_CLOUD_ID: {{ .Values.elasticsearch.external.cloudId | quote }}
{{- end }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.cloudApiKey }}
ELASTIC_SEARCH_CLOUD_API_KEY: {{ .Values.elasticsearch.external.cloudApiKey | quote }}
{{- end }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.apiKey }}
ELASTIC_SEARCH_API_KEY: {{ .Values.elasticsearch.external.apiKey | quote }}
{{- end }}
{{- if include "patchworks.elasticsearch.username" . }}
ELASTIC_SEARCH_USERNAME: {{ include "patchworks.elasticsearch.username" . | quote }}
ELASTICSEARCH_USER: {{ include "patchworks.elasticsearch.username" . | quote }}
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
{{- $appKeySecret := fromJson (include "patchworks.appKeySecret" .) }}
{{ include "patchworks.secretEnv" (dict "name" "APP_KEY" "value" .Values.app.key "secret" $appKeySecret) }}
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
{{ include "patchworks.env.elasticSearchCloudId" . }}
{{ include "patchworks.env.elasticSearchCloudApiKey" . }}
{{ include "patchworks.env.elasticSearchApiKey" . }}
{{ include "patchworks.env.elasticSearchUsername" . }}
{{ include "patchworks.env.elasticSearchPassword" . }}
{{ include "patchworks.env.s3AccessKey" . }}
{{ include "patchworks.env.s3SecretKey" . }}
{{- $s3Secret := fromJson (include "patchworks.s3.existingSecret" .) }}
{{- $s3AccessSecret := dict "name" $s3Secret.name "key" $s3Secret.accessKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s3AccessSecret) }}
{{- $s3SecretSecret := dict "name" $s3Secret.name "key" $s3Secret.secretKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s3SecretSecret) }}
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
{{- $ingressGateway := .Values.ingress.hosts.gateway | default "" }}
{{- $ingressStart   := .Values.ingress.hosts.start   | default "" }}
{{- $ingressFabric := .Values.ingress.hosts.fabric | default "" }}
{{- $ingressDashboard := .Values.ingress.hosts.dashboard | default "" }}
{{- $gatewayScheme := ternary "https" "http" (ne (dig "gateway" "secretName" "" .Values.ingress.tls) "") }}
{{- $startScheme := ternary "https" "http" (ne (dig "start" "secretName" "" .Values.ingress.tls) "") }}
{{- $fabricScheme := ternary "https" "http" (ne (dig "fabric" "secretName" "" .Values.ingress.tls) "") }}
{{- $dashboardScheme := ternary "https" "http" (ne (dig "dashboard" "secretName" "" .Values.ingress.tls) "") }}
{{- $coreUrl   := .Values.dashboard.coreUrl   | default (ternary (printf "%s://%s" $gatewayScheme $ingressGateway) (printf "http://%s-gateway.%s.svc.cluster.local" $fullname (include "patchworks.gateway.namespace" .)) (ne $ingressGateway "")) }}
{{- $startUrl  := .Values.dashboard.startUrl  | default (ternary (printf "%s://%s" $startScheme $ingressStart)   (printf "http://%s-start.%s.svc.cluster.local"   $fullname (include "patchworks.start.namespace" .))   (ne $ingressStart "")) }}
{{- $fabricUrl := .Values.dashboard.fabricUrl | default (ternary (printf "%s://%s/fabric" $dashboardScheme $ingressDashboard) (ternary (printf "%s://%s" $fabricScheme $ingressFabric) (printf "http://%s-fabric.%s.svc.cluster.local" $fullname (include "patchworks.fabric.namespace" .)) (ne $ingressFabric "")) (ne $ingressDashboard "")) }}
{{- $mcpUrl    := .Values.dashboard.mcpUrl    | default (ternary (printf "%s://%s/api/v1/mcp" $gatewayScheme $ingressGateway) (printf "http://%s-gateway.%s.svc.cluster.local/api/v1/mcp" $fullname (include "patchworks.gateway.namespace" .)) (ne $ingressGateway "")) }}
NUXT_PUBLIC_CORE_MAIN_URL: {{ $coreUrl | quote }}
NUXT_PUBLIC_CORE_START_URL: {{ $startUrl | quote }}
NUXT_PUBLIC_FABRIC_URL: {{ $fabricUrl | quote }}
NUXT_PUBLIC_MCP_URL: {{ $mcpUrl | quote }}
NUXT_PUBLIC_INBOUND_URL: {{ .Values.dashboard.inboundUrl | quote }}
NUXT_PUBLIC_GA4_TAG: {{ .Values.dashboard.ga4Tag | quote }}
NUXT_PUBLIC_ZENDESK_URL: {{ .Values.dashboard.zendeskUrl | quote }}
NUXT_PUBLIC_FORCE_REGISTRATION_REQUESTS: {{ .Values.dashboard.forceRegistrationRequest | quote }}
{{- if include "patchworks.pusher.isConfigured" . }}
NUXT_PUBLIC_BROADCASTING_HOST: {{ include "patchworks.pusher.host" . | quote }}
NUXT_PUBLIC_BROADCASTING_PORT: {{ include "patchworks.pusher.port" . | quote }}
NUXT_PUBLIC_BROADCASTING_SCHEME: {{ include "patchworks.pusher.scheme" . | quote }}
{{- end }}
{{- end }}

{{/*
Dashboard Nuxt runtime env vars that may come from existingSecret-backed values.
*/}}
{{- define "patchworks.dashboardNuxtSecretEnvRefs" -}}
{{- if include "patchworks.pusher.isConfigured" . }}
{{- $es := .Values.pusher.existingSecret }}
{{ include "patchworks.secretEnv" (dict "name" "NUXT_PUBLIC_BROADCASTING_APP_ID" "value" .Values.pusher.appId "secret" (dict "name" $es.name "key" $es.appIdKey)) }}
{{ include "patchworks.secretEnv" (dict "name" "NUXT_PUBLIC_BROADCASTING_APP_KEY" "value" .Values.pusher.appKey "secret" (dict "name" $es.name "key" $es.appKeyKey)) }}
{{ include "patchworks.secretEnv" (dict "name" "NUXT_PUBLIC_BROADCASTING_CLUSTER" "value" .Values.pusher.appCluster "secret" (dict "name" $es.name "key" $es.appClusterKey)) }}
{{- end }}
{{- end }}

{{/* ── Managed Secret helpers ─────────────────────────────────────────────────── */}}

{{/*
stringData content for the managed patchworks-secret.
Each key is only included when NOT backed by an existingSecret.
*/}}
{{- define "patchworks.appSecretData" -}}
{{- $appKeySecret := fromJson (include "patchworks.appKeySecret" .) }}
{{- if not $appKeySecret.name }}
APP_KEY: {{ .Values.app.key | quote }}
{{- end }}
{{- $dbSecret := fromJson (include "patchworks.mysql.existingSecret" .) }}
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
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.cloudId (not .Values.elasticsearch.external.existingSecret.name) }}
ELASTIC_SEARCH_CLOUD_ID: {{ .Values.elasticsearch.external.cloudId | quote }}
{{- end }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.cloudApiKey (not .Values.elasticsearch.external.existingSecret.name) }}
ELASTIC_SEARCH_CLOUD_API_KEY: {{ .Values.elasticsearch.external.cloudApiKey | quote }}
{{- end }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.apiKey (not .Values.elasticsearch.external.existingSecret.name) }}
ELASTIC_SEARCH_API_KEY: {{ .Values.elasticsearch.external.apiKey | quote }}
{{- end }}
{{- $esSecret := fromJson (include "patchworks.elasticsearch.secret" .) }}
{{- if not $esSecret.name }}
{{- if include "patchworks.elasticsearch.username" . }}
ELASTIC_SEARCH_USERNAME: {{ include "patchworks.elasticsearch.username" . | quote }}
{{- end }}
{{- if include "patchworks.elasticsearch.password" . }}
ELASTIC_SEARCH_PASSWORD: {{ include "patchworks.elasticsearch.password" . | quote }}
{{- end }}
{{- end }}
{{- $s3ExSecret := fromJson (include "patchworks.s3.existingSecret" .) }}
{{- if not $s3ExSecret.name }}
AWS_ACCESS_KEY_ID: {{ include "patchworks.s3.accessKey" . | quote }}
AWS_SECRET_ACCESS_KEY: {{ include "patchworks.s3.secretKey" . | quote }}
TENANT_CACHE_AWS_ACCESS_KEY_ID: {{ include "patchworks.s3.accessKey" . | quote }}
TENANT_CACHE_AWS_SECRET_ACCESS_KEY: {{ include "patchworks.s3.secretKey" . | quote }}
{{- end }}
{{- $rmqSecret := fromJson (include "patchworks.rabbitmq.passwordSecret" .) }}
{{- if not $rmqSecret.name }}
RABBITMQ_PASSWORD: {{ include "patchworks.rabbitmq.password" . | quote }}
{{- /* Pre-build the URL with urlquery-encoded password so special chars (@, :, etc.) don't break URL parsing at runtime. */}}
RABBITMQ_URL: {{ printf "amqp://%s:%s@%s:%s/%s"
  (include "patchworks.rabbitmq.username" .)
  (urlquery (include "patchworks.rabbitmq.password" .))
  (include "patchworks.rabbitmq.host" .)
  (include "patchworks.rabbitmq.port" .)
  (trimPrefix "/" (include "patchworks.rabbitmq.vhost" .)) | quote }}
{{- end }}
{{- if include "patchworks.pusher.isConfigured" . }}
{{- if not .Values.pusher.existingSecret.name }}
PUSHER_APP_ID: {{ .Values.pusher.appId | quote }}
PUSHER_APP_KEY: {{ .Values.pusher.appKey | quote }}
PUSHER_APP_SECRET: {{ .Values.pusher.appSecret | quote }}
PUSHER_APP_CLUSTER: {{ .Values.pusher.appCluster | quote }}
{{- end }}
{{- end }}
{{- if and .Values.kubefaas.auth.enabled (not .Values.kubefaas.auth.existingSecret.name) (not (include "patchworks.kubefaas.authGenerated" .)) }}
KUBEFAAS_FUNCTIONS_USERNAME: {{ .Values.kubefaas.auth.username | quote }}
KUBEFAAS_FUNCTIONS_PASSWORD: {{ .Values.kubefaas.auth.password | quote }}
{{- end }}
{{- end }}

{{- define "patchworks.appSecretHasData" -}}
{{- if (include "patchworks.appSecretData" . | trim) -}}true{{- end -}}
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
{{ include "patchworks.env.rabbitmqUrlOnly" . }}
{{- $appKeySecret := fromJson (include "patchworks.appKeySecret" .) }}
{{- if $appKeySecret.name }}
- name: APP_KEY
  valueFrom:
    secretKeyRef:
      name: {{ $appKeySecret.name }}
      key: {{ $appKeySecret.key }}
{{- end }}
{{- $dbSecret := fromJson (include "patchworks.mysql.existingSecret" .) }}
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
{{ include "patchworks.env.elasticSearchCloudId" . }}
{{ include "patchworks.env.elasticSearchCloudApiKey" . }}
{{ include "patchworks.env.elasticSearchApiKey" . }}
{{ include "patchworks.env.elasticSearchUsername" . }}
{{ include "patchworks.env.elasticSearchPassword" . }}
{{ include "patchworks.env.mappingElasticsearchPassword" . }}
{{- $s3Secret := fromJson (include "patchworks.s3.existingSecret" .) }}
{{- if $s3Secret.name }}
- name: AWS_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ $s3Secret.name }}
      key: {{ $s3Secret.accessKeyKey }}
- name: AWS_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ $s3Secret.name }}
      key: {{ $s3Secret.secretKeyKey }}
- name: TENANT_CACHE_AWS_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ $s3Secret.name }}
      key: {{ $s3Secret.accessKeyKey }}
- name: TENANT_CACHE_AWS_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ $s3Secret.name }}
      key: {{ $s3Secret.secretKeyKey }}
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
{{- if or .Values.kubefaas.auth.existingSecret.name (include "patchworks.kubefaas.authGenerated" .) }}
{{- $authSecretName := .Values.kubefaas.auth.existingSecret.name | default (include "patchworks.kubefaas.authSecretName" .) }}
{{- $authUsernameKey := .Values.kubefaas.auth.existingSecret.usernameKey | default (include "patchworks.kubefaas.authUsernameKey" .) }}
{{- $authPasswordKey := .Values.kubefaas.auth.existingSecret.passwordKey | default (include "patchworks.kubefaas.authPasswordKey" .) }}
- name: KUBEFAAS_FUNCTIONS_USERNAME
  valueFrom:
    secretKeyRef:
      name: {{ $authSecretName }}
      key: {{ $authUsernameKey }}
- name: KUBEFAAS_FUNCTIONS_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ $authSecretName }}
      key: {{ $authPasswordKey }}
{{- end }}
{{- end }}

{{/*
stringData content for the managed patchworks-fabric-secret.
Same as appSecretData but uses Fabric MySQL/Redis password helpers.
*/}}
{{- define "patchworks.fabricSecretData" -}}
{{- $appKeySecret := fromJson (include "patchworks.appKeySecret" .) }}
{{- if not $appKeySecret.name }}
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
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.cloudId (not .Values.elasticsearch.external.existingSecret.name) }}
ELASTIC_SEARCH_CLOUD_ID: {{ .Values.elasticsearch.external.cloudId | quote }}
{{- end }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.cloudApiKey (not .Values.elasticsearch.external.existingSecret.name) }}
ELASTIC_SEARCH_CLOUD_API_KEY: {{ .Values.elasticsearch.external.cloudApiKey | quote }}
{{- end }}
{{- if and (not .Values.elasticsearch.enabled) .Values.elasticsearch.external.apiKey (not .Values.elasticsearch.external.existingSecret.name) }}
ELASTIC_SEARCH_API_KEY: {{ .Values.elasticsearch.external.apiKey | quote }}
{{- end }}
{{- $esSecret := fromJson (include "patchworks.elasticsearch.secret" .) }}
{{- if not $esSecret.name }}
{{- if include "patchworks.elasticsearch.username" . }}
ELASTIC_SEARCH_USERNAME: {{ include "patchworks.elasticsearch.username" . | quote }}
{{- end }}
{{- if include "patchworks.elasticsearch.password" . }}
ELASTIC_SEARCH_PASSWORD: {{ include "patchworks.elasticsearch.password" . | quote }}
{{- end }}
{{- end }}
{{- $s3ExSecret := fromJson (include "patchworks.s3.existingSecret" .) }}
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

{{- define "patchworks.fabricSecretHasData" -}}
{{- if (include "patchworks.fabricSecretData" . | trim) -}}true{{- end -}}
{{- end }}

{{/*
env list items for managed fabric Secret: existingSecret overrides only.
Fabric does not connect to RabbitMQ.
*/}}
{{- define "patchworks.fabricSecretEnvRefs" -}}
{{- $appKeySecret := fromJson (include "patchworks.appKeySecret" .) }}
{{- if $appKeySecret.name }}
- name: APP_KEY
  valueFrom:
    secretKeyRef:
      name: {{ $appKeySecret.name }}
      key: {{ $appKeySecret.key }}
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
{{ include "patchworks.env.elasticSearchCloudId" . }}
{{ include "patchworks.env.elasticSearchCloudApiKey" . }}
{{ include "patchworks.env.elasticSearchApiKey" . }}
{{ include "patchworks.env.elasticSearchUsername" . }}
{{ include "patchworks.env.elasticSearchPassword" . }}
{{- $s3Secret := fromJson (include "patchworks.s3.existingSecret" .) }}
{{- if $s3Secret.name }}
- name: AWS_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ $s3Secret.name }}
      key: {{ $s3Secret.accessKeyKey }}
- name: AWS_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ $s3Secret.name }}
      key: {{ $s3Secret.secretKeyKey }}
- name: TENANT_CACHE_AWS_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ $s3Secret.name }}
      key: {{ $s3Secret.accessKeyKey }}
- name: TENANT_CACHE_AWS_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ $s3Secret.name }}
      key: {{ $s3Secret.secretKeyKey }}
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
{{- $appKeySecret := fromJson (include "patchworks.appKeySecret" .) }}
{{ include "patchworks.secretEnv" (dict "name" "APP_KEY" "value" .Values.app.key "secret" $appKeySecret) }}
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
{{- $landlordDbSecret := fromJson (include "patchworks.mysql.existingSecret" .) }}
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
- name: ELASTIC_SEARCH_HOSTS
  value: {{ include "patchworks.elasticsearch.url" . | quote }}
- name: ELASTICSEARCH_HOST
  value: {{ include "patchworks.elasticsearch.url" . | quote }}
{{ include "patchworks.env.elasticSearchCloudId" . }}
{{ include "patchworks.env.elasticSearchCloudApiKey" . }}
{{ include "patchworks.env.elasticSearchApiKey" . }}
{{ include "patchworks.env.elasticSearchUsername" . }}
{{ include "patchworks.env.elasticSearchPassword" . }}
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
{{- $tenantDbSecret := fromJson (include "patchworks.mysql.existingSecret" .) }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_DB_PASSWORD" "value" (include "patchworks.mysql.password" .) "secret" (dict "name" $tenantDbSecret.name "key" $tenantDbSecret.passwordKey)) }}
- name: FABRIC_DB_CONNECTION
  value: fabric
- name: FABRIC_DB_HOST
  value: {{ include "patchworks.fabric.mysql.host" . | quote }}
- name: FABRIC_DB_PORT
  value: {{ include "patchworks.fabric.mysql.port" . | quote }}
- name: FABRIC_DB_DATABASE
  value: {{ include "patchworks.fabric.mysql.database" . | quote }}
- name: FABRIC_DB_USERNAME
  value: {{ include "patchworks.fabric.mysql.username" . | quote }}
{{ include "patchworks.env.fabricDbPassword" . }}
{{- if eq .Values.workers.type "mono" }}
- name: MONOCORE_URL
  value: {{ include "patchworks.monocore.url" . | quote }}
- name: MONOCORE_TIMEOUT
  value: {{ .Values.monocore.timeout | quote }}
{{- end }}
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
{{- $s3Secret := fromJson (include "patchworks.s3.existingSecret" .) }}
{{- $s3AccessSecret := dict "name" $s3Secret.name "key" $s3Secret.accessKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s3AccessSecret) }}
{{- $s3SecretSecret := dict "name" $s3Secret.name "key" $s3Secret.secretKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s3SecretSecret) }}
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
{{- $appKeySecret := fromJson (include "patchworks.appKeySecret" .) }}
{{ include "patchworks.secretEnv" (dict "name" "APP_KEY" "value" .Values.app.key "secret" $appKeySecret) }}
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
- name: ELASTIC_SEARCH_HOSTS
  value: {{ include "patchworks.elasticsearch.url" . | quote }}
- name: ELASTICSEARCH_HOST
  value: {{ include "patchworks.elasticsearch.url" . | quote }}
{{ include "patchworks.env.elasticSearchCloudId" . }}
{{ include "patchworks.env.elasticSearchCloudApiKey" . }}
{{ include "patchworks.env.elasticSearchApiKey" . }}
{{ include "patchworks.env.elasticSearchUsername" . }}
{{ include "patchworks.env.elasticSearchPassword" . }}
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
{{- $s3Secret := fromJson (include "patchworks.s3.existingSecret" .) }}
{{- $s3AccessSecret := dict "name" $s3Secret.name "key" $s3Secret.accessKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_ACCESS_KEY_ID" "value" (include "patchworks.s3.accessKey" .) "secret" $s3AccessSecret) }}
{{- $s3SecretSecret := dict "name" $s3Secret.name "key" $s3Secret.secretKeyKey }}
{{ include "patchworks.secretEnv" (dict "name" "TENANT_CACHE_AWS_SECRET_ACCESS_KEY" "value" (include "patchworks.s3.secretKey" .) "secret" $s3SecretSecret) }}
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

{{/* Fabric web session env only. Keep this out of fabric-init/seeder jobs. */}}
{{- define "patchworks.fabricRuntimeEnv" -}}
- name: SESSION_DRIVER
  value: {{ .Values.fabric.session.driver | quote }}
- name: SESSION_LIFETIME
  value: {{ .Values.fabric.session.lifetime | quote }}
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
      {{- if .Values.s3.enabled }}
      echo "Waiting for S3..."
      until nc -z {{ include "patchworks.s3.host" . }} 9000; do sleep 2; done
      {{- end }}
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
