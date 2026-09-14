{{/* Resolve one default connection while retaining legacy mysql.* defaults.
An explicit inline password (including "") replaces an inherited Secret.
*/}}
{{- define "patchworks.database.connection" -}}
{{- $root := .root -}}
{{- $cfg := index ($root.Values.database | default dict) .role | default dict -}}
{{- $secret := fromJson (include "patchworks.mysql.existingSecret" $root) -}}
{{- $password := include "patchworks.mysql.password" $root -}}
{{- if and (hasKey $cfg "password") (ne $cfg.password nil) -}}
  {{- $password = toString $cfg.password -}}
  {{- $secret = dict "name" "" "passwordKey" "password" -}}
{{- end -}}
{{- if dig "existingSecret" "name" "" $cfg -}}
  {{- $secret = dict "name" $cfg.existingSecret.name "passwordKey" ($cfg.existingSecret.passwordKey | default "password") -}}
{{- end -}}
{{- $database := $cfg.database | default (include "patchworks.mysql.database" $root) -}}
{{- if eq .role "tenant" -}}{{- $database = "information_schema" -}}{{- end -}}
{{- $port := $cfg.port | default (include "patchworks.mysql.port" $root) | toString -}}
{{- if or (not (regexMatch "^[0-9]+$" $port)) (lt (int $port) 1) (gt (int $port) 65535) -}}
  {{- fail (printf "database.%s.port must resolve to a port between 1 and 65535" .role) -}}
{{- end -}}
{{- dict "host" ($cfg.host | default (include "patchworks.mysql.host" $root)) "port" $port "database" $database "username" ($cfg.username | default (include "patchworks.mysql.username" $root)) "password" $password "existingSecret" $secret "readHost" ($cfg.readHost | default "") | toJson -}}
{{- end -}}

{{/* Assigned tenant servers are keyed by Fabric database_servers.credential_id.
Their credentials are independent of the default connection.
*/}}
{{- define "patchworks.database.servers" -}}
{{- $servers := dict -}}
{{- range $id, $cfg := dig "tenant" "servers" dict (.Values.database | default dict) -}}
  {{- if not (regexMatch "^[A-Za-z0-9_]+$" $id) -}}
    {{- fail "database.tenant.servers keys must contain only letters, numbers, and underscores (Fabric credential IDs)" -}}
  {{- end -}}
  {{- $host := required (printf "database.tenant.servers.%s.host is required" $id) $cfg.host -}}
  {{- $username := required (printf "database.tenant.servers.%s.username is required" $id) $cfg.username -}}
  {{- $secret := $cfg.existingSecret | default dict -}}
  {{- $password := $cfg.password | default "" | toString -}}
  {{- if not $secret.name -}}
    {{- $password = required (printf "database.tenant.servers.%s requires password or existingSecret.name" $id) $password -}}
    {{- if eq $password "0" -}}{{- fail (printf "database.tenant.servers.%s.password cannot be zero" $id) -}}{{- end -}}
  {{- end -}}
  {{- $port := "3306" -}}
  {{- if hasKey $cfg "port" -}}{{- $port = toString $cfg.port -}}{{- end -}}
  {{- if or (eq (toString $host) "0") (eq (toString $username) "0") (not (regexMatch "^[0-9]+$" $port)) (lt (int $port) 1) (gt (int $port) 65535) -}}
    {{- fail (printf "database.tenant.servers.%s requires nonzero host/username and a port between 1 and 65535" $id) -}}
  {{- end -}}
  {{- $_ := set $servers $id (dict "host" $host "port" $port "username" $username "password" $password "readHost" ($cfg.readHost | default "") "existingSecret" (dict "name" ($secret.name | default "") "passwordKey" ($secret.passwordKey | default "password"))) -}}
{{- end -}}
{{- $servers | toJson -}}
{{- end -}}

{{/* Core env families. DB_* remains a compatibility alias for the landlord. */}}
{{- define "patchworks.database.coreConnections" -}}
{{- $landlord := fromJson (include "patchworks.database.connection" (dict "root" . "role" "landlord")) -}}
{{- $tenant := fromJson (include "patchworks.database.connection" (dict "root" . "role" "tenant")) -}}
{{- $connections := list (dict "prefix" "DB" "suffix" "" "connection" "mysql" "config" $landlord) (dict "prefix" "LANDLORD_DB" "suffix" "" "connection" "landlord" "config" $landlord) (dict "prefix" "TENANT_DB" "suffix" "" "connection" "tenant" "config" $tenant) -}}
{{- range $id, $cfg := fromJson (include "patchworks.database.servers" .) -}}
  {{- $connections = append $connections (dict "prefix" "TENANT_DB" "suffix" (printf "_%s" $id) "config" $cfg) -}}
{{- end -}}
{{- $connections | toJson -}}
{{- end -}}

{{- define "patchworks.database.coreConfigData" -}}
{{- $poolSize := dig "tenant" "pool" "maxSizePerServer" nil (.Values.database | default dict) -}}
{{- if not (kindIs "invalid" $poolSize) }}
{{- $poolSizeString := toString $poolSize -}}
{{- if not (regexMatch "^[0-9]+$" $poolSizeString) -}}
  {{- fail "database.tenant.pool.maxSizePerServer must be a non-negative integer" -}}
{{- end }}
DATABASE_POOL_MAX_SIZE: {{ $poolSizeString | quote }}
{{- end }}
{{- with dig "tenant" "primaryServerId" "" (.Values.database | default dict) }}
PRIMARY_TENANT_DATABASE_SERVER_ID: {{ . | quote }}
{{- end }}
{{- range $entry := fromJsonArray (include "patchworks.database.coreConnections" .) }}
{{- $cfg := $entry.config }}
{{- if $entry.connection }}
{{ $entry.prefix }}_CONNECTION: {{ $entry.connection | quote }}
{{- end }}
{{ $entry.prefix }}_HOST{{ $entry.suffix }}: {{ $cfg.host | quote }}
{{ $entry.prefix }}_PORT{{ $entry.suffix }}: {{ $cfg.port | quote }}
{{ $entry.prefix }}_USERNAME{{ $entry.suffix }}: {{ $cfg.username | quote }}
{{- if $cfg.database }}
{{ $entry.prefix }}_DATABASE{{ $entry.suffix }}: {{ $cfg.database | quote }}
{{- end }}
{{- if $cfg.readHost }}
{{ $entry.prefix }}_READ_HOST{{ $entry.suffix }}: {{ $cfg.readHost | quote }}
{{- end }}
{{- end }}
{{- end -}}

{{- define "patchworks.database.coreSecretData" -}}
{{- range $entry := fromJsonArray (include "patchworks.database.coreConnections" .) }}
{{- if not $entry.config.existingSecret.name }}
{{ $entry.prefix }}_PASSWORD{{ $entry.suffix }}: {{ $entry.config.password | quote }}
{{- end }}
{{- end }}
{{- end -}}

{{/* refsOnly selects BYO Secrets for pods loading inline values via envFrom. */}}
{{- define "patchworks.database.coreSecretEnv" -}}
{{- range $entry := fromJsonArray (include "patchworks.database.coreConnections" .root) }}
{{- $cfg := $entry.config }}
{{- if or (not $.refsOnly) $cfg.existingSecret.name }}
{{ include "patchworks.secretEnv" (dict "name" (printf "%s_PASSWORD%s" $entry.prefix $entry.suffix) "value" $cfg.password "secret" (dict "name" $cfg.existingSecret.name "key" $cfg.existingSecret.passwordKey)) }}
{{- end }}
{{- end }}
{{- end -}}

{{- define "patchworks.database.coreEnv" -}}
{{- range $name, $value := fromYaml (include "patchworks.database.coreConfigData" .) }}
- name: {{ $name }}
  value: {{ $value | quote }}
{{- end }}
{{ include "patchworks.database.coreSecretEnv" (dict "root" . "refsOnly" false) }}
{{- end -}}

{{/* Monocore reads DB_TENANT_<id>_FIELD, unlike Core's TENANT_DB_FIELD_<id>.
Keep legacy DSN inputs using Kubernetes password substitution and emit tenant
aliases for binaries predating the canonical DB_TENANT_* naming.
*/}}
{{- define "patchworks.database.monoEnv" -}}
{{- $connections := dict "DB_LANDLORD" (fromJson (include "patchworks.database.connection" (dict "root" . "role" "landlord"))) "DB_TENANT" (fromJson (include "patchworks.database.connection" (dict "root" . "role" "tenant"))) -}}
{{- $_ := set $connections "DB_FABRIC" (dict "host" (include "patchworks.fabric.mysql.host" .) "port" (include "patchworks.fabric.mysql.port" .) "database" (include "patchworks.fabric.mysql.database" .) "username" (include "patchworks.fabric.mysql.username" .) "password" (include "patchworks.fabric.mysql.password" .) "existingSecret" (fromJson (include "patchworks.fabric.mysql.existingSecret" .))) -}}
{{- range $id, $cfg := fromJson (include "patchworks.database.servers" .) -}}
  {{- $_ := set $connections (printf "DB_TENANT_%s" $id) $cfg -}}
{{- end -}}
{{- range $prefix, $cfg := $connections }}
- name: {{ $prefix }}_HOST
  value: {{ $cfg.host | quote }}
- name: {{ $prefix }}_PORT
  value: {{ $cfg.port | quote }}
- name: {{ $prefix }}_USERNAME
  value: {{ $cfg.username | quote }}
{{- if $cfg.database }}
- name: {{ $prefix }}_DATABASE
  value: {{ $cfg.database | quote }}
{{- end }}
{{ include "patchworks.secretEnv" (dict "name" (printf "%s_PASSWORD" $prefix) "value" $cfg.password "secret" (dict "name" $cfg.existingSecret.name "key" $cfg.existingSecret.passwordKey)) }}
{{- if or (eq $prefix "DB_LANDLORD") (eq $prefix "DB_FABRIC") }}
- name: {{ $prefix }}_DSN
  value: {{ printf "%s:$(%s_PASSWORD)@tcp(%s:%s)/%s?parseTime=true" $cfg.username $prefix $cfg.host $cfg.port $cfg.database | quote }}
{{- end }}
{{- end }}
{{- range $entry := fromJsonArray (include "patchworks.database.coreConnections" .) }}
{{- if eq $entry.prefix "TENANT_DB" }}
{{- $cfg := $entry.config }}
- name: TENANT_DB_HOST{{ $entry.suffix }}
  value: {{ $cfg.host | quote }}
- name: TENANT_DB_PORT{{ $entry.suffix }}
  value: {{ $cfg.port | quote }}
- name: TENANT_DB_USERNAME{{ $entry.suffix }}
  value: {{ $cfg.username | quote }}
{{ include "patchworks.secretEnv" (dict "name" (printf "TENANT_DB_PASSWORD%s" $entry.suffix) "value" $cfg.password "secret" (dict "name" $cfg.existingSecret.name "key" $cfg.existingSecret.passwordKey)) }}
{{- end }}
{{- end }}
{{- end -}}

{{/* Wait for the actual landlord and tenant write endpoints, deduplicated. */}}
{{- define "patchworks.database.wait" -}}
{{- $connections := list (fromJson (include "patchworks.database.connection" (dict "root" . "role" "landlord"))) (fromJson (include "patchworks.database.connection" (dict "root" . "role" "tenant"))) -}}
{{- range $cfg := fromJson (include "patchworks.database.servers" .) -}}
{{- $connections = append $connections $cfg -}}
{{- end -}}
{{- $seen := dict -}}
{{- range $cfg := $connections -}}
{{- $address := printf "%s:%s" $cfg.host $cfg.port -}}
{{- if not (hasKey $seen $address) }}
until nc -z '{{ $cfg.host | replace "'" "'\"'\"'" }}' '{{ $cfg.port | replace "'" "'\"'\"'" }}'; do sleep 2; done
{{- $_ := set $seen $address true -}}
{{- end -}}
{{- end -}}
{{- end -}}
