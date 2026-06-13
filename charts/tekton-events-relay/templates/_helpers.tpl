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
