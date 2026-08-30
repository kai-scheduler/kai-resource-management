# Reference

Look things up. These pages assume you know what you are looking for — if you do not,
start with [concepts](../concepts/README.md).

| Document | Covers |
| --- | --- |
| [API reference](api.md) | Every field of every custom resource: type, default, and what it does |
| [Labels and annotations](labels-and-annotations.md) | Every key KRM reads or writes, and which are configurable |
| [Conditions and phases](conditions-and-phases.md) | Every status value, what it means, and what to do about it |

## Reading the API from the cluster

These pages are hand-written and cover the fields you are likely to set. The cluster always
has the complete, authoritative schema:

```bash
kubectl explain nodepool.spec --recursive
kubectl explain project.spec.queues
kubectl explain krmconfig.spec.global
```

That is the better source for anything not covered here, and for confirming what your
installed version actually supports.

## Where the API comes from

The `kai.resources` types are defined in
[`kai-resource-management-api`](https://github.com/kai-scheduler/kai-resource-management-api)
and consumed by this repository. The CRD manifests installed by the chart are copies of
what that module released — so the version of the API you have is pinned by the version of
KRM you installed.

Some fields are typed by KAI Scheduler rather than KRM — the scheduling shard's plugins,
actions and placement strategy among them. Those are noted where they appear, and their
values come from
[KAI Scheduler](https://github.com/kai-scheduler/KAI-Scheduler)'s documentation.
