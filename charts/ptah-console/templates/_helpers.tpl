{{/* The name of every object, the release's unless it already says ptah-console. */}}
{{- define "ptah-console.fullname" -}}
{{- if contains "ptah-console" .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-ptah-console" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "ptah-console.selectorLabels" -}}
app.kubernetes.io/name: ptah-console
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "ptah-console.labels" -}}
{{ include "ptah-console.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{/* The Kubernetes username an account acts as: built here and in the
console from the same two values, and from nothing else. */}}
{{- define "ptah-console.username" -}}
{{- printf "ptah-console:%s:%s" .installationID .id -}}
{{- end -}}

{{/* The host of publicURL. */}}
{{- define "ptah-console.host" -}}
{{- .Values.publicURL | trimPrefix "https://" | trimSuffix "/" -}}
{{- end -}}

{{- define "ptah-console.tls" -}}
{{- if not .Values.tls.terminatedUpstream -}}true{{- end -}}
{{- end -}}

{{- define "ptah-console.port" -}}
{{- if .Values.service.port -}}
{{- .Values.service.port -}}
{{- else if include "ptah-console.tls" . -}}
443
{{- else -}}
80
{{- end -}}
{{- end -}}

{{- define "ptah-console.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- if .Values.image.digest -}}
{{- printf "%s:%s@%s" .Values.image.repository $tag .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}
{{- end -}}

{{/* What the schema cannot say: one TLS choice, and no ID or login twice. */}}
{{- define "ptah-console.validate" -}}
{{- if and .Values.tls.secretName .Values.tls.terminatedUpstream -}}
{{- fail "tls: give secretName or terminatedUpstream, not both" -}}
{{- end -}}
{{- if not (or .Values.tls.secretName .Values.tls.terminatedUpstream) -}}
{{- fail "tls: a hosted console serves HTTPS only: give tls.secretName, or tls.terminatedUpstream when an Ingress or a Gateway terminates TLS" -}}
{{- end -}}
{{- $ids := dict -}}
{{- $logins := dict -}}
{{- range .Values.accounts -}}
{{- if hasKey $ids .id -}}
{{- fail (printf "accounts: the id %s is given twice" .id) -}}
{{- end -}}
{{- $_ := set $ids .id true -}}
{{- if hasKey $logins (lower .login) -}}
{{- fail (printf "accounts: the login %s is given twice, ignoring case" .login) -}}
{{- end -}}
{{- $_ := set $logins (lower .login) true -}}
{{- end -}}
{{- if and .Values.networkPolicy.enabled (not .Values.networkPolicy.apiServer.cidrs) -}}
{{- fail "networkPolicy: apiServer.cidrs names the API server the console has to reach" -}}
{{- end -}}
{{- end -}}
