# Quickstart

Install KAI Resource Management, define a node pool, a department and a project, and watch
an ordinary pod get placed and scheduled through them.

Around 15 minutes. At the end you will have seen every moving part of KRM do its job once,
which is the fastest way to make the [concepts](../concepts/README.md) concrete.

**Do this on a cluster you can throw away.** It creates cluster-scoped objects, installs
CRDs and admission webhooks, and labels a node.

## Prerequisites

- A Kubernetes cluster and `kubectl` pointed at it. One node is enough — you will still
  create a node pool and watch that node move into it.
- **Helm 3**.
- Permission to create CRDs, cluster-scoped RBAC, and admission webhooks.
- `jq`, for reading status conditions.
- The Prometheus Operator's `ServiceMonitor` CRD, **or** install with
  `--set serviceMonitor.create=false`. The chart renders `ServiceMonitor` objects, and
  without the type they fail to apply. If you have no Prometheus:

  ```bash
  kubectl apply --server-side -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.88.0/example/prometheus-operator-crd/monitoring.coreos.com_servicemonitors.yaml
  ```

No GPUs are needed. The workload at the end asks for CPU and memory only.

> Contributors working from a checkout can get a disposable three-node kind cluster with
> everything above already done: `hack/setup-e2e-cluster.sh`. See
> [building from source](building-from-source.md).

## 1. Install

KAI Scheduler is bundled as a subchart, so this installs both.

```bash
export KRM_NAMESPACE=kai-resource-management

helm upgrade --install krm \
  oci://ghcr.io/kai-scheduler/kai-resource-management/kai-resource-management \
  --namespace "${KRM_NAMESPACE}" \
  --create-namespace \
  --wait --timeout 10m
```

`--wait` matters here. The chart runs a pre-install hook that applies the CRDs, and the
objects you create in step 3 need those CRDs to exist.

## 2. Verify the installation

The single most useful check is the `KRMConfig`, which is where the operator reports what
it has managed to install:

```bash
kubectl get krmconfig krm-config -o jsonpath='{.status.conditions}' | jq
```

Wait for `Ready` to be `True`. If it is not, `Deployed`, `Available` and
`DependenciesFulfilled` say which part is outstanding — see
[troubleshooting](../how-to/troubleshooting.md).

Then confirm the four services are running. Helm installs only the operator; the operator
installs the other three, so they appear a moment later:

```bash
kubectl -n "${KRM_NAMESPACE}" get deploy
```

```text
NAME                  READY   UP-TO-DATE   AVAILABLE   AGE
krm-operator          1/1     1            1           2m
nodepool-controller   1/1     1            1           1m
pod-group-assigner    1/1     1            1           1m
project-controller    1/1     1            1           1m
```

The chart also creates the catch-all `default` node pool. Every node in the cluster
belongs to it right now:

```bash
kubectl get nodepool -o custom-columns=NAME:.metadata.name,PHASE:.status.phase
```

```text
NAME      PHASE
default   Ready
```

> KRM's custom resources define no printer columns, so a bare `kubectl get` shows only
> `NAME` and `AGE`. Every command below asks for the fields it wants explicitly.

## 3. Create a node pool

A node pool selects its nodes by one label key and one label value. **You do not have to
invent that label** — in most clusters a suitable one is already there.

Look at what your nodes already carry:

```bash
NODE=$(kubectl get nodes -o name \
  -l '!node-role.kubernetes.io/control-plane' | head -1 | cut -d/ -f2)

kubectl get "node/${NODE}" -o jsonpath='{.metadata.labels}' | jq
```

Labels that are commonly already present, and make good node pool selectors:

| Label | Set by | Typical value |
| --- | --- | --- |
| `nvidia.com/gpu.product` | NVIDIA GPU Operator / GPU Feature Discovery | `NVIDIA-H100-80GB-HBM3` |
| `nvidia.com/gpu.count` | NVIDIA GPU Operator | `8` |
| `node.kubernetes.io/instance-type` | Cloud provider | `p5.48xlarge` |
| `topology.kubernetes.io/zone` | Cloud provider | `us-east-1a` |
| `kubernetes.io/arch` | kubelet | `amd64` |

Selecting on a label your infrastructure already maintains is the better habit: new nodes
join the right node pool the moment they register, with nothing for you to remember.

**If one of those fits**, take its exact value and put it in the manifest below —
`labelValue` must match character for character, so copy it rather than typing it.

**If none fits** — a cluster with no GPU operator, or a split that has no hardware
meaning, like separating a team's dedicated nodes — add your own:

```bash
kubectl label "node/${NODE}" nvidia.com/gpu.product=H100
echo "labelled ${NODE}"
```

Either way, create the node pool that claims the node. The example selects
`nvidia.com/gpu.product=H100`; if you chose an existing label instead, edit `labelKey` and
`labelValue` to match it before applying:

```bash
kubectl apply -f docs/concepts/examples/nodepool.yaml
```

A node pool whose pair matches no node is not an error — it comes up `Empty` and waits. If
that happens, compare the pair against the node's actual labels; a mismatched value is the
usual cause.

<details>
<summary>Not working from a checkout? The manifest inline.</summary>

```yaml
apiVersion: kai.resources/v1alpha1
kind: NodePool
metadata:
  name: h100
spec:
  labelKey: nvidia.com/gpu.product
  labelValue: H100
```

</details>

Within a few seconds it claims the node:

```bash
kubectl get nodepool -o custom-columns=NAME:.metadata.name,PHASE:.status.phase
kubectl get nodepool h100 -o jsonpath='{.status.nodes}' | jq
```

```text
NAME      PHASE
default   Ready
h100      Ready
```

```json
[{"name":"<your-node>","status":"Ready"}]
```

The node also picked up a node-pool label from the controller, which is how the scheduler
knows where it belongs:

```bash
kubectl get "node/${NODE}" -o jsonpath='{.metadata.labels.kai\.scheduler/node-pool}'
```

```text
h100
```

And the node pool got its own scheduler shard — one scheduler instance responsible
for this pool's nodes:

```bash
kubectl get schedulingshard h100 \
  -o custom-columns=NAME:.metadata.name,PARTITION:.spec.partitionLabelValue
```

## 4. Create a department and a project

Order matters: the validating webhook rejects a project whose parent department does not
yet exist.

```bash
kubectl apply -f docs/concepts/examples/department.yaml
kubectl apply -f docs/concepts/examples/project.yaml
```

The department holds the quota its projects share; the project is the team. Both declare
one queue per node pool — the `h100` node pool, and the `default` node pool as a fallback.

The project gets a namespace:

```bash
kubectl get project -o custom-columns=\
NAME:.metadata.name,PHASE:.status.phase,NAMESPACE:.status.namespace
```

```text
NAME       PHASE   NAMESPACE
research   Ready   kai-research
```

And the queues appear, with the project's parented to the department's for the *same* node
pool:

```bash
kubectl get queue -o custom-columns=\
NAME:.metadata.name,PARENT:.spec.parentQueue,GPU_QUOTA:.spec.resources.gpu.quota
```

```text
NAME                PARENT             GPU_QUOTA
engineering         <none>             0
engineering-h100    <none>             8
research            engineering        0
research-h100       engineering-h100   4
```

That parenting is the hierarchy doing its work: `research-h100` is guaranteed 4 GPUs, and
can never exceed what `engineering-h100` allows however much it borrows. See
[queues and quota](../concepts/queues-and-quota.md).

If `PHASE` stays `NotReady`, the conditions say which piece is missing:

```bash
kubectl get project research -o jsonpath='{.status.conditions}' | jq
```

## 5. Submit a workload

Now act as a team member. Submit a plain pod into the project's namespace — it names no
queue and no node pool:

```bash
kubectl apply -f docs/concepts/examples/workload-pod.yaml
```

<details>
<summary>The manifest inline.</summary>

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: sample-workload
  namespace: kai-research
spec:
  schedulerName: kai-scheduler
  restartPolicy: Never
  containers:
    - name: workload
      image: registry.k8s.io/pause:3.9
      resources:
        requests:
          cpu: 100m
          memory: 128Mi
```

</details>

### See what admission did to it

```bash
kubectl get pod sample-workload -n kai-research \
  -o jsonpath='scheduler={.spec.schedulerName}{"\n"}project={.metadata.labels.project}{"\n"}'
```

```text
scheduler=kai-scheduler
project=research
```

The `project` label was not in the manifest. Admission resolved it from the namespace. It
also added node affinity restricting the pod to the project's `defaultNodePools`:

```bash
kubectl get pod sample-workload -n kai-research \
  -o jsonpath='{.spec.affinity.nodeAffinity}' | jq
```

### See where it was placed

KAI Scheduler grouped the pod into a `PodGroup`; the pod group assigner chose a node pool
for it and charged it to the matching queue:

```bash
kubectl get podgroup -n kai-research -o custom-columns=\
NAME:.metadata.name,QUEUE:.spec.queue,POOL:'.metadata.labels.kai\.scheduler/node-pool'
```

```text
NAME                  QUEUE           POOL
pg-sample-workload    research-h100   h100
```

Neither the queue nor the node pool was named by the person who submitted the pod. That is
the whole point — see [workload placement](../concepts/workload-placement.md) for how each
was resolved.

### See it running

```bash
kubectl get pod sample-workload -n kai-research -o wide
```

It is on the node you labelled in step 3 — the only node in the `h100` node pool.

If it stays `Pending`, ask the scheduler why:

```bash
kubectl describe pod sample-workload -n kai-research | tail -20
kubectl get podgroup -n kai-research -o jsonpath='{.items[0].status}' | jq
```

## 6. Clean up

**Order matters**, because finalizers enforce it. Deleting out of order does not corrupt
anything, but objects sit in `Terminating` until their dependants are gone.

```bash
# 1. The workload — before the project that owns its namespace.
kubectl delete pod sample-workload -n kai-research

# 2. The project — before the department it is parented to.
kubectl delete project research

# 3. The department. Blocked until no project references it.
kubectl delete department engineering

# 4. The node pool. Blocked while a project still has a queue for it,
#    which is why it comes after the project.
kubectl delete nodepool h100

# 5. The node label.
kubectl label "node/${NODE}" nvidia.com/gpu.product-
```

If a delete appears to hang, it is waiting rather than stuck, and it will say what for:

```bash
kubectl get department engineering -o jsonpath='{.status.conditions}' | jq
kubectl get nodepool h100 -o jsonpath='{.status.conditions}' | jq
```

The `default` node pool cannot be deleted — it is the catch-all, and nothing recreates it.

To remove KRM itself:

```bash
helm uninstall krm --namespace "${KRM_NAMESPACE}"
```

Helm deliberately retains CRDs, and Kubernetes therefore retains any custom resources you
left behind. Remove those by hand only when you intend permanent data removal — see the
[chart documentation](../../deployments/kai-resource-management-chart/README.md#uninstall).

## What to read next

| If you want to | Read |
| --- | --- |
| Understand what you just created | [Concepts](../concepts/README.md) |
| Split a real fleet into node pools | [Partition nodes into node pools](../how-to/partition-nodes-into-node-pools.md) |
| Give several teams a shared budget | [Model an org with departments](../how-to/model-an-org-with-departments.md) |
| Control where workloads land | [Place workloads across node pools](../how-to/place-workloads-across-node-pools.md) |
| Install this properly, for real | [Chart documentation](../../deployments/kai-resource-management-chart/README.md) |
| Work out why something did not appear | [Troubleshooting](../how-to/troubleshooting.md) |
