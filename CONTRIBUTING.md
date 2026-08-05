# Contributing to KAI Resource Management

Thank you for contributing to KAI Resource Management.

Read the [Code of Conduct](CODE_OF_CONDUCT.md) and the
[Developer Certificate of Origin](CLA.md) before submitting a change.

## Getting started

1. Fork and clone the repository.
2. Create a focused branch from `main`.
3. Make the change, including appropriate tests and documentation.
4. Run `make validate`.
5. Commit with DCO sign-off and open a pull request.

Developer setup and repository conventions are documented in
[docs/getting-started/building-from-source.md](docs/getting-started/building-from-source.md).

## Repository conventions

- Keep one root `go.mod` and one root Makefile.
- Put executable entry points under `cmd/`.
- Put shared Go implementation under `pkg/`.
- Put deployment configuration and Helm charts under `deployments/`.
- Put examples and sample manifests alongside their documentation under
  `docs/`.
- Put shared agent skills and assets under `.agents/`. Harness-specific
  directories such as `.claude/` and `.codex/` are local state and stay ignored.

Detailed coding and testing guidance is maintained in [AGENTS.md](AGENTS.md).
Its rules apply to both human and automated contributors.

## Documentation

Documentation is part of the change:

- User-visible behavior and configuration require user-facing documentation.
- Installation or upgrade changes require administrator guidance.
- Architectural changes require developer documentation or a design document.
- Examples must explain prerequisites, expected results, and cleanup.

Documentation requests can be opened through the repository's GitHub issue
templates.

## Tests and validation

Add or update tests appropriate to the behavior being changed. Unit tests belong
beside the Go package they exercise and must end in `_test.go`.

Run the repository validation before opening a pull request:

```bash
make validate
```

The root Makefile is the supported command interface. Component-specific
Makefiles are not allowed.

End-to-end infrastructure is developed separately. Do not add cluster-mutating
tests without the centralized safety preflight described in
[test/e2e/README.md](test/e2e/README.md).

## Pull request titles

Pull request titles must follow
[Conventional Commits](https://www.conventionalcommits.org/):

```text
<type>[optional scope]: <description>
```

Common types include:

- `feat` — new behavior.
- `fix` — bug fix.
- `docs` — documentation-only change.
- `refactor` — internal change without behavior changes.
- `test` — test-only change.
- `build` — build or dependency change.
- `ci` — continuous-integration change.
- `chore` — repository maintenance.

Useful scopes include `chart`, `api`, `nodepool-controller`,
`project-controller`, `pod-group-assigner`, `docs`, `release`, and `deps`.

Use imperative mood, keep the title concise, and do not end it with a period.
Mark breaking changes with `!`, for example:

```text
feat(chart)!: rename project configuration values
```

## Changelog fragments

Behavior changes must add a changelog fragment. Do not edit `CHANGELOG.md`
directly.

For automated or non-interactive use:

```bash
make changelog KIND=Added BODY="Add configurable project namespace prefix"
```

Valid kinds are `Added`, `Changed`, `Fixed`, and `Removed`. Keep the body clear,
user-facing, and under 20 words.

Changelog fragments are not required for internal refactors, tests,
documentation, or CI-only changes.

Maintainers fold the accumulated fragments into `CHANGELOG.md` at release time.
See [docs/releasing.md](docs/releasing.md).

## DCO sign-off

Every commit must be signed off to certify that the contribution complies with
the [Developer Certificate of Origin](CLA.md):

```bash
git commit -s -m "feat(chart): add resource management chart"
```

This adds:

```text
Signed-off-by: Your Name <your.email@example.com>
```

The `DCO Check` job fails a pull request containing any unsigned commit. To sign
commits you have already made:

```bash
# Amend the last commit
git commit --amend -s

# Sign every commit on your branch at once
git rebase --signoff origin/main
```

Both rewrite history, so force-push the branch afterwards with
`git push --force-with-lease`.

## Reporting issues

Use the GitHub issue templates for bugs, enhancements, and documentation
requests. Security vulnerabilities must be reported privately according to
[SECURITY.md](SECURITY.md).
