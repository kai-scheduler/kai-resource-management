# API reference

Every field of the `kai.resources/v1alpha1` custom resources. All of them are
cluster-scoped.

| Kind | Short name | Purpose |
| --- | --- | --- |
| [NodePool](#nodepool) | `np` | A named slice of the cluster's nodes |
| [Project](#project) | | A team, its quota, and its namespace |
| [Department](#department) | | A group of projects sharing quota |
| [ManagedNodesConfig](#managednodesconfig) | | Restricts which nodes KRM manages |
| [KRMConfig](#krmconfig) | `krmconfig` | Describes the installation itself |

Notation used below:

| Marker | Meaning |
| --- | --- |
| **immutable** | Cannot be changed after creation |
| **install-time** | Changing it on a running cluster breaks things until both sides agree |
| **controller-written** | Set by KRM, not by you |

For anything not covered here, the cluster has the full schema:
`kubectl explain <kind>.spec --recursive`.

---

## NodePool

### `spec`

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `labelKey` | string | — | The node label key this node pool selects on. **Immutable.** Must be empty on `default` and non-empty on every other node pool |
| `labelValue` | string | — | The value that key must have. **Immutable.** Matched exactly. The key/value pair must be unique across all node pools |
| `preferredNetworkTopologyName` | string | — | A KAI `Topology` to prefer for workloads on this node pool. Applied as a *preferred* PodGroup constraint at the topology's lowest level |
| `schedulingShardConfig` | object | — | Per-pool scheduler settings. See [below](#specschedulingshardconfig) |

### `spec.schedulingShardConfig`

Compiled into the node pool's KAI `SchedulingShard`. Unset fields fall back to the
scheduler's own defaults. See [tuning per-node-pool
scheduling](../how-to/tune-per-node-pool-scheduling.md).

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `placementStrategy.gpu` | string | `binpack` | `binpack` or `spread` |
| `placementStrategy.cpu` | string | `binpack` | `binpack` or `spread` |
| `minRuntime.preemptMinRuntime` | duration | — | Minimum runtime before a workload may be preempted |
| `minRuntime.reclaimMinRuntime` | duration | — | Minimum runtime before its borrowed quota may be reclaimed |
| `plugins` | map[string]object | — | Keyed by plugin name. Each takes `enabled` (bool), `priority` (int), `arguments` (map). Names come from KAI Scheduler |
| `actions` | map[string]object | — | Keyed by action name. Each takes `enabled` (bool) and `priority` (int) |
| `queueDepthPerAction` | map[string]int | — | Maximum jobs tried per action, per queue |
| `args` | map[string]string | — | Raw scheduler flags, passed through unvalidated. Escape hatch |
| `timeBasedFairShare` | object | — | Usage-aware fairness. See [below](#specschedulingshardconfigtimebasedfairshare) |

Built-in plugin and action priorities run 0–10000 in steps of 100; higher runs first.

### `spec.schedulingShardConfig.timeBasedFairShare`

Requires Prometheus — usage is read from the metrics backend.

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `enabled` | bool | `false` | Everything else here is ignored while false |
| `historicalUsageWeight` | float | `1.0` | How strongly past usage lowers current fair share. `0` ignores history. Minimum `0` |
| `halfLifePeriod` | duration | `0` | Decay half-life of past usage. `0` weighs all history equally. Pattern: digits with `m`, `h` or `d` |
| `window.type` | string | `sliding` | `sliding`, `tumbling`, or `cron` |
| `window.size` | duration | `7d` | Window length |
| `window.tumblingStartTime` | RFC-3339 time | — | Anchor. Only used when `type: tumbling` |
| `window.cronString` | string | — | Only used when `type: cron` |
| `sampling.fetchInterval` | duration | `1m` | How often usage is fetched |
| `sampling.stalenessPeriod` | duration | `5m` | When fetched usage is considered stale |
| `sampling.waitTimeout` | duration | `1m` | Fetch wait timeout |

### `status`

All **controller-written**.

| Field | Type | Notes |
| --- | --- | --- |
| `phase` | string | `Ready`, `Empty`, `Unschedulable`, `MissingPrerequisites`, `Deleting`. See [conditions and phases](conditions-and-phases.md#nodepool-phases) |
| `message` | string | Human-readable explanation of the phase, naming the nodes involved |
| `nodes[].name` | string | A node in this node pool |
| `nodes[].status` | string | `Ready`, `Unschedulable`, `MissingNrtHealthyPrerequisite` |
| `nodes[].topologyMismatch` | bool | The node is missing labels the node pool's network topology requires |
| `conditions` | array | `NodeTopologyMismatch`, `ProjectReferencesExist`, `MissingNrtHealthyPrerequisite` |

### Annotations

See [labels and annotations](labels-and-annotations.md#nodepool-annotations) for the
GPU-network-acceleration annotations this kind accepts.

---

## Project

### `spec`

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `queues` | array | **required** | One entry per node pool. See [QueueConfig](#queueconfig). May be an empty list |
| `parent` | string | — | The department this project belongs to. Must exist. Empty means the project is its own root |
| `defaultNodePools` | []string | — | Ordered placement preference. Every node pool listed must also have a queue in `queues` |
| `namespace` | string | — | Adopt this existing namespace instead of creating `<prefix>-<name>`. It must already exist |
| `enforceKaiScheduler` | bool | `false` | Put every pod in the namespace on the KAI scheduler, whether it asked or not |
| `deletionType` | string | — | `Blocking` requires the namespace be emptied of configured blockers before the project deletes |

Validated on create **and** update: the parent must exist, every referenced node pool must
exist, no two queues may name the same pool, and every `defaultNodePools` entry must have
a queue.

### `status`

All **controller-written**.

| Field | Type | Notes |
| --- | --- | --- |
| `phase` | string | `Ready` or `NotReady` |
| `namespace` | string | The namespace actually in use — read this rather than deriving it |
| `message` | string | Why the project is in this phase |
| `conditions` | array | `NamespaceReady`, `QueuesReady`, `RoleBindingsReady`, plus one per configured delete-blocker group |
| `nodePoolsQuotaStatuses[].nodePoolName` | string | Which node pool this entry is for |
| `nodePoolsQuotaStatuses[].queueStatus` | object | That node pool's queue status: `requested`, `allocated`, `allocatedNonPreemptible`, `childQueues`, `conditions` |
| `quotaStatus` | object | The same three figures summed across all node pools |

---

## Department

### `spec`

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `queues` | array | **required** | One entry per node pool. See [QueueConfig](#queueconfig) |

A department has no namespace, no `parent`, and runs nothing. Its queues are the parents of
its projects' queues **for the same node pool**.

### `status`

| Field | Type | Notes |
| --- | --- | --- |
| `conditions` | array | `DepartmentDeletionBlocked` — **controller-written** |

---

## QueueConfig

The element type of `spec.queues` on both Project and Department.

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `name` | string | **required** | The queue's name. If it collides with another owner's queue, a random suffix is appended, so read the real name back |
| `nodepool` | string | **required** | The node pool this queue's quota applies to. Must exist |
| `priority` | int32 | `100` | Ordering against sibling queues; higher wins |
| `resources.gpu` | object | — | See below |
| `resources.cpu` | object | — | See below |
| `resources.memory` | object | — | See below |

Each of `gpu`, `cpu` and `memory` takes the same three fields:

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `deserved` | float | `0` | Guaranteed amount, always reclaimable |
| `limit` | float | `0` | Hard ceiling. **`-1` means no ceiling** |
| `overQuotaWeight` | float | `0` | Relative share of spare capacity. `0` means never borrow |

**Units**, which are easy to get wrong:

| Resource | Unit | Example |
| --- | --- | --- |
| `gpu` | Whole GPUs, fractions allowed | `0.5` = half a GPU |
| `cpu` | Millicores | `1000` = 1 core |
| `memory` | Megabytes (10⁶ bytes) | `128000` = 128 GB |

Plain numbers, not Kubernetes quantity strings — `"32Gi"` is not valid.

> Every field defaults to `0`, and `0` is a real number. A `limit` of `0` is a ceiling of
> zero, not an absent ceiling. Omitting `resources` entirely gives a queue that is
> guaranteed nothing and capped at nothing.

If `name` is omitted, KRM derives `<owner>-<nodepool>`, or bare `<owner>` for the default
node pool.

---

## ManagedNodesConfig

Only the singleton named `kai-managed-nodes-config` is reconciled — the name is
configurable at install time. Any other name is ignored.

### `spec`

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `inclusion_criteria` | NodeSelector | — | Nodes to **keep** managed. Note the underscore, unlike the rest of the API. Terms are OR-ed; expressions within a term are AND-ed |

This is a whitelist. Every node not matching is moved to the reserved excluded node pool.
See [excluding nodes from management](../how-to/exclude-nodes-from-management.md).

### `status`

| Field | Type | Notes |
| --- | --- | --- |
| `conditions` | array | `Applied`, with reason `AllNodesIncludedCorrectly` or `ToBeExcludedNodes` |
| `observedGeneration` | int64 | The generation last acted on |

---

## KRMConfig

Only the singleton named `krm-config` is reconciled. In a normal install the Helm chart
creates and maintains it — configure through chart values rather than editing it. See
[KRMConfig](../concepts/krm-config.md).

This is by far the largest of the five. The fields below are the ones worth setting by
hand; use `kubectl explain krmconfig.spec --recursive` for the rest.

### `spec`

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `namespace` | string | The operator's own namespace | Where the services are deployed |
| `global` | object | — | Inherited by every service. See below |
| `nodePoolController` | object | — | Per-service configuration |
| `projectController` | object | — | Per-service configuration |
| `podGroupAssigner` | object | — | Per-service configuration |

### `spec.global`

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `schedulerName` | string | service default | **Install-time.** The scheduler the controllers bind workloads to |
| `queueLabelKey` | string | service default | **Install-time.** Label key carrying a workload's queue |
| `nodePoolLabelKey` | string | service default | **Install-time.** Label key carrying a node's node pool |
| `defaultNodePoolName` | string | `default` | Name of the catch-all node pool |
| `finalizerDomain` | string | service default | Domain prefix for finalizers |
| `namespaceProjectLabelKey` | string | service default | Label key linking a namespace to its project |
| `projectLabelKey` | string | service default | Label key carrying a workload's project |
| `enforceSchedulerAnnotationKey` | string | service default | Annotation key forcing a workload onto the scheduler |
| `replicaCount` | int32 | `1` | Default replicas for services that set none |
| `leaderElection` | bool | `false` | Also implied by a replica count above one |
| `imagePullSecrets` | []string | — | Added to every service pod |
| `nodeSelector` | map | — | Applied to every service pod |
| `tolerations` | []Toleration | — | Applied to every service pod |
| `affinity` | Affinity | — | Applied to services that set none |
| `requireDefaultPodAntiAffinityTerm` | bool | `false` | Make the default per-host anti-affinity required rather than preferred |
| `securityContext` | SecurityContext | runAsUser 10000, non-root, no privilege escalation, all capabilities dropped | Ignored on OpenShift |
| `priorityClassName` | string | — | Applied to every service pod |
| `openshift` | bool | `false` | Force OpenShift behaviour instead of detecting it |
| `fipsMode` | string | `off` | `off`, `on`, `only`. Run-time mode only — the image is chosen by tag. See [FIPS 140-3](../fips.md) |
| `vpa` | object | — | Default Vertical Pod Autoscaler configuration |
| `serviceMonitor.enabled` | bool | `true` | Skipped silently when the Prometheus CRD is absent |
| `serviceMonitor.accounting` | bool | `true` | The second nodepool-controller ServiceMonitor, for the accounting Prometheus |

The three install-time fields are the ones to be careful with: nothing reconciles them
against the scheduler, and changing one on a running cluster stops workloads being
scheduled until both sides agree.

### Per-service blocks

All three of `nodePoolController`, `projectController` and `podGroupAssigner` share this
shape:

| Field | Type | Notes |
| --- | --- | --- |
| `service` | object | Enablement, image, resources |
| `controllerService` | object | The ports the service listens on and publishes |
| `webhooks` | object | Webhook toggles and the TLS secret name. **Set these through chart values** — the chart renders the matching webhook configuration and certificate, and the two must agree |
| `args` | object | The service's own flags. Unset means the flag is not passed and the binary's default applies |
| `extraArgs` | []string | Appended after every other flag, so a repeat overrides. Escape hatch |
| `replicas` | int32 | Overrides `global.replicaCount` |
| `vpa` | object | Overrides `global.vpa` |

`projectController` adds:

| Field | Type | Notes |
| --- | --- | --- |
| `features` | object | `createNamespaces`, `createRoleBindings`, `limitRange` — which per-project resources the controller manages. Each also gates the matching chart RBAC |
| `extraProjectRoleBindings` | array | Extra RoleBindings to replicate into every project namespace. Each takes `name`, `serviceAccountName`, and optionally `clusterRoleName` (defaults to `name`). The ClusterRole must already exist |
| `roleBindingsConfigMapName` | string | The ConfigMap those bindings are built into |
| `deleteBlockers` | array | Resource kinds whose presence blocks deleting a project. Each takes `displayName`, `version`, `kind`, optionally `group` and `labelSelector`. Empty means nothing blocks deletion. You must grant the controller `get`, `list` and `watch` on every kind you list |
| `profiling` | object | `enabled` and `apiPort` for the profiler API |

Default resource requests and limits per service:

| Service | Requests | Limits |
| --- | --- | --- |
| `nodepool-controller` | 450m CPU, 512Mi | 900m CPU, 1Gi |
| `project-controller` | 150m CPU, 1Gi | 300m CPU, 2Gi |
| `pod-group-assigner` | 100m CPU, 256Mi | 200m CPU, 512Mi |

### `status`

| Field | Type | Notes |
| --- | --- | --- |
| `conditions` | array | `Ready`, `Deployed`, `Available`, `DependenciesFulfilled`, `Reconciling`. See [conditions and phases](conditions-and-phases.md#krmconfig-conditions) |

---

## See also

- [Labels and annotations](labels-and-annotations.md) — the keys these fields configure.
- [Conditions and phases](conditions-and-phases.md) — every status value.
- [Chart documentation](../../deployments/kai-resource-management-chart/README.md) — the
  values that produce a `KRMConfig`.
