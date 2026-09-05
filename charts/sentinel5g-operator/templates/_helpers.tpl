{{- define "sentinel5g-operator.name" -}}
sentinel5g-operator
{{- end -}}

{{- define "sentinel5g-operator.fullname" -}}
{{- if .Release.Name | eq "sentinel5g" -}}
sentinel5g-operator
{{- else -}}
{{ .Release.Name }}-sentinel5g-operator
{{- end -}}
{{- end -}}

{{- define "sentinel5g-operator.labels" -}}
app.kubernetes.io/name: {{ include "sentinel5g-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "sentinel5g-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{ .Values.serviceAccount.name | default (include "sentinel5g-operator.fullname" .) }}
{{- else -}}
{{ .Values.serviceAccount.name | default "default" }}
{{- end -}}
{{- end -}}
