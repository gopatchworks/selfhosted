{{/* http-level settings for the Nginx sidecar in front of Fabric's PHP-FPM, empty
unless configured so the shipped site.conf and Nginx's own defaults apply. */}}
{{- define "patchworks.fabricNginxTimeouts" -}}
{{- $seconds := dig "nginx" "fastcgiReadTimeoutSeconds" 0 .Values.fabric -}}
{{- if not (regexMatch "^[0-9]+$" (toString $seconds)) -}}
{{- fail "fabric.nginx.fastcgiReadTimeoutSeconds must be non-negative integer seconds" -}}
{{- end -}}
{{- if gt (int $seconds) 0 -}}
fastcgi_read_timeout {{ int $seconds }}s;
{{- end -}}
{{- end }}
