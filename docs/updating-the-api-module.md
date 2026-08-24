# Updating the API module

Audience: maintainers changing the `kai.resources` CRD contracts.

The `kai.resources` types — currently Project, Department, NodePool,
ManagedNodesConfig and KRMConfig, in `v1alpha1` — are owned by
[`github.com/kai-scheduler/kai-resource-management-api`][api]. This repository
consumes them and never defines them. Nothing here generates CRDs; `crds/` is a
copy of the manifests released by that module.

Changing a type therefore takes two pull requests: one in the API repository to
release the change, and one here to consume it.

## 1. Release the change from the API repository

```bash
# Edit the types.
vim kai/v1alpha1/nodepool_types.go

# Regenerate DeepCopy implementations and the CRD manifests.
make generate
make manifests

make validate
git commit -s -m "feat: ..."
```

Open the pull request and merge it, then record the version in `CHANGELOG.md`
(that repository writes it by hand).

Publish the release from the GitHub UI:

1. Open the [API repository][api] and go to **Releases** in the right-hand
   sidebar.
2. Click **Draft a new release**.
3. Under **Choose a tag**, type the new version — `v0.2.0` — and pick
   **Create new tag: v0.2.0 on publish**.
4. Leave **Target** on `main`.
5. Set the release title to the same version and describe what changed.
6. Click **Publish release**.

Publishing creates the tag, which is what `go get` resolves. The API repository
has no release workflow, so nothing else runs.

## 2. Consume the new version here

```bash
go get github.com/kai-scheduler/kai-resource-management-api@v0.2.0
go mod tidy
make sync-crds
make validate
```

`make validate` runs both guards: `sync-crds-check` fails if `crds/` no longer
matches the pinned module, and `crd-rbac-check` fails if a CRD is missing from
the chart's RBAC.

Review `git diff -- deployments/kai-resource-management-chart/crds/` before
committing. Open the pull request with the `skip-changelog` label.

## Things that catch people out

**A new CRD kind needs one manual edit.** Add its plural name to `resourceNames`
in `templates/rbac/crd-manager.yaml`. The pre-install hook applies the CRDs under
a ClusterRole restricted by name, so without the entry the install fails.
`make crd-rbac-check` reports the exact missing name.

**A new kind in an existing version needs no scheme change.** `AddToScheme`
registers every type in its package, so adding a kind to `kai/v1alpha1` leaves
the `init` functions in `cmd/*/main.go` working untouched.

**A new API version does need a scheme change.** Each group-version is a separate
Go package with its own `SchemeBuilder` and `AddToScheme`. Introducing
`kai/v1beta1` means importing it and adding a line to `init` in every
`cmd/*/main.go`, alongside the existing one:

```go
utilruntime.Must(kaires.AddToScheme(scheme))
utilruntime.Must(kairesv1beta1.AddToScheme(scheme))
```

Nothing fails at compile time if you forget — the controller only fails at
runtime, when it cannot recognise objects of the new version.

**Never delete and recreate a published release or tag.** `go.sum` pins the
content hash of the released version, so a tag that moves produces a checksum
mismatch that looks like tampering to every consumer. Publish a new patch version
instead.

**Do not hand-edit `crds/`.** It is generated output. Fix the API repository,
release it, and bump the pin.

[api]: https://github.com/kai-scheduler/kai-resource-management-api
