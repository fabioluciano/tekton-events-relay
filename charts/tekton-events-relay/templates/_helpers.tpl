{{/* vim: set filetype=mustache: */}}

{{/*
Expand the name of the chart.
*/}}
{{- define "tekton-events-relay.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "tekton-events-relay.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "tekton-events-relay.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "tekton-events-relay.labels" -}}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/name: {{ include "tekton-events-relay.name" . }}
app.kubernetes.io/part-of: {{ include "tekton-events-relay.name" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{/* helm.sh/chart format: "chartname-version". Using include helper ensures consistent formatting and truncation. */}}
helm.sh/chart: {{ include "tekton-events-relay.chart" . }}
{{- if .Values.commonLabels }}
{{ toYaml .Values.commonLabels }}
{{- end }}
{{- end }}

{{/*
Merge common labels with optional extra labels.
This helper includes all standard labels plus any additional labels passed via the optional parameter.
If no extra labels are provided, only standard labels are returned (no output difference).
Usage: {{ include "tekton-events-relay.optionalLabels" (dict "context" . "extraLabels" .Values.customLabels) }}
*/}}
{{- define "tekton-events-relay.optionalLabels" -}}
{{- include "tekton-events-relay.labels" .context }}
{{- if .extraLabels }}
{{ toYaml .extraLabels | indent 0 }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "tekton-events-relay.selectorLabels" -}}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/name: {{ include "tekton-events-relay.name" . }}
{{- end }}

{{/*
Common annotations
*/}}
{{- define "tekton-events-relay.annotations" -}}
{{- if .Values.commonAnnotations }}
{{ toYaml .Values.commonAnnotations }}
{{- end }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "tekton-events-relay.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "tekton-events-relay.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the secret to use
*/}}
{{- define "tekton-events-relay.secretName" -}}
{{- include "tekton-events-relay.fullname" . }}-tokens
{{- end }}

{{/*
Default template key mapping based on action type and provider.
Returns the default ConfigMap key for a given action type.
Usage: {{ include "tekton-events-relay.defaultTemplateKey" (dict "type" "pr_comment" "provider" "github") }}
*/}}
{{- define "tekton-events-relay.defaultTemplateKey" -}}
{{- $type := .type -}}
{{- $provider := .provider -}}
{{- if eq $type "pr_comment" -}}
{{- if eq $provider "gitlab" -}}
gitlab-note.tmpl
{{- else -}}
{{- printf "%s-pr-comment.tmpl" $provider -}}
{{- end -}}
{{- else if eq $type "issue_comment" -}}
{{- printf "%s-issue-comment.tmpl" $provider -}}
{{- else if eq $type "discussion_comment" -}}
{{- printf "%s-discussion-comment.tmpl" $provider -}}
{{- else if eq $type "check_run" -}}
{{- printf "%s-checkrun.tmpl" $provider -}}
{{- else if eq $type "commit_status" -}}
{{- printf "%s-commit-status.tmpl" $provider -}}
{{- else if eq $type "deployment_status" -}}
{{- printf "%s-deployment-status.tmpl" $provider -}}
{{- else if eq $type "label" -}}
{{- printf "%s-label.tmpl" $provider -}}
{{- else if eq $type "slack" -}}
slack-default.tmpl
{{- else if eq $type "teams" -}}
teams-default.tmpl
{{- else if eq $type "discord" -}}
discord-default.tmpl
{{- else if eq $type "pagerduty" -}}
pagerduty-default.tmpl
{{- else if eq $type "datadog" -}}
datadog-default.tmpl
{{- else if eq $type "accumulator" -}}
accumulator-default.tmpl
{{- else if eq $type "webhook" -}}
webhook-default.tmpl
{{- else -}}
{{- printf "%s-%s.tmpl" $provider $type -}}
{{- end -}}
{{- end }}

{{/*
Fail fast when the embedded Valkey subchart is enabled but the store backend
is not set to "valkey". The embedded instance would be deployed but unused.
*/}}
{{- define "tekton-events-relay.validateEmbeddedValkey" -}}
{{- if and (index .Values "config" "store" "valkey" "embedded" "enabled") (ne (.Values.config.store.backend | default "memory") "valkey") -}}
{{- fail "config.store.valkey.embedded.enabled=true requires config.store.backend=valkey" -}}
{{- end -}}
{{- end -}}

{{/*
Fail fast when the per-pod memory store is combined with multiple replicas:
deduplication and accumulator state would diverge between pods, producing
duplicate or fragmented notifications. Set config.store.backend to valkey
or olric before scaling out, or acknowledge the risk explicitly with
unsafe.allowMemoryStoreWithMultipleReplicas=true.
*/}}
{{- define "tekton-events-relay.validateStore" -}}
{{- $multi := or (gt (int .Values.replicaCount) 1) .Values.autoscaling.enabled -}}
{{- $memory := eq (.Values.config.store.backend | default "memory") "memory" -}}
{{- if and $multi $memory (not .Values.unsafe.allowMemoryStoreWithMultipleReplicas) -}}
{{- fail (printf "tekton-events-relay: replicaCount=%d / autoscaling.enabled=%t with config.store.backend=memory is unsafe: dedupe and accumulator state are per-pod, causing duplicate or fragmented notifications. Set config.store.backend to 'valkey' or 'olric', reduce replicas to 1, or set unsafe.allowMemoryStoreWithMultipleReplicas=true to override." (int .Values.replicaCount) .Values.autoscaling.enabled) -}}
{{- end -}}
{{- end -}}

{{/*
=============================================================================
  value/secretRef/configmapRef HELPERS
=============================================================================
*/}}

{{/*
Resolve a value/secretRef/configmapRef field to an inline string for config rendering.
- If .value is set, returns the inline value directly.
- If .secretRef is set, returns the mount path (/etc/secrets/{provider}/{instance}/{key}).
- If .configmapRef is set, returns the mount path (/etc/secrets/{provider}/{instance}/{key}).
Usage: {{ include "tekton-events-relay.resolveValue" (dict "field" .auth "provider" "github" "instance" .name "key" "token") }}
       where .auth has .secretRef or .value
*/}}
{{- define "tekton-events-relay.resolveValue" -}}
{{- if .field.value }}
{{- .field.value | quote }}
{{- else if .field.secretRef }}
{{- printf "/etc/secrets/%s/%s/%s" .provider .instance .key | quote }}
{{- else if .field.configmapRef }}
{{- printf "/etc/secrets/%s/%s/%s" .provider .instance .key | quote }}
{{- end }}
{{- end }}

{{/*
Resolve a value/secretRef/configmapRef field to an inline string with a custom mount base.
Usage: {{ include "tekton-events-relay.resolveValueWithBase" (dict "field" .auth "base" "/etc/github-app" "instance" .name "key" "private-key.pem") }}
*/}}
{{- define "tekton-events-relay.resolveValueWithBase" -}}
{{- if .field.value }}
{{- .field.value | quote }}
{{- else if .field.secretRef }}
{{- printf "%s/%s/%s" .base .instance .key | quote }}
{{- else if .field.configmapRef }}
{{- printf "%s/%s/%s" .base .instance .key | quote }}
{{- end }}
{{- end }}

{{/*
Check if a field has a secretRef or configmapRef that requires a volume mount.
Returns "true" if the field has a reference.
Usage: {{ include "tekton-events-relay.hasRef" .auth.secretRef }}
*/}}
{{- define "tekton-events-relay.hasRef" -}}
{{- if or .secretRef .configmapRef -}}true{{- end -}}
{{- end }}

{{/*
Extract the Secret/ConfigMap name from a secretRef or configmapRef field.
Usage: {{ include "tekton-events-relay.refName" .auth.secretRef }}
*/}}
{{- define "tekton-events-relay.refName" -}}
{{- if .secretRef }}
{{- .secretRef.name }}
{{- else if .configmapRef }}
{{- .configmapRef.name }}
{{- end }}
{{- end }}

{{/*
Extract the key within the Secret/ConfigMap from a secretRef or configmapRef field.
Usage: {{ include "tekton-events-relay.refKey" .auth.secretRef }}
*/}}
{{- define "tekton-events-relay.refKey" -}}
{{- if .secretRef }}
{{- .secretRef.key }}
{{- else if .configmapRef }}
{{- .configmapRef.key }}
{{- end }}
{{- end }}

{{/*
Check if the source is a secretRef (vs configmapRef).
Returns "true" if secretRef.
Usage: {{ include "tekton-events-relay.isSecretRef" .auth.secretRef }}
*/}}
{{- define "tekton-events-relay.isSecretRef" -}}
{{- if .secretRef -}}true{{- end -}}
{{- end }}

{{/*
Render a volume from a secretRef or configmapRef field.
Usage: {{ include "tekton-events-relay.valueFromVolume" (dict "name" "vol-name" "field" .auth.secretRef) }}
*/}}
{{- define "tekton-events-relay.valueFromVolume" -}}
{{- if .field.secretRef }}
- name: {{ .name }}
  secret:
    secretName: {{ .field.secretRef.name }}
{{- else if .field.configmapRef }}
- name: {{ .name }}
  configMap:
    name: {{ .field.configmapRef.name }}
{{- end }}
{{- end }}

{{/*
Render a volume from a secretRef or configmapRef field with items (specific key).
Usage: {{ include "tekton-events-relay.valueFromVolumeWithKey" (dict "name" "vol-name" "field" .auth.private_key "path" "private-key.pem") }}
*/}}
{{- define "tekton-events-relay.valueFromVolumeWithKey" -}}
{{- if .field.secretRef }}
- name: {{ .name }}
  secret:
    secretName: {{ .field.secretRef.name }}
    items:
      - key: {{ .field.secretRef.key }}
        path: {{ .path }}
{{- else if .field.configmapRef }}
- name: {{ .name }}
  configMap:
    name: {{ .field.configmapRef.name }}
    items:
      - key: {{ .field.configmapRef.key }}
        path: {{ .path }}
{{- end }}
{{- end }}

{{/*
Render volume mounts for an SCM provider instance.
Checks each potential secretRef/configmapRef field and emits mounts as needed.
Usage: {{ include "tekton-events-relay.scmVolumeMounts" (dict "provider" "github" "instance" $inst) }}
*/}}
{{- define "tekton-events-relay.scmVolumeMounts" -}}
{{- $p := .provider }}
{{- $i := .instance }}
{{- $mountPaths := dict }}
{{- if and $i.auth (include "tekton-events-relay.hasRef" $i.auth) }}
{{- $secretName := include "tekton-events-relay.refName" $i.auth }}
{{- $mountPath := printf "/etc/secrets/%s/%s" $p $i.name }}
{{- if not (hasKey $mountPaths $mountPath) }}
{{- $_ := set $mountPaths $mountPath true }}
- mountPath: {{ $mountPath }}
  name: {{ $secretName | replace "/" "-" | replace "." "-" }}
  readOnly: true
{{- end }}
{{- end }}
{{- if and $i.auth $i.auth.private_key (include "tekton-events-relay.hasRef" $i.auth.private_key) }}
- mountPath: /etc/github-app/{{ $i.name }}
  name: {{ $p }}-{{ $i.name }}-app-key
  readOnly: true
{{- end }}
{{- if and $i.auth $i.auth.oauth2 }}
{{- $secretName := include "tekton-events-relay.refName" $i.auth.oauth2.client_secret }}
{{- $mountPath := printf "/etc/secrets/%s/%s" $p $i.name }}
{{- if not (hasKey $mountPaths $mountPath) }}
{{- $_ := set $mountPaths $mountPath true }}
- mountPath: {{ $mountPath }}
  name: {{ $secretName | replace "/" "-" | replace "." "-" }}
  readOnly: true
{{- end }}
{{- end }}
{{- if and $i.auth $i.auth.username (include "tekton-events-relay.hasRef" $i.auth.username) }}
{{- $secretName := include "tekton-events-relay.refName" $i.auth.username }}
{{- $mountPath := printf "/etc/secrets/%s/%s" $p $i.name }}
{{- if not (hasKey $mountPaths $mountPath) }}
{{- $_ := set $mountPaths $mountPath true }}
- mountPath: {{ $mountPath }}
  name: {{ $secretName | replace "/" "-" | replace "." "-" }}
  readOnly: true
{{- end }}
{{- end }}
{{- if and $i.auth $i.auth.app_password (include "tekton-events-relay.hasRef" $i.auth.app_password) }}
{{- $secretName := include "tekton-events-relay.refName" $i.auth.app_password }}
{{- $mountPath := printf "/etc/secrets/%s/%s" $p $i.name }}
{{- if not (hasKey $mountPaths $mountPath) }}
{{- $_ := set $mountPaths $mountPath true }}
- mountPath: {{ $mountPath }}
  name: {{ $secretName | replace "/" "-" | replace "." "-" }}
  readOnly: true
{{- end }}
{{- end }}
{{- end }}

{{/*
Render volumes for an SCM provider instance.
Usage: {{ include "tekton-events-relay.scmVolumes" (dict "provider" "github" "instance" $inst) }}
*/}}
{{- define "tekton-events-relay.scmVolumes" -}}
{{- $p := .provider }}
{{- $i := .instance }}
{{- $volumeNames := dict }}
{{- if and $i.auth (include "tekton-events-relay.hasRef" $i.auth) }}
{{- $secretName := include "tekton-events-relay.refName" $i.auth }}
{{- if not (hasKey $volumeNames $secretName) }}
{{- $_ := set $volumeNames $secretName true }}
- name: {{ $secretName | replace "/" "-" | replace "." "-" }}
  secret:
    secretName: {{ $secretName }}
{{- end }}
{{- end }}
{{- if and $i.auth $i.auth.private_key (include "tekton-events-relay.hasRef" $i.auth.private_key) }}
{{- include "tekton-events-relay.valueFromVolumeWithKey" (dict "name" (printf "%s-%s-app-key" $p $i.name) "field" $i.auth.private_key "path" "private-key.pem") }}
{{- end }}
{{- if and $i.auth $i.auth.oauth2 }}
{{- $secretName := include "tekton-events-relay.refName" $i.auth.oauth2.client_secret }}
{{- if not (hasKey $volumeNames $secretName) }}
{{- $_ := set $volumeNames $secretName true }}
- name: {{ $secretName | replace "/" "-" | replace "." "-" }}
  secret:
    secretName: {{ $secretName }}
{{- end }}
{{- end }}
{{- if and $i.auth $i.auth.username (include "tekton-events-relay.hasRef" $i.auth.username) }}
{{- $secretName := include "tekton-events-relay.refName" $i.auth.username }}
{{- if not (hasKey $volumeNames $secretName) }}
{{- $_ := set $volumeNames $secretName true }}
- name: {{ $secretName | replace "/" "-" | replace "." "-" }}
  secret:
    secretName: {{ $secretName }}
{{- end }}
{{- end }}
{{- if and $i.auth $i.auth.app_password (include "tekton-events-relay.hasRef" $i.auth.app_password) }}
{{- $secretName := include "tekton-events-relay.refName" $i.auth.app_password }}
{{- if not (hasKey $volumeNames $secretName) }}
{{- $_ := set $volumeNames $secretName true }}
- name: {{ $secretName | replace "/" "-" | replace "." "-" }}
  secret:
    secretName: {{ $secretName }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Render volume mounts for a notifier instance.
Usage: {{ include "tekton-events-relay.notifierVolumeMounts" (dict "provider" "slack" "instance" $inst) }}
*/}}
{{- define "tekton-events-relay.notifierVolumeMounts" -}}
{{- $p := .provider }}
{{- $i := .instance }}
{{- if and $i.webhook_url (include "tekton-events-relay.hasRef" $i.webhook_url) }}
- mountPath: /etc/secrets/{{ $p }}/{{ $i.name }}
  name: {{ $p }}-{{ $i.name }}-secret
  readOnly: true
{{- else if and $i.token (include "tekton-events-relay.hasRef" $i.token) }}
- mountPath: /etc/secrets/{{ $p }}/{{ $i.name }}
  name: {{ $p }}-{{ $i.name }}-secret
  readOnly: true
{{- else if and $i.integration_key (include "tekton-events-relay.hasRef" $i.integration_key) }}
- mountPath: /etc/secrets/{{ $p }}/{{ $i.name }}
  name: {{ $p }}-{{ $i.name }}-secret
  readOnly: true
{{- else if and $i.api_key (include "tekton-events-relay.hasRef" $i.api_key) }}
- mountPath: /etc/secrets/{{ $p }}/{{ $i.name }}
  name: {{ $p }}-{{ $i.name }}-secret
  readOnly: true
{{- else if and $i.password (include "tekton-events-relay.hasRef" $i.password) }}
- mountPath: /etc/secrets/{{ $p }}/{{ $i.name }}
  name: {{ $p }}-{{ $i.name }}-secret
  readOnly: true
{{- else if and $i.url (include "tekton-events-relay.hasRef" $i.url) }}
- mountPath: /etc/secrets/{{ $p }}/{{ $i.name }}
  name: {{ $p }}-{{ $i.name }}-secret
  readOnly: true
{{- else if and $i.bot_token $i.bot_token.token (include "tekton-events-relay.hasRef" $i.bot_token.token) }}
- mountPath: /etc/secrets/{{ $p }}/{{ $i.name }}
  name: {{ $p }}-{{ $i.name }}-secret
  readOnly: true
{{- else if and $i.auth $i.auth.token (include "tekton-events-relay.hasRef" $i.auth.token) }}
- mountPath: /etc/secrets/{{ $p }}/{{ $i.name }}
  name: {{ $p }}-{{ $i.name }}-secret
  readOnly: true
{{- else if and $i.auth $i.auth.password (include "tekton-events-relay.hasRef" $i.auth.password) }}
- mountPath: /etc/secrets/{{ $p }}/{{ $i.name }}
  name: {{ $p }}-{{ $i.name }}-secret
  readOnly: true
{{- else if and $i.auth $i.auth.hmac_secret (include "tekton-events-relay.hasRef" $i.auth.hmac_secret) }}
- mountPath: /etc/secrets/{{ $p }}/{{ $i.name }}
  name: {{ $p }}-{{ $i.name }}-secret
  readOnly: true
{{- else if and $i.auth $i.auth.username (include "tekton-events-relay.hasRef" $i.auth.username) }}
- mountPath: /etc/secrets/{{ $p }}/{{ $i.name }}
  name: {{ $p }}-{{ $i.name }}-secret
  readOnly: true
{{- end }}
{{- end }}

{{/*
Render volumes for a notifier instance.
Usage: {{ include "tekton-events-relay.notifierVolumes" (dict "provider" "slack" "instance" $inst) }}
*/}}
{{- define "tekton-events-relay.notifierVolumes" -}}
{{- $p := .provider }}
{{- $i := .instance }}
{{- if and $i.webhook_url (include "tekton-events-relay.hasRef" $i.webhook_url) }}
{{- include "tekton-events-relay.valueFromVolume" (dict "name" (printf "%s-%s-secret" $p $i.name) "field" $i.webhook_url) }}
{{- else if and $i.token (include "tekton-events-relay.hasRef" $i.token) }}
{{- include "tekton-events-relay.valueFromVolume" (dict "name" (printf "%s-%s-secret" $p $i.name) "field" $i.token) }}
{{- else if and $i.integration_key (include "tekton-events-relay.hasRef" $i.integration_key) }}
{{- include "tekton-events-relay.valueFromVolume" (dict "name" (printf "%s-%s-secret" $p $i.name) "field" $i.integration_key) }}
{{- else if and $i.api_key (include "tekton-events-relay.hasRef" $i.api_key) }}
{{- include "tekton-events-relay.valueFromVolume" (dict "name" (printf "%s-%s-secret" $p $i.name) "field" $i.api_key) }}
{{- else if and $i.password (include "tekton-events-relay.hasRef" $i.password) }}
{{- include "tekton-events-relay.valueFromVolume" (dict "name" (printf "%s-%s-secret" $p $i.name) "field" $i.password) }}
{{- else if and $i.url (include "tekton-events-relay.hasRef" $i.url) }}
{{- include "tekton-events-relay.valueFromVolume" (dict "name" (printf "%s-%s-secret" $p $i.name) "field" $i.url) }}
{{- else if and $i.bot_token $i.bot_token.token (include "tekton-events-relay.hasRef" $i.bot_token.token) }}
{{- include "tekton-events-relay.valueFromVolume" (dict "name" (printf "%s-%s-secret" $p $i.name) "field" $i.bot_token.token) }}
{{- else if and $i.auth $i.auth.token (include "tekton-events-relay.hasRef" $i.auth.token) }}
{{- include "tekton-events-relay.valueFromVolume" (dict "name" (printf "%s-%s-secret" $p $i.name) "field" $i.auth.token) }}
{{- else if and $i.auth $i.auth.password (include "tekton-events-relay.hasRef" $i.auth.password) }}
{{- include "tekton-events-relay.valueFromVolume" (dict "name" (printf "%s-%s-secret" $p $i.name) "field" $i.auth.password) }}
{{- else if and $i.auth $i.auth.hmac_secret (include "tekton-events-relay.hasRef" $i.auth.hmac_secret) }}
{{- include "tekton-events-relay.valueFromVolume" (dict "name" (printf "%s-%s-secret" $p $i.name) "field" $i.auth.hmac_secret) }}
{{- else if and $i.auth $i.auth.username (include "tekton-events-relay.hasRef" $i.auth.username) }}
{{- include "tekton-events-relay.valueFromVolume" (dict "name" (printf "%s-%s-secret" $p $i.name) "field" $i.auth.username) }}
{{- end }}
{{- end }}
