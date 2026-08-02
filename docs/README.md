# Documentation

Documentation is a product surface. Changes to behavior, configuration,
installation, or operations must update the relevant documentation in the same
pull request.

## Audiences

Organize new documentation by its intended reader:

- `getting-started/` — first installation and first successful use.
- `concepts/` — projects, departments, node pools, queues, and ownership models.
- `guides/` — task-oriented operational workflows.
- `reference/` — configuration, Helm values, APIs, metrics, and compatibility.
- `migration-guides/` — release-to-release upgrade and breaking-change guidance.
- `developer/` — architecture, designs, local development, and maintenance.

Create these directories when the first real document in that category is
added. Do not add empty directory placeholders.

## Examples

Examples and sample YAML belong beside the guide or concept that explains them.
Do not create a top-level `examples` directory. A sample without surrounding
documentation is incomplete.

## Writing requirements

- State the intended audience and prerequisites.
- Prefer commands that can be copied and run.
- Document defaults, side effects, permissions, and rollback behavior.
- Keep user-facing explanations separate from implementation details.
- Update stale documentation as part of the code change that makes it stale.
- Do not describe planned behavior as if it is already released.

Developer setup begins in
[developer/building-from-source.md](developer/building-from-source.md).

Cluster administrators can start with the
[KAI Resource Management chart documentation](../charts/kai-resource-management/README.md).
