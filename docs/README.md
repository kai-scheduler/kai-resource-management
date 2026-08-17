# Documentation

Documentation is a product surface. Changes to behavior, configuration,
installation, or operations must update the relevant documentation in the same
pull request.

## Structure

The initial documentation structure is intentionally small:

- `getting-started/` — local setup, first installation, and first successful use.
- `designs/` — architecture decisions and implementation designs.
- `fips.md` — FIPS 140-3 images and run-time modes.
- `releasing.md` — the maintainer release process.
- `updating-the-api-module.md` — releasing a `kai.resources` CRD change and
  consuming it here.

Chart-specific build, configuration, upgrade, and uninstall documentation
belongs with the chart under `deployments/kai-resource-management-chart/`.

## Examples

Examples and sample YAML belong beside the documentation that explains them. Do
not create a top-level `examples` directory. A sample without surrounding
documentation is incomplete.

## Writing requirements

- State the intended audience and prerequisites.
- Prefer commands that can be copied and run.
- Document defaults, side effects, permissions, and rollback behavior.
- Keep user-facing explanations separate from implementation details.
- Update stale documentation as part of the code change that makes it stale.
- Do not describe planned behavior as if it is already released.

Developer setup begins in
[getting-started/building-from-source.md](getting-started/building-from-source.md).

Cluster administrators can start with the
[KAI Resource Management chart documentation](../deployments/kai-resource-management-chart/README.md).

Maintainers cutting a release start with [releasing.md](releasing.md).

Maintainers changing a `kai.resources` CRD start with
[updating-the-api-module.md](updating-the-api-module.md).
