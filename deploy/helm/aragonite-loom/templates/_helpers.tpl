{{- define "loom.name" -}}aragonite-loom{{- end }}
{{- define "loom.labels" -}}
app.kubernetes.io/name: {{ include "loom.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}
{{- define "loom.selector" -}}
app.kubernetes.io/name: {{ include "loom.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
{{- define "loom.secretName" -}}
{{- default (include "loom.name" .) .Values.secrets.existingSecret -}}
{{- end }}
