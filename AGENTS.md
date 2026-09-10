# KAI Resource Management — Agent Development Guide

KAI Resource Management is an open source, Kubernetes-native resource
management layer for KAI Scheduler. It provides the organizational and
infrastructure abstractions around scheduling, including projects,
departments, node pools, queues, and workload placement.

These instructions apply to the entire repository.

When KAI Scheduler already has a repository pattern for Makefiles, directories,
workflows, or development tooling, mirror it closely. Do not introduce a new
abstraction or structure unless this repository requires it or the user approves
the divergence.

## Supported commands

Use the root Makefile as the public development interface:

```bash
make help             # List supported targets
make fmt-go           # Format Go files
make vet-go           # Run go vet
make lint-go          # Run the pinned golangci-lint version
make lint-go-host     # Same linter, host toolchain: no Docker
make lint             # Run formatting and static checks
make test-chart       # Run chart unit tests in the pinned container
make test             # Run non-e2e Go and Helm tests
make validate         # Run all non-mutating repository validation except tests
make gen-license      # Add missing Apache-2.0 source headers
```

For a behavior-changing pull request, create a changelog fragment:

```bash
make changelog KIND=Added BODY="Add configurable project namespace prefix"
```

Valid changelog kinds are `Added`, `Changed`, `Fixed`, and `Removed`. Keep the
body user-facing and under 20 words.

Never edit `CHANGELOG.md` directly; it is written at release time. Pull requests
that do not change behavior apply the `skip-changelog` label instead of adding a
fragment; dependency updates use `dependencies`.

## Repository structure

The repository deliberately uses one Go module and one Makefile.

- `cmd/<name>/` — entry points for the binaries the images ship.
- `pkg/<name>/` — shared Go implementation.
- `build/` — the shared makefiles, the builder image, and tooling that runs
  only in the pipeline, each tool in one `build/<name>/` package.
- `deployments/<name>/` — deployment configuration and Helm charts. The
  primary chart belongs at `deployments/kai-resource-management-chart/`.
- `docs/` — user, administrator, reference, and developer documentation.
- `hack/` — maintenance, generation, and developer integration scripts.
- `test/e2e/` — end-to-end suites and framework when their separate
  infrastructure is introduced.
- `.agents/` — repository-owned skills and other shared agent assets.

Do not add:

- Nested `go.mod` or `go.work` files.
- Component-specific Makefiles.
- A top-level `examples` directory.
- Generated build output or downloaded Helm dependencies.

Examples and sample manifests belong beside their documentation under `docs/`.

## Go conventions

### Package and command design

- Keep the `main` packages under `cmd` small. Configuration parsing and process
  wiring belong there; testable behavior belongs in `pkg`. A pipeline-only tool
  under `build/` is the exception: nothing imports it, so its whole
  implementation stays in its own `main` package and exports nothing.
- Keep packages cohesive and domain-named. Avoid generic `utils`, `helpers`, or
  `common` packages unless the domain itself is genuinely common.
- Prefer the standard library before introducing dependencies.
- Accept `context.Context` as the first parameter for operations that perform
  I/O, block, or may be canceled.
- Return errors to the caller. Do not panic for expected runtime failures.
- Wrap errors with actionable context using `%w`.
- Avoid mutable package globals.

### Imports

Organize imports into three groups separated by blank lines:

```go
import (
    "context"
    "fmt"

    corev1 "k8s.io/api/core/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"

    "github.com/kai-scheduler/kai-resource-management/pkg/example"
)
```

The groups are standard library, external dependencies, and repository
packages.

### Naming

- Go files use `snake_case.go`.
- Exported identifiers use PascalCase; unexported identifiers use camelCase.
- Boolean functions use an `Is`, `Has`, `Can`, or `Should` prefix where it
  improves clarity.
- Interfaces describe behavior and normally use an `-er` name.
- Avoid abbreviations unless they are standard Kubernetes or domain terms.

### Logging

- Use structured logging and stable field names.
- Obtain request or reconciliation loggers from context when supported.
- Include object kind, namespace, and name where relevant.
- Do not log credentials, tokens, certificates, complete Secrets, or
  personally identifiable information.
- Use debug verbosity for high-volume diagnostic events.

### Comments and headers

- `make gen-license` is authoritative for headers on hand-written source and
  configuration files and emits language-native line comments.
- `hack/boilerplate.go.txt` and `hack/boilerplate.yaml.txt` provide the same
  headers to source generators such as `controller-gen`.
- Comment an exported Go declaration only when the comment says something the
  signature does not. Never restate the function name, its parameters or its
  return type: `// Scheme returns the scheme` on `func (c *Controller) Scheme()
  *runtime.Scheme` is noise, and so is describing a parameter that is already
  visible in the signature. Prefer no comment to an obvious one.
- Comments explain why a choice or invariant exists, not what obvious code
  does.
- Preserve upstream headers on generated files.
- Use kubebuilder markers immediately above the declaration they affect.

Run `make gen-license` after adding source files, and `make validate` and
`make test` before finishing.

## Kubernetes controller conventions

- Reconciliation must be idempotent.
- Act only on objects the component is explicitly responsible for.
- Use owner references or stable ownership labels for created resources.
- Never delete or overwrite foreign resources merely because their names
  match.
- Use least-privilege RBAC and document cluster-scoped permissions.
- Do not hardcode installation namespaces. Use explicit configuration or the
  Helm release namespace.
- Handle conflict, not-found, and deletion races deliberately.
- Give every external call a context and bounded timeout where appropriate.
- Emit Events for user-actionable reconciliation failures.

## Helm conventions

When the chart is introduced:

- Keep it at `deployments/kai-resource-management-chart`.
- Keep `Chart.yaml`, values, templates, CRDs, chart tests, and chart
  documentation together.
- Expose chart commands through the root Makefile; do not add a chart Makefile.
- Use `.Release.Namespace` as the installation namespace source of truth unless
  an external component's namespace is a distinct documented concept.
- Put reusable naming and label logic in `_helpers.tpl`.
- Keep user-facing `values.yaml` focused and document every public value.
- Render deterministic resources and avoid unnecessary cluster lookups.
- Add Helm unit tests for template conditionals, RBAC, namespaces, security
  contexts, images, and upgrade-sensitive behavior.
- Do not commit downloaded dependency archives under a chart's nested
  `charts/` directory.

### Generated CRDs

The `kai.resources` API types belong to
`github.com/kai-scheduler/kai-resource-management-api`. This repository consumes
them and never defines them.

`deployments/kai-resource-management-chart/crds/` is **generated output**. It is
produced by `make sync-crds`, which copies the CRD manifests from the API module
version pinned in `go.mod`. Never hand-edit those files: `make validate` runs
`sync-crds-check` and fails when they drift from the pinned module.

Changing a CRD means changing the API repository, releasing it, and bumping the
pin here — not editing the manifests. The procedure is in
`docs/updating-the-api-module.md`.

Do not add `controller-gen` to this repository, and do not generate Kubernetes
clientsets, informers or listers. The API module deliberately ships none;
controller-runtime's client with `AddToScheme` is the intended path.

When adding a CRD to the API module, also add it to `resourceNames` in
`templates/rbac/crd-manager.yaml`, otherwise the pre-install hook cannot apply
it. `make crd-rbac-check` enforces this, as does a chart test that pins the
list.

## Testing

### Unit and integration tests

- Keep Go tests beside the package they exercise.
- Test files end in `_test.go`.
- Prefer table-driven tests for input/output behavior.
- Cover successful behavior, invalid input, ownership boundaries, and error
  paths.
- Avoid arbitrary sleeps; use observable conditions and bounded polling.
- Keep tests deterministic and safe to run in parallel where practical.

`make test` must remain safe for a developer machine and must not connect to or
mutate a Kubernetes cluster.

### End-to-end tests

Suites live in `test/e2e/suites/`, one per service under test, on the shared
framework in `test/e2e/modules/`. They mutate whatever cluster `KUBECONFIG`
points at and are deliberately outside `make test`; `test/e2e/README.md` covers
running them and states the safety contract they rely on.

Every e2e suite must:

- Take its client from `modules/context.GetConnectivity`, the only path that
  runs the preflight guard.
- Build the objects it creates with `modules/resources`, so each one carries the
  ownership labels preflight and cleanup match on.
- Clean up only what its own run created, and restore any node label or shared
  object it changed. Suites share one cluster and run in a random order.
- Wait on observable conditions through `modules/wait`, never on a fixed sleep.
- Add no override that can authorize a development or production cluster. The
  guard in `modules/context/preflight.go` deliberately has none.

## Documentation

Documentation is part of the definition of done.

- Update user-facing docs for behavior, configuration, installation, and
  operational changes.
- Update reference docs when APIs, flags, Helm values, metrics, or defaults
  change.
- Put architecture and design material under `docs/designs/`.
- Put onboarding and local development material under `docs/getting-started/`.
- Put examples beside the guide or concept that explains them.
- State prerequisites, expected results, side effects, permissions, and
  cleanup.
- Do not document planned behavior as released behavior.
- Keep links and commands current.

Use `docs/README.md` to select the correct audience and location.

## Pull request requirements

Pull request titles follow Conventional Commits:

```text
<type>[optional scope]: <description>
```

Common types are `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `ci`, and
`chore`. Useful scopes include `chart`, `api`, `nodepool-controller`,
`project-controller`, `pod-group-assigner`, `docs`, `release`, and `deps`.

Behavior changes require a changelog fragment. Refactors, tests, documentation,
and CI-only changes do not.

Every commit must include DCO sign-off as described in `CONTRIBUTING.md`.

## Completion checklist

Before declaring work complete:

1. Review the final diff for unrelated changes and generated artifacts.
2. Add or update tests appropriate to the change.
3. Update documentation for every affected audience.
4. Add a changelog fragment for user-visible behavior changes.
5. Run `make validate` and `make test`.
6. Run any additional component-specific checks documented by the root
   Makefile.
7. Confirm `git diff --check` succeeds.
8. Report what changed, what was tested, and any remaining limitations.
