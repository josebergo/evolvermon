{{/* EvolverMon Helm helpers */}}
{{- define "evolvermon.fullname" -}}
{{- printf "%s" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* 服务通用标签 */}}
{{- define "evolvermon.labels" -}}
app.kubernetes.io/name: evolvermon-{{ .svc }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/part-of: evolvermon
{{- end -}}

{{/* 内部 DSN 派生（按服务库名） */}}
{{- define "evolvermon.dsn" -}}
{{- $host := .Values.postgresql.host -}}
{{- if and .Values.postgresql.internal (not $host) -}}
{{- $host = printf "%s-postgresql" .Release.Name -}}
{{- end -}}
host={{ $host }} port=5432 user={{ .Values.postgresql.auth.user }} password={{ .Values.postgresql.auth.password }} dbname=evolvermon_{{ .db }} sslmode=disable TimeZone=Asia/Shanghai
{{- end -}}

{{/* 域名服务地址 */}}
{{- define "evolvermon.url" -}}
http://{{ .Release.Name }}-{{ .svc }}:{{ .port }}
{{- end -}}
