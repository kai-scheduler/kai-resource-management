# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [v0.18.2] - 2026-10-01

### Changed
- Bundle KAI Scheduler v0.18.2

## [v0.18.1] - 2026-09-29

### Changed
- Bundle KAI Scheduler v0.18.1, which fixes known CVEs in go-openapi/swag, golang.org/x/crypto and golang.org/x/mod

### Fixed
- Keep a placed PodGroup on its node pool after KAI Scheduler clears its scheduling conditions

## [v0.18.0] - 2026-09-28

### Added
- Publish development images and chart on merges to main

### Changed
- Service images are now built on scratch instead of distroless
- Helm hook Jobs run a Go binary on scratch instead of an Alpine image with kubectl
- Helm hook Jobs honour global.fipsMode at run time, like the services
- global.fipsMode covers the bundled scheduler, must be a string, and adds tlsmlkem=0 when "only"
- Bundle KAI Scheduler v0.18.0, now also the minimum supported version

### Fixed
- The operator takes ownership of an object that already exists instead of failing the reconcile

## [v0.0.3] - 2026-09-20

### Added
- Bind a project namespace's own service account from a project RoleBinding

## [v0.0.2] - 2026-09-20

### Added
- Attach images.yaml listing every release image with its per-platform digest to each GitHub Release

## [v0.0.1] - 2026-09-16
