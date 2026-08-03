# Building from source

The commands below are the stable entry points for building and validating the
repository's Go code and Helm chart.

## Prerequisites

- The Go version declared in the root `go.mod`.
- GNU Make or a compatible Make implementation.
- Git.
- Helm 3 for dependency resolution, linting, rendering, and packaging.
- Docker for Go builds, service images, and the chart unit-test image.

Helm dependency resolution requires network access to GHCR.

## Common commands

```bash
make help
make fmt-go
make lint
make test
make validate
```

`make test` runs all non-e2e Go tests with the local Go toolchain and runs the
chart unit tests. `make validate` verifies formatting, module tidiness, static
analysis, tests, and source license headers without changing tracked files.

The root Makefile exposes one chart-specific test target:

```bash
make test-chart
```

Run Go tests directly with the local Go toolchain, either for all non-e2e
packages or for one selected package tree:

```bash
make test-go
make test-go TEST_TARGETS=./pkg/<name>/...
```

When services are introduced, add their names to `SERVICE_NAMES` in the root
Makefile. The aggregate and single-service build commands then follow the same
interface as KAI Scheduler:

```bash
make build
make build-go SERVICE_NAME=<name>
```

Go and Docker build mechanics are kept under `build/makefile/`; the root
Makefile remains the public development interface.

Package the chart with Helm:

```bash
helm dependency build ./deployments/kai-resource-management-chart
mkdir -p ./bin/charts
helm package ./deployments/kai-resource-management-chart \
  --destination ./bin/charts \
  --app-version 0.1.0 \
  --version 0.1.0
```

Downloaded subchart archives and packaged build output are ignored by Git.

To add missing source headers intentionally:

```bash
make gen-license
```

The Go version is declared by `go.mod`. Developer tools are version-pinned by
the Makefile and installed into the ignored `bin/` directory.

## Repository rules

- Use the single root `go.mod`.
- Use the single root `Makefile`.
- Add executables under `cmd/<name>`.
- Add shared implementation under `pkg/<name>`.
- Add deployment configuration and Helm charts under `deployments/<name>`.
- Keep examples with their documentation under `docs/`.

## Before opening a pull request

1. Add or update tests appropriate to the change.
2. Update user-facing and developer documentation where relevant.
3. Add a changelog fragment for behavior changes.
4. Run `make validate`.
5. Review `git diff --check` and the full diff.
