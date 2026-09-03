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

End-to-end helpers:

- `setup-e2e-cluster.sh` — create a kind cluster and install the chart built from
  this tree. `--skip-krm-install` gives a cluster with no chart on it.
- `run-e2e-kind.sh` — setup, then `make test-e2e`, then delete the cluster.
- `run-e2e-upgrade-kind.sh` — setup, install the newest published release, then
  run the upgrade suite against a chart built from this tree.

All three mutate the cluster `KUBECONFIG` points at; `test/e2e/README.md` covers
the safety contract they rely on.
