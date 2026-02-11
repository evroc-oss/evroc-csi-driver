{{/*
Expand the name of the chart.
*/}}
{{- define "evroc-csi-driver.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "evroc-csi-driver.fullname" -}}
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
{{- define "evroc-csi-driver.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "evroc-csi-driver.labels" -}}
helm.sh/chart: {{ include "evroc-csi-driver.chart" . }}
{{ include "evroc-csi-driver.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.labels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "evroc-csi-driver.selectorLabels" -}}
app.kubernetes.io/name: {{ include "evroc-csi-driver.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Controller labels
*/}}
{{- define "evroc-csi-driver.controller.labels" -}}
{{ include "evroc-csi-driver.labels" . }}
app.kubernetes.io/component: controller
{{- end }}

{{/*
Node labels
*/}}
{{- define "evroc-csi-driver.node.labels" -}}
{{ include "evroc-csi-driver.labels" . }}
app.kubernetes.io/component: node
{{- end }}

{{/*
Create the name of the controller service account to use
*/}}
{{- define "evroc-csi-driver.controller.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (printf "%s-controller" (include "evroc-csi-driver.fullname" .)) .Values.serviceAccount.controller.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.controller.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the node service account to use
*/}}
{{- define "evroc-csi-driver.node.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (printf "%s-node" (include "evroc-csi-driver.fullname" .)) .Values.serviceAccount.node.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.node.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the config secret
*/}}
{{- define "evroc-csi-driver.configSecretName" -}}
{{- required "evroc.existingConfigSecret is required" .Values.evroc.existingConfigSecret }}
{{- end }}

{{/*
CSI driver name
*/}}
{{- define "evroc-csi-driver.driverName" -}}
{{- .Values.csiDriver.name }}
{{- end }}

{{/*
Controller socket path
*/}}
{{- define "evroc-csi-driver.controller.socketPath" -}}
/var/lib/csi/sockets/pluginproxy/csi.sock
{{- end }}

{{/*
Node socket path
*/}}
{{- define "evroc-csi-driver.node.socketPath" -}}
/csi/csi.sock
{{- end }}
