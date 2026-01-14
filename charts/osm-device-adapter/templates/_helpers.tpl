{{/*
Expand the name of the chart.
*/}}
{{- define "osm-device-adapter.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "osm-device-adapter.fullname" -}}
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
{{- define "osm-device-adapter.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "osm-device-adapter.labels" -}}
helm.sh/chart: {{ include "osm-device-adapter.chart" . }}
{{ include "osm-device-adapter.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "osm-device-adapter.selectorLabels" -}}
app.kubernetes.io/name: {{ include "osm-device-adapter.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "osm-device-adapter.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "osm-device-adapter.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Get the secret name for OSM credentials
*/}}
{{- define "osm-device-adapter.secretName" -}}
{{- if .Values.osm.existingSecret }}
{{- .Values.osm.existingSecret }}
{{- else }}
{{- include "osm-device-adapter.fullname" . }}
{{- end }}
{{- end }}

{{/*
Get the database secret name
*/}}
{{- define "osm-device-adapter.databaseSecretName" -}}
{{- if .Values.database.existingSecret }}
{{- .Values.database.existingSecret }}
{{- else }}
{{- include "osm-device-adapter.fullname" . }}
{{- end }}
{{- end }}

{{/*
Get the Redis secret name
*/}}
{{- define "osm-device-adapter.redisSecretName" -}}
{{- if .Values.redis.existingSecret }}
{{- .Values.redis.existingSecret }}
{{- else }}
{{- include "osm-device-adapter.fullname" . }}
{{- end }}
{{- end }}
