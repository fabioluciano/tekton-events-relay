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
