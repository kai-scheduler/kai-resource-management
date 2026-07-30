# Building from source

This repository currently contains its development infrastructure but no
buildable services or Helm chart. The commands below are the stable entry points
that future components will extend.

## Prerequisites

- Go 1.26.3 or newer within the Go 1.26 release line.
- GNU Make or a compatible Make implementation.
- Git.

Additional component-specific requirements, such as Helm, Docker, Kind, or
controller-generation tools, will be documented when those components are
introduced.

## Common commands

```bash
make help
make fmt-go
make lint
make test
make validate
```

`make validate` is non-mutating. It verifies formatting, module tidiness, static
analysis, tests, and source license headers.

To add missing source headers intentionally:

```bash
make gen-license
```

Tools are version-pinned by the Makefile and installed into the ignored `bin/`
directory.

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
