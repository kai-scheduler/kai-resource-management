# End-to-end tests

Ginkgo suites that run against a live cluster with the chart installed.

- `modules/` — the shared framework: connectivity, the cluster guard, resource
  builders, and waits.
- `suites/health` — the install is up. Fails first, so a broken install says why
  instead of every other suite timing out.
- `suites/flows` — the main flow and its variations: node pool, department,
  project, queues, then a pod through to an assigned pod group.
- `suites/nodepool-controller`, `suites/project-controller`,
  `suites/pod-group-assigner` — one per controller.
- `suites/upgrade` — installs nothing itself: it builds a world on whatever
  version is installed, upgrades the release to another chart, and asks whether
  the world came through. Excluded from `make test-e2e`; see below.

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
make test-e2e E2E_LABEL_FILTER=flows
```

`E2E_LABEL_FILTER` defaults to `!upgrade`, so an ordinary run skips the upgrade
suite. Narrow the run with that variable rather than with a `--label-filter` in
`GINKGO_FLAGS`: a second one would win and quietly re-enable `upgrade`.

`CLUSTER_NAME` (default `krm-e2e`) and `K8S_VERSION` override the defaults.

The cluster needs more than one node: `hack/kind-config.yaml` gives three
workers so node pools can own different nodes at the same time, and test pods
carry no control-plane toleration. `hack/e2e-values.yaml` lowers the resource
requests so everything fits on a small machine.

CI runs the same suites on every pull request; see the `e2e-tests` and
`e2e-upgrade-tests` jobs and `.github/actions/setup-e2e-cluster`.

## Upgrade tests

The upgrade suite needs what an ordinary run has not got: a cluster already
running the previous release, and a chart to upgrade it to. One command arranges
both:

```bash
hack/run-e2e-upgrade-kind.sh
```

It builds a cluster, installs the newest published release, packages this tree's
chart and upgrades onto it. It takes the same `--skip-build` and
`--preserve-cluster` flags as `run-e2e-kind.sh`, and `UPGRADE_FROM_VERSION` pins
the release to start from.

With no release published it prints a line and exits 0. There is nothing to
upgrade from, and upgrading from this same tree would test a path no user takes.
`CHART_OCI` points the script at another registry, which is how to exercise it
before the first release.

Against a cluster that already has a previous release installed, the suite alone
is:

```bash
UPGRADE_CHART_PATH=/abs/path/to/chart.tgz \
UPGRADE_VALUES_FILE=$PWD/hack/e2e-values.yaml \
UPGRADE_IMAGE_TAG=0.0.0 \
  make test-e2e-upgrade
```

Those paths must be absolute: ginkgo runs each suite with its own package
directory as the working directory, and the `helm` the suite shells out to
inherits it. `UPGRADE_VALUES_FILE` and `UPGRADE_IMAGE_TAG` have to repeat
whatever the install used — helm gives no way to hold values steady across an
upgrade, so leaving them out changes the configuration underneath the test.

## Safety contract

The suites create and delete cluster-scoped objects, so they must never run
against a real cluster. `modules/context` enforces this:

- `GetConnectivity` is the only way a spec gets a client, and it runs the
  preflight guard before returning one.
- Preflight refuses if the cluster holds a Project, Department, NodePool or
  ownerless Queue the suites did not create. The chart's own default node pool
  is expected and allowed.
- **There is no override.** Clean the objects up, or switch context.
- The upgrade suite additionally runs `helm upgrade` against the `krm` release,
  a stronger mutation than the other suites make. It is bounded by the same
  guard, and by being labelled out of `make test-e2e`.
- Every created object carries `kai.resources/e2e=true`, which preflight matches
  on, and `kai.resources/e2e-run=<id>` identifying the process that made it, so
  a run only ever deletes its own.

The project-controller suite also grants the controller cluster-scoped read on
Secrets, which the delete blocker in `hack/e2e-values.yaml` needs. Blockers are
runtime config, so whoever configures one grants its RBAC. It is named per run
and removed afterwards.

`preflight_test.go` covers the guard. It sits outside `make test` with the rest
of `test/e2e`, matching KAI-scheduler.
