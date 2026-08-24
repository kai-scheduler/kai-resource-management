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

## Private API module access (temporary)

> **This entire section should be deleted once the repositories are public.**
> It exists only because `github.com/kai-scheduler/kai-resource-management-api`
> is currently a private repository. Once it is public, the public Go module
> proxy serves it anonymously and none of the configuration below is needed.

The API types and CRD manifests for the `kai.resources` group live in
`github.com/kai-scheduler/kai-resource-management-api`. While that repository is
private, `proxy.golang.org` cannot read it and returns `404` for every version.
Go must be told to bypass the proxy and fetch the module directly over
authenticated git; that is what `GOPRIVATE` does.

Without this configuration you will see one of:

```text
410 Gone
fatal: could not read Username for 'https://github.com'
```

Two execution contexts need configuring, and they fail for different reasons.

### Commands that run Go on your machine

`make test`, `make test-go`, `make vet-go` and the host portion of
`make validate` use your local Go toolchain. Set `GOPRIVATE`, appending rather
than replacing — an existing entry such as `github.com/run-ai/*` is common:

```bash
current=$(go env GOPRIVATE)
go env -w GOPRIVATE="${current:+$current,}github.com/kai-scheduler/kai-resource-management-api"
```

`${current:+$current,}` appends a separator only when there is already a value,
so this is also correct on a machine where `GOPRIVATE` is unset.

The value is scoped to the exact module rather than
`github.com/kai-scheduler/*` on purpose. `GOPRIVATE` also disables
checksum-database verification, and the sibling module
`github.com/kai-scheduler/api` is public and is published in the checksum
database; a wildcard would silently stop verifying it. `go.sum` still pins the
private module either way.

If `GOPRIVATE` is exported from your shell profile, an exported environment
variable takes precedence over the value `go env -w` writes, and the command
above will appear to have no effect. Check which one is in force:

```bash
env | grep GOPRIVATE          # exported: edit your shell profile instead
go env GOPRIVATE              # the effective value Go will use
```

You also need working GitHub git credentials, for example:

```bash
gh auth setup-git
```

### Commands that run Go in a container

`make build` and `make lint-go` run the Go toolchain inside Docker. These
containers resolve modules against their own `GOPATH` volume
(`~/.cache/go-build-docker-gopath`), which is a different module cache from your
host's, so they must be able to fetch the module themselves. A macOS or Linux
keychain credential helper cannot work inside the Linux container.

Give the container its own credential: point `GIT_CONFIG_GLOBAL` at a git
configuration file containing a token-based URL rewrite.
`build/makefile/golang.mk` mounts that file into the container read-only. This
is what CI does, and it works for every containerized target:

```bash
mkdir -p "$HOME/.config"
cat > "$HOME/.config/kai-module-gitconfig" <<'CONF'
[url "https://x-access-token:<TOKEN>@github.com/kai-scheduler/kai-resource-management-api"]
	insteadOf = https://github.com/kai-scheduler/kai-resource-management-api
CONF
chmod 0600 "$HOME/.config/kai-module-gitconfig"

make build GIT_CONFIG_GLOBAL="$HOME/.config/kai-module-gitconfig"
make lint  GIT_CONFIG_GLOBAL="$HOME/.config/kai-module-gitconfig"
```

`<TOKEN>` is a personal access token with read access to that one repository.
Scope the rewrite to the exact module rather than `github.com/kai-scheduler/`,
so the token is never offered to any other repository — the same reasoning that
keeps `GOPRIVATE` narrow.

The container reads `GIT_CONFIG_GLOBAL` rather than `~/.gitconfig` because it
runs as a numeric uid with no passwd entry, so `HOME` is `/` and git would look
for `//.gitconfig`.

**That file contains a credential.** Keep it outside this repository, and give
it `0600` permissions.

> **Do not point `GOPATH_HOST_DIR` at your host `GOPATH`** to share the module
> cache instead. `build/makefile/golang.mk` mounts it at `/go`, which is also
> where the `golangci-lint` image keeps its binary, so the mount replaces that
> Linux binary with your host's and `make lint-go` fails with
> `exec format error`. The variable exists to relocate the containers' own
> cache directory, not to share yours.

### Continuous integration

CI needs no manual setup. The `validate-and-test`, `build`, `build-and-push` and
`build-and-push-fips` jobs mint a short-lived installation token from the
`kai-module-reader` GitHub App and configure `GOPRIVATE` and a git URL rewrite
before any Go command runs.
The repository secrets `KAI_MODULE_READER_APP_ID` and
`KAI_MODULE_READER_PRIVATE_KEY` back this. The default `GITHUB_TOKEN` cannot be
used: it is scoped to this repository alone.

### Removing this

Once the repositories are public, delete every block marked
`private API module access`:

```bash
grep -rn "private API module access" .
```

That covers `.github/workflows/on-pr.yaml` (two jobs),
`.github/workflows/push-artifacts.yaml` (two jobs), `build/makefile/golang.mk`, this
section, and the note in `AGENTS.md`. Then revoke the `kai-module-reader` App
installation and delete the two repository secrets.

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
analysis and source license headers without changing tracked files; it does not
run the tests.

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
