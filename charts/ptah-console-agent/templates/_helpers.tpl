{{/* The name of every object, the release's unless it already says ptah-console-agent. */}}
{{- define "ptah-console-agent.fullname" -}}
{{- if contains "ptah-console-agent" .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-ptah-console-agent" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "ptah-console-agent.selectorLabels" -}}
app.kubernetes.io/name: ptah-console-agent
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "ptah-console-agent.labels" -}}
{{ include "ptah-console-agent.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "ptah-console-agent.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- if .Values.image.digest -}}
{{- printf "%s:%s@%s" .Values.image.repository $tag .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}
{{- end -}}

{{/* The Kubernetes username an account acts as. */}}
{{- define "ptah-console-agent.username" -}}
{{- printf "ptah-console:%s:%s" .installationID .id -}}
{{- end -}}

{{/* The usernames the agent may act as, one per account: built here and in
the agent from the same two values, and from nothing else. */}}
{{- define "ptah-console-agent.usernames" -}}
{{- $usernames := list -}}
{{- range .Values.accounts -}}
{{- $usernames = append $usernames (include "ptah-console-agent.username" (dict "installationID" $.Values.installationID "id" .id)) -}}
{{- end -}}
{{- toYaml $usernames -}}
{{- end -}}

{{/* What the schema cannot say. */}}
{{- define "ptah-console-agent.validate" -}}
{{- if not .Values.accounts -}}
{{- fail "accounts: an empty list would leave the impersonation rule without resourceNames, which grants every user" -}}
{{- end -}}
{{- $ids := dict -}}
{{- range .Values.accounts -}}
{{- if hasKey $ids .id -}}
{{- fail (printf "accounts: the id %s is given twice" .id) -}}
{{- end -}}
{{- $_ := set $ids .id true -}}
{{- end -}}
{{- if and .Values.networkPolicy.enabled (not .Values.networkPolicy.apiServer.cidrs) -}}
{{- fail "networkPolicy: apiServer.cidrs names the API server, which the agent has to reach" -}}
{{- end -}}
{{- if and .Values.networkPolicy.enabled (not .Values.networkPolicy.console.cidrs) -}}
{{- fail "networkPolicy: console.cidrs names the console's listener, which the agent has to reach" -}}
{{- end -}}
{{- end -}}
