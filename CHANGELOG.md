# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

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
