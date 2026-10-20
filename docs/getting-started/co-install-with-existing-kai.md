# Installing alongside an existing KAI Scheduler

This guide is for administrators whose cluster already runs a standalone KAI Scheduler, with
its own scheduling shards, queues and label keys, who want to add KAI Resource Management
without rebuilding any of them. On a cluster with no scheduler, follow the
[quickstart](quickstart.md) instead: it installs KAI Scheduler for you.

Read the whole guide before you install. Two choices are hard to undo once KRM is running:
which shards it takes over, and whether it may touch queues it did not create.

## What KRM takes over

**Every `SchedulingShard` you do not exclude becomes a node pool.** A shard serving partition
`P` is owned by a node pool named `P`; the shard with an empty `partitionLabelValue` is owned
by the `default` node pool. The shard keeps its name, and its settings move onto the node
pool, so from then on you change them there. The shard is labelled
`kai.resources/adoption=adopted`.

**Deleting that node pool deletes the shard**, and with it the scheduler KAI runs for that
partition. That is what ownership means, for a shard KRM took over exactly as for one it
created. See [deleting a node pool](../concepts/node-pools.md#deleting-a-node-pool).

**Queues and workloads KRM did not create are left alone**, as long as you install with
`allowExternalQueues` on (step 2). Your existing queue hierarchy, and the workloads that use
it, keep scheduling as before.

## Before you start

- **KAI Scheduler v0.18.0 or later**, already running. A build of KAI's `main` branch,
  tagged `0.0.0-<commit>`, is also accepted. The install checks this and fails on an older
  version; see [the version check](../../deployments/kai-resource-management-chart/README.md#kai-scheduler-version-check).
- **Kubernetes 1.30 or later.** The webhook that keeps your queue tree apart from KRM's
  relies on match conditions.
- **Helm 3**, and permission to create CRDs, cluster-scoped RBAC and admission webhooks.
- The Prometheus Operator's `ServiceMonitor` CRD, or install with
  `--set serviceMonitor.create=false`, as in the [quickstart](quickstart.md#prerequisites).
- **Know where KAI Scheduler runs.** KRM creates a `ServiceMonitor` per node pool beside
  the scheduler it monitors, so it needs KAI Scheduler's namespace. Find it in the KAI
  Scheduler config, and choose KRM's own namespace. The same namespace is simplest; a
  different one works too:

  ```bash
  export KAI_NAMESPACE=$(kubectl get configs.kai.scheduler kai-config \
    -o jsonpath='{.spec.namespace}')
  export KRM_NAMESPACE="${KAI_NAMESPACE}"
  echo "${KAI_NAMESPACE}"
  ```

- **A release of its own.** A release of this chart that installed KAI Scheduler cannot be
  switched to `kai-scheduler.enabled=false`: the upgrade is refused, because it would
  uninstall that scheduler.

- **A copy of what you have today**, so you can compare after installing:

  ```bash
  kubectl get schedulingshards -o yaml > shards-before-krm.yaml
  kubectl get queues -o yaml > queues-before-krm.yaml
  ```

## 1. Choose which shards KRM manages

List the shards and the partitions they serve:

```bash
kubectl get schedulingshards \
  -o custom-columns=NAME:.metadata.name,PARTITION:.spec.partitionLabelValue
```

For every partition that should stay with the scheduler serving it today, label its shard:

```bash
kubectl label schedulingshard <name> kai/ignore-shard-for-krm=true
```

KRM then creates no node pool for that partition, never writes its shard, and never
relabels, cordons or counts its nodes. See
[leaving a partition to another scheduler](../concepts/node-pools.md#leaving-a-partition-to-another-scheduler).

**Label before you install.** The install takes over every shard without the label, and once
KRM is running its webhook refuses to add, change or remove the label on an existing shard.

**The `default` shard cannot be excluded.** The shard with an empty `partitionLabelValue`
always belongs to the `default` node pool, which KRM needs. If it carries the label, the
install removes it.

While you are here, check for shards the install cannot take over. It fails, and changes
nothing, when:

- a partition is not a valid node pool name, such as `Team_A`, or is the `default` node
  pool's name;
- two shards serve the same partition.

Fix those first, or label the shards.

## 2. Match KRM to the scheduler

Three settings must agree with the KAI Scheduler, or workloads bind to the wrong queue or node
pool:

| Chart value | KAI Scheduler setting | Default when unset |
| --- | --- | --- |
| `kai-scheduler.global.nodePoolLabelKey` | `kai-config` `spec.global.nodePoolLabelKey` | `kai.scheduler/node-pool` |
| `kai-scheduler.podgrouper.queueLabelKey` | `kai-config` `spec.global.queueLabelKey` | `kai.scheduler/queue` |
| `commonArgs.schedulerName` | `kai-config` `spec.global.schedulerName` | `kai-scheduler` |

With `kai-scheduler.enabled=false`, the chart reads them from the live `kai-config` when it
installs. A value you set yourself always wins, and a key missing from `kai-config` falls back
to KAI Scheduler's own default. To see what KAI uses:

```bash
kubectl get configs.kai.scheduler kai-config -o jsonpath='{.spec.global}'
```

The install fails if KAI Scheduler reads node pools from a different label than KRM would use,
since its node pools would then select different nodes. The error names the value to set.

Also turn on **`commonArgs.allowExternalQueues`**. Without it, KRM treats every queue as its
own, and the first project or department whose name matches one of your queues takes it
over. Turning it on later does not hand a taken-over queue back. See
[queues KRM did not create](../concepts/queues-and-quota.md#queues-krm-did-not-create).

## 3. Install

```bash
helm upgrade --install krm \
  oci://ghcr.io/kai-scheduler/kai-resource-management/kai-resource-management \
  --namespace "${KRM_NAMESPACE}" \
  --create-namespace \
  --set kai-scheduler.enabled=false \
  --set schedulerNamespace="${KAI_NAMESPACE}" \
  --set commonArgs.allowExternalQueues=true \
  --wait --timeout 10m
```

`kai-scheduler.enabled=false` keeps the chart from installing a second KAI Scheduler; your
existing `kai-config` stays the single source of truth. `schedulerNamespace` puts the
per-node-pool `ServiceMonitor`s, and the permission to manage them, in KAI Scheduler's
namespace.

Before any KRM controller starts, three Helm hooks run in order:

1. **Version check.** Fails when KAI Scheduler is older than the minimum. See
   [the version check fails](../how-to/troubleshooting.md#helm-install-fails-on-the-kai-scheduler-version-check).
2. **CRDs.** Installs or upgrades KRM's custom resource definitions. KAI Scheduler's are
   left as they are.
3. **Shard migration.** Takes over every unlabelled shard as described in
   [What KRM takes over](#what-krm-takes-over), and creates the `default` node pool from
   KAI's default shard, or from `defaultNodePool.spec` when there is none.

If a shard cannot be carried over exactly, the install fails before any shard is changed,
and the error names the shard and what to change. Read it from the Job:

```bash
kubectl logs job/kai-resource-management-migrate-shards -n "${KRM_NAMESPACE}"
```

Fix the cause and run the same `helm upgrade --install` again. Each reason is listed in
[installing on a cluster that already has shards](../concepts/node-pools.md#installing-on-a-cluster-that-already-has-shards).

## 4. Verify

The notes Helm prints after installing list the scheduler name and label keys KRM is using.
Check them against step 2. `KAI default` means the key is set neither in the chart nor in
`kai-config`, so KRM uses KAI Scheduler's own default, as the scheduler does.

There is one node pool per partition KRM manages, plus `default`:

```bash
kubectl get nodepools
```

Each shard KRM took over is owned by its node pool and labelled as adopted. Each shard you
labelled has no owner:

```bash
kubectl get schedulingshards -o custom-columns=\
NAME:.metadata.name,\
PARTITION:.spec.partitionLabelValue,\
OWNER:.metadata.ownerReferences[0].name,\
ADOPTED:.metadata.labels.kai\\.resources/adoption,\
IGNORED:.metadata.labels.kai/ignore-shard-for-krm
```

Each node pool's scheduler is monitored from KAI Scheduler's namespace:

```bash
kubectl get servicemonitors -n "${KAI_NAMESPACE}"
```

Your queues are unchanged. Compare them with the copy you took:

```bash
kubectl get queues -o yaml > queues-after-krm.yaml
diff queues-before-krm.yaml queues-after-krm.yaml
```

Apart from `status` and `resourceVersion`, which change as workloads run, they should be
identical. Workloads that were running keep running, and new ones in your existing queues
schedule as before.

## Adding a shard later

On a cluster with KRM, every partition is either a node pool's or explicitly left to
another scheduler. When you add one:

- **To manage it with KRM, create a node pool.** KRM creates the shard for it. See
  [partitioning nodes into node pools](../how-to/partition-nodes-into-node-pools.md).
- **To leave it to another scheduler, create the shard with the label** in its manifest:

  ```yaml
  metadata:
    labels:
      kai/ignore-shard-for-krm: "true"
  ```

KRM's webhook rejects any other new shard:

```text
scheduling shard "team-b" cannot use partitionLabelValue "team-b": no nodepool "team-b"
exists for it; create that nodepool, or label the shard kai/ignore-shard-for-krm=true to
leave its nodes to another scheduler
```

If a shard gets in anyway, for example while the webhook was down, KRM never writes it, and a
node pool for its partition reports `Unschedulable`. Run the same `helm upgrade` again to
take the shard over, or delete it. See
[node pool is Unschedulable](../how-to/troubleshooting.md#node-pool-is-unschedulable).

## Installing without Helm hooks

`helm template | kubectl apply`, `helm install --no-hooks`, and GitOps tools that render the
chart offline differ from a plain `helm install` in two ways:

- **Nothing is read from the cluster.** The three settings in step 2 are not filled from
  `kai-config`, and KRM falls back to KAI Scheduler's defaults, which may not be the ones
  your scheduler uses. Set them explicitly, along with `schedulerNamespace` and
  `allowExternalQueues`.
- **Hooks that do not run do nothing.** Without them there is no version check, no shard is
  taken over, and in this mode no `default` node pool is created. Unlabelled shards then
  block their partitions' node pools. Run `helm upgrade --install` with hooks for the first
  install and for every upgrade, or check that your tool runs the chart's `pre-install` and
  `pre-upgrade` hooks.

The other values an offline install needs are in the chart's
[KRM operator](../../deployments/kai-resource-management-chart/README.md#krm-operator) section.

## Uninstalling

```bash
helm uninstall krm --namespace "${KRM_NAMESPACE}"
```

Once KRM's controllers are gone, a post-delete hook hands every shard KRM took over back to
the scheduler: its node pool owner reference and KRM's labels and annotations are removed,
and it stays in place and keeps scheduling. It keeps its **current** settings, including any
changes you made through its node pool since the install, not the ones it had before KRM.
Compare it with `shards-before-krm.yaml` if you want those back.

Node pools are not deleted, because Helm retains KRM's CRDs and their objects. The hook
removes their finalizers, so you can delete them afterwards without them hanging. Deleting
a node pool then deletes only a shard KRM created, never one it released. Installing KRM
again takes the released shards over again.

The hook does this only while `postCleanup.enabled` is on, so the chart refuses to render
with it off in this mode. If the hook fails, some shards may still belong to their node
pools; **do not delete those node pools** before following
[`helm uninstall` fails on the cleanup hook](../how-to/troubleshooting.md#helm-uninstall-fails-on-the-cleanup-hook).

The rest of what uninstall keeps and removes is in the chart's
[uninstall](../../deployments/kai-resource-management-chart/README.md#uninstall) section.

## Limitations

- **Queue hierarchies deeper than two levels stay unmanaged.** KRM's own tree is a department
  with projects under it. A deeper tree you built keeps working, but it cannot be turned into
  departments and projects, and a queue of yours cannot be placed under one of KRM's. See
  [keeping the two kinds of tree apart](../concepts/queues-and-quota.md#keeping-the-two-kinds-of-tree-apart).
- **No switching an existing release.** A release that installed KAI Scheduler stays
  bundled; see [Before you start](#before-you-start).

## What to read next

| Next | Why |
| --- | --- |
| [Node pools](../concepts/node-pools.md) | What a node pool does with the shard it now owns |
| [Queues and quota](../concepts/queues-and-quota.md) | How KRM's queues sit next to yours |
| [Labels and annotations](../reference/labels-and-annotations.md) | Every key KRM reads and writes |
| [Troubleshooting](../how-to/troubleshooting.md) | When something does not appear |
