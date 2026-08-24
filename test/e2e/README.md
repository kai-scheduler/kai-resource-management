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

```bash
kind create cluster --name krm-test --config hack/kind-config.yaml
kubectl apply --server-side -f \
  https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.88.0/example/prometheus-operator-crd/monitoring.coreos.com_servicemonitors.yaml

make build
for image in krm-operator project-controller nodepool-controller pod-group-assigner helm-hooks; do
  kind load docker-image --name krm-test registry/local/kai-resource-management/$image:0.0.0
done

helm package deployments/kai-resource-management-chart -d /tmp
helm install krm /tmp/kai-resource-management-0.0.0.tgz \
  -n kai-resource-management --create-namespace \
  --values ./hack/e2e-values.yaml --wait

make test-e2e
```

Pass ginkgo flags with `GINKGO_FLAGS`, for example
`make test-e2e GINKGO_FLAGS="--label-filter=flows"`.

The cluster needs more than one node: `hack/kind-config.yaml` gives three
workers so node pools can own different nodes at the same time, and test pods
carry no control-plane toleration.

CI runs the same thing on every pull request; see the `e2e-tests` job and
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

`preflight_test.go` covers the guard. It sits outside `make test` with the rest
of `test/e2e`, matching KAI-scheduler.
