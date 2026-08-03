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
make lint             # Run formatting and static checks
make test-chart       # Run chart unit tests in the pinned container
make test             # Run non-e2e Go and Helm tests
make validate         # Run all non-mutating repository validation
make gen-license      # Add missing Apache-2.0 source headers
```

For a behavior-changing pull request, create a changelog fragment:

```bash
make changelog KIND=Added BODY="Add configurable project namespace prefix"
```

Valid changelog kinds are `Added`, `Changed`, `Fixed`, and `Removed`. Keep the
body user-facing and under 20 words.

## Repository structure

The repository deliberately uses one Go module and one Makefile.

- `cmd/<name>/` — executable entry points and process wiring.
- `pkg/<name>/` — shared Go implementation.
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

- Keep `main` packages small. Configuration parsing and process wiring belong
  in `cmd`; testable behavior belongs in `pkg`.
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
- Exported Go declarations require useful GoDoc comments.
- Comments explain why a choice or invariant exists, not what obvious code
  does.
- Preserve upstream headers on generated files.
- Use kubebuilder markers immediately above the declaration they affect.

Run `make gen-license` after adding source files and `make validate` before
finishing.

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

End-to-end implementation is maintained under a separate ticket. Do not add
cluster-mutating tests until the shared framework implements the safety
contract in `test/e2e/README.md`.

Every future e2e suite must:

- Run one centralized preflight before creating or mutating resources.
- Require explicit evidence that the current cluster is disposable.
- Fail closed on ambiguity or foreign resource-management objects.
- Label and clean up only resources owned by its test run.
- Avoid override flags that can silently authorize a development or production
  cluster.

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

Every commit must include DCO sign-off as described in `CLA.md`.

## Completion checklist

Before declaring work complete:

1. Review the final diff for unrelated changes and generated artifacts.
2. Add or update tests appropriate to the change.
3. Update documentation for every affected audience.
4. Add a changelog fragment for user-visible behavior changes.
5. Run `make validate`.
6. Run any additional component-specific checks documented by the root
   Makefile.
7. Confirm `git diff --check` succeeds.
8. Report what changed, what was tested, and any remaining limitations.
