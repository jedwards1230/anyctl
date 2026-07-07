{{- define "anyctl-mcp.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "anyctl-mcp.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "anyctl-mcp.labels" -}}
app.kubernetes.io/name: {{ include "anyctl-mcp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: mcp-server
app.kubernetes.io/part-of: anyctl
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- end -}}

{{- define "anyctl-mcp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "anyctl-mcp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "anyctl-mcp.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}

{{/*
Resolve the secret name holding the op service-account token. When a
OnePasswordItem itemPath is set, the operator syncs a secret named after this
release; otherwise an existingSecret name must be supplied.
*/}}
{{- define "anyctl-mcp.secretName" -}}
{{- if .Values.auth.onePasswordItem.itemPath -}}
{{- printf "%s-op-token" (include "anyctl-mcp.fullname" .) -}}
{{- else -}}
{{- .Values.auth.existingSecret.name -}}
{{- end -}}
{{- end -}}

{{- define "anyctl-mcp.secretKey" -}}
{{- if .Values.auth.onePasswordItem.itemPath -}}
{{- .Values.auth.onePasswordItem.key -}}
{{- else -}}
{{- .Values.auth.existingSecret.key -}}
{{- end -}}
{{- end -}}

{{/*
Resolve the secret name holding the MCP bearer token (transport-layer auth on
the /mcp endpoint). When mcp.auth.onePasswordItem.itemPath is set, the operator
syncs a secret named after this release; otherwise mcp.auth.existingSecret.name
must be supplied. Empty result signals "no source configured".
*/}}
{{- define "anyctl-mcp.mcpAuthSecretName" -}}
{{- if .Values.mcp.auth.onePasswordItem.itemPath -}}
{{- printf "%s-mcp-auth-token" (include "anyctl-mcp.fullname" .) -}}
{{- else -}}
{{- .Values.mcp.auth.existingSecret.name -}}
{{- end -}}
{{- end -}}

{{- define "anyctl-mcp.mcpAuthSecretKey" -}}
{{- if .Values.mcp.auth.onePasswordItem.itemPath -}}
{{- .Values.mcp.auth.onePasswordItem.key -}}
{{- else -}}
{{- .Values.mcp.auth.existingSecret.key -}}
{{- end -}}
{{- end -}}
