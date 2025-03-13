{{/*
Expand the name of the chart.
*/}}
{{- define "quota-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "quota-operator.fullname" -}}
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
Name of the clusterrole(binding) if in-cluster config is used.
*/}}
{{- define "quota-operator.clusterrole" -}}
{{- print "openmcp.cloud:" ( include "quota-operator.fullname" . ) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Use <image>:<tag> or <image>@<sha256>, depending on which is given.
*/}}
{{- define "image" -}}
{{- if hasPrefix "sha256:" (required "$.tag is required" $.tag) -}}
{{ required "$.repository is required" $.repository }}@{{ required "$.tag is required" $.tag }}
{{- else -}}
{{ required "$.repository is required" $.repository }}:{{ required "$.tag is required" $.tag }}
{{- end -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "quota-operator.labels" -}}
helm.sh/chart-name: {{ .Chart.Name }}
helm.sh/chart-version: {{ .Chart.Version | quote }}
{{ include "quota-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "quota-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "quota-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
