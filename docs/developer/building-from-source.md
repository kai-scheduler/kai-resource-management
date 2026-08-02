# Building from source

The commands below are the stable entry points for building and validating the
repository's Go code and Helm chart.

## Prerequisites

- The Go version declared in the root `go.mod`.
- GNU Make or a compatible Make implementation.
- Git.
- Helm 3 for dependency resolution, linting, rendering, and packaging.
- Docker for the chart unit-test image used by `make test-chart`.

Helm dependency resolution requires network access to GHCR.

## Common commands

```bash
make help
make fmt-go
make lint
make test
make validate
```

`make test` runs Go tests and the chart unit tests. `make validate` verifies
formatting, module tidiness, static analysis, tests, and source license headers
without changing tracked files.

The root Makefile exposes one chart-specific test target:

```bash
make test-chart
```

Package the chart with Helm:

```bash
helm dependency build ./charts/kai-resource-management
mkdir -p ./bin/charts
helm package ./charts/kai-resource-management \
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
- Add Helm charts under `charts/<chart-name>`.
- Keep examples with their documentation under `docs/`.
- Keep small copied dependencies under `third_party/` with provenance and
  licensing information.

## Before opening a pull request

1. Add or update tests appropriate to the change.
2. Update user-facing and developer documentation where relevant.
3. Add a changelog fragment for behavior changes.
4. Run `make validate`.
5. Review `git diff --check` and the full diff.
