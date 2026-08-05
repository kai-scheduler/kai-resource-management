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
| `crdUpgrader` | Image and resources for the CRD install/upgrade hook. |
| `serviceMonitor` | Prometheus Operator monitoring resources. |
| `defaultNodePool` | The chart-managed catch-all NodePool. |
| `nodepoolController` | Node-pool controller deployment and arguments. |
| `projectController` | Project controller deployment, features, and arguments. |
| `podGroupAssigner` | PodGroup assigner deployment, arguments, and webhook. |

`values.yaml` documents the supported public surface. `internal_values.yaml` is
a developer reference for template defaults and is not included in packaged
charts.

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

## Uninstall

```bash
helm uninstall kai-resource-management --namespace "${KRM_NAMESPACE}"
```

Helm intentionally retains CRDs installed from `crds/`, and Kubernetes therefore
retains their custom resources. Back up and delete those resources and CRDs only
when permanent data removal is intended.
