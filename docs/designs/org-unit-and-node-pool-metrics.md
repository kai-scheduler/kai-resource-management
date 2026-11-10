# Node pool, project and department metrics

| | |
| --- | --- |
| Status | Draft |
| Owners | @natasharomm, @noiyemini |
| Issue | #184 |
| Scope | krm-operator, project-controller, KRM API module (one field), a new rules package |
| Depends on | KAI queue-controller metrics and a new KAI per-pod GPU metric, kube-state-metrics, kubelet cAdvisor, node-exporter, DCGM exporter; gpu-fractioning metricsd for shared GPUs |

## Goal

Prometheus recording rules for capacity, quota, allocation and utilization per node pool,
project and department, ready for dashboards and alerts.

## Overview

The rules are Go code, built by krm-operator into one `PrometheusRule`. They build on
metrics that already exist, plus two small changes:

1. **Org-unit labels on KAI's queue metrics.** krm-operator configures KAI's
   queue-controller to copy each queue's project, department and node pool labels onto
   its metrics (similar to what runai does today).
2. **Two new KRM metrics**, exported by project-controller:
   - each project's namespace, so pods can be attributed to projects;
   - each queue's ancestors, so usage rolls up a department tree of any depth.

Rules are layered so each level is a plain sum of the one below:

```text
pod metrics ──┐
              ├─► _per_queue ─► _per_project / _per_department
KAI queues ───┘        │
                       └─────► _per_nodepool ◄── _per_node ◄── node metrics
```

A `_per_queue` series covers the queue's whole subtree, so a project or department is the
sum of its own queues and never needs to know what sits below it.

`<p>` below is KRM's metrics namespace (the nodepool-controller's, default `kai`). `<k>` is
KAI's queue-controller metrics namespace, read from `kai-config`
(`spec.queueController.metricsNamespace`, default `kai`).

## Implementation

### 1. Queue labels on KAI metrics

KAI's queue-controller copies queue labels onto its `<k>_queue_*` metrics when
`kai-config` sets `spec.queueController.queueLabelToMetricLabel`. KRM queues already carry
the three labels it needs: project, department name and node pool.

- krm-operator writes two fields on `kai-config`, bundled or pre-existing KAI alike:
  - `queueLabelToMetricLabel`: `<project label key>=project,<department label key>=department,<node pool label key>=nodepool`
  - `queueLabelToDefaultMetricValue`: `<node pool label key>=<default node pool name>`
  - same as runai does today
- It is a merge patch of only these fields. KAI applies `kai-config` server-side and never
  sends them, so KAI upgrades keep them.
- An existing value is extended, not replaced. If a key KRM needs is already mapped to a
  different label, KRM leaves the field alone and reports a KRMConfig condition. Resolving
  that is the admin's job, and the co-install docs say so.
- The write is a step in the krm-operator reconcile, outside the operand machinery, so
  `kai-config` never gets an owner reference. Uninstall leaves the fields; they are
  harmless.
- RBAC: krm-operator gains `patch` on `configs`, resource name `kai-config`.

Result: `<k>_queue_*{queue_name, project, department, nodepool}`.
- Project queues have `project` set.
- Department queues have `project=""`.
- Queues KRM does not own (e.g. pre-existing KAI queues) have both empty, which keeps them
  visible.

### 2. project-controller metrics

project-controller exports, for every Project:

```text
<p>_project_namespace_info{project, department, namespace} 1
```

and, for every Queue, one series per queue on its path to the root, itself included:

```text
<p>_queue_ancestor_info{queue_name, ancestor_queue} 1
```

For example, with the department `research` holding the department `vision`, which holds the
project `team-a`, on the node pool `h100`:

```text
<p>_queue_ancestor_info{queue_name="team-a-h100",   ancestor_queue="team-a-h100"}   1
<p>_queue_ancestor_info{queue_name="team-a-h100",   ancestor_queue="vision-h100"}   1
<p>_queue_ancestor_info{queue_name="team-a-h100",   ancestor_queue="research-h100"} 1
<p>_queue_ancestor_info{queue_name="vision-h100",   ancestor_queue="vision-h100"}   1
<p>_queue_ancestor_info{queue_name="vision-h100",   ancestor_queue="research-h100"} 1
<p>_queue_ancestor_info{queue_name="research-h100", ancestor_queue="research-h100"} 1
```

- **Why queues, not departments.** The path follows each Queue's `spec.parentQueue`, the tree
  KAI actually schedules with. It can differ from the department tree:
  - a sub-department's queue is a root queue in a node pool its parent department has no
    queue in;
  - project-controller keeps a department's queue a root queue when nesting it would close a
    loop.
- **Why the whole path, not just the parent.** PromQL cannot walk a tree of unknown depth.
  With the whole path, rolling a subtree up is one join and one `sum by (ancestor_queue)`.
- **Every Queue, owned or not.** That answers question 1 (resolved below) and costs one
  series per Queue per level. Department nesting is capped at 7 levels, so a KRM queue has at
  most 8 series.
- krm-operator passes project-controller the same metrics namespace it already passes the
  nodepool-controller.
- Both metrics are exported only while `spec.global.prometheusRule.enabled` is true, so
  turning the rules off means KRM adds no new series at all.

### 3. Rules package

`pkg/prometheus-rules` returns `[]monitoringv1.RuleGroup`:

- Written as Go with `fmt.Sprintf` label lists, one function per rule family (quota,
  allocation, capacity, CPU, memory, GPU).
- Takes `Params{MetricsPrefix, KAIMetricsPrefix}` and nothing else, so the import path is
  stable and other products can import it instead of keeping their own copy.
- Very similar to what runai does today.

### 4. PrometheusRule operand

A new operand in `pkg/operator/operands/prometheus-rules`:

- Renders the groups into one `PrometheusRule` in the KRM namespace.
- Gated by a new KRMConfig field, `spec.global.prometheusRule.{enabled, labels}`:
  - `labels` matches a Prometheus `ruleSelector`.
  - The field is an API module change, released and then pinned here.
- Skipped while the PrometheusRule CRD is absent, like ServiceMonitors.
- It never carries `kai.scheduler/accounting`, which would route it to KAI's accounting
  Prometheus only.
- A platform that embeds KRM and records its own org-unit series sets `enabled: false`.
- Also very similar to what runai does today.

### 5. Tests

- Ginkgo specs in the rules package run `promtool test rules`: fixture series in, expected
  samples out, one spec per rule.
- promtool runs from a pinned container behind a root Makefile target that `make test`
  calls.
- Operand specs cover the gate, labels and namespace. kai-config specs cover add, merge,
  collision and no-op.

## Recorded metrics

Names follow `<p>_<measure>_per_<scope>`.

| Measure | Unit | Queue | Project | Department | Node | Node pool |
| --- | --- | :-: | :-: | :-: | :-: | :-: |
| `gpu_count` | GPUs | | | | ✅ | ✅ |
| `cpu_cores_count` | cores | | | | ✅ | ✅ |
| `total_memory_bytes` | bytes | | | | ✅ | ✅ |
| `gpu_memory_total_mebibytes` | MiB | | | | ✅ | ✅ |
| `deserved_gpu_count` | GPUs | ✅ | ✅ | ✅ | | |
| `deserved_cpu_cores` | cores | ✅ | ✅ | ✅ | | |
| `deserved_memory_bytes` | bytes | ✅ | ✅ | ✅ | | |
| `allocated_gpu_count` | GPUs | ✅ | ✅ | ✅ | | ✅ |
| `allocated_cpu_cores` | cores | ✅ | ✅ | ✅ | | ✅ |
| `allocated_memory_bytes` | bytes | ✅ | ✅ | ✅ | | ✅ |
| `used_cpu_cores` | cores | ✅ | ✅ | ✅ | ✅ | ✅ |
| `used_memory_bytes` | bytes | ✅ | ✅ | ✅ | ✅ | ✅ |
| `gpu_utilization` | % | ✅ | ✅ | ✅ | ✅ | ✅ |
| `cpu_utilization` | % | ✅ | ✅ | ✅ | ✅ | ✅ |
| `memory_utilization` | % | ✅ | ✅ | ✅ | ✅ | ✅ |
| `gpu_memory_utilization` | % | | | | ✅ | ✅ |

- **Deserved** is the quota; `-1` means unlimited.
- **Allocated** is what KAI gave.
- **Used** is what is actually consumed. For a queue, project or department it counts their
  own pods only. For a node it counts everything on the machine, including system
  processes, so it is higher than the sum of the projects on that node.
- **Utilization** differs by scope:
  - Queue, project and department: how much of what they were allocated they use. Only
    their own pods count, and a department includes its sub-departments.
  - Node and node pool: how busy the hardware is. Every GPU and core counts, allocated or
    not.
- GPU utilization is the share of time the GPU was running work (see below).

### Per queue

Labels: `queue_name, project, department, nodepool`.

| Metric | Expression |
| --- | --- |
| `deserved_gpu_count`, `deserved_cpu_cores`, `deserved_memory_bytes` | `<k>_queue_deserved_gpus`, `<k>_queue_quota_cpu_cores`, `<k>_queue_quota_memory_bytes` |
| `allocated_gpu_count`, `allocated_cpu_cores`, `allocated_memory_bytes` | `<k>_queue_allocated_{gpus,cpu_cores,memory_bytes}` |
| `used_cpu_cores`, `used_memory_bytes` | sum of the pods' usage over the queue's subtree |
| `cpu_utilization`, `memory_utilization` | `100 × used / allocated` |
| `gpu_utilization` | GPU formula below, over the pods in the queue's subtree |

KAI already reports a parent queue's deserved and allocated over its subtree. Usage is built
to match in two steps:
1. **Pods to a project queue:**
   - Pod → project: by `namespace`, through `<p>_project_namespace_info`.
   - Pod → node pool: by `kube_pod_info{node}` × `<p>_node_nodepool == 1`.
   - Project and node pool → queue: through the `project` and `nodepool` labels on
     `<k>_queue_*`.
2. **Project queue to every queue above it:** join on `queue_name` with
   `<p>_queue_ancestor_info`, then `sum by (ancestor_queue)`. Each department queue gets its
   whole subtree, however deep.

CPU and memory usage:
- **CPU:** `rate(container_cpu_usage_seconds_total{container!=""}[2m])`.
- **Memory:** `container_memory_usage_bytes{container!=""}`.

#### GPU utilization

**What it means.** The share of time the GPU was running work. It is not GPU memory, and
not how much of the GPU's compute a kernel used: one small kernel running nonstop reads
100%.

**The formula.** An average over the pods, where a pod that holds more GPU counts more:

```text
gpu_utilization = Σ (pod utilization × pod GPUs) / Σ (pod GPUs)
```

Both sums run over the same pods: those with a utilization reading. A pod we cannot
measure drops out of both, so the result covers fewer pods but is never pulled down.

Example: one pod holds 2 GPUs at 50%, another holds half a GPU at 30%.
(2 × 50 + 0.5 × 30) / 2.5 = 46%. A plain average of the two pods would say 40%.

**Where each number comes from:**

| Pod | Utilization | GPUs it holds |
| --- | --- | --- |
| Whole GPUs | DCGM `DCGM_FI_DEV_GPU_UTIL`, averaged over the pod's GPUs | KAI, new per-pod metric |
| Shared GPU | metricsd `gpu_fractioning_gpu_sm_utilization_percent` | KAI, new per-pod metric |
| MIG slice | DCGM `DCGM_FI_PROF_GR_ENGINE_ACTIVE × 100`, since `GPU_UTIL` does not exist for MIG | KAI, new per-pod metric |

- **Why metricsd for shared GPUs.** DCGM measures a whole GPU and cannot split it between
  the pods sharing it. metricsd asks the GPU driver how busy each process is, and maps each
  process to its pod.
- **Why the raw metricsd value.** It is measured against the whole GPU, like DCGM. The
  weight already scales it by the pod's share. metricsd's normalized value divides by the
  share as well, which would count it twice.
- **Why KAI for the weight.** KAI decides how much GPU each pod gets, and already works it
  out for every pod, even for pods that asked for GPU memory instead of a fraction. It
  does not export it per pod yet; see resolved question 2.

### Per project and department

Labels: `project, department` / `department`, summed over node pools. A project's
`department` is its direct parent.

| Metric | Expression |
| --- | --- |
| `deserved_*` | sum of the org unit's own queues; `-1` (unlimited) if any queue is unlimited |
| `allocated_*`, `used_cpu_cores`, `used_memory_bytes` | sum of the org unit's own queues |
| `cpu_utilization`, `memory_utilization` | `100 × used / allocated` |
| `gpu_utilization` | the GPU formula above, over the org unit's subtree |

A department's own queues are those with `project=""` and `department` set to its name.
Every per-queue series already covers its subtree, so a department at any depth includes
its sub-departments and their projects, and nothing is counted twice.

### Per node and node pool

Labels: `node, nodepool, modelName` / `nodepool`; `ready` and `schedulable` on nodes are
optional. A node joins its pool through
`<p>_node_nodepool == 1`. DCGM series join on `Hostname`.

| Metric | Expression |
| --- | --- |
| `gpu_count`, `cpu_cores_count`, `total_memory_bytes` | `kube_node_status_capacity{resource="nvidia_com_gpu" / "cpu" / "memory"}` |
| `gpu_memory_total_mebibytes` | DCGM `FB_USED + FB_FREE + FB_RESERVED` |
| `allocated_*` (per node pool only) | sum of the root queues in the pool |
| `gpu_utilization` | average `DCGM_FI_DEV_GPU_UTIL` over the pool's GPUs |
| `gpu_memory_utilization` | `100 × FB_USED / gpu_memory_total` |
| `used_cpu_cores`, `used_memory_bytes` | node-exporter used CPU and memory |
| `cpu_utilization`, `memory_utilization` | `100 × used / capacity` |

A root queue has only itself as an ancestor:
`count by (queue_name) (<p>_queue_ancestor_info) == 1`. Each root queue already covers its
subtree, so summing the roots counts every queue once:
- top-level departments;
- projects with no department;
- sub-departments held as roots (see section 2);
- queues KRM does not own.

## What we need from KAI

KAI's allocation numbers come from one chain:

```text
pod ─► podgroup-controller ─► PodGroup status ─► queue-controller ─► Queue status ─► queue_allocated_*
       "what this pod holds"    sum of its pods    sum of PodGroups and child queues
```

We use that chain as is, with three changes to KAI:

1. **New: GPUs per pod.** podgroup-controller already works out how much GPU each pod
   holds. It should also export it, for example
   `<k>_pod_allocated_gpus{namespace, pod, pod_uid}`. The rules use it as the weight in GPU
   utilization. It comes from the same calculation as `queue_allocated_gpus`, so the two
   always agree.
2. **Fix: a fraction on several GPUs.** A pod with `gpu-fraction: 0.5` on 2 GPUs holds 1
   GPU, but KAI counts 0.5. It ignores `gpu-fraction-num-devices`.
3. **Fix: DRA GPUs.** They are stored under the device class name, for example
   `gpu.nvidia.com`. `queue_allocated_gpus` only reads names ending in `/gpu`, so they are
   left out.

Until 1 is released, GPU utilization counts whole-GPU pods only, which the rules can weigh
by their GPU request from kube-state-metrics. Until 2 and 3 are fixed, those pods are
under-counted in allocation, and in utilization once 1 exists.

## Limitations

- Allocation is what KAI allocated. Pods KAI does not schedule are not counted.
- Parent-queue totals lag a few seconds per tree level.
- GPU utilization needs DCGM with pod mapping turned on, and metricsd for shared GPUs. A
  pod with no reading is left out of the average, not counted as idle.
- The totals follow KAI's queue tree, not the department tree. A sub-department whose parent
  has no queue in a node pool, or that is held as a root to avoid a loop, is missing from its
  parent department's totals in that pool. Its own series and the node pool totals still
  count it.
- After a re-parent, `queue_ancestor_info` changes at once, but the queues' totals take a
  few seconds, so rollups can be off for one evaluation.

## Out of scope

- **GPU-memory allocation per project and department, and their GPU-memory utilization.**
  KAI keeps no allocated GPU memory per queue. This needs a new KAI queue metric first.
- `resources_per_nodepool`, which the shard defaults point time-based fair share at, is
  recorded by no KRM or KAI rule. It is tracked separately.
- **Allocation per node.** KAI reports allocation per queue only. A per-node number would
  be a sum of pod requests, which can differ from what KAI charges the queues.
- **Metrics per GPU.** The rules compute utilization per GPU on the way to node and node
  pool, but do not record it. It can be added later for per-GPU dashboards.

## Resolved questions

### 1. Should node pool allocation include queues KRM does not own?

**Yes, and KRM exports the tree.** Nested departments forced the question anyway:
- Summing the department queues counts a sub-department twice: once on its own and once
  inside its parent.
- It also misses projects that have no department.

The rule must sum root queues only, so it needs each queue's position in the tree. KAI's
queue metrics do not carry it.

project-controller exports `<p>_queue_ancestor_info` (section 2) for every Queue. Queues KRM
does not own are counted at no extra cost. Without a node pool label they fall into the
default node pool, through `queueLabelToDefaultMetricValue`.

The rejected options:
- **Sum only department queues.** Wrong once departments nest.
- **Export only the parent** (`<p>_queue_parent_info`). A root check needs only the parent,
  but subtree usage would need one join per level.
- **A `parent_queue` label from KAI's queue-controller.** It would help every KAI user, but
  it needs a KAI release first, and the subtree problem remains. It is worth proposing to
  KAI later; the rules could then drop the root check.

### 2. How much GPU does a shared-GPU pod hold?

**The problem.** GPU utilization weighs each pod by how much GPU it holds. For a pod with
whole GPUs that is its GPU count. For a pod on a shared GPU it is its share, for example
0.5, and no metric exports that today.

A pod can ask for its share in two ways:
- `gpu-fraction: "0.5"`: the share is the request.
- `gpu-memory: "20000"`: the share depends on the GPU it lands on. On an 80 GB GPU it is
  0.25, on a 40 GB GPU 0.5. It is only known after scheduling.

**Chosen: KAI exports it** (see What we need from KAI).
- KAI decides the share, and podgroup-controller already turns a memory request into a
  share using the node's `nvidia.com/gpu.memory` label.
- The weight and `queue_allocated_gpus` then come from the same calculation.
- KAI is always installed with KRM.
- runai works the same way: its own components write the share on the pod after scheduling
  and export it.

**Rejected:**
- **metricsd exports it.** It also knows the share, but it only runs where gpu-fractioning
  is installed, and its share is computed separately from KAI's allocation, so the two can
  disagree.
- **kube-state-metrics exposes the pod's `gpu-fraction` annotation.** It needs setup in
  every install, and gives nothing for `gpu-memory` pods.

## Security considerations

- **Writing to KAI's configuration.** krm-operator gains `patch` on the `kai-config` Config
  only, by resource name.
  - It touches two fields, and only ever adds to them.
  - It never overwrites an entry an admin set; it reports a condition instead.
  - The worst case is wrong labels on KAI's queue metrics. Scheduling does not read these
    fields.
- **New RBAC for rules.** krm-operator gains create/update/delete on `prometheusrules`. The
  rules are written only in the KRM namespace.
- **Names on metrics endpoints.**
  - The new metrics put project, department, namespace and queue names on project-controller's
    metrics port.
  - Like the other KRM controllers' ports today, it is plain HTTP with no authentication.
    Anyone who can reach the pod can read these names.
  - The same names can already be read by anyone allowed to list Projects or Queues, but
    reaching a pod is a different permission. Installs that care should restrict the port
    with a NetworkPolicy.
  - Securing the metrics endpoints is separate work, shared by all KRM controllers.
- **Tenant data in rules.** The rules carry no names or secrets, only label matchers. Each
  recorded series is as visible as the Prometheus that stores it.

## Rollout and compatibility

- The change is additive. Rollback is `spec.global.prometheusRule.enabled=false`.
- The `kai-config` fields stay, and only add labels to KAI's queue metrics.
- New series:
  - one `project_namespace_info` per Project
  - one `queue_ancestor_info` per Queue per level, at most 8 per KRM queue
  - the recorded series: a few per queue, node and node pool

## Stories

1. API module: `spec.global.prometheusRule`; release and bump the pin.
2. krm-operator: write the queue-label mapping onto `kai-config`, with merge, collision
   condition and RBAC.
3. project-controller: `<p>_project_namespace_info` and `<p>_queue_ancestor_info`.
4. Rules package, promtool harness and Makefile target; quota and allocation rules.
5. krm-operator: PrometheusRule operand (needs 1 and 4).
6. Node and node pool capacity, CPU and memory utilization rules.
7. Project and department CPU and memory usage and utilization rules, rolled up through
   `queue_ancestor_info`.
8. GPU rules: weighted utilization from DCGM and metricsd, node pool GPU memory. Whole-GPU
   pods first; shared-GPU pods once story 10 is released.
9. Docs: metrics reference, the KRMConfig field, wiring Prometheus (`ruleSelector`
   labels, `external_labels` for the cluster label, required exporters), and resolving
   `kai-config` collisions in co-install.
10. KAI: export GPUs per pod, and fix fractions on several GPUs and DRA GPUs in
    `queue_allocated_gpus` (see What we need from KAI).
