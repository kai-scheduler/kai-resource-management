# Labels and annotations

Every key KAI Resource Management reads or writes. The defaults below are what you get
from an unconfigured install; most are configurable, and the last column says how.

**Read** means KRM looks for it and you may set it. **Written** means KRM sets it — do not
edit it by hand.

## Node labels

| Key | Default | R/W | Meaning |
| --- | --- | --- | --- |
| `kai.scheduler/node-pool` | configurable | Written | The node pool this node belongs to. **Absent means the `default` node pool** |
| `kai.scheduler/unschedulable` | configurable | Written | KRM cordoned this node to drain it before moving it between node pools. Clears itself |
| `kai.scheduler/to-exclude` | configurable | Written | The node is draining ahead of exclusion by a `ManagedNodesConfig` |
| *your own* | — | Read | Whatever `labelKey`/`labelValue` a node pool selects on |

Configurable through `global.nodePoolLabelKey`, and the nodepool-controller's
`unschedulableLabel` and `toExcludeLabel` args.

Find nodes in each state:

```bash
kubectl get nodes -L kai.scheduler/node-pool
kubectl get nodes -l kai.scheduler/unschedulable
kubectl get nodes -l kai.scheduler/to-exclude=true
```

## Node annotations

| Key | R/W | Meaning |
| --- | --- | --- |
| `kai.resources/topology-manager-policy` | Written | The node's kubelet Topology Manager policy, as read from its `NodeResourceTopology`. Only on NUMA-enabled node pools |
| `gpuNetworkAccelerationLabelKey` | Written | The label key used to detect GPU network acceleration on this node. Only when detection is configured on its node pool |

## Namespace labels and annotations

| Key | Default | R/W | Meaning |
| --- | --- | --- | --- |
| `kai/project` | configurable | Written | The project this namespace belongs to. This is the link admission follows to resolve a pod's project |
| `kai.resources/namespace-version` | configurable | Written | Schema version the namespace was initialised with |
| `openshift.io/cluster-monitoring` | fixed | Written | Set to `true` on OpenShift only |
| `kai.resources/enforce-scheduler-name` | configurable | Written | Annotation carrying the project's `enforceKaiScheduler`, read back by pod admission |

Configurable through `global.namespaceProjectLabelKey` and
`global.enforceSchedulerAnnotationKey`.

A namespace missing `kai/project` is not linked to any project. Pods created in it are not
labelled, not enforced, and get no default node pools — which is the usual cause of a
workload silently escaping quota.

```bash
kubectl get namespace kai-research -o jsonpath='{.metadata.labels}' | jq
kubectl get namespace kai-research -o jsonpath='{.metadata.annotations}' | jq
```

## Pod labels and annotations

| Key | Default | R/W | Meaning |
| --- | --- | --- | --- |
| `project` | configurable | Read, then written | The pod's project. An explicit value is kept; otherwise resolved from the namespace |
| `kai.scheduler/node-pool` | configurable | Read | An explicit node pool for this pod, taking precedence over the project's defaults |
| `kai.scheduler/node-pools` | configurable | Read | **Annotation.** Space-separated list of node pools in preference order. Highest precedence of all placement sources |
| `pod-group-name` | fixed | Read | **Annotation.** The PodGroup this pod belongs to. Set by KAI Scheduler |

Configurable through `global.projectLabelKey`, `global.nodePoolLabelKey`, and the
pod-group-assigner's `annotationNodepoolsKey` arg.

## Queue labels

All written by the project-controller.

| Key | Default | Meaning |
| --- | --- | --- |
| `project` | configurable | The project owning this queue |
| `kai.resources/project-id` | configurable | That project's UID |
| `kai.resources/department-name` | configurable | The department — on a department's own queues, and on its projects' queues |
| `kai.scheduler/node-pool` | configurable | The node pool. **Absent means the `default` node pool** |

The department label is on both a department's queues and its projects' queues, so it is
not sufficient to tell them apart — ownership is. To list one project's queues:

```bash
kubectl get queue -l project=research

# Queues for the default node pool need an absence selector
kubectl get queue -l '!kai.scheduler/node-pool'
```

## PodGroup labels and annotations

| Key | Default | R/W | Meaning |
| --- | --- | --- | --- |
| `kai.scheduler/node-pool` | configurable | Written | The assigned node pool. Absent means `default`; a sentinel value means not yet assigned |
| `kai.scheduler/queue` | configurable | Written | The queue this workload is charged to |
| `project` | configurable | Read | The workload's project, if the creator set one |
| `topology.kai/source` | fixed | Written | **Annotation.** `system` marks a topology constraint KRM added, which it may refresh or clear. Its absence on a set constraint means you own it, and KRM will never touch it |

The unassigned sentinel — `kai-unexisting-node-pool` by default — is deliberately not an
absent label: absence already means "the default node pool", so a third state was needed.

## NodePool annotations

The only annotations you set yourself, rather than reading.

| Key | Values | R/W | Meaning |
| --- | --- | --- | --- |
| `kai/gpu-network-acceleration-detection` | `Auto`, `Use`, `DontUse` | Read | How to decide whether this node pool's nodes have GPU network acceleration. Absent means detection is off |
| `kai/gpu-network-acceleration-label-key` | a label key | Read | The node label to detect on. Defaults to `nvidia.com/gpu.clique` |
| `kai/gpu-network-acceleration-detected` | `true`/`false` | Written | The result. Only written when detection is configured |

| Mode | Effect |
| --- | --- |
| `Auto` | Detected from the label key above, present on any node in the node pool |
| `Use` | Forced on, regardless of node labels |
| `DontUse` | Forced off |

## Project and Department annotations

| Key | Values | R/W | Meaning |
| --- | --- | --- | --- |
| `kai/force-delete` | `true` | Read | Skip deletion blockers and delete anyway. Anything left behind is yours to clean up |

## The manual-override label

| Key | Default | R/W | Meaning |
| --- | --- | --- | --- |
| `kai.resources/resource-manual-override` | configurable | Read | Set to `true` on an object KRM created, and KRM stops managing it — no updates, no deletion |

Honoured on namespaces, queues, limit ranges, and on a project itself. Use sparingly: an
overridden object no longer tracks its spec, so the spec stops being the truth about what
exists.

```bash
kubectl label namespace kai-research kai.resources/resource-manual-override=true
```

## Finalizers

Written by the controllers. Never remove one by hand — it exists to enforce an ordering,
and removing it skips whatever cleanup it was guarding.

| Finalizer | Default | On | Blocks deletion while |
| --- | --- | --- | --- |
| `nodepool.kai.scheduler/finalize` | configurable | NodePool | Nodes still need reassigning, or a project still has a queue for the pool |
| `kai.resources.finalizers.project` | configurable | Project | Queues and namespace are still being cleaned up, or a delete-blocker matches |
| `kai.resources.finalizers.department` | configurable | Department | Any project still names it as parent |

An object sitting in `Terminating` is waiting on one of these, and its status conditions
say what for.

## Admission webhooks

Not labels, but the other thing that acts on your objects at create time.

| Webhook | Kind | Intercepts | Turn off with |
| --- | --- | --- | --- |
| `kai-pod-group-mutation` | Mutating | `podgroups` on create | Always on |
| `kai-pod-mutation` | Mutating | `pods` on create | `podGroupAssigner.webhook.pod` |
| `kai-nodepool-validation` | Validating | `nodepools` on create and delete | `nodePoolController.webhook.nodepool` |
| `kai-project-validation` | Validating | `projects`, `departments` on create and update | `projectController.webhook.project`, `.department` |

All use `failurePolicy: Fail`, so an unreachable controller means the API server rejects
the resources it intercepts. Turn a webhook off through its value rather than by scaling
its controller to zero.

## Seeing your installation's actual keys

Every "configurable" above is set at install time and may differ on your cluster. The
authoritative answer is the flags the services were started with:

```bash
kubectl -n kai-resource-management get deploy project-controller \
  -o jsonpath='{.spec.template.spec.containers[0].args}' | jq
```

Or the `KRMConfig` itself:

```bash
kubectl get krmconfig krm-config -o jsonpath='{.spec.global}' | jq
```

An unset field there means the service is using its own built-in default — the one shown
in the tables above.

## See also

- [API reference](api.md) — the fields that configure these keys.
- [Workload placement](../concepts/workload-placement.md) — how the pod and PodGroup keys
  are used.
