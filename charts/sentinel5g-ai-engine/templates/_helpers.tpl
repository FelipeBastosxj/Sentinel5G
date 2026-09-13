{{- define "sentinel5g-ai-engine.name" -}}
sentinel5g-ai-engine
{{- end -}}

{{- define "sentinel5g-ai-engine.fullname" -}}
{{- if .Release.Name | eq "sentinel5g-ai-engine" -}}
sentinel5g-ai-engine
{{- else -}}
{{ .Release.Name }}-sentinel5g-ai-engine
{{- end -}}
{{- end -}}

{{- define "sentinel5g-ai-engine.labels" -}}
app.kubernetes.io/name: {{ include "sentinel5g-ai-engine.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "sentinel5g-ai-engine.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{ .Values.serviceAccount.name | default (include "sentinel5g-ai-engine.fullname" .) }}
{{- else -}}
{{ .Values.serviceAccount.name | default "default" }}
{{- end -}}
{{- end -}}

{{/*
Refuses to render unless exactly one model source is configured.

This is the whole reason this chart exists. ROADMAP.md Phase 2.5 describes
the failure it replaces: the AI engine needs autoencoder.onnx + .onnx.data +
.norm.json, those artifacts are deliberately not committed, and a deployment
that skipped delivering them came up healthy and simply never published a
ThreatScoreEvent -- leaving every TelecomSecurityPolicy at Phase: Monitoring
forever with no signal anywhere. Failing the install is strictly better than
installing something that cannot work.
*/}}
{{- define "sentinel5g-ai-engine.validateModel" -}}
{{- $image := .Values.model.image.repository -}}
{{- $secret := .Values.model.existingSecret -}}
{{- if and $image $secret -}}
{{- fail "model.image.repository and model.existingSecret are mutually exclusive -- set exactly one. See docs/production-install.md." -}}
{{- end -}}
{{- if not (or $image $secret) -}}
{{- fail "no model source configured: set either model.image.repository (a published model artifact, the default) or model.existingSecret (the name of a Secret holding autoencoder.onnx, autoencoder.onnx.data and autoencoder.norm.json). Without one the AI engine cannot score anything and every TelecomSecurityPolicy would sit at Phase: Monitoring forever. See docs/production-install.md." -}}
{{- end -}}
{{- end -}}

{{/*
The port /metrics is actually served on. In nats mode the worker has no
HTTP app, so sentinel_ai/server.py starts a dedicated prometheus_client
server on config.metricsAddr; in http mode /metrics is exposed by the FastAPI
app on config.httpAddr and NOTHING listens on metricsAddr. A Service and
ServiceMonitor that always targeted metricsAddr therefore scraped a dead
port in http mode -- caught in review, not by a test, because the chart
defaults to nats mode.
*/}}
{{- define "sentinel5g-ai-engine.metricsPort" -}}
{{- if eq .Values.config.mode "http" -}}
{{ .Values.config.httpAddr | trimPrefix ":" | int }}
{{- else -}}
{{ .Values.config.metricsAddr | trimPrefix ":" | int }}
{{- end -}}
{{- end -}}
