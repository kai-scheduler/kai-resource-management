# Design — Co-installing the OSS Resource-Management package with a pre-existing KAI-scheduler

## Problem

The OSS package (chart: nodepool-controller, project-controller, pod-group-assigner, `kai.resources` CRDs,
KAI-scheduler as an optional subchart) installs cleanly on a cluster with no scheduler present. We need it to
also install into a cluster **already running a standalone KAI-scheduler** — with the admin's own label keys, queues, shards,
and queue hierarchy. `kai-scheduler.enabled=false` in the new chart is step one. The rest is: **configure our controllers to
match the existing scheduler**, and **coexist with objects it already owns**.

## The two models

| | KAI-scheduler (already installed) | OSS package (what we add) |
|---|---|---|
| **Queues** | Cluster-scoped, arbitrary depth; workload → queue **by label** (`queueLabelKey`) | project-controller: Project/Department → Queue, **2 levels only**, parent by OwnerReference; touches only queues it owns |
| **Partitions** | One global `nodePoolLabelKey`; per-partition value on each `SchedulingShard.spec.partitionLabelValue` (`""` = default) | nodepool-controller: NodePool → SchedulingShard, invariant **shard name == partitionLabelValue == nodepool name** (`default` → `""`) |
| **Pods** | pod-grouper copies queue label → `PodGroup.spec.queue` | pod-group-assigner: namespace project-label + node pools → PodGroup queue |
| **Webhooks** | Validates Queues; **none** for SchedulingShard/Config | We add shard + queue-tree webhooks (below) |

The mismatch to bridge: OSS binds workloads to queues **through the namespace**, KAI **through a label** — both
keyed on the **same global config the existing scheduler already uses**.

## Install shape

- `kai-scheduler.enabled=false` → skip bundled scheduler, its CRDs/Config/config-deployer. Existing `kai-config`
  stays the single source of truth.
- We still install our `kai.resources` CRDs (no collision — KAI doesn't ship them).
- We rely on the already-present `scheduling.run.ai` (Queue, PodGroup) and `kai.scheduler` (Config, Shard,
  Topology) CRDs.
- We deploy the three controllers + the `default` NodePool.

## Supported KAI-scheduler versions

Co-install only — with the bundled subchart the version is ours.

A **minimum** supported version, no maximum: a maximum makes every KAI release a KRM
release blocker. The minimum lives in one place in the repo, read by both the chart and
the preflight.

**Check:** `pre-install`/`pre-upgrade` hook Job — same image as the migration — reads the
installed KAI release's chart version from its Helm release Secret and fails the install
below the minimum. CRD introspection is not enough: some behavior we depend on isn't
visible in the API surface. On `pre-upgrade` too, since the admin can move KAI underneath
us.

**Hook order** (weights): preflight → CRD upgrader → migration. Nothing touches the
cluster before the version is accepted.

> **RBAC:** reading Helm release Secrets needs cluster-wide `get`/`list` on Secrets for
> the hook's ServiceAccount. Created and deleted with the hook.
>
> **No release Secret** (GitOps applying rendered manifests): the version is undetectable.
> An explicit chart value overrides detection; with neither, fail closed with instructions.

## Namespace placement (where we install vs. where KAI runs)

Only **nodepool-controller** cares where the scheduler runs: it creates the per-shard **ServiceMonitors** in the
scheduler's namespace so Prometheus scrapes the shards.

The scheduler namespace should be a single top-level chart value — **not** under `common` (that block is scheduler
*vocabulary*; this is a co-install *deployment location*). Empty ⇒ `.Release.Namespace`.

| Install mode | Scheduler namespace | ServiceMonitor RBAC |
|---|---|---|
| **Bundled subchart** (no existing KAI) | always `.Release.Namespace` — **not configurable** (the subchart installs into the release ns); guard with `helm fail` if the value is set while `kai-scheduler.enabled=true` | namespaced Role in the release ns (as today) |
| **Co-install, same namespace** (recommended) | `.Release.Namespace` — install OSS into the existing KAI's namespace | namespaced Role in the release ns (as today) — no change |
| **Co-install, different namespace** | the external KAI's namespace (set explicitly, or `lookup`) | the **one** nodepool-controller ServiceMonitor Role + RoleBinding is created **in the scheduler namespace** instead of the release ns |

**Consequence if misconfigured:** per-shard ServiceMonitors land in a namespace with no scheduler (or the
controller lacks RBAC to create them there) → scheduler/node-pool metrics silently break. Nothing else depends
on this value.

**Effort to support the different-namespace case:** small — make the scheduler-namespace value configurable
(default `.Release.Namespace`), render the single nodepool-controller ServiceMonitor Role + RoleBinding in that
namespace (relocate it, don't add a second), and add the subchart guard. Recommendation: support it — the
alternative ("same-namespace only") is a hard limitation for zero saved effort.

## Config alignment

Our controllers must speak the scheduler's vocabulary or nothing lines up. **Three** fields on the singleton
`kai-config` must match:

| `kai-config` field | Chart value | Used by |
|---|---|---|
| `spec.global.nodePoolLabelKey` | `global.nodePoolLabelKey` | all three |
| `spec.global.queueLabelKey` | `global.queueLabelKey` | project-controller, pod-group-assigner |
| `spec.global.schedulerName` | `global.schedulerName` | nodepool-controller |

**Mechanism:** exposed as chart values, defaulted via Helm `lookup` of the live `kai-config`, overridable.
Other OSS label keys (`projectLabelKey`, etc.) aren't in `kai-config` — they keep their `kai.scheduler/*`
defaults.

**Absent ≠ default.** KAI's chart writes `nodePoolLabelKey`/`queueLabelKey` only when set and never writes
`schedulerName`, so `lookup` routinely returns a Config missing them. The fallback must be **KAI's** default
for the detected version, not KRM's — if the two ever diverge we bind workloads to the wrong key.

> **Offline note:** `lookup` reads the live cluster, so it's empty under `helm template` / GitOps (ArgoCD).
> There the three values must be passed explicitly — same fallback the chart already uses for OpenShift.

## Coexistence guards

- **project-controller:** never update/delete a Queue lacking its OwnerReference. Already scoped for deletes;
  make it an explicit **tested** requirement, and fix `resource-manual-override` (today it logs the override but
  still updates the queue).
- **pod-group-assigner:** `/mutate-pod-group` mutates every PodGroup on create — must **skip PodGroups not owned
  by an OSS project/queue** so foreign workloads are untouched.
- **Deep (3+) hierarchies:** unsupported. Left as-is/unmanaged; new OSS trees stay separate. Documented
  limitation (conversion = future work).
- **Queue-tree separation (validating webhook):** validating webhook rejecting a Queue whose `parentQueue` points
  at an OSS-owned queue unless it's itself OSS-owned. Keeps admin trees separate; chains with KAI's queue webhook.

## Existing shards → NodePools

**Why:** nodepool-controller must be the **sole** shard owner (a foreign shard = a competing scheduler on that
partition), and the package requires a `default` NodePool. So every partition needs a representing NodePool.

**Migration — `pre-install`/`pre-upgrade` Helm hook Job** running a subcommand of the shared helm-hooks image
(RBAC: list Shards, patch Shard labels and ownerReferences, get/create/update NodePools):

1. Per existing shard: target NodePool `N` = `partitionLabelValue` (or `default` when empty).
2. **Create-or-update** NodePool `N` — if it exists (e.g. the shipped `default`), update its spec to match the
   shard so config (placementStrategy/minRuntime/plugins/args + label value) is preserved, not overwritten.
3. **Set `N` as the shard's OwnerReference** and stamp it `adopted`, in the same step.

**The hook is the only thing that adopts.** The controller never takes over a shard at runtime — a second
adoption path would mean two mechanisms racing to claim the same shard. A shard created outside the install flow
is taken over by re-running the upgrade, which is why this runs on `pre-upgrade` too and is idempotent.

**Shard webhook (optional — see appendix):** validating webhook rejecting manually-created shards, allowing
only NodePool-owned ones or ones that are labeled with the ignore-for-krm label. New; KAI ships none. Nothing depends on it: the controller fails closed without one.

Idempotent, so a re-run is a no-op; the shard webhook keeps new un-owned shards from appearing in between.

## Phases

**Phase 1 (co-install works & is safe):** the three lookup-aligned values · scheduler-namespace placement
(configurable value + subchart guard + ServiceMonitor Role relocation for the different-namespace case) ·
project-controller + pod-group-assigner guards (+ tests) · shards→NodePools migration · version preflight ·
unmanaged-shard opt-out · NodePool webhook · merge-not-replace reconciliation · uninstall revert ·
queue-tree webhook · docs.

**Later:** shard webhook; convert 3+-level trees; attach a queue to an existing project.

## Open questions

- **Pre-existing shards whose name ≠ partition value** — decided: **option B**, keep the original name.
  Option A deletes a live shard during pre-install: an availability event on the cluster we were asked to
  coexist with, and unrecoverable if the install then fails. See *Adoption, unmanaged shards, and uninstall*.

## Validation

Standalone KAI with **non-default** label keys, admin queues (incl. a 3-level tree), their PodGroups and shards.
Install with `kai-scheduler.enabled=false`, confirm:

- controllers pick up the configured keys via `lookup`;
- existing shards → NodePools, with no partition doubled;
- admin queues/tree/PodGroups untouched and still schedule;
- a manual shard and a cross-tree queue are rejected;
- a new Project → namespace → PodGroup → queue schedules on the existing scheduler;
- with OSS installed in a **different** namespace than KAI, the per-shard ServiceMonitors are created in the
  scheduler's namespace and its metrics scrape; and setting the scheduler-namespace value while the subchart is
  enabled fails the render (guard);
- a shard labelled unmanaged keeps its nodes and its spec, and gets no NodePool;
- a NodePool claiming an unmanaged shard's partition is rejected at create;
- `helm uninstall` leaves every adopted shard alive and un-owned;
- a below-minimum KAI fails preflight.

## Adoption, unmanaged shards, and uninstall

**We take over.** Every pre-existing shard is adopted and *owned* — OwnerReference to its NodePool, same as the
ones we create. Documented plainly: deleting a NodePool deletes its owned objects, shard and scheduler
deployment included.

**Shard identity — keep the shard's name** (option B). Resolve a NodePool's shard by `spec.partitionLabelValue`
through a field index; the name is then free. ServiceMonitor name and selector must follow the **shard** name —
the KAI operator names the shard's Service from the shard, and today we derive both from the NodePool.

**Opting out — `kai.scheduler/ignore-shard-for-krm: "true"` on the shard.** The label is the only source of
truth; no values list to drift. Off for every shard by default, set before install. An unmanaged shard means:

- migration creates no NodePool for its partition;
- nodepool-controller ignores the shard and **skips its nodes entirely** — no relabel, no cordon, no accounting.
  This is the part that bites: today a node whose node-pool label names a non-existent NodePool is relabelled
  into another pool, so without this the admin's partition is swept into `default` on our first reconcile;
- no NodePool may claim its partition (below).

**`default` is never unmanaged** — `""` is the default partition and the package requires a `default` NodePool.
The pre-install/pre-upgrade hook removes the label from the `default` shard and logs it.

**The label only counts before adoption.** Owned beats labelled: a shard already carrying our OwnerReference
stays managed, and the hook strips the label on the next upgrade. Releasing an adopted shard is an uninstall,
not a label.

**Collisions — rejected at NodePool create.** A validating webhook on NodePool (our CRD, existing webhook infra)
rejects a NodePool whose partition value is already claimed by a shard that isn't its own — an unmanaged shard
first and foremost. Two schedulers on one partition never gets created. This is what closes the duplicate-shard
risk that argued against option B. The pre-install hook applies the same check and fails the install on a
pre-existing collision.

**Shard resolution.** For NodePool `N` with partition `P`, over the shards whose `spec.partitionLabelValue == P`:

| Shards matching `P` | Action |
|---|---|
| none | create one, named `N`, stamped `created` |
| one, owned by `N` | reconcile — merge |
| anything else | **blocked** — no shard write, NodePool status condition + Event |

Blocked covers an un-owned shard, two shards on one partition, or one owned by another NodePool — states the
webhook can't prevent, e.g. a shard created after the NodePool. The controller never guesses and never adopts;
the remedy is to label the shard or re-run the upgrade.

**Reconciliation merges, never replaces.** The controller writes only the shard fields a NodePool models and
leaves the rest alone — `actions`, `scenarioSearchBudgets`, admin-set args. Today it overwrites `spec` wholesale,
silently dropping them. Always, not just at adoption.

**Provenance — `kai.resources/adoption: adopted | created`**, `adopted` stamped by the migration hook,
`created` by the controller when it makes a shard itself. Only `adopted` shards are reverted. No spec snapshot: if the admin edits a NodePool we edit its shard,
and both keep the change after uninstall — that's fine.

**Uninstall — `pre-delete` hook plus NodePool finalizer.** Order matters:

1. remove our webhook configurations — a `Fail` policy with the service already gone blocks the writes the
   revert itself needs;
2. stop the controllers, so nothing re-adopts mid-revert;
3. per adopted shard: drop the OwnerReference and our labels. **The contract is that the shard survives**, in
   whatever state it currently is;
4. strip KRM finalizers from NodePools — Helm deletes neither CRDs nor user-created NodePools, and they would
   hang on delete with no controller left.

Shards KRM created are not reverted; they go with their NodePools.

**Dual Helm ownership of `default`.** KAI's chart ships the `default` shard as a release resource
(`helm.sh/resource-policy: keep`), so every KAI `helm upgrade` re-applies it. Merge-not-replace keeps the
tug-of-war down to fields both sides set.

## Appendix — an optional SchedulingShard webhook

Nothing above depends on it; the controller fails closed without one, and KRM may sit on a KAI version that
predates it. A KRM-shipped validating webhook would only move two failures from NodePool status to `kubectl`:
a second shard on a claimed partition value, and an un-owned shard on a partition a NodePool already claims.
