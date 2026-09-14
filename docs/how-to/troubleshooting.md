# Troubleshooting

Symptom, what to check, what it means. Work down from wherever your problem starts —
installation, then the objects you created, then the workload.

## Start here

Three commands answer most questions:

```bash
# 1. Is the installation healthy?
kubectl get krmconfig krm-config -o jsonpath='{.status.conditions}' | jq

# 2. Are the objects you created healthy?
kubectl get nodepool -o custom-columns=NAME:.metadata.name,PHASE:.status.phase
kubectl get project  -o custom-columns=NAME:.metadata.name,PHASE:.status.phase,NAMESPACE:.status.namespace

# 3. Where did the workload get to?
kubectl get podgroup -n <namespace> -o custom-columns=\
NAME:.metadata.name,QUEUE:.spec.queue,POOL:'.metadata.labels.kai\.scheduler/node-pool'
```

> KRM's custom resources define no printer columns, so a bare `kubectl get` shows only
> `NAME` and `AGE` and `-o wide` adds nothing. Ask for fields explicitly, as above.

Almost everything reports its own problem in a status condition. Read the condition before
reading logs.

## Installation

### `KRMConfig` is not `Ready`

Look at the other conditions — they say which part is outstanding.

| Condition `False` | Meaning | Check |
| --- | --- | --- |
| `Deployed` | The operator has not created everything yet | `kubectl -n <ns> get deploy`; operator logs |
| `Available` | Objects exist but a workload is not running | `kubectl -n <ns> get pods`; describe the failing one |
| `DependenciesFulfilled` | Something the installation needs is missing | The condition message names it |

```bash
kubectl -n kai-resource-management logs deploy/krm-operator --tail=100
```

### `DependenciesFulfilled` names a missing CRD

```text
NodePoolController is missing CRDs schedulingshards.kai.scheduler/v1, topologies.kai.scheduler/v1alpha1
```

The KAI Scheduler CRDs that service reads are not installed, or no longer serve
the API version it reads them through. The chart installs KAI as a subchart, but
it can be upgraded or uninstalled on its own afterwards — so it can be removed or
rolled back under a running KRM. Confirm with:

```bash
kubectl get crd | grep -E 'kai\.scheduler|scheduling\.run\.ai'
kubectl get crd queues.scheduling.run.ai -o jsonpath='{.spec.versions[*].name}'
```

Reinstall or re-upgrade KAI Scheduler. Nothing needs restarting afterwards: the
operator re-checks every `--dependency-check-interval` (default one minute) and
clears the condition itself. Until then it keeps the services deployed, so
`Deployed` and `Available` stay meaningful — the pods will be failing on the
missing CRD, which is what this condition explains.

[Which CRDs each service needs](../reference/conditions-and-phases.md#what-dependenciesfulfilled-covers).

### `DependenciesFulfilled` names the KAI Scheduler config

```text
KAI Scheduler Config "kai-config" does not exist
```

The scheduler's CRDs are installed but nothing applied its `Config` CR — usually
because KAI was installed with `kaiConfigDeployer.enabled=false` and nothing
applied the CR in the hook's place, or because the CR was deleted out from under
the deployer.

```bash
kubectl get config kai-config
kubectl get config kai-config -o jsonpath='{.status.conditions}' | jq
```

A variant names readiness instead:

```text
KAI Scheduler Config "kai-config" is not ready: <reason>
```

That verdict is KAI's own, copied onto our condition — the reason to act on is on
the `Config`, and it is KAI's operator to investigate, not KRM's. Expect it
transiently during a KAI upgrade; it clears itself.

A third names the version:

```text
KAI Scheduler v0.14.2 is older than the minimum supported v0.16.9
```

```bash
kubectl -n <kai-namespace> get deploy kai-operator \
  -o jsonpath='{.spec.template.spec.containers[0].image}'
```

Upgrade KAI, or if the image is deliberately re-tagged (air-gapped mirrors do
this) accept any version by setting `krmOperator.args.minKaiSchedulerVersion=0.0.0`.

### No `KRMConfig` exists at all

```bash
kubectl get krmconfig
```

If that is empty, the operator is idling with nothing to do. Either the chart was
installed with both `krmConfigDeployer.enabled` and `krmConfig.render` off — in which case
you are expected to create it yourself — or the post-install hook Job failed:

```bash
kubectl -n kai-resource-management get jobs
kubectl -n kai-resource-management logs job/<krm-config-deployer-job>
```

Only the object named exactly `krm-config` is reconciled. One with any other name is
ignored.

### Only `krm-operator` is running

That is correct for the first few seconds — Helm installs only the operator, which then
installs the other three. If the others never appear, the operator cannot proceed: check
`KRMConfig` conditions and the operator's logs.

### The install fails on `ServiceMonitor`

```text
no matches for kind "ServiceMonitor" in version "monitoring.coreos.com/v1"
```

The chart renders `ServiceMonitor` objects and the CRD is not installed. Either install
the Prometheus Operator CRD, or install with `--set serviceMonitor.create=false`.

### `helm upgrade` fails on the CRD hook

The release stops before any workload is touched, which is the intended behaviour.

```bash
kubectl -n kai-resource-management logs job/kai-resource-management-crd-upgrader
```

A failed Job is replaced automatically on the next attempt.

## Node pools

### Node pool is `Empty` but the nodes look right

`labelValue` is matched exactly. The usual cause is a value that looks right but is not —
`nvidia.com/gpu.product` is `NVIDIA-H100-80GB-HBM3`, not `H100`.

```bash
kubectl get nodepool h100 -o jsonpath='{.spec.labelKey}={.spec.labelValue}{"\n"}'
kubectl get nodes --show-labels | tr ',' '\n' | grep nvidia
```

`Empty` is not an error. The node pool is a valid placement target and will claim nodes as
soon as any match.

### Node pool is `Unschedulable`

It has nodes, but none are ready. Either the nodes are genuinely unhealthy, or they were
cordoned while moving between node pools:

```bash
kubectl get nodepool h100 -o jsonpath='{.status.message}'
kubectl get nodes -l kai.scheduler/unschedulable
```

A node carrying `kai.scheduler/unschedulable` was cordoned by KRM to drain it before a
move. It clears itself once the old node pool's workloads finish.

### Node pool is `MissingPrerequisites`

A scheduling feature you enabled cannot be honoured on some nodes — in practice, NUMA. The
message says which prerequisite:

```bash
kubectl get nodepool h100 -o jsonpath='{.status.message}'
kubectl get nodepool h100 -o jsonpath='{.status.nodes}' | jq
```

See [tuning per-node-pool
scheduling](tune-per-node-pool-scheduling.md#numa-aware-scheduling). Workloads are still
placed on the pool; alignment may just not be honoured.

### Nodes never move into a new node pool

Workloads of their current node pool are still running on them. Nothing is ever evicted:

```bash
kubectl get nodepool h100 -o jsonpath='{.status.message}'
```

```text
The following node(s) are assigned to this node pool and will be added once they
finish draining: worker-1
```

Delete those workloads if you need the capacity now.

### Node pool will not delete

```bash
kubectl get nodepool h100 -o jsonpath='{.status.conditions}' | jq
```

`ProjectReferencesExist` names the projects still holding a queue for it. Remove those
queues, or delete the projects.

The `default` node pool cannot be deleted at all — it is the catch-all, and nothing
recreates it.

### Rejected on create

| Message | Cause |
| --- | --- |
| `must set a non-empty labelKey and labelValue` | Only `default` may leave them empty |
| `the "default" nodepool must not set labelKey or labelValue` | The catch-all selects by exclusion |
| `labelKey ... duplicates existing nodepool` | Another node pool has that exact pair |
| `labelKey and labelValue are immutable` | Delete and recreate instead |

## Projects and departments

### Project stuck `NotReady`

```bash
kubectl get project research -o jsonpath='{.status.conditions}' | jq
```

| Condition `False` | Meaning |
| --- | --- |
| `NamespaceReady` | The namespace could not be created, found, or updated |
| `QueuesReady` | A queue could not be created — often a name collision or a missing node pool |
| `RoleBindingsReady` | A role binding could not be replicated into the namespace |

The message carries the underlying error.

### Project rejected on create

| Message | Fix |
| --- | --- |
| `parent department "x" does not exist` | Create the department first |
| `node pool "x" does not exist` | Create the node pool first |
| `node pool "x" is already referenced by another queue` | Two queues in one spec cannot share a node pool |
| `the nodepool "x" from the DefaultNodePools list has no queue defined in the spec` | Add a queue for it |

### Department will not delete

```bash
kubectl get department engineering -o jsonpath='{.status.conditions}' | jq
```

`DepartmentDeletionBlocked` is `True` while any project names it as parent. Delete or
re-parent them.

### Project will not delete

Only happens with `deletionType: Blocking` and delete blockers configured at install time.
The condition names the group of resources still present. Delete them, or force it:

```bash
kubectl annotate project research kai/force-delete=true
```

Anything left in the namespace is then yours to clean up.

A condition that names resources you cannot find usually means the controller cannot watch
or list that kind. It reports the blocker as failed either way, so check its RBAC before
hunting for the resources. It lists through a cache, so `watch` is as necessary as `list`
and is the one more often left out:

```bash
kubectl auth can-i watch persistentvolumeclaims \
  --as=system:serviceaccount:<install namespace>:project-controller -A
kubectl auth can-i list persistentvolumeclaims \
  --as=system:serviceaccount:<install namespace>:project-controller -A
```

### An object is not being reconciled

Check for the manual-override label, which tells the controller to leave it alone:

```bash
kubectl get namespace kai-research -o jsonpath='{.metadata.labels}' | jq
```

`kai.resources/resource-manual-override: "true"` means intentional, by someone.

## Workloads

### The pod is not on the KAI scheduler

```bash
kubectl get pod <pod> -n <ns> -o jsonpath='{.spec.schedulerName}'
```

If that is not your KAI scheduler name, the pod is invisible to KRM and running outside
all quota. Either it did not name the scheduler and the project does not enforce it:

```bash
kubectl get project research -o jsonpath='{.spec.enforceKaiScheduler}'
```

or the namespace is not linked to a project:

```bash
kubectl get namespace <ns> -o jsonpath='{.metadata.labels}' | jq
kubectl get namespace <ns> -o jsonpath='{.metadata.annotations}' | jq
```

The namespace needs the project label, and the enforce annotation for enforcement to
apply.

**A pod's scheduler name is immutable.** An already-created pod cannot be fixed — delete
and recreate it once the project is right.

### No PodGroup was created

The PodGroup comes from KAI Scheduler, not KRM. If the pod is on the KAI scheduler and no
PodGroup appeared, look at KAI Scheduler's own components rather than at KRM.

### PodGroup has no queue

```bash
kubectl get podgroup -n <ns> -o custom-columns=\
NAME:.metadata.name,QUEUE:.spec.queue,POOL:'.metadata.labels.kai\.scheduler/node-pool'
```

An empty `QUEUE` means the pod group assigner could not resolve one. Usual causes:

- The project has no queue for the node pool that was chosen.
- The pod's project could not be resolved — its namespace lacks the project label.
- Every candidate node pool is unavailable, so no pool was chosen at all.

```bash
kubectl -n kai-resource-management logs deploy/pod-group-assigner --tail=100
```

### PodGroup is on the wrong node pool

Work back up the resolution order — the first source that produces anything wins. The
numbers below are positions in that five-source order, so they are the ones you can inspect
from the pod and the project. Source 3 is skipped deliberately: it is a label on the
PodGroup, not on the pod.

```bash
# source 1 — annotation on the pod
kubectl get pod <pod> -n <ns> -o jsonpath='{.metadata.annotations}' | jq
# source 2 — node affinity on the pod
kubectl get pod <pod> -n <ns> -o jsonpath='{.spec.affinity.nodeAffinity}' | jq
# source 4 — the project's defaults
kubectl get project research -o jsonpath='{.spec.defaultNodePools}'
```

To check source 3, read the node pool label on the PodGroup itself:

```bash
kubectl get podgroup -n <ns> \
  -o custom-columns=NAME:.metadata.name,POOL:'.metadata.labels.kai\.scheduler/node-pool'
```

See [placing workloads across node pools](place-workloads-across-node-pools.md).

### Pod is `Pending`

```bash
kubectl describe pod <pod> -n <ns> | tail -20
kubectl get podgroup -n <ns> -o jsonpath='{.items[0].status}' | jq
```

Common causes, in rough order of likelihood:

| Cause | Check |
| --- | --- |
| The queue is at its `limit` | `kubectl get project research -o jsonpath='{.status.nodePoolsQuotaStatuses}' \| jq` — compare `requested` against `allocated` |
| The queue has no quota for the resource requested | Every resource field defaults to `0`, and `0` is a real ceiling. A GPU-only queue rejects a pod requesting CPU |
| The node pool has no capacity | `kubectl get nodepool <pool> -o jsonpath='{.status.nodes}' \| jq` |
| Node affinity matches no node | Admission added the project's `defaultNodePools` as required affinity |
| The pod is annotated for one node pool and has affinity for another | The `kai.scheduler/node-pools` annotation moves the queue assignment but not the affinity. See below |
| The node pool is unschedulable | Its phase |

A workload with only one candidate node pool waits there indefinitely rather than being
marked unschedulable — there is nowhere for it to move to.

### Everything lands in the `default` node pool

The project has no `defaultNodePools`, and the pods express no preference of their own, so
resolution falls through to the last resort.

Remember the default node pool is the **absence** of the node-pool label, so a node or queue
showing nothing under `kai.scheduler/node-pool` is in `default`, not unassigned.

### `OverLimit: ... Limit is 0 cores`

```text
Warning  Unschedulable  kai-scheduler  OverLimit: research-a100 quota has reached the
allowable limit of CPU cores. Limit is 0 cores, currently 0 cores allocated and
workload requested 0.05 cores.
```

The queue has no quota for the resource the pod asked for. Almost always a queue that sets
`gpu` and leaves `cpu` and `memory` unset — they default to `0`, and a `limit` of `0` is a
real ceiling of zero, not an absent one.

```bash
kubectl get queue research-a100 -o jsonpath='{.spec.resources}' | jq
```

Set the missing resources on the project's queue, using `-1` for "no ceiling".

### The pod is annotated for one node pool but will not schedule there

```text
Warning  Unschedulable  kai-scheduler  no nodes with enough resources were found:
1 node(s) didn't match Pod's node affinity/selector.
```

Compare the two halves — they disagree:

```bash
kubectl get pod <pod> -n <ns> -o jsonpath='{.spec.affinity.nodeAffinity}' | jq
kubectl get podgroup -n <ns> -o custom-columns=\
NAME:.metadata.name,POOL:'.metadata.labels.kai\.scheduler/node-pool'
```

The `kai.scheduler/node-pools` annotation sets the node pool the workload is *charged to*,
but admission does not read it — it still applies the project's `defaultNodePools` as
required node affinity. So the PodGroup moves and the pod's affinity does not.

Set matching node affinity as well, or use affinity alone. See
[placing workloads across node pools](place-workloads-across-node-pools.md#by-node-affinity--recommended).

## Webhooks

### Everything is rejected with a webhook error

```text
failed calling webhook "...": connect: connection refused
```

All KRM webhooks use `failurePolicy: Fail`, so an unreachable controller means the API
server rejects the resources it intercepts. Check the controller is running:

```bash
kubectl -n kai-resource-management get pods
kubectl get validatingwebhookconfiguration,mutatingwebhookconfiguration | grep kai
```

**Never scale a controller to zero to disable its webhook** — that is exactly this
failure. Turn the webhook off through its chart value instead, which removes the
configuration and tells the controller to stop serving it, in step.

### A certificate problem after `helm template` or with ArgoCD

The chart reuses an existing serving certificate by looking it up in the cluster, and that
lookup returns nothing during an offline render. So a fresh certificate is minted on every
render, and the pods must roll for it to take effect. See the [chart
documentation](../../deployments/kai-resource-management-chart/README.md#tls-certificates).

## Collecting logs

```bash
NS=kai-resource-management
for d in krm-operator nodepool-controller project-controller pod-group-assigner; do
  echo "===== $d"
  kubectl -n "$NS" logs "deploy/$d" --tail=200
done
```

Raise verbosity on one service with its `args.debug` chart value, for example
`--set nodePoolController.args.debug=true`, and upgrade.

## Still stuck

Gather this before opening an issue:

```bash
kubectl get krmconfig krm-config -o yaml
kubectl get nodepool,project,department -o yaml
kubectl -n kai-resource-management get deploy,pods
```

Report it at
[github.com/kai-scheduler/kai-resource-management/issues](https://github.com/kai-scheduler/kai-resource-management/issues).
