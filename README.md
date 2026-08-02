# KAI Resource Management

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

KAI Resource Management is an open source, Kubernetes-native resource management
layer for [KAI Scheduler](https://github.com/kai-scheduler/KAI-Scheduler). It is
designed to add organizational and infrastructure abstractions such as projects,
departments, node pools, queues, and workload placement.

## Project status

The KAI Resource Management Helm chart source is available under
[`charts/kai-resource-management`](charts/kai-resource-management/README.md).
There is not yet a published release, so cluster administrators must currently
package the chart from source and supply compatible controller images.

## Scope

KAI Resource Management is intended to provide:

- Project and department resource hierarchies.
- Node-pool management and scheduler partition configuration.
- Queue and workload-placement integration with KAI Scheduler.
- A Helm-based installation and upgrade experience for cluster administrators.

## Repository layout

| Path | Purpose |
| --- | --- |
| `charts/` | Helm charts, including the main `kai-resource-management` chart. |
| `cmd/` | Executable entry points, one subdirectory per binary. |
| `pkg/` | Shared Go packages used by repository binaries and integrations. |
| `docs/` | User, administrator, reference, and developer documentation. |
| `hack/` | Development, generation, and repository-maintenance scripts. |
| `test/e2e/` | Reserved for the separately developed end-to-end test suites. |
| `third_party/` | Small, explicitly reviewed source copies with retained provenance and licensing. |
| `.agents/` | Repository-owned agent skills and other shared agent assets. |

The repository intentionally uses one root `go.mod` and one root `Makefile`.

## Documentation

Start with the [documentation index](docs/README.md). Developer setup and
validation commands are described in
[building from source](docs/developer/building-from-source.md).

Examples and sample manifests belong alongside the documentation that explains
them. This repository does not use a separate top-level `examples` directory.

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening
a pull request. By participating, contributors agree to follow the
[Code of Conduct](CODE_OF_CONDUCT.md).

Security vulnerabilities must be reported privately as described in
[SECURITY.md](SECURITY.md).
