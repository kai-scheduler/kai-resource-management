# End-to-end tests

Ginkgo suites that run against a live cluster with the chart installed.

- `modules/` — the shared framework: connectivity, the cluster guard, resource
  builders, and waits.
- `suites/health` — the install is up. Fails first, so a broken install says why
  instead of every other suite timing out.
- `suites/flows` — the main flow and its variations: node pool, department,
  project, queues, then a pod through to an assigned pod group.

## Running them

**These mutate the cluster `KUBECONFIG` points at.** They are not part of
`make test`.

One command builds a kind cluster, installs the chart into it and runs the
suites:

```bash
hack/run-e2e-kind.sh
```

The cluster is deleted afterwards. While working on a spec, keep it and skip the
rebuild on the next run:

```bash
hack/run-e2e-kind.sh --preserve-cluster     # first run
hack/run-e2e-kind.sh --preserve-cluster --skip-build
```

`hack/setup-e2e-cluster.sh` does the cluster half on its own, if you want a
cluster to poke at without running anything: add `--skip-krm-install` to get one
with no chart on it. Both scripts take `-h`.

Against a cluster that is already set up, the suites alone are:

```bash
make test-e2e
make test-e2e GINKGO_FLAGS="--label-filter=flows"
```

`CLUSTER_NAME` (default `krm-e2e`) and `K8S_VERSION` override the defaults.

The cluster needs more than one node: `hack/kind-config.yaml` gives three
workers so node pools can own different nodes at the same time, and test pods
carry no control-plane toleration. `hack/e2e-values.yaml` lowers the resource
requests so everything fits on a small machine.

CI runs the same suites on every pull request; see the `e2e-tests` job and
`.github/actions/setup-e2e-cluster`.

## Safety contract

The suites create and delete cluster-scoped objects, so they must never run
against a real cluster. `modules/context` enforces this:

- `GetConnectivity` is the only way a spec gets a client, and it runs the
  preflight guard before returning one.
- Preflight refuses if the cluster holds a Project, Department, NodePool or
  ownerless Queue the suites did not create. The chart's own default node pool
  is expected and allowed.
- **There is no override.** Clean the objects up, or switch context.
- Every created object carries `kai.resources/e2e=true`, which preflight matches
  on, and `kai.resources/e2e-run=<id>` identifying the process that made it, so
  a run only ever deletes its own.

The project-controller suite also grants the controller cluster-scoped read on
Secrets, which the delete blocker in `hack/e2e-values.yaml` needs. Blockers are
runtime config, so whoever configures one grants its RBAC. It is named per run
and removed afterwards.

`preflight_test.go` covers the guard. It sits outside `make test` with the rest
of `test/e2e`, matching KAI-scheduler.
