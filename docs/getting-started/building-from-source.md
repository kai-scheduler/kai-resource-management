# Building from source

Audience: contributors changing KAI Resource Management itself. If you only want to
install and use it, follow the [quickstart](quickstart.md) instead.

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
analysis, source license headers and air-gap image lock coverage without changing
tracked files; it does not run the tests.

Image lock coverage is the one check that reads the rendered chart rather than the
source: it fails if the chart would run a container image from a registry neither
this release nor the bundled KAI Scheduler publishes to. Adding a service, or bumping
KAI Scheduler, needs no change there. Pulling in a third-party image does — see
[`cmd/imagelock`](../../cmd/imagelock/README.md).

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

The end-to-end suites need a cluster, so they are excluded from `make test`.
`hack/run-e2e-kind.sh` builds a kind cluster, installs the chart into it and
runs them; see [test/e2e/README.md](../../test/e2e/README.md).

`SERVICE_NAMES` in the root Makefile lists the services that are built. Add each
new service there when its `cmd/<name>/main.go` entry point is introduced. The
aggregate and single-service build commands follow the same interface as KAI
Scheduler:

```bash
make build
make build-go SERVICE_NAME=<name>
```

`make build` cross-compiles every service for `linux/amd64` and `linux/arm64` in
the pinned builder image and builds its container image.

Add `FIPS=1` to build against the validated Go cryptographic module and tag the
images `<version>-fips`. See [FIPS 140-3](../fips.md).

```bash
make build FIPS=1
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
