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
Resolves and validates global.fipsMode, returning "off", "on" or "only". Bools are
coerced so `--set global.fipsMode=true` behaves. An unrecognised value fails the
render: silently ignoring a typo installs something that looks FIPS-enabled and is
not. Pass the root context (.).
*/}}
{{- define "kai-resource-management.fipsMode" -}}
{{- $mode := .Values.global.fipsMode | default "off" -}}
{{- if kindIs "bool" $mode -}}{{- $mode = ternary "on" "off" $mode -}}{{- end -}}
{{- if not (has $mode (list "off" "on" "only")) -}}
{{- fail (printf "global.fipsMode must be one of: off, on, only (got %q)" $mode) -}}
{{- end -}}
{{- $mode -}}
{{- end -}}

{{/*
Resolves a component image tag: explicit per-component tag, then .Values.image.tag, then
the chart appVersion. Any global.fipsMode but "off" appends "-fips": both enabled modes
need the FIPS-built binary and differ only in run-time strictness. Usage:
  {{ include "kai-resource-management.imageTag" (dict "root" $ "tag" .Values.<comp>.image.tag) }}
*/}}
{{- define "kai-resource-management.imageTag" -}}
{{- $tag := .tag | default .root.Values.image.tag | default .root.Chart.AppVersion -}}
{{- if ne (include "kai-resource-management.fipsMode" .root) "off" -}}
{{- $tag = printf "%s-fips" $tag -}}
{{- end -}}
{{- $tag -}}
{{- end -}}

{{/*
The GODEBUG env entry putting a Go binary into the requested FIPS mode. A FIPS image
already defaults to fips140=on, so this is what reaches "only" and what turns FIPS off
without changing the image. Our Go services only, never the kubectl hook Jobs.
Pass the root context (.); emit under a container's `env:`.
*/}}
{{- define "kai-resource-management.godebug" -}}
- name: GODEBUG
  value: {{ printf "fips140=%s" (include "kai-resource-management.fipsMode" .) | quote }}
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

{{/* Container securityContext; user global.securityContext merges over the defaults. runAsUser
matches the uid the OpenShift SCC pins (see templates/rbac/scc.yaml). */}}
{{- define "kai-resource-management.securityContext" -}}
{{- $default := dict "allowPrivilegeEscalation" false "runAsNonRoot" true "runAsUser" 10000 "capabilities" (dict "drop" (list "all")) -}}
securityContext:
  {{- toYaml (merge (deepCopy (.Values.global.securityContext | default dict)) $default) | nindent 2 }}
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

{{/*
Annotations shared by the post-delete cleanup Job and its RBAC. The ArgoCD pair is
required: without it ArgoCD treats Helm hook resources as ordinary sync-phase
resources (needs ArgoCD >= 2.10).
*/}}
{{- define "kai-resource-management.post-delete-hook-annotations" -}}
"helm.sh/hook": post-delete
"helm.sh/hook-delete-policy": hook-succeeded
argocd.argoproj.io/hook: PostDelete
argocd.argoproj.io/hook-delete-policy: BeforeHookCreation,HookSucceeded
{{- end -}}

{{/*
Renders the KRMConfig CR the operator reconciles. Used by the krm-config-deployer
hook ConfigMap so the CR can be applied out-of-band of the Helm release, and
rendered inline as a release resource when krmConfig.render=true (GitOps/ArgoCD).

Every optional field is emitted only when set. The CR is server-side applied, so an
emitted empty value would take ownership of that field and stop the operator's own
default from applying. Pass the root context (.).
*/}}
{{- define "kai-resource-management.krm-config" -}}
apiVersion: kai.resources/v1alpha1
kind: KRMConfig
metadata:
  name: krm-config
  {{- if (.Values.krmConfig | default dict).render }}
  annotations:
    # SkipDryRunOnMissingResource: ArgoCD dry-runs sync-phase resources before the
    # CRD is established on a fresh cluster.
    # ServerSideApply: adopts a CR previously field-managed by krm-config-deployer.
    argocd.argoproj.io/sync-options: SkipDryRunOnMissingResource=true,ServerSideApply=true
  {{- end }}
spec:
  namespace: {{ .Release.Namespace }}
  {{- $globalBody := include "kai-resource-management.krm-config-global" . }}
  {{- if trim $globalBody }}
  global:
    {{- trim $globalBody | nindent 4 }}
  {{- end }}
  {{- include "kai-resource-management.krm-config-service" (dict "root" $ "key" "nodePoolController" "comp" .Values.nodepoolController) }}
  {{- include "kai-resource-management.krm-config-service" (dict "root" $ "key" "projectController" "comp" .Values.projectController) }}
  {{- include "kai-resource-management.krm-config-project-controller" $ }}
  {{- include "kai-resource-management.krm-config-service" (dict "root" $ "key" "podGroupAssigner" "comp" .Values.podGroupAssigner) }}
  {{- include "kai-resource-management.krm-config-pod-group-assigner" $ }}
{{- end -}}

{{/*
The spec.global body. Rendered separately so the `global:` key can be dropped
entirely when nothing is set: an emitted empty value would be YAML null, and under
server-side apply that takes ownership of the field and defeats the operator's own
defaults.
*/}}
{{- define "kai-resource-management.krm-config-global" -}}
{{- $commonArgs := .Values.commonArgs | default dict -}}
{{- $kai := index .Values "kai-scheduler" | default dict -}}
{{- $nodePoolLabelKey := ($kai.global | default dict).nodePoolLabelKey -}}
{{- $queueLabelKey := ($kai.podgrouper | default dict).queueLabelKey -}}
{{- if $commonArgs.schedulerName }}
schedulerName: {{ $commonArgs.schedulerName | quote }}
{{- end }}
{{- if $queueLabelKey }}
queueLabelKey: {{ $queueLabelKey | quote }}
{{- end }}
{{- if $nodePoolLabelKey }}
nodePoolLabelKey: {{ $nodePoolLabelKey | quote }}
{{- end }}
{{- if $commonArgs.finalizerDomain }}
finalizerDomain: {{ $commonArgs.finalizerDomain | quote }}
{{- end }}
{{- if $commonArgs.namespaceProjectLabelKey }}
namespaceProjectLabelKey: {{ $commonArgs.namespaceProjectLabelKey | quote }}
{{- end }}
{{- if $commonArgs.projectLabelKey }}
projectLabelKey: {{ $commonArgs.projectLabelKey | quote }}
{{- end }}
{{- if $commonArgs.enforceSchedulerAnnotationKey }}
enforceSchedulerAnnotationKey: {{ $commonArgs.enforceSchedulerAnnotationKey | quote }}
{{- end }}
{{- with (.Values.defaultNodePool | default dict).name }}
defaultNodePoolName: {{ . | quote }}
{{- end }}
{{- if .Values.global.leaderElection }}
leaderElection: true
{{- end }}
{{- if not .Values.serviceMonitor.create }}
serviceMonitor:
  enabled: false
{{- end }}
{{- if (include "kai-resource-management.openshift" .) }}
openshift: true
{{- end }}
{{- $fipsMode := include "kai-resource-management.fipsMode" . }}
{{- if ne $fipsMode "off" }}
fipsMode: {{ $fipsMode | quote }}
{{- end }}
{{- with .Values.global.imagePullSecrets }}
# The CR takes secret names; the chart's own value is a list of {name: ...}.
imagePullSecrets:
  {{- range . }}
  - {{ .name | quote }}
  {{- end }}
{{- end }}
{{- with .Values.global.nodeSelector }}
nodeSelector:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.global.tolerations }}
tolerations:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.global.affinity }}
affinity:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.global.securityContext }}
securityContext:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end -}}

{{/*
One service block of the KRMConfig, rendered from the same component values the
chart's own Deployment uses - one source, two consumers. Usage:
  {{- include "kai-resource-management.krm-config-service" (dict "root" $ "key" "projectController" "comp" .Values.projectController) }}
*/}}
{{- define "kai-resource-management.krm-config-service" -}}
{{- $comp := .comp | default dict }}
  {{ .key }}:
    service:
      enabled: {{ $comp.enabled | default false }}
      image:
        name: {{ $comp.image.name | quote }}
        repository: {{ ($comp.image.registry | default .root.Values.image.registry) | quote }}
        tag: {{ include "kai-resource-management.imageTag" (dict "root" .root "tag" $comp.image.tag) | quote }}
        pullPolicy: {{ include "kai-resource-management.imagePullPolicy" (dict "root" .root "image" $comp.image) | quote }}
      {{- with $comp.resources }}
      resources:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- $args := $comp.args | default dict }}
      {{- if or $args.qps $args.burst }}
      k8sClientConfig:
        {{- with $args.qps }}
        qps: {{ . | int }}
        {{- end }}
        {{- with $args.burst }}
        burst: {{ . | int }}
        {{- end }}
      {{- end }}
    {{- if $comp.replicas }}
    replicas: {{ $comp.replicas }}
    {{- end }}
{{- end -}}

{{/*
The project-controller keys the generic service block does not cover, emitted as
siblings of it. Values are read from the same projectController.* keys the chart's
own RBAC and webhook templates use, so a setting has one home and two readers.
*/}}
{{- define "kai-resource-management.krm-config-project-controller" -}}
{{- include "kai-resource-management.reject-shared-arg-override" (dict "comp" "projectController" "args" (($.Values.projectController | default dict).args | default dict)) }}
{{- $comp := .Values.projectController | default dict -}}
{{- $webhook := $comp.webhook | default dict -}}
{{- $features := $comp.features | default dict -}}
{{- $profiling := $comp.profiling | default dict -}}
{{- $args := $comp.args | default dict -}}
{{- $svc := $comp.service | default dict -}}
{{- $metrics := $svc.metrics | default dict -}}
    {{- if or $metrics $webhook.port $webhook.targetPort }}
    controllerService:
      {{- with $metrics }}
      metrics:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- if or $webhook.port $webhook.targetPort }}
      webhook:
        {{- with $webhook.port }}
        port: {{ . | int }}
        {{- end }}
        {{- with $webhook.targetPort }}
        targetPort: {{ . | int }}
        {{- end }}
      {{- end }}
    {{- end }}
    {{- if or (hasKey $webhook "project") (hasKey $webhook "department") $webhook.certSecretName }}
    webhooks:
      {{- if hasKey $webhook "project" }}
      enableProjectValidation: {{ $webhook.project }}
      {{- end }}
      {{- if hasKey $webhook "department" }}
      enableDepartmentValidation: {{ $webhook.department }}
      {{- end }}
      {{- with $webhook.certSecretName }}
      certSecretName: {{ . | quote }}
      {{- end }}
    {{- end }}
    {{- with $features }}
    features:
      {{- toYaml . | nindent 6 }}
    {{- end }}
    {{- if or (hasKey $profiling "enabled") $profiling.apiPort }}
    profiling:
      {{- if hasKey $profiling "enabled" }}
      enabled: {{ $profiling.enabled }}
      {{- end }}
      {{- with $profiling.apiPort }}
      apiPort: {{ . | int }}
      {{- end }}
    {{- end }}
    {{- $argsBody := include "kai-resource-management.krm-config-project-controller-args" $args }}
    {{- if trim $argsBody }}
    args:
      {{- trim $argsBody | nindent 6 }}
    {{- end }}
    {{- with $comp.extraArgs }}
    extraArgs:
      {{- toYaml . | nindent 6 }}
    {{- end }}
    {{- with $comp.extraProjectRoleBindings }}
    extraProjectRoleBindings:
      {{- toYaml . | nindent 6 }}
    {{- end }}
    {{- with $comp.roleBindingsConfigMapName }}
    roleBindingsConfigMapName: {{ . | quote }}
    {{- end }}
    {{- with $comp.deleteBlockers }}
    deleteBlockers:
      {{- toYaml . | nindent 6 }}
    {{- end }}
{{- end -}}

{{/*
The typed args of the KRMConfig, taken from projectController.args. Only these keys
are modelled; anything else set there is ignored, and belongs in extraArgs.
qps and burst are deliberately absent: they live on service.k8sClientConfig.
*/}}
{{- define "kai-resource-management.krm-config-project-controller-args" -}}
{{- $args := . -}}
{{- range $key := list "debug" "leaderElect" "projectNamePrefix" "projectIdLabelKey"
    "queueDepartmentNameLabelKey" "namespaceVersionLabelKey" "resourceManualOverrideLabelKey" "limitRangeName" }}
{{- if hasKey $args $key }}
{{- $value := index $args $key }}
{{- if kindIs "bool" $value }}
{{ $key }}: {{ $value }}
{{- else if and (not (kindIs "invalid" $value)) (ne (toString $value) "") }}
{{ $key }}: {{ toString $value | quote }}
{{- end }}
{{- end }}
{{- end }}
{{- end -}}

{{/*
The pod-group-assigner keys the generic service block does not cover, emitted as
siblings of it. Values are read from the same podGroupAssigner.* keys the chart's
own webhook template uses, so a setting has one home and two readers.
*/}}
{{- define "kai-resource-management.krm-config-pod-group-assigner" -}}
{{- include "kai-resource-management.reject-shared-arg-override" (dict "comp" "podGroupAssigner" "args" (($.Values.podGroupAssigner | default dict).args | default dict)) }}
{{- $comp := .Values.podGroupAssigner | default dict -}}
{{- $webhook := $comp.webhook | default dict -}}
{{- $args := $comp.args | default dict -}}
    {{- if or $webhook.port $webhook.targetPort }}
    controllerService:
      webhook:
        {{- with $webhook.port }}
        port: {{ . | int }}
        {{- end }}
        {{- with $webhook.targetPort }}
        targetPort: {{ . | int }}
        {{- end }}
    {{- end }}
    {{- if or (hasKey $webhook "pod") $webhook.certSecretName }}
    webhooks:
      {{- if hasKey $webhook "pod" }}
      enablePodWebhook: {{ $webhook.pod }}
      {{- end }}
      {{- with $webhook.certSecretName }}
      certSecretName: {{ . | quote }}
      {{- end }}
    {{- end }}
    {{- $argsBody := include "kai-resource-management.krm-config-pod-group-assigner-args" $args }}
    {{- if trim $argsBody }}
    args:
      {{- trim $argsBody | nindent 6 }}
    {{- end }}
    {{- with $comp.extraArgs }}
    extraArgs:
      {{- toYaml . | nindent 6 }}
    {{- end }}
{{- end -}}

{{/*
The typed args of the KRMConfig, taken from podGroupAssigner.args. Only these keys
are modelled; anything else set there is ignored, and belongs in extraArgs.
qps and burst are deliberately absent: they live on service.k8sClientConfig.
*/}}
{{- define "kai-resource-management.krm-config-pod-group-assigner-args" -}}
{{- $args := . -}}
{{- range $key := list "debug" "leaderElect" "unexistingNodepoolSentinel" "annotationNodepoolsKey" }}
{{- if hasKey $args $key }}
{{- $value := index $args $key }}
{{- if kindIs "bool" $value }}
{{ $key }}: {{ $value }}
{{- else if and (not (kindIs "invalid" $value)) (ne (toString $value) "") }}
{{ $key }}: {{ toString $value | quote }}
{{- end }}
{{- end }}
{{- end }}
{{- end -}}

{{/*
Shared vocabulary must agree across every service - a scheduler name or finalizer
domain that differed between two of them would simply be wrong - so commonArgs is
the only source and a per-service copy is refused rather than ignored. Usage:
  {{- include "kai-resource-management.reject-shared-arg-override" (dict "comp" "podGroupAssigner" "args" $args) }}
*/}}
{{- define "kai-resource-management.reject-shared-arg-override" -}}
{{- $shared := list "schedulerName" "finalizerDomain" "projectLabelKey" "namespaceProjectLabelKey" "enforceSchedulerAnnotationKey" -}}
{{- range $key := $shared }}
{{- if hasKey ($.args | default dict) $key }}
{{- fail (printf "%s.args.%s is not settable: %s is shared vocabulary, set commonArgs.%s instead" $.comp $key $key $key) }}
{{- end }}
{{- end }}
{{- end -}}
