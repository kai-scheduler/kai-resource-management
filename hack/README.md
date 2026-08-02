# Development scripts

The `hack/` directory contains repository-maintenance, code-generation, and
developer integration scripts that do not belong in production binaries.

Scripts must:

- Run from the repository root.
- Fail on errors and invalid inputs.
- Avoid mutating a Kubernetes cluster unless the command explicitly documents
  that behavior.
- Pin or verify external tool versions.
- Be exposed through the root Makefile when they are part of the supported
  developer workflow.

End-to-end cluster setup and execution are intentionally deferred to the
separate test-infrastructure work.
