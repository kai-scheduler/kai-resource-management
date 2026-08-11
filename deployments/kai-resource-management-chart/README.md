# KAI Resource Management Helm chart

This chart installs KAI Resource Management and its bundled KAI Scheduler
dependency. It deploys the node-pool controller, project controller, and PodGroup
assigner, together with their service accounts, RBAC, services, monitoring
resources, webhook configuration, and KAI Resource Management CRDs.

## Prerequisites

- A Kubernetes cluster and Helm 3.
- Permission to create CRDs, cluster-scoped RBAC, and admission webhooks.
- Images for the three KAI Resource Management controllers in a registry that
  every cluster node can pull from.
- The Prometheus Operator `ServiceMonitor` CRD when `serviceMonitor.create=true`.
  Set `serviceMonitor.create=false` when that CRD is not installed.

The KAI Scheduler dependency version is pinned in `Chart.yaml`. Building or
testing this chart requires network access to pull it from GHCR.

## Build and test

Run chart commands from the repository root:

```bash
helm dependency build ./deployments/kai-resource-management-chart
helm lint ./deployments/kai-resource-management-chart
helm template kai-resource-management ./deployments/kai-resource-management-chart \
  --namespace kai-resource-management >/dev/null
make test-chart
mkdir -p ./bin/charts
helm package ./deployments/kai-resource-management-chart \
  --destination ./bin/charts \
  --app-version 0.1.0 \
  --version 0.1.0
```

`test-chart` runs Helm unittest in the pinned `helmunittest/helm-unittest`
container. The package is written to
`bin/charts/kai-resource-management-0.1.0.tgz`; generated packages and
downloaded dependency archives are ignored by Git.

## Install

Choose the release namespace and the image location for the controller images:

```bash
export KRM_NAMESPACE=kai-resource-management
export KRM_IMAGE_REGISTRY=registry.example.com/kai-resource-management
export KRM_IMAGE_TAG=0.1.0

helm upgrade --install kai-resource-management \
  ./bin/charts/kai-resource-management-0.1.0.tgz \
  --namespace "${KRM_NAMESPACE}" \
  --create-namespace \
  --set-string image.registry="${KRM_IMAGE_REGISTRY}" \
  --set-string image.tag="${KRM_IMAGE_TAG}"
```

`.Release.Namespace` is the single installation-namespace source of truth; the
chart does not hardcode an installation namespace. The default installation
includes the KAI Scheduler subchart and creates a catch-all `default` NodePool.

## Configure

Review every public value and its default before installation:

```bash
helm show values ./bin/charts/kai-resource-management-0.1.0.tgz
```

The main configuration groups are:

| Value | Purpose |
| --- | --- |
| `image`, `global` | Controller images, pull policy, FIPS mode, pod security, and scheduling. |
| `kai-scheduler` | Values passed to the bundled KAI Scheduler chart. |
| `rbac.create` | Creation of required roles and bindings. |
| `openshift` | OpenShift mode: SecurityContextConstraints and uid handling. |
| `crdUpgrader` | Image and resources for the CRD install/upgrade hook. |
| `serviceMonitor` | Prometheus Operator monitoring resources. |
| `defaultNodePool` | The chart-managed catch-all NodePool. |
| `nodepoolController` | Node-pool controller deployment, arguments, and webhook. |
| `projectController` | Project controller deployment, features, arguments, and webhooks. |
| `podGroupAssigner` | PodGroup assigner deployment, arguments, and webhooks. |

`values.yaml` documents the supported public surface. `internal_values.yaml` is
a developer reference for template defaults and is not included in packaged
charts.

## Admission webhooks

All three controllers serve admission webhooks. The chart declares the webhook
configurations and provisions their serving certificates.

| Webhook object | Kind | Intercepts | Value |
| --- | --- | --- | --- |
| `kai-pod-group-mutation` | Mutating | `podgroups` on create | always on |
| `kai-pod-mutation` | Mutating | `pods` on create | `podGroupAssigner.webhook.pod` |
| `kai-nodepool-validation` | Validating | `nodepools` on create | `nodepoolController.webhook.nodepool` |
| `kai-project-validation` | Validating | `projects`, `departments` on create and update | `projectController.webhook.project`, `.department` |

Every value defaults to `true`. Setting one to `false` removes that webhook
configuration and passes the matching controller `--enable-…-webhook=false`, so
the object and the handler are never out of step. Pod-group mutation has no value
because the binary registers it unconditionally; a switch could not honour its own
name.

Project and department validation runs on create and update but never on delete.
Deletion ordering is enforced by the controllers' finalizers instead.

The webhook configurations are cluster-scoped and named without the release name,
so only one release of this chart per cluster is supported.

> Every webhook uses `failurePolicy: Fail`. If a controller is unreachable, the
> API server rejects the resources it intercepts. Turn a webhook off through its
> value rather than by scaling its controller to zero.

### TLS certificates

Off OpenShift the chart mints each controller a self-signed CA and serving
certificate at render time, writing the key pair into a `kubernetes.io/tls` Secret
(`<controller>-tls-secret`) and the CA into the webhook's `caBundle` from a single
template invocation, so the two always agree.

Certificates are sticky: an existing Secret is reused, so `helm upgrade` does not
publish a new `caBundle` while a pod is still serving the old certificate. Two
consequences:

- The install needs `get` on Secrets in the release namespace — the reuse check is
  a `lookup`, which runs with your credentials, not the controllers'.
- `lookup` returns nothing when the chart is rendered offline, so `helm template`
  and GitOps tools such as ArgoCD mint a fresh certificate on every render. The
  pods must roll for it to take effect.

Certificates are valid for ten years and are never renewed automatically — Helm
cannot inspect an existing certificate's expiry. To rotate one, delete its Secret
and upgrade; the chart mints a new pair and republishes the matching `caBundle` in
the same release:

```bash
kubectl delete secret pod-group-assigner-tls-secret --namespace "${KRM_NAMESPACE}"
helm upgrade kai-resource-management ... --namespace "${KRM_NAMESPACE}"
```

On OpenShift the platform owns the material instead: the chart renders no Secret
and no `caBundle`, and annotates each Service with
`service.beta.openshift.io/serving-cert-secret-name` and each webhook
configuration with `service.beta.openshift.io/inject-cabundle`, leaving the
service-CA operator to mint the certificate and inject the matching CA.

## OpenShift

OpenShift is detected automatically through a cluster lookup. That lookup returns
nothing when the chart is rendered offline, so set the value explicitly for
`helm template` and for GitOps tools such as ArgoCD:

```bash
helm upgrade --install kai-resource-management ... --set openshift=true
```

In OpenShift mode the chart creates three cluster-scoped objects, subject to
`rbac.create`:

| Object | Name |
| --- | --- |
| `SecurityContextConstraints` | `kai-resource-management` |
| `ClusterRole` (verb `use`) | `kai-resource-management-scc` |
| `ClusterRoleBinding` | `kai-resource-management-scc` |

The SCC grants the chart's ServiceAccounts a fixed uid of 10000, which is the uid
the containers request, regardless of the namespace `openshift.io/sa.scc.uid-range`
annotation. It is deliberately separate from the `kai-system` SCC that the bundled
KAI Scheduler chart creates, so the chart also works where KAI Scheduler was
installed independently.

Two consequences worth planning for. The objects are cluster-scoped and named
without the release name, so only one release of this chart per cluster is
supported. They are also Helm hooks that intentionally outlive the hook run, since
pods are admitted against the SCC on every restart — which means `helm uninstall`
leaves them behind (see below).

With `rbac.create: false` the chart creates neither the SCC nor its `use` grant,
and you are responsible for granting the ServiceAccounts an equivalent SCC.

OpenShift mode also changes how webhook serving certificates are provisioned; see
[TLS certificates](#tls-certificates).

## Upgrade

Helm installs files under `crds/` only on first installation and never updates
them afterwards. The chart handles this itself: a `pre-install` and `pre-upgrade`
hook Job named `kai-resource-management-crd-upgrader` applies the packaged CRDs
on every install and upgrade, so no manual CRD step is required.

```bash
helm upgrade kai-resource-management \
  ./bin/charts/kai-resource-management-0.1.0.tgz \
  --namespace "${KRM_NAMESPACE}" \
  --set-string image.registry="${KRM_IMAGE_REGISTRY}" \
  --set-string image.tag="${KRM_IMAGE_TAG}"
```

The hook runs as the `kai-resource-management-crd-manager` ServiceAccount, whose
ClusterRole is restricted to this chart's own CRDs, and both are removed once the
hook succeeds. It applies server-side with `--force-conflicts` to take field
ownership of the CRDs from Helm. Because the CRDs are baked into the
`crd-upgrader` image at build time, the image and the chart always carry the same
CRD revision. Set `crdUpgrader.image.registry` to serve that image from a mirror
in air-gapped installations.

If the hook fails, the release stops before any workload is updated. Inspect it
with `kubectl logs job/kai-resource-management-crd-upgrader -n "${KRM_NAMESPACE}"`;
a failed Job is replaced automatically on the next upgrade attempt.

Use a values file for persistent overrides and review new defaults before each
upgrade. Do not assume `helm rollback` is a safe downgrade path: the bundled KAI
Scheduler uses lifecycle hooks and retained resources. Back up custom resources
and follow the target release's migration guidance before downgrading.

### Bumping the bundled KAI Scheduler

The chart bundles KAI Scheduler as a subchart. Upgrading it is one line in
`Chart.yaml`:

```yaml
dependencies:
  - name: kai-scheduler
    repository: oci://ghcr.io/kai-scheduler/kai-scheduler
    version: "<VERSION>"
```

`Chart.lock` and `charts/*.tgz` are generated and gitignored, so the version is
the only tracked change. Run `make test-chart` and you are done.

Two things are worth checking first, because neither fails loudly.

**Take the version from the [releases page][kai-releases], not from the registry
tag list.**

**Confirm the values this chart overrides still exist.** Helm ignores unknown
values silently, so a key KAI renames turns our override into a no-op with no
error. The dangerous one is `defaultShard.enabled: false`: if it stops applying,
KAI creates its own default SchedulingShard and competes with the
nodepool-controller. Compare against the new subchart's defaults:

```bash
helm show values oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  --version <VERSION> > /tmp/new-values.yaml
```

and check every key this chart sets under `kai-scheduler:` in `values.yaml`
still appears there.

[kai-releases]: https://github.com/NVIDIA/KAI-Scheduler/releases

## Uninstall

```bash
helm uninstall kai-resource-management --namespace "${KRM_NAMESPACE}"
```

Helm intentionally retains CRDs installed from `crds/`, and Kubernetes therefore
retains their custom resources. Back up and delete those resources and CRDs only
when permanent data removal is intended.

Helm does not track hook resources in the release manifest, so on OpenShift the
SecurityContextConstraints and its grant also survive uninstallation. Remove them
once no release of this chart remains in the cluster:

```bash
oc delete scc kai-resource-management
oc delete clusterrolebinding kai-resource-management-scc
oc delete clusterrole kai-resource-management-scc
```
