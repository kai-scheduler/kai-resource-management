{{/*
Webhook serving certificate for a component. Returns YAML with base64-encoded ca/crt/key,
so callers parse it once with fromYaml and render both the Secret and the webhook
configuration's caBundle from the SAME invocation - they must agree or admission fails.

Sticky: an existing Secret's material is reused, so `helm upgrade` does not rotate the cert
out from under a running pod. Plain genCA-per-render would publish a new caBundle
immediately while the pod kept serving the old cert until the kubelet refreshed the mount,
and every webhook in this chart is failurePolicy: Fail.

lookup returns an empty dict under offline rendering (helm template, GitOps tools such as
ArgoCD), so a fresh cert is minted on every such render - same caveat as
kai-resource-management.openshift. Rendering offline therefore requires the pods to roll.

Sticky reuse also means the chart never renews: Sprig cannot parse a certificate's expiry,
so there is nothing to compare against. Certs are minted for 10 years and rotation is a
deliberate operator action - delete the Secret and run `helm upgrade`, which mints a fresh
pair and republishes the matching caBundle in the same release.

Requires the installing credentials to have `get` on Secrets in the release namespace.

Usage:
  {{- $certs := fromYaml (include "kai-resource-management.webhookCerts"
      (dict "root" $ "service" "pod-group-assigner" "secretName" $secretName)) }}
*/}}
{{- define "kai-resource-management.webhookCerts" -}}
{{- $ns := .root.Release.Namespace -}}
{{- $svc := .service -}}
{{- $existing := lookup "v1" "Secret" $ns .secretName -}}
{{- $data := dict -}}
{{- if $existing -}}
{{- $data = $existing.data | default dict -}}
{{- end -}}
{{- if and (index $data "ca.crt") (index $data "tls.crt") (index $data "tls.key") -}}
ca: {{ index $data "ca.crt" }}
crt: {{ index $data "tls.crt" }}
key: {{ index $data "tls.key" }}
{{- else -}}
{{- $dnsNames := list
    (printf "%s.%s" $svc $ns)
    (printf "%s.%s.svc" $svc $ns)
    (printf "%s.%s.svc.cluster.local" $svc $ns) -}}
{{- $ca := genCA (printf "%s-ca" $svc) 3650 -}}
{{- $cert := genSignedCert $svc nil $dnsNames 3650 $ca -}}
ca: {{ $ca.Cert | b64enc }}
crt: {{ $cert.Cert | b64enc }}
key: {{ $cert.Key | b64enc }}
{{- end -}}
{{- end -}}

{{/*
The TLS Secret a webhook server mounts. Rendered only off OpenShift - there the service-CA
operator mints the same Secret name itself, keyed off the Service's
service.beta.openshift.io/serving-cert-secret-name annotation.

ca.crt is stored alongside the key pair purely so webhookCerts can restore it on the next
render; the webhook server itself reads only tls.crt/tls.key.

Usage:
  {{- include "kai-resource-management.webhookSecret"
      (dict "root" $ "service" "pod-group-assigner" "secretName" $secretName "certs" $certs) }}
*/}}
{{- define "kai-resource-management.webhookSecret" -}}
apiVersion: v1
kind: Secret
metadata:
  name: {{ .secretName }}
  namespace: {{ .root.Release.Namespace }}
  labels:
    app: {{ .service }}
    {{- include "kai-resource-management.labels" .root | nindent 4 }}
type: kubernetes.io/tls
data:
  ca.crt: {{ .certs.ca }}
  tls.crt: {{ .certs.crt }}
  tls.key: {{ .certs.key }}
{{- end -}}
