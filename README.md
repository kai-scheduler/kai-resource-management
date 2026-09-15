# KAI Resource Management

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE) [![Coverage](https://github.com/kai-scheduler/kai-resource-management/raw/coverage-badge/badges/coverage.svg)](https://github.com/kai-scheduler/kai-resource-management/blob/main/.github/workflows/update-coverage-badge.yaml)

KAI Resource Management is an open source, Kubernetes-native resource management
layer for [KAI Scheduler](https://github.com/kai-scheduler/KAI-Scheduler). It is
designed to add organizational and infrastructure abstractions such as projects,
departments, node pools, queues, and workload placement.

## Project status

The KAI Resource Management Helm chart source is available under
[`deployments/kai-resource-management-chart`](deployments/kai-resource-management-chart/README.md).
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
| `deployments/` | Deployment assets, including the `kai-resource-management-chart` Helm chart. |
| `cmd/` | Executable entry points, one subdirectory per binary. |
| `pkg/` | Shared Go packages used by repository binaries and integrations. |
| `docs/` | User, administrator, reference, and developer documentation. |
| `hack/` | Development, generation, and repository-maintenance scripts. |
| `test/e2e/` | End-to-end suites and the framework that runs them against a cluster. |
| `.agents/` | Repository-owned agent skills and other shared agent assets. |

The repository intentionally uses one root `go.mod` and one root `Makefile`.

## Documentation

New to KAI Resource Management? Read the [overview](docs/overview.md), then install it and
run a workload through it with the [quickstart](docs/getting-started/quickstart.md).

| I want to | Read |
| --- | --- |
| Understand what this is | [Overview](docs/overview.md) |
| Install it and try it | [Quickstart](docs/getting-started/quickstart.md) |
| Understand the objects I create | [Concepts](docs/concepts/README.md) |
| Do one specific thing | [How-to guides](docs/how-to/README.md) |
| Look up a field, label or condition | [Reference](docs/reference/README.md) |
| Configure the Helm chart | [Chart documentation](deployments/kai-resource-management-chart/README.md) |
| Build and test the repository | [Building from source](docs/getting-started/building-from-source.md) |

The full [documentation index](docs/README.md) lists everything, including maintainer
material.

Examples and sample manifests belong alongside the documentation that explains
them. This repository does not use a separate top-level `examples` directory.

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening
a pull request. By participating, contributors agree to follow the
[Code of Conduct](CODE_OF_CONDUCT.md).

Security vulnerabilities must be reported privately as described in
[SECURITY.md](SECURITY.md).
