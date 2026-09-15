# Place workloads across node pools

**Goal:** control which node pool a workload runs on — and give it somewhere to fall back
to when that pool is full.

**You need:** KRM installed, a project, and more than one node pool to choose between.

## The resolution order

Every workload ends up with an **ordered list of candidate node pools**. The first source
below that produces anything wins outright; the rest are not consulted.

| # | Source | Where it is set | Use it for |
| --- | --- | --- | --- |
| 1 | `kai.scheduler/node-pools` annotation | On the pod | A specific fallback order. Only alongside matching affinity — see below |
| 2 | Required node affinity | On the pod | Expressing hardware requirements portably. **The one to reach for** |
| 3 | Node pool label | On the PodGroup | Tooling that assigns directly |
| 4 | `defaultNodePools` | On the project | The normal case — every workload in the project |
| 5 | The `default` node pool | Nothing | The final fallback |

The pod group assigner walks the resulting list and takes the first node pool whose phase
is `Ready` or `Empty`. Node pools that are `Unschedulable`, `Deleting` or
`MissingPrerequisites` are skipped.

## Set the project default

Most workloads should need no annotation at all. Put the answer on the project:

```yaml
apiVersion: kai.resources/v1alpha1
kind: Project
metadata:
  name: research
spec:
  parent: engineering
  defaultNodePools:
    - h100
    - a100
  queues:
    - name: research-h100
      nodepool: h100
      resources:
        gpu: { deserved: 4, limit: 8, overQuotaWeight: 1 }
        cpu: { deserved: 16000, limit: -1, overQuotaWeight: 1 }
        memory: { deserved: 64000, limit: -1, overQuotaWeight: 1 }
    - name: research-a100
      nodepool: a100
      resources:
        gpu: { deserved: 8, limit: 16, overQuotaWeight: 1 }
        cpu: { deserved: 16000, limit: -1, overQuotaWeight: 1 }
        memory: { deserved: 64000, limit: -1, overQuotaWeight: 1 }
```

Set `cpu` and `memory` too, not just `gpu`: an unset `limit` is `0`, which is a real ceiling
of zero, and a GPU-only queue rejects every pod that requests CPU.

**Order is preference.** Work goes to `h100` when it can, and falls back to `a100` when it
cannot.

**Every node pool listed must have a queue in the same spec.** The webhook enforces this,
and the reason is that a pool with no queue is a pool the work cannot be charged to:

```text
the nodepool "a100" from the DefaultNodePools list has no queue defined in the spec
```

Admission also turns `defaultNodePools` into required node affinity on each pod, so the
pods physically cannot land outside those node pools.

## Override for one workload

**Prefer node affinity.** Two mechanisms exist, and only one of them settles both halves of
the question. A workload's placement has to agree on two things — which node pool it is
*charged to*, and which nodes it may *land on* — and the annotation sets only the first.

### By node affinity — recommended

```yaml
spec:
  schedulerName: kai-scheduler
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: nvidia.com/gpu.product
                operator: In
                values: ["NVIDIA-H100-80GB-HBM3"]
```

This settles both halves at once. Kubernetes uses the affinity to pick the node, and the
pod group assigner matches the same expressions against each node pool's label pair to pick
the node pool and queue — so the two cannot disagree. It is also ordinary Kubernetes
affinity, which means the manifest still says the right thing on a cluster without KRM.

Constraints:

- **Only the `In` operator is matched to node pools**, plus the special case below.
  `NotIn`, `Exists` and `Gt`/`Lt` are still honoured by Kubernetes as ordinary affinity;
  they just do not select a node pool.
- Only **required** affinity is considered. `preferredDuringScheduling...` does not select
  a node pool.
- Several node pools named in one term collapse to the set of pools matched; each term is
  an alternative.

Because setting a pod's own node affinity counts as expressing a preference, admission
leaves it alone rather than adding the project's defaults on top.

### By annotation — only alongside matching affinity

```yaml
metadata:
  annotations:
    kai.scheduler/node-pools: "h100 a100"
```

Space-separated, in preference order. It is the highest-precedence source **for the node
pool and queue the workload is charged to**, overriding the project's defaults.

> **It does not touch the pod's node affinity**, and that asymmetry will strand a workload.
> Admission does not read this annotation; it still applies the project's `defaultNodePools`
> as required affinity. So on a project defaulting to `h100`, a pod annotated for `a100`
> gets a PodGroup on `a100` and node affinity for `h100`, and never schedules:
>
> ```text
> Warning  Unschedulable  kai-scheduler  no nodes with enough resources were found:
> 1 node(s) didn't match Pod's node affinity/selector.
> ```
>
> If you use the annotation, set matching node affinity as well — or use affinity alone,
> which is why it is the recommendation above.

The annotation earns its place when the *order* of several node pools matters, since
affinity expresses a set rather than a preference order.

### Targeting the default node pool

The `default` node pool is represented by the **absence** of the node-pool label, so it is
selected with `DoesNotExist`, not by naming it:

```yaml
- key: kai.scheduler/node-pool
  operator: DoesNotExist
```

By annotation it is simply named:

```yaml
kai.scheduler/node-pools: "default"
```

## The fallback, when the first node pool cannot take it

A workload with more than one candidate node pool is not stuck with its first pick. If the
scheduler for that pool cannot place it, the assigner moves it to the next pool in the
list, wrapping around.

```mermaid
flowchart LR
    h["h100"] -->|"cannot schedule"| a["a100"]
    a -->|"cannot schedule"| d["default"]
    d -->|"cannot schedule"| h
```

Two behaviours follow from the list's length, and they are quite different:

- **One candidate node pool.** There is nowhere to go. The workload waits in that pool
  indefinitely until capacity frees up. It is not marked unschedulable, because there is
  nothing to report — it is simply queued.
- **Several.** The workload cycles. On reaching the last node pool in the list it is marked
  unschedulable, which is what surfaces the failure rather than letting it spin silently.
  If a node pool it already tried frees up, cycling continues.

A workload whose pods are **all already running** is never moved, even if its node pool
later goes unschedulable. Reassignment applies to work that has not started.

Watch it move:

```bash
kubectl get podgroup -n kai-research -w -o custom-columns=\
NAME:.metadata.name,POOL:'.metadata.labels.kai\.scheduler/node-pool',QUEUE:.spec.queue
```

## Verify what a workload actually got

```bash
# What admission put on the pod
kubectl get pod needs-h100 -n kai-research -o jsonpath='{.spec.affinity.nodeAffinity}' | jq

# Which pool and queue the assigner chose
kubectl get podgroup -n kai-research -o custom-columns=\
NAME:.metadata.name,POOL:'.metadata.labels.kai\.scheduler/node-pool',QUEUE:.spec.queue

# Where it ended up
kubectl get pod needs-h100 -n kai-research -o wide
```

## Common mistakes

| Symptom | Cause |
| --- | --- |
| Workload ignores `defaultNodePools` | The pod sets its own affinity or annotation, which takes precedence |
| Project rejected on create | A node pool in `defaultNodePools` has no queue in the same spec |
| Everything lands in `default` | The project has no `defaultNodePools`, and the pod expressed no preference |
| Affinity is set but no node pool is selected | The operator is not `In`, the affinity is `preferred` rather than `required`, or the value does not exactly match a node pool's `labelValue` |
| PodGroup never gets a queue | The project has no queue for the node pool that was chosen |
| Workload never falls back | It has only one candidate node pool |

## See also

- [Workload placement](../concepts/workload-placement.md) — the full path a pod takes.
- [Queues and quota](../concepts/queues-and-quota.md) — what it is charged to once placed.
- [Troubleshooting](troubleshooting.md) — when a pod does not reach the scheduler at all.
