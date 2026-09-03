# How-to guides

One task per guide. Each assumes KAI Resource Management is installed — if it is not,
start with the [quickstart](../getting-started/quickstart.md) — and each explains the
reasoning as well as the commands, so you can adapt it rather than only copy it.

| Guide | Use it when |
| --- | --- |
| [Partition nodes into node pools](partition-nodes-into-node-pools.md) | Your cluster has more than one kind of hardware and you want them treated as separate capacity |
| [Model an org with departments](model-an-org-with-departments.md) | Several teams need to share a budget without stepping on each other |
| [Place workloads across node pools](place-workloads-across-node-pools.md) | You need control over where a workload lands, or a fallback when its first choice is full |
| [Exclude nodes from management](exclude-nodes-from-management.md) | Some nodes must stay outside KRM's control |
| [Tune per-node-pool scheduling](tune-per-node-pool-scheduling.md) | One pool needs different scheduling behaviour from another |
| [Install in an air-gapped cluster](install-in-an-air-gapped-cluster.md) | The cluster has no route to the internet and every image has to come from your own registry |
| [Troubleshooting](troubleshooting.md) | Something did not appear, or a workload is not running |

If you are not sure which abstraction you need, read [concepts](../concepts/README.md)
first. These guides assume you know what a node pool and a queue are.
