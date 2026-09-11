{{/*
Managed schema names are interpolated into a SQL heredoc. Keep the supported
names deliberately small, and escape underscores in GRANT database patterns
so privileges on tenant_a do not also match tenantXa.
*/}}
{{- define "patchworks.mysql.setupDatabase" -}}
{{- if not (kindIs "string" .) -}}
{{- fail "mysql.databases entries and managed MySQL database names must be strings" -}}
{{- end -}}
{{- if not (regexMatch "^[A-Za-z0-9_-]{1,64}$" .) -}}
{{- fail (printf "managed MySQL database name %q must contain 1-64 letters, digits, underscores, or hyphens" .) -}}
{{- end -}}
{{- . -}}
{{- end -}}

{{/* SQL strings use doubled quotes with NO_BACKSLASH_ESCAPES set by the job. */}}
{{- define "patchworks.mysql.setupUsername" -}}
{{- $username := include "patchworks.mysql.username" . -}}
{{- if or (empty $username) (regexMatch "[\r\n\x00]" $username) -}}
{{- fail "managed MySQL username must be nonempty and must not contain NUL or line breaks" -}}
{{- end -}}
{{- $username | replace "'" "''" -}}
{{- end -}}
