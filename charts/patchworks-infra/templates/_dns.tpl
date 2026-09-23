{{/*
Render optional pod DNS configuration from scopes ordered least to most specific.
Missing, null and empty settings inherit; zero is an explicit ndots value.
Keep this helper identical in the app and infra charts.
*/}}
{{- define "patchworks.podDnsConfig" -}}
{{- $ndots := "" -}}
{{- range . -}}
  {{- if and (hasKey . "ndots") (ne .ndots nil) (ne (toString .ndots) "") -}}
    {{- $ndots = toString .ndots -}}
  {{- end -}}
{{- end -}}
{{- with $ndots -}}
dnsConfig:
  options:
    - name: ndots
      value: {{ . | quote }}
{{- end -}}
{{- end -}}
