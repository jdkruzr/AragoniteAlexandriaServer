{{- define "alexandria.name" -}}aragonite-alexandria-server{{- end }}
{{- define "alexandria.labels" -}}
app.kubernetes.io/name: {{ include "alexandria.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}
{{- define "alexandria.selector" -}}
app.kubernetes.io/name: {{ include "alexandria.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
{{- define "alexandria.secretName" -}}
{{- default (include "alexandria.name" .) .Values.secrets.existingSecret -}}
{{- end }}
