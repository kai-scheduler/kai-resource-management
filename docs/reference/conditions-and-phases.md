# Conditions and phases

Every status value KAI Resource Management reports, what it means, and what to do about
it.

A general rule worth internalising: **a KRM object that appears stuck is usually waiting,
not broken**, and it says what it is waiting for. Read the condition before reading logs.

```bash
kubectl get <kind> <name> -o jsonpath='{.status.conditions}' | jq
```

---

## NodePool phases

`status.phase`, with `status.message` explaining it.

| Phase | Meaning | Accepts workloads? | What to do |
| --- | --- | --- | --- |
| `Ready` | At least one node in the node pool is ready | Yes | Nothing |
| `Empty` | The node pool has no nodes | Yes | Nothing, if intended. Otherwise check `labelValue` matches your nodes exactly |
| `Unschedulable` | The node pool has nodes but none are ready | No | Check node health, and for nodes cordoned mid-move |
| `MissingPrerequisites` | Nodes are ready, but an enabled scheduling feature cannot be honoured | Yes | Read `status.message`. See [NUMA prerequisites](../how-to/tune-per-node-pool-scheduling.md#numa-aware-scheduling) |
| `Deleting` | Being deleted, and something is still holding it | No | Read the conditions — a project reference, or nodes still draining |

`Empty` is not an error. A node pool with no nodes yet is a valid placement target.

`MissingPrerequisites` is a warning, not a blockage: work is still placed on the node
pool, it just may not get the alignment you asked for.

### NodePool conditions

| Condition | `True` means | What to do |
| --- | --- | --- |
| `ProjectReferencesExist` | A project still has a queue for this node pool, blocking deletion | The message names the projects. Remove those queues, or delete the projects |
| `MissingNrtHealthyPrerequisite` | A node in this NUMA-enabled node pool has missing or invalid topology data | The message names the unmet prerequisite. See below |
| `NodeTopologyMismatch` | A node is missing labels the node pool's network topology requires | Add the missing node labels, or clear `preferredNetworkTopologyName` |

Absent is the same as `False` — these conditions are only added once they first become
true.

### Per-node status

`status.nodes[].status`:

| Value | Meaning |
| --- | --- |
| `Ready` | Schedulable and healthy |
| `Unschedulable` | Not ready — cordoned, or failing its `Ready` condition |
| `MissingNrtHealthyPrerequisite` | Ready, but its NUMA topology data is missing or unusable |

`status.nodes[].topologyMismatch: true` means the node lacks a label the node pool's network
topology requires.

### The three NUMA prerequisite messages

| Message | Cause | Fix |
| --- | --- | --- |
| `NodeResourceTopology custom resource missing` | No topology object for this node, or its CRD is not installed | Install or repair the topology agent |
| `NodeResourceTopology custom resource invalid` | The object exists but exposes no NUMA-node zones | Check the agent is reporting correctly |
| `Kubelet Topology Manager Policy misconfigured` | The node's Topology Manager policy is `none`, so nothing is enforced | Set a policy other than `none` and restart kubelet |

---

## Project phases

| Phase | Meaning |
| --- | --- |
| `Ready` | Everything the project needs exists |
| `NotReady` | Something does not. The conditions say what |

### Project conditions

| Condition | `False` means | What to do |
| --- | --- | --- |
| `NamespaceReady` | The namespace could not be created, found, or updated | Read the message. If `spec.namespace` is set, that namespace must already exist |
| `QueuesReady` | A queue could not be created or updated | Usually a referenced node pool is missing, or a name collides |
| `RoleBindingsReady` | A RoleBinding could not be replicated into the namespace | Check the ClusterRole it binds exists |

Plus **one condition per configured delete-blocker group**, named after the group. These
only matter during deletion — `False` means resources of that kind are still present in the
namespace and the project cannot go. The message lists them.

Delete-blocker groups are configured at install time via the chart's
`projectController.deleteBlockers`. With none configured, there are no such conditions and
nothing blocks a project's deletion.

### Common `NamespaceReady` messages

| Message | Cause |
| --- | --- |
| `Can't find namespace for project` | `spec.namespace` names a namespace that does not exist |
| `Failed to create namespace for project` | The controller lacks permission, or the name is taken |
| `'kai/project' label missing from namespace` | The namespace already belongs to a different project. One namespace, one project |

---

## Department conditions

| Condition | `True` means | What to do |
| --- | --- | --- |
| `DepartmentDeletionBlocked` | Projects still name this department as parent | The message names one. Delete or re-parent them |

A department has no phase — it either exists or it is waiting to be deleted.

---

## ManagedNodesConfig conditions

| Condition | Status | Reason | Meaning |
| --- | --- | --- | --- |
| `Applied` | `True` | `AllNodesIncludedCorrectly` | Every node is where the criteria say it should be |
| `Applied` | `False` | `ToBeExcludedNodes` | Nodes are draining before exclusion. **In progress, not failed** |

The message lists up to five node names, then `and N more`. Nothing is ever evicted — a
node moves once its current node pool's workloads finish. Delete them if you need it out
now.

`status.observedGeneration` below `metadata.generation` means your latest edit has not been
acted on yet.

---

## KRMConfig conditions

The first thing to check when an installation does not come up.

| Condition | `True` means | `False` — what to check |
| --- | --- | --- |
| `Ready` | Everything below is fine | Read the others; this one only summarises |
| `Deployed` | Every object the installation needs exists | `kubectl -n <ns> get deploy`, then operator logs |
| `Available` | Every deployed workload reports available | `kubectl -n <ns> get pods`, then describe the failing one |
| `DependenciesFulfilled` | Nothing the installation needs is missing | The message names what is missing |
| `Reconciling` | A reconcile is in flight | Normal, transiently. Persistently `True` means it cannot finish |

```bash
kubectl get krmconfig krm-config -o jsonpath='{.status.conditions[?(@.type=="Ready")]}' | jq
```

Reasons attached to these are the condition name or its negation —
`Deployed`/`NotDeployed`, `Available`/`NotAvailable`, `Ready`/`NotReady`,
`DependenciesFulfilled`/`DependenciesMissing`, `Reconciled`/`Reconciling`.

### What `DependenciesFulfilled` covers

The chart bundles KAI Scheduler as a subchart, so a default install brings it
along — but KRM does not own it from then on. It can be upgraded or uninstalled
on its own afterwards, and an installation can be pointed at a scheduler someone
else put there. However they got there, the CRDs have to be present, and the
operator cannot assume they still are. It re-checks two things on every reconcile.

**The scheduler itself.** The cluster-scoped `Config` named `kai-config` can be
read, and reports `Ready`. One read covers both an uninstalled scheduler — the
`kai.scheduler/v1` API does not resolve at all — and an installed one whose CR
was never applied. The readiness verdict is KAI's own, repeated rather than
re-derived, so the two never disagree:

```text
KAI Scheduler is not installed: no kai.scheduler/v1 Config API
KAI Scheduler Config "kai-config" does not exist
KAI Scheduler Config "kai-config" is not ready: <what KAI says>
KAI Scheduler Config "kai-config" has not reported readiness
```

The last is normal for a few seconds after KAI is installed, before its operator
first reconciles the CR.

Its version is checked against the oldest this release supports. Both this and
readiness are reported, so neither hides the other — a scheduler downgraded past
the minimum usually reports itself unready as well:

```text
KAI Scheduler v0.14.2 is older than the minimum supported v0.17.0
```

The version is read from the `kai-operator` Deployment's image tag, the only
place the running version is written down, falling back to its `MS_TAG`
environment variable when the image is pinned by digest. A tag that is not a
version — `latest`, or an air-gapped mirror's own — is skipped rather than
reported, because guessing wrong would hold back an installation that is fine;
`--min-kai-scheduler-version=0.0.0` accepts any version.

**The CRDs each enabled service reads**, serving the API version it reads them
through:

| Service | CRDs |
| --- | --- |
| nodepool-controller | `schedulingshards.kai.scheduler/v1`, `topologies.kai.scheduler/v1alpha1`, `podgroups.scheduling.run.ai/v2alpha2` |
| project-controller | `queues.scheduling.run.ai/v2` |
| pod-group-assigner | `queues.scheduling.run.ai/v2`, `podgroups.scheduling.run.ai/v2alpha2`, `topologies.kai.scheduler/v1alpha1` |

A CRD that exists but no longer serves the listed version counts as missing —
that is what a scheduler downgrade looks like. A service turned off in the
`KRMConfig` is not checked, because nothing was deployed to depend on it.

Everything unmet lands in the one message, separated by `;`:

```text
ProjectController is missing CRD queues.scheduling.run.ai/v2; KAI Scheduler Config "kai-config" does not exist
```

Nothing here fails the reconcile: the operator keeps deploying and reporting
`Deployed` and `Available` truthfully, and clears the condition on its own once
the dependency is back. Because nothing watches those CRDs, that takes up to one
`--dependency-check-interval` (default one minute; `0` turns the periodic
re-check off).

---

## Where a phase is *not* reported

Two things people look for and do not find:

- **Queues have their own status**, maintained by KAI Scheduler rather than KRM. A
  project's `status.nodePoolsQuotaStatuses` surfaces the useful part —
  `requested`, `allocated`, `allocatedNonPreemptible` per node pool.
- **PodGroups report scheduling conditions**, also from KAI Scheduler. When a workload
  cannot be placed, that is where the reason is:

  ```bash
  kubectl get podgroup -n <ns> -o jsonpath='{.items[0].status}' | jq
  ```

## See also

- [Troubleshooting](../how-to/troubleshooting.md) — organised by symptom rather than by
  object.
- [API reference](api.md) — the spec fields these statuses report on.
