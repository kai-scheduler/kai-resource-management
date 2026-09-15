# Partition nodes into node pools

**Goal:** split a mixed cluster into node pools, so different hardware is treated as
different capacity with independent quota and scheduling.

**You need:** KRM installed, and permission to label nodes.

## 1. Decide the boundary before you decide the label

A node pool boundary is worth drawing when the two sides are genuinely different capacity.
Good reasons:

- **Different hardware.** H100s and A100s are not interchangeable, and quota measured in
  "GPUs" is meaningless across them.
- **Different scheduling behaviour.** One node pool bin-packs for throughput, another
  spreads for latency. Each node pool gets its own scheduler, so this is only expressible
  per pool.
- **Dedicated capacity.** Nodes a team paid for, which nobody else should borrow.

Poor reasons — these are better expressed with plain node affinity on the workload:

- A property only some workloads care about, where the rest of the cluster is fungible.
- Anything that changes frequently. The label pair is immutable after creation, so a node
  pool is not a good place to encode something you will want to re-cut.

## 2. Find the label you already have

Look before you invent one:

```bash
kubectl get nodes -o custom-columns=\
NAME:.metadata.name,GPU:'.metadata.labels.nvidia\.com/gpu\.product',TYPE:'.metadata.labels.node\.kubernetes\.io/instance-type'
```

```text
NAME       GPU                     TYPE
worker-1   NVIDIA-H100-80GB-HBM3   p5.48xlarge
worker-2   NVIDIA-H100-80GB-HBM3   p5.48xlarge
worker-3   NVIDIA-A100-SXM4-80GB   p4d.24xlarge
worker-4   <none>                  m6i.8xlarge
```

That cluster partitions itself: two H100 nodes, one A100 node, one CPU-only node.
`nvidia.com/gpu.product` is maintained by the NVIDIA GPU Operator, so a node pool built on
it picks up new nodes with no further action from anyone.

To see everything a node carries:

```bash
kubectl get node worker-1 -o jsonpath='{.metadata.labels}' | jq
```

Label by hand only when nothing existing expresses the boundary:

```bash
kubectl label node worker-5 example.com/reserved-for=research
```

## 3. Create one node pool per group

```yaml
apiVersion: kai.resources/v1alpha1
kind: NodePool
metadata:
  name: h100
spec:
  labelKey: nvidia.com/gpu.product
  labelValue: NVIDIA-H100-80GB-HBM3
---
apiVersion: kai.resources/v1alpha1
kind: NodePool
metadata:
  name: a100
spec:
  labelKey: nvidia.com/gpu.product
  labelValue: NVIDIA-A100-SXM4-80GB
```

Note that two node pools **may** share a `labelKey` — what must be unique is the key *and*
value together. Splitting one dimension into several node pools is the normal case.

The CPU-only node needs nothing. It stays in the `default` node pool, which catches
everything no other pool claims.

Copy `labelValue` from the output above rather than typing it. It is matched exactly, and
a mismatched value produces a node pool that is `Empty` rather than an error.

Confirm the split:

```bash
kubectl get nodepool -o custom-columns=NAME:.metadata.name,PHASE:.status.phase
for pool in h100 a100 default; do
  echo "== $pool"
  kubectl get nodepool "$pool" -o jsonpath='{.status.nodes[*].name}'; echo
done
```

## 4. Give the node pools quota

A node pool with no queue holds capacity that nothing can be charged against. Add a queue
per pool to each project and department that should use it:

```yaml
spec:
  queues:
    - name: research
      nodepool: default
      resources:
        gpu: { deserved: 0, limit: 0, overQuotaWeight: 0 }
        cpu: { deserved: 4000, limit: -1, overQuotaWeight: 1 }
        memory: { deserved: 16000, limit: -1, overQuotaWeight: 1 }
    - name: research-h100
      nodepool: h100
      resources:
        gpu: { deserved: 4, limit: 8, overQuotaWeight: 1 }
        cpu: { deserved: 16000, limit: -1, overQuotaWeight: 1 }
        memory: { deserved: 64000, limit: -1, overQuotaWeight: 1 }
    - name: research-a100
      nodepool: a100
      resources:
        gpu: { deserved: 2, limit: 4, overQuotaWeight: 1 }
        cpu: { deserved: 8000, limit: -1, overQuotaWeight: 1 }
        memory: { deserved: 32000, limit: -1, overQuotaWeight: 1 }
```

**Set `cpu` and `memory`, not just `gpu`.** Every unset field defaults to `0`, and a `limit`
of `0` is a real ceiling of zero — a GPU-only queue rejects every pod that requests CPU,
with `OverLimit: ... Limit is 0 cores`. Write `-1` for "no ceiling".

Quota does not cross node pools: idle A100 quota does not become H100 quota. See
[queues and quota](../concepts/queues-and-quota.md).

## What happens to workloads already running

Nothing is evicted, ever. When you create a node pool that claims nodes currently in another
pool, each node moves only once no workload of its old pool is still running on it.

Until then the node is cordoned and the new node pool reports what it is waiting for:

```bash
kubectl get nodepool h100 -o jsonpath='{.status.message}'
```

```text
The following node(s) are assigned to this node pool and will be added once they
finish draining: worker-1, worker-2
```

That is a wait, not a failure. If the workloads are long-running and you want the capacity
now, delete them. See [node
pools](../concepts/node-pools.md#how-a-node-moves-between-node-pools).

Plan the cut for a quiet window if the cluster is busy.

## Re-cutting a node pool later

`labelKey` and `labelValue` are immutable — you cannot re-target a node pool in place. To
change the boundary:

1. Create the new node pool alongside the old one.
2. Move each project's queues over to it.
3. Wait for workloads charged to the old node pool's queues to drain.
4. Delete the old node pool. It refuses while any project still has a queue for it, which is
   the ordering being enforced for you.

The alternative — relabelling nodes so they fall out of the old node pool and into the new
one — works too, and drains node by node rather than all at once.

## Common mistakes

| Symptom | Cause |
| --- | --- |
| Node pool is `Empty` but nodes look right | `labelValue` does not match exactly. Compare with `kubectl get node <node> --show-labels` |
| `nodepool ... labelKey ... duplicates existing nodepool` on create | Another node pool already has that exact key/value pair |
| `must set a non-empty labelKey and labelValue` | Only the `default` node pool may leave them empty |
| Nodes never leave the old node pool | Workloads of the old pool are still running on them |
| Workloads land in `default` unexpectedly | The project has no queue for the node pool you expected, or no `defaultNodePools` |

## See also

- [Node pools](../concepts/node-pools.md) — the concept in full.
- [Tune per-node-pool scheduling](tune-per-node-pool-scheduling.md) — now that pools are
  separate, give them different scheduling behaviour.
- [Exclude nodes from management](exclude-nodes-from-management.md) — for nodes that
  should be in no node pool at all.
