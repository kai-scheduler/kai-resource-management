# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [v0.19.0] - 2026-11-11

### Added
- Helm chart OCI tags without v prefix, enabling semver ranges like ~0.18.0
- Set a node pool's usage Prometheus address and query parameters with timeBasedFairShare.connectionString and extraParams
- Installing or upgrading turns every pre-existing SchedulingShard into a NodePool that owns it, keeping its settings
- Release adopted SchedulingShards and strip NodePool finalizers on uninstall beside an existing KAI Scheduler
- Install beside an existing KAI Scheduler with kai-scheduler.enabled=false, taking its label keys and scheduler name from its kai-config
- Add schedulerNamespace to place ServiceMonitors beside an existing KAI Scheduler
- The department webhook rejects a spec.parent that is missing, terminating, forms a loop, or nests past 7 levels [#150](https://github.com/kai-scheduler/kai-resource-management/issues/150) [noiyemini](https://github.com/noiyemini)
- A department cannot be deleted while another department names it as its parent [#150](https://github.com/kai-scheduler/kai-resource-management/issues/150) [noiyemini](https://github.com/noiyemini)
- Departments can nest under another department, up to 7 levels deep, through spec.parent [#150](https://github.com/kai-scheduler/kai-resource-management/issues/150) [noiyemini](https://github.com/noiyemini)
- Projects and departments can set preemptMinRuntime and reclaimMinRuntime per node pool in spec.queues[].minRuntime, overriding the node pool's minimum runtimes [#164](https://github.com/kai-scheduler/kai-resource-management/issues/164) [noiyemini](https://github.com/noiyemini)

### Changed
- commonArgs.allowExternalQueues defaults to true when installed beside an existing KAI Scheduler (kai-scheduler.enabled=false)
- A NodePool's minRuntime values must be durations such as 5m or 2d; a value KAI Scheduler could not parse, which it treated as 0s, is now rejected [#164](https://github.com/kai-scheduler/kai-resource-management/issues/164) [noiyemini](https://github.com/noiyemini)
- Bundle KAI Scheduler v0.19.0

### Fixed
- Pod group assignment reads only pods in the pod group's own namespace [#169](https://github.com/kai-scheduler/kai-resource-management/issues/169) [blacksoxx](https://github.com/blacksoxx)
- Updating a project namespace keeps labels the project-controller does not own [#171](https://github.com/kai-scheduler/kai-resource-management/issues/171) [blacksoxx](https://github.com/blacksoxx)
- A project's queue moves under its department's queue when that queue is created after the project
- A department or project no longer loses its queue to a new, randomly suffixed one when two reconciles of it race, and a project named like a department now gets its own queue instead of none [noiyemini](https://github.com/noiyemini)
- The project controller leaves an externally referenced namespace alone while it carries the manual-override label [#197](https://github.com/kai-scheduler/kai-resource-management/issues/197) [blacksoxx](https://github.com/blacksoxx)

## [v0.18.4] - 2026-10-12

### Added
- Publish development images and chart on merges to main
- Add commonArgs.allowExternalQueues, which leaves Queues no Project or Department owns untouched
- Accept KAI Scheduler main branch builds instead of reporting them as older than the minimum supported version
- With allowExternalQueues, pod-group-assigner skips PodGroups outside KRM projects and queues it does not own
- Leave SchedulingShards labelled kai/ignore-shard-for-krm, and their nodes, to the scheduler already serving them
- Reject SchedulingShards and NodePools that would put two schedulers on one partitionLabelValue
- Refuse to install beside a KAI Scheduler older than the supported minimum
- With allowExternalQueues, reject a Queue that KRM does not own when its parent is a KRM queue

### Changed
- Service images are now built on scratch instead of distroless
- Helm hook Jobs run a Go binary on scratch instead of an Alpine image with kubectl
- Helm hook Jobs honour global.fipsMode at run time, like the services
- global.fipsMode covers the bundled scheduler, must be a string, and adds tlsmlkem=0 when "only"
- Bundle KAI Scheduler v0.18.0, now also the minimum supported version
- Bundle KAI Scheduler v0.18.1, which fixes known CVEs in go-openapi/swag, golang.org/x/crypto and golang.org/x/mod
- Find a node pool's SchedulingShard by partition value instead of by name
- Keep SchedulingShard fields and args a node pool doesn't set instead of overwriting them
- Node pools no longer take over a SchedulingShard they do not own; they report the conflict

### Fixed
- The operator takes ownership of an object that already exists instead of failing the reconcile
- Keep a placed PodGroup on its node pool after KAI Scheduler clears its scheduling conditions
- Adopt a pre-existing Service on upgrade, fixing pod-group-assigner stuck without certificates on OpenShift
- Update go.mongodb.org/mongo-driver/v2 to v2.9.1 to fix CVE-2026-88031

## [v0.0.3] - 2026-09-20

### Added
- Bind a project namespace's own service account from a project RoleBinding

## [v0.0.2] - 2026-09-20

### Added
- Attach images.yaml listing every release image with its per-platform digest to each GitHub Release

## [v0.0.1] - 2026-09-16
