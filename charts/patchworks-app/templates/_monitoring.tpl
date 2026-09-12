{{/*
A monitor stays in its target Service namespace, so auth Secret references resolve
locally. Call from the owning component only after its workload gate has passed.
*/}}
{{- define "patchworks.serviceMonitor" -}}
{{- $root := .root -}}
{{- $monitor := .options -}}
{{- if $monitor.enabled }}
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: {{ .name }}
  namespace: {{ .namespace }}
  labels:
    {{- $labels := mergeOverwrite (fromYaml (include "patchworks.labels" $root)) (dict "app.kubernetes.io/component" .component) ($monitor.additionalLabels | default (dict)) }}
    {{- toYaml $labels | nindent 4 }}
spec:
  namespaceSelector:
    matchNames:
      - {{ .namespace | quote }}
  selector:
    matchLabels:
      app.kubernetes.io/instance: {{ $root.Release.Name | quote }}
      app.kubernetes.io/component: {{ .component | quote }}
  endpoints:
    - port: {{ .port }}
      path: {{ $monitor.path | quote }}
      interval: {{ $monitor.interval }}
      scrapeTimeout: {{ $monitor.scrapeTimeout }}
      honorLabels: {{ $monitor.honorLabels }}
      {{- with .basicAuth }}
      basicAuth:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with $monitor.relabelings }}
      relabelings:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with $monitor.metricRelabelings }}
      metricRelabelings:
        {{- toYaml . | nindent 8 }}
      {{- end }}
{{- end }}
{{- end }}
