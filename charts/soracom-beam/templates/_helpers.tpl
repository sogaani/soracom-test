{{- define "soracom-beam.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "soracom-beam.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: soracom-beam
{{- end -}}

{{/* Selector labels for a component: include "soracom-beam.selector" (list . "server") */}}
{{- define "soracom-beam.selector" -}}
app.kubernetes.io/name: {{ index . 0 | include "soracom-beam.fullname" }}-{{ index . 1 }}
app.kubernetes.io/instance: {{ (index . 0).Release.Name }}
app.kubernetes.io/component: {{ index . 1 }}
{{- end -}}

{{- define "soracom-beam.secretName" -}}
{{- .Values.existingSecret | default (include "soracom-beam.fullname" .) -}}
{{- end -}}

{{- define "soracom-beam.mongodbHost" -}}
{{ include "soracom-beam.fullname" . }}-mongodb
{{- end -}}

{{- define "soracom-beam.serverHost" -}}
{{ include "soracom-beam.fullname" . }}-server
{{- end -}}

{{/* Common + selector labels for a component's workload: include "soracom-beam.componentLabels" (list . "server") */}}
{{- define "soracom-beam.componentLabels" -}}
{{ include "soracom-beam.selector" . }}
helm.sh/chart: {{ printf "%s-%s" (index . 0).Chart.Name (index . 0).Chart.Version }}
app.kubernetes.io/managed-by: {{ (index . 0).Release.Service }}
app.kubernetes.io/part-of: soracom-beam
{{- end -}}

{{- define "soracom-beam.grafanaProvisioning" -}}
datasources.yaml: |
  apiVersion: 1
  datasources:
    - name: SORACOM
      uid: soracom
      type: yesoreyeram-infinity-datasource
      access: proxy
      isDefault: true
      # Base URL; dashboard queries use paths like /api/v1/messages.
      url: http://{{ include "soracom-beam.serverHost" . }}:8081
      jsonData:
        allowedHosts:
          - http://{{ include "soracom-beam.serverHost" . }}:8081
dashboards.yaml: |
  apiVersion: 1
  providers:
    - name: soracom
      folder: SORACOM
      type: file
      allowUiUpdates: false
      options:
        path: /etc/grafana/dashboards
{{- end -}}
