{{/*
Returns a non-empty string when installing on OpenShift: either forced via
.Values.openshift or auto-detected from the OpenShift ClusterVersion CRD.
Autodetection relies on lookup, which returns nil under offline rendering
(helm template, GitOps tools such as ArgoCD) - set openshift=true explicitly there.
*/}}
{{- define "kai-resource-management.openshift" -}}
{{- if or .Values.openshift (lookup "apiextensions.k8s.io/v1" "CustomResourceDefinition" "" "clusterversions.config.openshift.io") -}}
true
{{- end -}}
{{- end -}}

{{/*
Resolves a component image tag: explicit per-component tag, then .Values.image.tag, then
the chart appVersion. When global.fips is set, appends "-fips" so the FIPS image
variants are used. Usage:
  {{ include "kai-resource-management.imageTag" (dict "root" $ "tag" .Values.<comp>.image.tag) }}
*/}}
{{- define "kai-resource-management.imageTag" -}}
{{- $tag := .tag | default .root.Values.image.tag | default .root.Chart.AppVersion -}}
{{- if .root.Values.global.fips -}}{{- $tag = printf "%s-fips" $tag -}}{{- end -}}
{{- $tag -}}
{{- end -}}

{{/*
Full image reference for a component. Registry falls back to .Values.image.registry, tag
to the imageTag helper. Usage:
  {{ include "kai-resource-management.image" (dict "root" $ "image" .Values.<comp>.image) }}
*/}}
{{- define "kai-resource-management.image" -}}
{{- $registry := .image.registry | default .root.Values.image.registry -}}
{{- $tag := include "kai-resource-management.imageTag" (dict "root" .root "tag" .image.tag) -}}
{{- if $registry -}}
{{- printf "%s/%s:%s" $registry .image.name $tag -}}
{{- else -}}
{{- printf "%s:%s" .image.name $tag -}}
{{- end -}}
{{- end -}}

{{/*
Image pull policy: per-component override, then global, then IfNotPresent.
*/}}
{{- define "kai-resource-management.imagePullPolicy" -}}
{{- .image.pullPolicy | default .root.Values.global.imagePullPolicy | default "IfNotPresent" -}}
{{- end -}}

{{/*
Common metadata labels applied to every rendered object. Selectors intentionally
use the plain `app: <name>` label (see selectorLabels) since Services,
ServiceMonitors and webhook selectors key off it - these labels are metadata only.
Pass the root context (.).
*/}}
{{- define "kai-resource-management.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: kai-resource-management
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- end -}}

{{/* Container securityContext; user global.securityContext merges over the defaults. Omitted on OpenShift. */}}
{{- define "kai-resource-management.securityContext" -}}
{{- if not (include "kai-resource-management.openshift" .) -}}
{{- $default := dict "allowPrivilegeEscalation" false "runAsNonRoot" true "runAsUser" 10000 "capabilities" (dict "drop" (list "all")) -}}
securityContext:
  {{- toYaml (merge (deepCopy (.Values.global.securityContext | default dict)) $default) | nindent 2 }}
{{- end -}}
{{- end -}}

{{/*
Renders string/int CLI flags from a map of {kebab-flag-name: value}. Only entries with a
non-nil, non-empty value are emitted, in sorted key order (deterministic). Bool/presence
flags stay inline at the call site. Usage:
  {{- include "kai-resource-management.stringArgs" (dict "nodepool-label-key" $x "qps" $y) | nindent 12 }}
*/}}
{{- define "kai-resource-management.stringArgs" -}}
{{- range $flag, $val := . }}
{{- if and (not (kindIs "invalid" $val)) (ne (toString $val) "") }}
- --{{ $flag }}
- {{ toString $val | quote }}
{{- end }}
{{- end }}
{{- end -}}

{{/*
Renders a component's `.args` map generically: each camelCase key becomes a
--kebab-case flag, emitted when the value is non-nil and non-empty (sorted key order).
This is what lets a parent chart drive any controller flag via <component>.args.<key>
with no template edit. `debug` and `leaderElect` are presence/bool flags handled inline
at the call site, so they are skipped here. Keys whose flag is not a plain camelCase→kebab
mapping (e.g. nodepool-label-key) are rendered explicitly via stringArgs instead.
*/}}
{{- define "kai-resource-management.componentArgs" -}}
{{- range $key, $val := . }}
{{- if not (or (eq $key "debug") (eq $key "leaderElect")) }}
{{- if and (not (kindIs "invalid" $val)) (ne (toString $val) "") }}
- --{{ kebabcase $key }}
- {{ toString $val | quote }}
{{- end }}
{{- end }}
{{- end }}
{{- end -}}

{{/*
Shared pod-scheduling blocks (imagePullSecrets/affinity/tolerations/nodeSelector),
rendered from global values. Pass the root context (.). Emits keys at pod-spec
indentation; use with nindent at the call site.
*/}}
{{- define "kai-resource-management.podScheduling" -}}
{{- with .Values.global.imagePullSecrets }}
imagePullSecrets:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.global.affinity }}
affinity:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.global.tolerations }}
tolerations:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.global.nodeSelector }}
nodeSelector:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end -}}
