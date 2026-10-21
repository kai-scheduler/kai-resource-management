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

**Most settings move over unchanged.** The node pool takes the shard's `args`, placement
strategy, queue depth, minimum runtime, plugins, actions and time-based fair share, including
the usage DB's `connectionString` and queries. Settings a node pool has no field for, such as
`goMemLimitRatio` and `scenarioSearchBudgets`, stay on the shard. Three are not kept:

- a `kValue` on a shard with no usage DB, where it has no effect;
- the usage DB's `clientType`, which becomes `prometheus`;
- the usage DB's `connectionStringEnvVar`. Put the address in `connectionString` instead.

**Each shard KRM takes over gets three args**, `cpu-worker-node-label-key`,
`gpu-worker-node-label-key` and `mig-worker-node-label-key`, so its scheduler restarts once.

**Deleting that node pool deletes the shard**, and with it the scheduler KAI runs for that
partition. That is what ownership means, for a shard KRM took over exactly as for one it
created. See [deleting a node pool](../concepts/node-pools.md#deleting-a-node-pool).

**The install takes over no queues and no workloads.** KRM never touches a workload in a
namespace that belongs to no KRM project. Queues KRM did not create are left alone
afterwards too, as long as you install with `allowExternalQueues` on (step 2). Your existing
queue hierarchy, and the workloads that use it, keep scheduling as before.

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
install removes it. It is also the one shard whose label the webhook lets you change later,
since there the label has no effect.

While you are here, check for shards the install cannot take over. It fails, and changes
nothing, for any reason in
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
This is checked only while a shard is left to take over, so normally only on the first
install. If you later change the key on either side, change it on the other too.

Also turn on **`commonArgs.allowExternalQueues`**. Installing with
`kai-scheduler.enabled=false` does not turn it on for you, and without it KRM treats every
queue as its own. A project whose queue has the same name as one of yours takes that queue
over: it moves the queue under the project's department and rewrites its quota and
priority, so any queues below it end up in KRM's tree. Turning the flag on later does not
hand it back. See
[queues KRM did not create](../concepts/queues-and-quota.md#queues-krm-did-not-create).

## 3. Install

```bash
helm upgrade --install krm \
  oci://ghcr.io/kai-scheduler/kai-resource-management/kai-resource-management \
  --version 0.19.0 \
  --namespace "${KRM_NAMESPACE}" \
  --create-namespace \
  --set kai-scheduler.enabled=false \
  --set schedulerNamespace="${KAI_NAMESPACE}" \
  --set commonArgs.allowExternalQueues=true \
  --wait --timeout 10m
```

Without the `ServiceMonitor` CRD, add `--set serviceMonitor.create=false`.

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
controllers.

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
Check them against step 2. `KAI default` means the key is set neither in the chart nor in
`kai-config`, so KRM uses KAI Scheduler's own default, as the scheduler does.

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

- **To manage it with KRM, create a node pool.** KRM creates the shard for it. See
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
    labelKey: kai.scheduler/node-pool   # or the key from step 2
    labelValue: team-b
  ```

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

The webhook fails closed, so a shard gets past it only while the webhook is not registered,
such as during an install or with `nodePoolController.webhook.nodepool=false`. KRM never
writes such a shard, and a node pool for its partition reports `Unschedulable`. Delete the
shard. See
[node pool is Unschedulable](../how-to/troubleshooting.md#node-pool-is-unschedulable).

Running the same `helm upgrade` again takes the shard over instead, but only if that node
pool selects its nodes by the node-pool label and the partition's value; otherwise the
upgrade fails. When it succeeds, the shard's settings **replace** the node pool's
`schedulingShardConfig`.

## Upgrading KAI Scheduler later

Upgrade KAI Scheduler as before. Its chart still renders the `default` shard, but the
settings that moved to the `default` node pool now belong to KRM. If an upgrade changes one
of them in KAI Scheduler's values, such as `scheduler.placementStrategy` or an arg in
`scheduler.args`, KRM puts the node pool's value back within seconds. Change those on the
`default` node pool instead. Settings that stayed on the shard, such as
`scheduler.goMemLimitRatio`, and args the node pool does not set still follow KAI
Scheduler's chart.

## Installing without Helm hooks

Install with `helm upgrade --install` as in step 3 when you can. The alternatives differ from
it:

- **`helm template | kubectl apply` runs every hook at once.** `helm template` prints the
  chart's hook Jobs along with everything else, so `kubectl apply` starts them together and
  in no order. That includes the uninstall cleanup Job, which can delete KRM's controllers and
  its `KRMConfig`, and hands back any shard already taken over. Never apply that output;
  render with `--no-hooks`, and read the next point.
- **Without hooks, their work does not happen.** With `--no-hooks` there is no version check,
  no shard is taken over, and no `default` node pool is created, so unlabelled shards block
  their partitions' node pools. No `KRMConfig` is applied either, so the KRM operator starts
  no controllers unless the chart renders it as a release resource; see the chart's
  [KRM operator](../../deployments/kai-resource-management-chart/README.md#how-the-krmconfig-is-created)
  section. On a `helm upgrade --no-hooks`, KRM's CRDs are not upgraded. Run every install
  and upgrade with hooks, or check that your GitOps tool runs the chart's `pre-install` and
  `pre-upgrade` hooks.
- **An offline render cannot read the cluster.** `helm template` and GitOps tools do not fill
  the three settings in step 2 from `kai-config`, and KRM falls back to KAI Scheduler's
  defaults, which may not be the ones your scheduler uses. Set them explicitly, along with
  `schedulerNamespace` and `allowExternalQueues`. `helm install --no-hooks` still reads them.

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

The hook does this only while `postCleanup.enabled` is on, so the chart refuses to render
with it off in this mode. If the hook fails, or never runs, some shards still belong to
their node pools; **do not delete those node pools** before following
[`helm uninstall` fails on the cleanup hook](../how-to/troubleshooting.md#helm-uninstall-fails-on-the-cleanup-hook).
The hook never runs when KRM is removed any other way: `helm uninstall --no-hooks`,
deleting what `helm template` rendered, or a GitOps tool that does not run the chart's
`post-delete` hook.

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
