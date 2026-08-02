# End-to-end tests

This directory is reserved for the repository's end-to-end test suites.
Implementation, cluster setup, and CI execution are tracked separately and are
not part of the repository bootstrap.

## Required safety contract

Future e2e infrastructure must fail closed before mutating a cluster:

- Use one centralized preflight called by every suite.
- Require explicit proof that the current cluster is disposable.
- Refuse to run when the target context is ambiguous or contains foreign
  resource-management objects.
- Label every test-created resource and only clean up resources owned by the
  test run.
- Never provide an automatic override that turns a development or production
  cluster into an accepted destructive target.

The separate e2e-infrastructure change must implement and test this contract
before adding destructive suites.
