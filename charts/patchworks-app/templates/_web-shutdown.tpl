{{/* RoadRunner must finish draining before Kubernetes signals its Octane parent. */}}
{{- define "patchworks.webTerminationGracePeriod" -}}
{{- if ne (include "patchworks.usesFrankenphp" .) "true" -}}
{{- $svc := .svc -}}
{{- range $value := list $svc.preStopSleepSeconds $svc.roadrunner.drainTimeoutSeconds $svc.terminationGracePeriodSeconds -}}
{{- if not (regexMatch "^[0-9]+$" (toString $value)) -}}
{{- fail "web shutdown timeouts must be non-negative integer seconds" -}}
{{- end -}}
{{- end -}}
{{- if or (lt (int $svc.roadrunner.drainTimeoutSeconds) 1) (le (int $svc.terminationGracePeriodSeconds) (add (int $svc.preStopSleepSeconds) (int $svc.roadrunner.drainTimeoutSeconds))) -}}
{{- fail "web.terminationGracePeriodSeconds must exceed preStopSleepSeconds + roadrunner.drainTimeoutSeconds, and the drain timeout must be positive" -}}
{{- end -}}
{{- if not (hasPrefix "/" $svc.roadrunner.stateFile) -}}
{{- fail "web.roadrunner.stateFile must be an absolute path to Octane's state file" -}}
{{- end -}}
terminationGracePeriodSeconds: {{ $svc.terminationGracePeriodSeconds }}
{{- end -}}
{{- end }}

{{- define "patchworks.webPreStop" -}}
{{- if ne (include "patchworks.usesFrankenphp" .) "true" -}}
lifecycle:
  preStop:
    exec:
      command:
        - /usr/local/bin/php
        - -r
        - |
          {{- .root.Files.Get "files/roadrunner-drain.php" | trimPrefix "<?php" | trim | nindent 10 }}
        - {{ .svc.preStopSleepSeconds | quote }}
        - {{ .svc.roadrunner.drainTimeoutSeconds | quote }}
        - {{ .svc.roadrunner.stateFile | quote }}
{{- end -}}
{{- end }}
