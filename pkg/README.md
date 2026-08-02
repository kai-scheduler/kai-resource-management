# Packages

Shared Go implementation belongs under `pkg/<package>/`.

Keep packages cohesive and narrowly scoped. Avoid generic dumping-ground
packages such as `utils`; name packages after the domain behavior they provide.
Repository binaries under `cmd/` should depend on these packages rather than
duplicating implementation.

All packages share the repository's root Go module.
