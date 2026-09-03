# KAI Resource Management documentation

KAI Resource Management (KRM) adds projects, departments, node pools and quota queues on
top of [KAI Scheduler](https://github.com/kai-scheduler/KAI-Scheduler), so that a shared
GPU cluster can be divided between teams and their workloads placed automatically.

New here? Read the [overview](overview.md), then follow the
[quickstart](getting-started/quickstart.md).

## Start here

| I want to | Read |
| --- | --- |
| Understand what KRM is and how it fits with KAI Scheduler | [Overview](overview.md) |
| Install it and run my first workload | [Quickstart](getting-started/quickstart.md) |
| Understand the objects I will be creating | [Concepts](concepts/README.md) |
| Do one specific thing | [How-to guides](how-to/README.md) |
| Look up a field, label or condition | [Reference](reference/README.md) |
| Configure the Helm chart | [Chart documentation](../deployments/kai-resource-management-chart/README.md) |

## Documentation map

### Concepts — what the objects mean

| Document | Covers |
| --- | --- |
| [Node pools](concepts/node-pools.md) | Slicing the cluster's nodes; the `default` node pool; node pool phases |
| [Projects and departments](concepts/projects-and-departments.md) | The team hierarchy, project namespaces, deletion order |
| [Queues and quota](concepts/queues-and-quota.md) | How quota is expressed and enforced per node pool |
| [Workload placement](concepts/workload-placement.md) | What happens to a pod between `kubectl apply` and running |
| [KRMConfig](concepts/krm-config.md) | The single object describing your installation |

Ready-to-apply manifests for every custom resource live in
[`concepts/examples/`](concepts/examples/).

### How-to guides — one task each

| Document | Covers |
| --- | --- |
| [Partition nodes into node pools](how-to/partition-nodes-into-node-pools.md) | Splitting a mixed fleet by node label |
| [Model an org with departments](how-to/model-an-org-with-departments.md) | Department quota shared across projects |
| [Place workloads across node pools](how-to/place-workloads-across-node-pools.md) | Choosing where a workload runs, and the fallback order |
| [Exclude nodes from management](how-to/exclude-nodes-from-management.md) | Keeping nodes out of KRM's control |
| [Tune per-node-pool scheduling](how-to/tune-per-node-pool-scheduling.md) | Placement strategy, fairness, NUMA |
| [Install in an air-gapped cluster](how-to/install-in-an-air-gapped-cluster.md) | Mirroring the release's images and installing with no internet access |
| [Troubleshooting](how-to/troubleshooting.md) | Symptom, what to check, what it means |

### Reference — look things up

| Document | Covers |
| --- | --- |
| [API reference](reference/api.md) | Every field of every custom resource |
| [Labels and annotations](reference/labels-and-annotations.md) | The keys KRM reads and writes |
| [Conditions and phases](reference/conditions-and-phases.md) | Every status value and what to do about it |

### Operations

| Document | Covers |
| --- | --- |
| [Chart documentation](../deployments/kai-resource-management-chart/README.md) | Install, configure, upgrade, uninstall |
| [FIPS 140-3](fips.md) | FIPS images and run-time modes |

## Contributing to KRM

These are for people changing KRM itself, not people running it.

| Document | Covers |
| --- | --- |
| [Building from source](getting-started/building-from-source.md) | Local build, test and validation commands |
| [Designs](designs/README.md) | Architecture decisions and implementation designs |
| [End-to-end tests](../test/e2e/README.md) | The e2e suites and their safety contract |
| [Updating the API module](updating-the-api-module.md) | Changing a `kai.resources` CRD |
| [Releasing](releasing.md) | The maintainer release process |

## Writing documentation

Documentation is a product surface. Changes to behavior, configuration, installation or
operations must update the relevant documentation in the same pull request.

- State the intended audience and prerequisites.
- Prefer commands that can be copied and run.
- Document defaults, side effects, permissions, and rollback behavior.
- Keep user-facing explanations separate from implementation details.
- Update stale documentation as part of the code change that makes it stale.
- Do not describe planned behavior as if it is already released.

Examples and sample YAML belong beside the documentation that explains them. Do not
create a top-level `examples` directory. A sample without surrounding documentation is
incomplete.

Chart-specific build, configuration, upgrade and uninstall documentation belongs with the
chart under `deployments/kai-resource-management-chart/`.
