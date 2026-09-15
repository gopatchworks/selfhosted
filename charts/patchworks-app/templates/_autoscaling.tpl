{{/* Render from the same resolved target as the Deployment, including company namespace/queue. */}}
{{- define "patchworks.workerAutoscaler" -}}
{{- $a := .options -}}
{{- if $a.enabled -}}
{{- if not (has $a.provider (list "hpa" "keda")) }}{{ fail "autoscaling.provider must be hpa or keda" }}{{ end -}}
{{- if or (lt (int $a.minReplicas) 0) (lt (int $a.maxReplicas) 1) (gt (int $a.minReplicas) (int $a.maxReplicas)) }}{{ fail "autoscaling requires 0 <= minReplicas <= maxReplicas and maxReplicas >= 1" }}{{ end -}}
{{- $name := printf "%s-autoscaler" .name -}}
{{- if eq $a.provider "hpa" -}}
{{- if lt (int $a.minReplicas) 1 }}{{ fail "HPA minReplicas must be at least 1" }}{{ end -}}
{{- $metrics := list -}}
{{- range $resource := list "cpu" "memory" -}}
{{- $target := index $a.hpa $resource -}}
{{- if gt (int $target) 0 -}}
{{- if not (dig "requests" $resource "" $.resources) }}{{ fail (printf "HPA %s utilization requires workers resource requests.%s" $resource $resource) }}{{ end -}}
{{- $metrics = append $metrics (dict "type" "Resource" "resource" (dict "name" $resource "target" (dict "type" "Utilization" "averageUtilization" (int $target)))) -}}
{{- end -}}
{{- end -}}
{{- if not $metrics }}{{ fail "HPA requires at least one positive hpa.cpu or hpa.memory target" }}{{ end }}
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{ $name }}
  namespace: {{ .namespace }}
  labels:
    {{- include "patchworks.labels" .root | nindent 4 }}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{ .name }}
  minReplicas: {{ $a.minReplicas }}
  maxReplicas: {{ $a.maxReplicas }}
  metrics: {{ toYaml $metrics | nindent 4 }}
  {{- with $a.behavior }}
  behavior: {{ toYaml . | nindent 4 }}
  {{- end }}
{{- else -}}
{{- $k := $a.keda -}}
{{- $r := $k.rabbitmq -}}
{{- $p := $k.prometheus -}}
{{- $triggers := deepCopy $k.extraTriggers -}}
{{- $auth := deepCopy $r.authenticationRef -}}
{{- if or (lt (int $k.pollingInterval) 1) (lt (int $k.cooldownPeriod) 0) }}{{ fail "KEDA pollingInterval must be positive and cooldownPeriod nonnegative" }}{{ end -}}
{{- if and (ne $k.pausedReplicas nil) (lt (int $k.pausedReplicas) 0) }}{{ fail "KEDA pausedReplicas must be nonnegative" }}{{ end -}}
{{- if $r.enabled -}}
{{- if not (has $r.protocol (list "http" "amqp" "auto")) }}{{ fail "RabbitMQ scaler protocol must be http, amqp or auto" }}{{ end -}}
{{- if and (or $r.messageRate.enabled $r.expectedQueueConsumptionTime.enabled) (ne $r.protocol "http") }}{{ fail "RabbitMQ MessageRate and ExpectedQueueConsumptionTime require protocol: http" }}{{ end -}}
{{- if and $r.expectedQueueConsumptionTime.enabled (ne (int $k.pollingInterval) 1) }}{{ fail "RabbitMQ ExpectedQueueConsumptionTime requires keda.pollingInterval: 1" }}{{ end -}}
{{- if and $r.existingSecret.name $auth.name }}{{ fail "Choose rabbitmq.existingSecret or authenticationRef, not both" }}{{ end -}}
{{- if and (not $r.host) (not $auth.name) (not (and $r.existingSecret.name $r.existingSecret.hostKey)) }}{{ fail "RabbitMQ scaler requires host, authenticationRef, or existingSecret.hostKey" }}{{ end -}}
{{- if contains "@" $r.host }}{{ fail "RabbitMQ scaler credentials must use a Secret, not an inline host URL" }}{{ end -}}
{{- if $r.existingSecret.name -}}
{{- $auth = dict "name" (printf "%s-rabbitmq" $name) -}}
{{- $refs := list -}}
{{- range $param := list "host" "username" "password" -}}
{{- $key := index $r.existingSecret (printf "%sKey" $param) -}}
{{- if $key }}{{ $refs = append $refs (dict "parameter" $param "name" $r.existingSecret.name "key" $key) }}{{ end -}}
{{- end -}}
{{- if not $refs }}{{ fail "RabbitMQ existingSecret must specify at least one credential key" }}{{ end }}
---
apiVersion: keda.sh/v1alpha1
kind: TriggerAuthentication
metadata:
  name: {{ $auth.name }}
  namespace: {{ .namespace }}
spec:
  secretTargetRef: {{ toYaml $refs | nindent 4 }}
{{- end -}}
{{- range $item := list (dict "mode" "QueueLength" "config" $r.queueLength) (dict "mode" "MessageRate" "config" $r.messageRate) (dict "mode" "ExpectedQueueConsumptionTime" "metricType" "Value" "config" $r.expectedQueueConsumptionTime) -}}
{{- if $item.config.enabled -}}
{{- if or (le (float64 $item.config.value) 0.0) (lt (float64 $item.config.activationValue) 0.0) }}{{ fail "RabbitMQ target must be positive and activationValue nonnegative" }}{{ end -}}
{{- $metadata := dict "protocol" $r.protocol "mode" $item.mode "value" (toString $item.config.value) "activationValue" (toString $item.config.activationValue) "queueName" ($r.queueName | default $.queue) "vhostName" $r.vhostName "unsafeSsl" (toString $r.unsafeSsl) -}}
{{- if $r.host }}{{ $_ := set $metadata "host" $r.host }}{{ end -}}
{{- $trigger := dict "type" "rabbitmq" "metadata" $metadata -}}
{{- if $item.metricType }}{{ $_ := set $trigger "metricType" $item.metricType }}{{ end -}}
{{- if $auth.name }}{{ $_ := set $trigger "authenticationRef" $auth }}{{ end -}}
{{- $triggers = append $triggers $trigger -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if $p.enabled -}}
{{- $query := required "Prometheus scaler query is required" $p.query -}}
{{- $query = $query | replace "__QUEUE__" (toJson .queue) | replace "__NAMESPACE__" (toJson .namespace) | replace "__DEPLOYMENT__" (toJson .name) | replace "__PROCESSES__" (toString .processes) -}}
{{- if not (has $p.metricType (list "Value" "AverageValue")) }}{{ fail "Prometheus metricType must be Value or AverageValue" }}{{ end -}}
{{- if or (le (float64 $p.threshold) 0.0) (lt (float64 $p.activationThreshold) 0.0) }}{{ fail "Prometheus threshold must be positive and activationThreshold nonnegative" }}{{ end -}}
{{- $metadata := mergeOverwrite (deepCopy $p.metadata) (dict "serverAddress" (required "Prometheus serverAddress is required" $p.serverAddress) "query" $query "threshold" (toString $p.threshold) "activationThreshold" (toString $p.activationThreshold) "ignoreNullValues" (toString $p.ignoreNullValues)) -}}
{{- range $key, $value := $metadata }}{{ $_ := set $metadata $key (toString $value) }}{{ end -}}
{{- $trigger := dict "type" "prometheus" "metricType" $p.metricType "metadata" $metadata -}}
{{- if $p.authenticationRef.name }}{{ $_ := set $trigger "authenticationRef" $p.authenticationRef }}{{ end -}}
{{- $triggers = append $triggers $trigger -}}
{{- end -}}
{{- if not $triggers }}{{ fail "KEDA autoscaling requires at least one enabled trigger" }}{{ end -}}
{{- $eventTrigger := false -}}
{{- range $triggers }}{{ if not (has .type (list "cpu" "memory")) }}{{ $eventTrigger = true }}{{ end }}{{ end -}}
{{- if and (eq (int $a.minReplicas) 0) (not $eventTrigger) }}{{ fail "Scale-to-zero requires a non CPU/memory trigger" }}{{ end }}
---
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: {{ $name }}
  namespace: {{ .namespace }}
  labels:
    {{- include "patchworks.labels" .root | nindent 4 }}
  {{- if ne $k.pausedReplicas nil }}
  annotations:
    autoscaling.keda.sh/paused-replicas: {{ $k.pausedReplicas | quote }}
  {{- end }}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{ .name }}
  minReplicaCount: {{ $a.minReplicas }}
  maxReplicaCount: {{ $a.maxReplicas }}
  pollingInterval: {{ $k.pollingInterval }}
  cooldownPeriod: {{ $k.cooldownPeriod }}
  {{- with $k.fallback }}
  fallback: {{ toYaml . | nindent 4 }}
  {{- end }}
  {{- with $a.behavior }}
  advanced:
    horizontalPodAutoscalerConfig:
      behavior: {{ toYaml . | nindent 8 }}
  {{- end }}
  triggers: {{ toYaml $triggers | nindent 4 }}
{{- end -}}
{{- end -}}
{{- end -}}
