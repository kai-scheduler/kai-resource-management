# Installing alongside an existing KAI Scheduler

This guide is for administrators whose cluster already runs a standalone KAI Scheduler, with
its own scheduling shards, queues and label keys, who want to add KAI Resource Management
without rebuilding any of them. On a cluster with no scheduler, follow the
[quickstart](quickstart.md) instead: it installs KAI Scheduler for you.

This guide assumes you know KRM's objects: node pools, projects and departments, and how they
map onto KAI Scheduler's shards and queues. If they are new to you, read the
[overview](../overview.md) first.

Read the whole guide before you install. Two choices are hard to undo once KRM is running:
which shards it takes over, and whether it may touch queues it did not create.

## What KRM takes over

| What you have today | Once KRM is installed |
| --- | --- |
| Shards you did not label | Each is owned by a node pool, with the same name and settings. Change its settings on the node pool. Deleting the node pool deletes the shard |
| Shards labelled `kai/ignore-shard-for-krm=true` | Left alone, and so are their nodes. Exception: the `default` shard is always taken over |
| Queues | Left alone by default. With `allowExternalQueues=false`, creating a project whose queue has the same name as one of yours takes that queue over |
| Workloads outside KRM's projects | Left alone |

**Every `SchedulingShard` becomes a node pool, unless explicitly excluded.** A shard serving
partition `P` is owned by a node pool named `P`; the shard with an empty `partitionLabelValue`
is owned by the `default` node pool. The shard keeps its name, and its settings move onto the
node pool, so from then on you change them there. The shard is labelled
`kai.resources/adoption=adopted`.

**The `default` node pool is KRM's catch-all.** It holds every node no other node pool
claims, and it cannot be deleted; see
[the `default` node pool](../concepts/node-pools.md#the-default-node-pool). The install
creates it from KAI's default shard, the one with an empty `partitionLabelValue`. If KAI has
no default shard, it is created from `defaultNodePool.spec`.

**Most settings move over unchanged.** The node pool takes the shard's `args`, placement
strategy, queue depth, minimum runtime, plugins, actions and time-based fair share. Settings a
node pool has no field for, such as `goMemLimitRatio` and `scenarioSearchBudgets`, stay on the
shard. If a shard uses time-based fair share, read
[the exceptions below](#if-a-shard-uses-time-based-fair-share).

**Each shard KRM takes over gets three args**, `cpu-worker-node-label-key`,
`gpu-worker-node-label-key` and `mig-worker-node-label-key`, so its scheduler restarts once.

**Deleting that node pool deletes the shard**, and with it the scheduler KAI runs for that
partition. That is what ownership means, for a shard KRM took over exactly as for one it
created. See [deleting a node pool](../concepts/node-pools.md#deleting-a-node-pool).

**The install takes over no queues and no workloads.** KRM never touches a workload in a
namespace that belongs to no KRM project. Queues KRM did not create are left alone
afterwards too while `allowExternalQueues` is `true`, the default here, so your existing
queue hierarchy and the workloads that use it keep scheduling as before. If you set it to
`false`, creating a project whose queue has the same name as one of yours takes that queue
over; see step 2.

### If a shard uses time-based fair share

Skip this unless a shard sets `usageDBConfig` or `kValue`. Three of those settings change:

- **A `kValue` without `usageDBConfig` is dropped.** It has no effect without a usage DB.
- **`connectionStringEnvVar` is dropped.** A node pool takes the usage DB's address only from
  `connectionString`. Without one, the scheduler uses the Prometheus configured in
  `kai-config`. To keep the address the shard uses today, put it in `connectionString`
  before installing.
- **`clientType` is always `prometheus`.** It is the only usage DB KAI Scheduler supports
  outside its own tests.

## Before you start

- **KAI Resource Management chart 0.19.0 or later.** Earlier charts ignore
  `kai-scheduler.enabled=false`: they try to install a second KAI Scheduler, fail on the
  existing one's resources, and leave KRM's CRDs behind.
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

  The rest of this guide uses both variables, so set them again in any new shell. If one is
  empty, Helm and kubectl quietly use your current namespace instead.

- **A release of its own.** Never switch a release of this chart that installed KAI Scheduler
  to `kai-scheduler.enabled=false`: that uninstalls the scheduler. `helm upgrade` refuses it,
  but `helm template` and GitOps tools cannot see the cluster, so nothing stops them. A
  GitOps tool that prunes deletes KAI Scheduler, and if it also manages KAI's CRDs, every
  queue, pod group and shard with them.

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

KRM takes over every shard, and we recommend letting it: manage each partition with a node
pool. Label a shard only in the specific case where its partition must stay outside KRM.
For every partition you do not want a node pool to take over, label its shard:

```bash
kubectl label schedulingshard <name> kai/ignore-shard-for-krm=true
```

KRM then creates no node pool for that partition, never writes its shard, and never
relabels, cordons or counts its nodes. See
[leaving a partition to another scheduler](../concepts/node-pools.md#leaving-a-partition-to-another-scheduler).

**Label before you install.** The install takes over every shard without the label. Once KRM
is running, its webhook refuses to add, change or remove the label on an existing shard.

**The `default` shard cannot be excluded.** The shard with an empty `partitionLabelValue`
always belongs to the `default` node pool, which KRM needs. If it carries the label, the
install removes it. It is also the one shard whose label the webhook lets you change later,
since there the label has no effect.

While you are here, check for shards the install cannot take over. When it finds one, the
install fails and changes nothing; every reason is in
[installing on a cluster that already has shards](../concepts/node-pools.md#installing-on-a-cluster-that-already-has-shards).
Before a first install, look for:

- a partition that is not a valid node pool name, such as `Team_A`, or is the `default` node
  pool's name;
- two shards serving the same partition;
- `cpu-worker-node-label-key`, `gpu-worker-node-label-key` or `mig-worker-node-label-key` in
  a shard's `args` with a value other than KRM's, which by default is
  `node-role.kubernetes.io/cpu-worker`, `node-role.kubernetes.io/gpu-worker` or
  `node-role.kubernetes.io/mig-enabled`. The same goes for any arg you set in
  `nodePoolController.args.schedulingShardArgs`.

Fix those first, or label the shards. A partition served by two shards needs **every** one
of them labelled: the install counts labelled shards too, so labelling one still fails it.

## 2. Match KRM to the scheduler

KRM must use the same scheduler name, node-pool label key and queue label key as KAI
Scheduler. With `kai-scheduler.enabled=false`, the chart reads all three from the live
`kai-config` when it installs, so leave `commonArgs.schedulerName`,
`kai-scheduler.global.nodePoolLabelKey` and `kai-scheduler.podgrouper.queueLabelKey` unset.

An offline render (`helm template`, GitOps) cannot read `kai-config` and uses KAI
Scheduler's defaults instead. If your `kai-config` sets any of the three, set the same
values in the chart.

**`commonArgs.allowExternalQueues`** controls whether KRM can take over a queue it did not
create. With `kai-scheduler.enabled=false` it defaults to `true`.

- **`true`:** KRM updates or deletes only queues its own projects and departments own, and
  leaves the rest alone.
- **`false`:** KRM treats every queue as its own. A project whose queue has the same name as
  one of yours takes that queue over, moves it under the project's department and rewrites
  its quota and priority, so any queues below it end up in KRM's tree. Turning the flag on
  later does not hand it back.

See [queues KRM did not create](../concepts/queues-and-quota.md#queues-krm-did-not-create).

## 3. Install

```bash
helm upgrade --install krm \
  oci://ghcr.io/kai-scheduler/kai-resource-management/kai-resource-management \
  --version 0.19.0 \
  --namespace "${KRM_NAMESPACE}" \
  --create-namespace \
  --set kai-scheduler.enabled=false \
  --set schedulerNamespace="${KAI_NAMESPACE}" \
  --wait --timeout 10m
```

Without the `ServiceMonitor` CRD, add `--set serviceMonitor.create=false`.

Every later `helm upgrade` of this release needs `kai-scheduler.enabled=false` too: keep it
in a values file, or pass `--reuse-values`. Without it, the upgrade fails.

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

Once the release is installed, a fourth hook applies the `KRMConfig`, which starts KRM's
controllers. Install with Helm hooks enabled: without them, none of this happens.

If a shard cannot be taken over, the install fails before any shard is changed, and the
error names the shard and what to change. Read it from the Job, which is kept only when it
fails:

```bash
kubectl logs job/kai-resource-management-migrate-shards -n "${KRM_NAMESPACE}"
```

Fix the cause and run the same `helm upgrade --install` again. Each reason is listed in
[installing on a cluster that already has shards](../concepts/node-pools.md#installing-on-a-cluster-that-already-has-shards).

## 4. Verify

The notes Helm prints after installing list the scheduler name and label keys KRM is using.
`KAI default` means `kai-config` does not set the key, so KRM uses KAI Scheduler's own
default, as the scheduler does.

There is one node pool per partition KRM manages, plus `default`:

```bash
kubectl get nodepools
```

Each shard KRM took over is owned by its node pool and labelled as adopted. Each shard you
labelled has no owner:

```bash
kubectl get schedulingshards -o custom-columns='NAME:.metadata.name,'\
'PARTITION:.spec.partitionLabelValue,'\
'OWNER:.metadata.ownerReferences[0].name,'\
'ADOPTED:.metadata.labels.kai\.resources/adoption,'\
'IGNORED:.metadata.labels.kai/ignore-shard-for-krm'
```

With a `default` shard, a `team-a` shard KRM took over and a `legacy` shard you labelled:

```text
NAME      PARTITION   OWNER     ADOPTED   IGNORED
default   <none>      default   adopted   <none>
legacy    legacy      <none>    <none>    true
team-a    team-a      team-a    adopted   <none>
```

The `default` shard serves the empty partition, so it shows `<none>`.

Unless you installed with `serviceMonitor.create=false`, each node pool's scheduler is
monitored from KAI Scheduler's namespace, by a `ServiceMonitor` named
`kai-scheduler-<node pool>`. When KRM shares that namespace, its own controllers'
`ServiceMonitor`s are listed there too.

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

- **Create a node pool. This is the recommended way.** KRM creates the shard for it. See
  [partitioning nodes into node pools](../how-to/partition-nodes-into-node-pools.md). To keep
  labelling nodes the way you do for KAI Scheduler today, do what the install did for the
  shards it took over: name the node pool after the partition and select on the node-pool
  label.

  ```yaml
  apiVersion: kai.resources/v1alpha1
  kind: NodePool
  metadata:
    name: team-b
  spec:
    labelKey: kai.scheduler/node-pool   # or kai-config's nodePoolLabelKey
    labelValue: team-b
  ```

- **Only if the partition must stay outside KRM, create its shard with the label** in its
  manifest:

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

The webhook fails closed, so a shard gets past it only while the webhook is not registered,
such as during an install or with `nodePoolController.webhook.nodepool=false`. KRM never
writes such a shard, and a node pool for its partition reports `Unschedulable`. Delete the
shard. See
[node pool is Unschedulable](../how-to/troubleshooting.md#node-pool-is-unschedulable).

To take such a shard over instead, run the same `helm upgrade` again. If the node pool for
its partition selects its nodes by anything other than the node-pool label and the
partition's value, the upgrade fails. When it succeeds, the shard's settings **replace** the
node pool's `schedulingShardConfig`.

## Upgrading KAI Scheduler later

Upgrade KAI Scheduler as before. Its chart still renders the `default` shard, but the
settings that moved to the `default` node pool now belong to KRM. If an upgrade changes one
of them in KAI Scheduler's values, such as `scheduler.placementStrategy` or an arg in
`scheduler.args`, KRM puts the node pool's value back within seconds. Change those on the
`default` node pool instead. Settings that stayed on the shard, such as
`scheduler.goMemLimitRatio`, and args the node pool does not set still follow KAI
Scheduler's chart.

## Uninstalling

```bash
helm uninstall krm --namespace "${KRM_NAMESPACE}"
```

Once KRM's controllers are gone, a post-delete hook hands every shard KRM took over back to
the scheduler: its node pool owner reference and its `kai.resources/` labels and annotations
are removed, and it stays in place and keeps scheduling. The `app: kai-scheduler` label KRM
added stays, and so do the node labels KRM wrote, so every node stays in its partition.

The shard keeps its **current** settings, including any changes you made through its node
pool since the install, not the ones it had before KRM. Compare it with
`shards-before-krm.yaml` if you want those back.

Node pools are not deleted, because Helm retains KRM's CRDs and their objects. The hook
removes their finalizers, so you can delete them afterwards without them hanging. Deleting
a node pool then deletes only a shard KRM created, never one it released. Installing KRM
again takes the released shards over again.

In this mode the chart refuses to render with `postCleanup.enabled` off, because the hook
runs only while it is on. If the hook fails, or never runs, some shards still belong to
their node pools; **do not delete those node pools** before following
[`helm uninstall` fails on the cleanup hook](../how-to/troubleshooting.md#helm-uninstall-fails-on-the-cleanup-hook).

The rest of what uninstall keeps and removes is in the chart's
[uninstall](../../deployments/kai-resource-management-chart/README.md#uninstall) section.

## Limitations

- **Queue hierarchies deeper than two levels stay unmanaged.** KRM's own tree is a department
  with projects under it. A deeper tree you built keeps working, but it cannot be turned into
  departments and projects, and a queue of yours cannot be placed under one of KRM's. See
  [keeping the two kinds of tree apart](../concepts/queues-and-quota.md#keeping-the-two-kinds-of-tree-apart).
- **No switching an existing release.** A release that installed KAI Scheduler must stay
  bundled; see [Before you start](#before-you-start).

## What to read next

| Next | Why |
| --- | --- |
| [Node pools](../concepts/node-pools.md) | What a node pool does with the shard it now owns |
| [Queues and quota](../concepts/queues-and-quota.md) | How KRM's queues sit next to yours |
| [Labels and annotations](../reference/labels-and-annotations.md) | Every key KRM reads and writes |
| [Troubleshooting](../how-to/troubleshooting.md) | When something does not appear |
