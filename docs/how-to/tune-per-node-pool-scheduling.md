# Tune per-node-pool scheduling

**Goal:** give one node pool different scheduling behaviour from another — bin-pack here
and spread there, guarantee minimum runtimes, weigh fairness by past usage, or enable
NUMA-aware placement.

**You need:** KRM installed and at least one node pool.

## Why this is per node pool

Each node pool gets its own scheduler instance — a KAI Scheduler `SchedulingShard` named
after the node pool. That is what makes these settings a per-pool choice rather than a
cluster-wide one: an inference pool can spread for latency while a training pool packs for
throughput, in the same cluster.

You configure the shard through the node pool's `schedulingShardConfig`. You never edit
the `SchedulingShard` itself — it is derived, and your edits are overwritten on the next
reconcile.

```yaml
apiVersion: kai.resources/v1alpha1
kind: NodePool
metadata:
  name: h100
spec:
  labelKey: nvidia.com/gpu.product
  labelValue: NVIDIA-H100-80GB-HBM3
  schedulingShardConfig:
    placementStrategy:
      gpu: binpack
      cpu: binpack
```

Every field is optional. Unset means the scheduler's own default applies.

Check what your change produced:

```bash
kubectl get schedulingshard h100 -o jsonpath='{.spec}' | jq
```

## Placement strategy

The one most people want.

```yaml
schedulingShardConfig:
  placementStrategy:
    gpu: binpack      # or spread
    cpu: binpack      # or spread
```

| Strategy | Effect | Choose it for |
| --- | --- | --- |
| `binpack` (default) | Fill nodes before starting new ones | Training. Leaves whole nodes free for large gang-scheduled jobs, and lets idle nodes scale down |
| `spread` | Distribute across as many nodes as possible | Inference and serving. Less contention per node, smaller blast radius |

GPU and CPU are set independently.

## Minimum runtimes

Stop short-lived work being killed the moment it starts.

```yaml
schedulingShardConfig:
  minRuntime:
    preemptMinRuntime: 5m
    reclaimMinRuntime: 10m
```

| Field | Guards against |
| --- | --- |
| `preemptMinRuntime` | A higher-priority workload preempting this one |
| `reclaimMinRuntime` | Another queue reclaiming quota this one had borrowed |

Both are durations. They buy a workload a floor of useful runtime before it can be
interrupted, at the cost of making the node pool slower to respond to a reclaim. Longer
values mean more wasted work when something *does* need the capacity back.

## Usage-aware fairness

By default fairness looks only at what a queue is using *right now*. A team that hammered
the cluster all week and a team that used nothing arrive at Friday with equal claim to
spare capacity.

`timeBasedFairShare` weighs past usage into that decision.

```yaml
schedulingShardConfig:
  timeBasedFairShare:
    enabled: true
    historicalUsageWeight: 1.0
    halfLifePeriod: 24h
    window:
      type: sliding
      size: 7d
```

| Field | Meaning | Default |
| --- | --- | --- |
| `enabled` | Turns the whole feature on. Everything else is ignored while false | `false` |
| `historicalUsageWeight` | How strongly past usage lowers current fair share. `0` ignores history; `1.0` is strong; above 1 stronger | `1.0` |
| `halfLifePeriod` | How fast old usage decays. `24h` means week-old usage counts about an eighth as much as yesterday's. `0` weighs all history equally | `0` |
| `window.type` | `sliding` (rolling period), `tumbling` (fixed buckets that reset), or `cron` | `sliding` |
| `window.size` | Length of that window | `7d` |

Durations accept `m`, `h` and `d`.

`tumbling` needs `window.tumblingStartTime` as an anchor; `cron` needs
`window.cronString`. `sampling` tunes how often usage is fetched and when it goes stale —
the defaults are sensible and most people should leave it unset.

> **This needs Prometheus.** Usage is read from the metrics backend. Without one, enabling
> it changes nothing observable, and the shard has no data to weigh.

## NUMA-aware scheduling

Align a workload's CPUs, memory and devices on the same NUMA node, which matters for
latency-sensitive and high-bandwidth workloads.

```yaml
schedulingShardConfig:
  plugins:
    numa:
      enabled: true
```

**This one has prerequisites, and turning it on without them changes the node pool's
phase.** Each node in the pool needs:

1. A `NodeResourceTopology` object — published by a topology agent such as the
   NUMA Resources Operator or `node-feature-discovery`'s topology-updater. Its CRD must be
   installed.
2. That object must expose NUMA-node zones.
3. Its kubelet Topology Manager policy must actually enforce alignment — a policy of
   `none` does not.

When any node fails one of those, the node pool goes to `MissingPrerequisites`:

```bash
kubectl get nodepool h100 -o custom-columns=NAME:.metadata.name,PHASE:.status.phase
kubectl get nodepool h100 -o jsonpath='{.status.message}'
```

```text
The following prerequisites are not met on one or more nodes in the node pool:
NodeResourceTopology custom resource missing. As a result, NUMA-aware scheduling
may be impacted.
```

Three distinct messages map to the three prerequisites above:

| Message | Fix |
| --- | --- |
| `NodeResourceTopology custom resource missing` | Install or repair the topology agent; check its CRD exists |
| `NodeResourceTopology custom resource invalid` | The object exists but exposes no NUMA zones |
| `Kubelet Topology Manager Policy misconfigured` | Set a kubelet Topology Manager policy other than `none` and restart kubelet |

Per-node detail:

```bash
kubectl get nodepool h100 -o jsonpath='{.status.nodes}' | jq
```

Nodes failing the check report `MissingNrtHealthyPrerequisite`.

`MissingPrerequisites` does **not** stop workloads being placed on the node pool — it is a
warning that NUMA alignment may not be honoured, not a blockage. Turning the plugin back
off clears it.

## Restrict scheduling to labelled nodes

By default the scheduler considers every node in the node pool, and places a workload
wherever it fits. `restrict-node-scheduling` narrows that to nodes you have explicitly
marked as workers, and stops GPU work landing on CPU nodes.

```yaml
schedulingShardConfig:
  args:
    restrict-node-scheduling: "true"
```

**Label your nodes before turning this on.** While it is enabled the scheduler drops every
node carrying neither the CPU nor the GPU worker label out of its cache entirely — an
unlabelled node becomes invisible, not merely deprioritised, and workloads on that pool
stop being placed.

```bash
kubectl label node <node> node-role.kubernetes.io/gpu-worker=""
kubectl label node <node> node-role.kubernetes.io/cpu-worker=""
```

Two things change once it is on:

| Effect | Result |
| --- | --- |
| Unlabelled nodes are filtered out of the scheduler's cache | Only labelled nodes are candidates at all |
| GPU and CPU work are separated | A workload requesting a GPU is refused a node without the GPU label; one requesting no GPU is refused a node without the CPU label |

### Changing the label keys

The keys themselves are set on the nodepool-controller, not per node pool, because the
controller writes them into every shard it creates:

| Flag | Default |
| --- | --- |
| `--cpu-worker-node-label-key` | `node-role.kubernetes.io/cpu-worker` |
| `--gpu-worker-node-label-key` | `node-role.kubernetes.io/gpu-worker` |
| `--mig-worker-node-label-key` | `node-role.kubernetes.io/mig-enabled` |

Override them only when your nodes already carry a different vocabulary — a cluster
migrated from another distribution, for instance. Changing a key without relabelling the
nodes has the same effect as leaving them unlabelled.

The `KRMConfig` does not model these flags, so set them through the nodepool-controller's
`extraArgs`, which the operator appends after every other argument:

```yaml
nodePoolController:
  extraArgs:
    - --cpu-worker-node-label-key=node-role.kubernetes.io/my-cpu-worker
    - --gpu-worker-node-label-key=node-role.kubernetes.io/my-gpu-worker
    - --mig-worker-node-label-key=node-role.kubernetes.io/my-mig-enabled
```

> **The MIG key is different.** It is read whether or not `restrict-node-scheduling` is on:
> the scheduler uses it to decide a node is MIG-enabled, falling back to detecting MIG
> resources when the label is absent. The CPU and GPU keys are inert while the feature is
> off.

## Other plugins and actions

```yaml
schedulingShardConfig:
  plugins:
    gpupack:
      enabled: true
      priority: 500
  actions:
    preempt:
      enabled: false
  queueDepthPerAction:
    allocate: 100
```

`plugins` and `actions` take an `enabled` flag and a `priority` that orders them; built-in
priorities run 0–10000 in steps of 100. `queueDepthPerAction` caps how many jobs each
action tries per queue, which trades scheduling thoroughness for latency on a large
cluster.

Plugin and action names come from KAI Scheduler, not from KRM. Consult its documentation
for the available set before enabling one.

## Raw scheduler flags

An escape hatch for anything the fields above do not model:

```yaml
schedulingShardConfig:
  args:
    verbosity: "4"
```

Keys are the scheduler's own flag names, matched exactly. Use this sparingly — it is
unvalidated, and a bad key is only discovered when the shard's scheduler fails to start.

## Applying to an existing node pool

`schedulingShardConfig` is mutable — unlike `labelKey` and `labelValue`. Edit the node
pool and the shard follows:

```bash
kubectl edit nodepool h100
kubectl get schedulingshard h100 -o jsonpath='{.spec}' | jq
```

The shard's scheduler restarts to pick up the change, so its node pool is briefly not
scheduling. Already-running workloads are unaffected. Change one thing at a time on a busy
cluster.

## Common mistakes

| Symptom | Cause |
| --- | --- |
| Edits to the `SchedulingShard` keep disappearing | It is derived from the node pool. Edit `schedulingShardConfig` instead |
| Node pool went to `MissingPrerequisites` after enabling NUMA | Expected without the topology prerequisites. Read `status.message` |
| `timeBasedFairShare` seems to do nothing | `enabled` is not `true`, or there is no Prometheus to read usage from |
| A plugin name is not recognised | Names come from KAI Scheduler; check its documentation |
| The shard's scheduler will not start | A bad key or value in `args` |
| Nothing schedules on a pool after enabling `restrict-node-scheduling` | Its nodes carry neither worker label, so the scheduler dropped them from its cache |
| Changing a worker-label key had no effect | `restrict-node-scheduling` is off, so the CPU and GPU keys are never read |

## See also

- [Node pools](../concepts/node-pools.md) — including the phases this can change.
- [Partition nodes into node pools](partition-nodes-into-node-pools.md) — you need
  separate node pools before per-pool tuning means anything.
- [KAI Scheduler](https://github.com/kai-scheduler/KAI-Scheduler) — the plugins, actions
  and flags themselves.
