# Node pool, project and department metrics

| | |
| --- | --- |
| Status | Draft |
| Scope | krm-operator, project-controller, KRM API module (one field), a new rules package |
| Depends on | KAI queue-controller metrics, kube-state-metrics, kubelet cAdvisor, node-exporter, DCGM exporter; gpu-fractioning metricsd for shared GPUs |

## Goal

Prometheus recording rules for capacity, quota, allocation and utilization per node pool,
project and department, ready for dashboards and alerts.

## Overview

The rules are Go code, built by krm-operator into one `PrometheusRule`. They build on
metrics that already exist, plus two small changes:

1. **Org-unit labels on KAI's queue metrics.** krm-operator configures KAI's
   queue-controller to copy each queue's project, department and node pool labels onto
   its metrics (similar to what runai does today).
2. **One new KRM metric**, exported by project-controller, mapping each project to its
   namespace. Utilization needs it to attribute pods to projects.

Rules are layered so each level is a plain sum of the one below:

```text
pod metrics ──┐
              ├─► _per_queue ─► _per_project / _per_department
KAI queues ───┘        │
                       └─────► _per_nodepool ◄── _per_node ◄── node metrics
```

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

### 2. Project namespace metric

project-controller exports, for every Project:

```text
<p>_project_namespace_info{project, department, namespace} 1
```

- krm-operator passes project-controller the same metrics namespace it already passes the
  nodepool-controller.
- It is exported only while `spec.global.prometheusRule.enabled` is true, so turning the
  rules off means KRM adds no new series at all.

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

### Per queue

Labels: `queue_name, project, department, nodepool`.

| Metric | Expression |
| --- | --- |
| `deserved_gpu_count`, `deserved_cpu_cores`, `deserved_memory_bytes` | `<k>_queue_deserved_gpus`, `<k>_queue_quota_cpu_cores`, `<k>_queue_quota_memory_bytes` |
| `allocated_gpu_count`, `allocated_cpu_cores`, `allocated_memory_bytes` | `<k>_queue_allocated_{gpus,cpu_cores,memory_bytes}` |
| `used_cpu_cores`, `used_memory_bytes` | sum of the pods' usage, keyed by project and node pool |
| `gpu_utilization` | GPU formula below, over the queue's pods |

Pods are attributed to a queue as follows:
- **Pod → project:** by `namespace`, through `<p>_project_namespace_info`.
- **Pod → node pool:** by `kube_pod_info{node}` × `<p>_node_nodepool == 1`.

CPU and memory usage:
- **CPU:** `rate(container_cpu_usage_seconds_total{container!=""}[2m])`.
- **Memory:** `container_memory_usage_bytes{container!=""}`.

**GPU utilization** is an allocation-weighted average over the pods, in one expression:

```text
gpu_utilization = Σ (pod GPU utilization × pod allocated GPUs) / allocated_gpu_count
```

- **Pod GPU utilization:**
  - whole GPUs: the average `DCGM_FI_DEV_GPU_UTIL` over the pod's GPUs (series carrying a
    pod label)
  - shared GPUs: metricsd's raw `gpu_fractioning_gpu_sm_utilization_percent`, measured
    against the whole GPU. Not the normalized metric: a fraction limits memory, not compute,
    so normalizing could push a pod past 100%.
- **Pod allocated GPUs:**
  - whole GPUs: the pod's GPU count
  - shared GPUs: the pod's fraction (see open question 2)

Example: 2.5 GPUs are allocated. One pod holds 2 GPUs at 50%, and a half-GPU pod uses 30%
of its GPU. The result is (2 × 50 + 0.5 × 30) / 2.5 = 46%.

### Per project and department

Labels: `project, department` / `department`, summed over node pools.

| Metric | Expression |
| --- | --- |
| `deserved_*` | sum of the queues; `-1` (unlimited) if any queue is unlimited |
| `allocated_*`, `used_cpu_cores`, `used_memory_bytes` | sum of the queues |
| `cpu_utilization`, `memory_utilization` | `100 × used / allocated` |
| `gpu_utilization` | the GPU formula above, over the org unit's pods |

Department deserved and allocated come from the department's own queues (`project=""`,
`department!=""`). KAI already sums a parent queue's subtree into it.

### Per node and node pool

Labels: `node, nodepool, modelName` / `nodepool`; `ready` and `schedulable` on nodes are
optional. A node joins its pool through
`<p>_node_nodepool == 1`. DCGM series join on `Hostname`.

| Metric | Expression |
| --- | --- |
| `gpu_count`, `cpu_cores_count`, `total_memory_bytes` | `kube_node_status_capacity{resource="nvidia_com_gpu" / "cpu" / "memory"}` |
| `gpu_memory_total_mebibytes` | DCGM `FB_USED + FB_FREE + FB_RESERVED` |
| `allocated_*` (per node pool only) | sum of the top-level queues in the pool (see open question 1) |
| `gpu_utilization` | average `DCGM_FI_DEV_GPU_UTIL` over the pool's GPUs |
| `gpu_memory_utilization` | `100 × FB_USED / gpu_memory_total` |
| `cpu_utilization`, `memory_utilization` | `100 × node-exporter used / capacity` |

## Limitations

- Allocation is what KAI allocated. Pods KAI does not schedule are not counted.
- Parent-queue totals lag a few seconds per tree level.
- DRA GPUs are missing from KAI's `queue_allocated_gpus`.
- A pod holding fractions of several GPUs may be under-counted in KAI's allocation.
- Without DCGM's pod mapping or metricsd, the matching GPU series are absent, not zero.

## Out of scope

- **GPU-memory allocation per project and department, and their GPU-memory utilization.**
  KAI keeps no allocated GPU memory per queue. This needs a new KAI queue metric first.
- `resources_per_nodepool`, which the shard defaults point time-based fair share at, is
  recorded by no KRM or KAI rule. It is tracked separately.
- **Multi-level project and department hierarchies.** The upcoming multi-hierarchy feature
  changes how org units nest. These rules must be revisited together with it.

## Open questions

### 1. Should node pool allocation include queues KRM does not own?

A cluster can keep KAI queues that KRM does not own, e.g. ones that existed before KRM was
installed. They already appear in the per-queue series with empty `project` and
`department`. The question is whether node pool allocation should count them too.

- **No:** node pool allocation is the sum of the department queues in the pool. In KAI, a
  parent queue's allocation already contains its children's, so this counts every project
  exactly once.
- **Yes:** the sum must also include the unowned queues, but only the top-level ones;
  counting a child and its parent would count the same pods twice. Unowned queues can form
  any tree, and KAI's queue metrics do not say which queue is a child of which. Two ways
  to get that:
  - **KRM exports the parent:** project-controller exports
    `<p>_queue_parent_info{queue_name, parent_queue}` for every Queue.
    - Pro: entirely in our hands.
    - Con: a second new metric, describing KAI objects from outside KAI.
  - **KAI exports the parent:** KAI's queue-controller adds a `parent_queue` label to its
    `queue_*` metrics.
    - Pro: every KAI user gets it.
    - Con: needs a KAI change and release first.

### 2. A shared-GPU pod's fraction

**The problem.** The GPU utilization formula weights each pod by its allocated GPUs. For
whole-GPU pods that is the GPU count, which is easy to get. For a pod on a shared GPU it is
the fraction it holds (e.g. 0.5), and no metric exports that today. metricsd computes it
internally for its normalized metric but does not export it.

**Option A: metricsd exports the fraction.** gpu-fractioning adds one per-pod metric with
the fraction it already computes.
- Pro: the number comes from the component that enforces it, with no setup per install.
- Con: needs a gpu-fractioning change and release.

**Option B: kube-state-metrics exposes the annotation.** The pod's GPU-fraction annotation
is allowlisted in kube-state-metrics, and the rule reads `kube_pod_annotations`.
- Pro: no code change anywhere.
- Con: every install must configure kube-state-metrics, and the rule must handle both
  fraction and GPU-memory annotations.

## Rollout and compatibility

- The change is additive. Rollback is `spec.global.prometheusRule.enabled=false`.
- The `kai-config` fields stay, and only add labels to KAI's queue metrics.
- New series:
  - one `project_namespace_info` per Project
  - the recorded series: a few per queue, node and node pool

## Stories

1. API module: `spec.global.prometheusRule`; release and bump the pin.
2. krm-operator: write the queue-label mapping onto `kai-config`, with merge, collision
   condition and RBAC.
3. project-controller: `<p>_project_namespace_info`.
4. Rules package, promtool harness and Makefile target; quota and allocation rules.
5. krm-operator: PrometheusRule operand (needs 1 and 4).
6. Node and node pool capacity, CPU and memory utilization rules.
7. Project and department CPU and memory usage and utilization rules.
8. GPU rules: allocation-weighted utilization from DCGM and metricsd, node pool GPU memory.
9. Docs: metrics reference, the KRMConfig field, wiring Prometheus (`ruleSelector`
   labels, `external_labels` for the cluster label, required exporters), and resolving
   `kai-config` collisions in co-install.
