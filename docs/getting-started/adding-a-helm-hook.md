# Adding a Helm hook

Every Job the chart runs around an install, upgrade or uninstall is the one `helm-hooks`
binary, published as its own image beside the controller images. Each Job selects a
subcommand with its `args`. A new lifecycle task is a new subcommand, not a new image.

| Subcommand | Hook | Weight |
| --- | --- | --- |
| `check-kai-scheduler-version` | `pre-install`, `pre-upgrade`, only with `kai-scheduler.enabled=false` | `1` |
| `apply-crds` | `pre-install`, `pre-upgrade` | `2` |
| `apply-config` | `post-install`, `post-upgrade` | `1` |
| `cleanup` | `post-delete` | none |

## Code

1. Put the logic in `pkg/helmhooks/<name>.go`: an exported function taking a
   `context.Context` and a `client.Client` or `client.Reader`.
2. Wire it in `cmd/helm-hooks/app/app.go`: a command constant, a `case` in `Run`, a
   function that parses the flags with `parseFlags` and calls the logic, and a line in
   `printUsage`. Validate the flags before calling `newClient()`, so a bad argument fails
   without connecting to the cluster. Register any new API type in the scheme in `init`.
3. Return errors that say what to do. `main` prints `Error while running helm-hooks: <error>`
   and exits 1, which fails the Job and the Helm operation with it.

## Chart

1. Add `templates/hooks/<pre|post>/<name>/job.yaml`, starting from
   `templates/hooks/pre/crd-upgrader.yaml`. Set only `args`; the image's entrypoint is the
   binary. Resolve the image with the `kai-resource-management.image` helper from a
   `<name>.image` block in `values.yaml` whose `name` is `helm-hooks`.
2. Add `rbac.yaml` beside it: a ServiceAccount, a role and its binding, with the same hook
   annotations at a lower weight than the Job, granting only what the subcommand reads or
   writes.
3. Add the ServiceAccount to both lists in `templates/rbac/scc.yaml`. Without it the Job
   never starts on OpenShift, and `make scc-check` fails.

## Tests

- `pkg/helmhooks/<name>_test.go`, with the controller-runtime fake client.
- `cmd/helm-hooks/app/app_test.go`, for flag handling against `unreachableClient`.
- `deployments/kai-resource-management-chart/tests/<name>_test.yaml`, for the hook
  annotations, weight, args and RBAC; and raise the subject count in `tests/scc_test.yaml`.
