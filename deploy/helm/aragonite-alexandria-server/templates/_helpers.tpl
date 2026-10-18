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

{{- define "alexandria.runtimeEnv" -}}
- name: ALEXANDRIA_DATABASE_URL
  valueFrom: { secretKeyRef: { name: {{ include "alexandria.secretName" . }}, key: database-url } }
- { name: ALEXANDRIA_OBJECT_ENDPOINT, value: {{ .Values.objectStore.endpoint | quote }} }
- { name: ALEXANDRIA_OBJECT_REGION, value: {{ .Values.objectStore.region | quote }} }
- { name: ALEXANDRIA_OBJECT_BUCKET, value: {{ .Values.objectStore.bucket | quote }} }
- name: ALEXANDRIA_OBJECT_ACCESS_KEY
  valueFrom: { secretKeyRef: { name: {{ include "alexandria.secretName" . }}, key: object-access-key } }
- name: ALEXANDRIA_OBJECT_SECRET_KEY
  valueFrom: { secretKeyRef: { name: {{ include "alexandria.secretName" . }}, key: object-secret-key } }
- { name: ALEXANDRIA_OBJECT_PATH_STYLE, value: {{ .Values.objectStore.pathStyle | quote }} }
{{- end }}

{{- define "alexandria.podSecurity" -}}
runAsNonRoot: true
seccompProfile: { type: RuntimeDefault }
{{- end }}

{{- define "alexandria.containerSecurity" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities: { drop: ["ALL"] }
{{- end }}
