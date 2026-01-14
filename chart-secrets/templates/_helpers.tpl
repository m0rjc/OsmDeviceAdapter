{{/*
Expand the name of the chart.
*/}}
{{- define "osm-device-adapter-secrets.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "osm-device-adapter-secrets.fullname" -}}
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
{{- define "osm-device-adapter-secrets.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "osm-device-adapter-secrets.labels" -}}
helm.sh/chart: {{ include "osm-device-adapter-secrets.chart" . }}
{{ include "osm-device-adapter-secrets.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "osm-device-adapter-secrets.selectorLabels" -}}
app.kubernetes.io/name: {{ include "osm-device-adapter-secrets.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Get the unified secret name
*/}}
{{- define "osm-device-adapter-secrets.unifiedSecretName" -}}
{{- if .Values.unifiedSecretName }}
{{- .Values.unifiedSecretName }}
{{- else }}
{{- .Release.Name }}
{{- end }}
{{- end }}

{{/*
Get the OSM secret name
*/}}
{{- define "osm-device-adapter-secrets.osmSecretName" -}}
{{- .Values.secretNames.osm | default "osm-oauth-credentials" }}
{{- end }}

{{/*
Get the database secret name
*/}}
{{- define "osm-device-adapter-secrets.databaseSecretName" -}}
{{- .Values.secretNames.database | default "database-credentials" }}
{{- end }}

{{/*
Get the Redis secret name
*/}}
{{- define "osm-device-adapter-secrets.redisSecretName" -}}
{{- .Values.secretNames.redis | default "redis-credentials" }}
{{- end }}

{{/*
Validate required values
*/}}
{{- define "osm-device-adapter-secrets.validateValues" -}}
{{- if not .Values.osm.clientId }}
{{- fail "osm.clientId is required" }}
{{- end }}
{{- if not .Values.osm.clientSecret }}
{{- fail "osm.clientSecret is required" }}
{{- end }}
{{- if not .Values.database.url }}
{{- fail "database.url is required" }}
{{- end }}
{{- if not .Values.redis.url }}
{{- fail "redis.url is required" }}
{{- end }}
{{- end }}
