<!-- Copyright 2026 NVIDIA CORPORATION -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# imagelock — air-gap image lock generator

For every tagged release this command writes a digest-pinned list of the container
images an install needs, so an air-gapped cluster can mirror exactly what that
release was built from. The files are attached to the GitHub Release as assets;
nothing in the chart changes.

This page is for developers working on the generator. The operator-facing guide is
[Install in an air-gapped cluster](../../docs/how-to/install-in-an-air-gapped-cluster.md).

## Scope

The lock covers **this repository's images only** — the bundled kai-scheduler
subchart publishes and pins its own. Ownership follows the registry, not a list of
names: anything under the release registry is locked, anything under
`ghcr.io/kai-scheduler/kai-scheduler` is skipped, and anything under neither stops
the run, because a third-party image reaches an air-gapped site only on purpose.

## What it does

1. Renders the chart with `helm template`, once per profile.
2. Collects every image the install would run — both a pod spec's `image: repo:tag`
   and the `{name, repository, tag}` objects inside the KRMConfig and KAI Config,
   which travel as ConfigMap text and name images in no pod spec at all.
3. Classifies each one against the registry catalog above.
4. Resolves each locked tag to its multi-arch index digest and the per-platform
   manifest digest inside it.
5. Writes one ImageLock per profile and platform — nothing until every digest is in
   hand, so a registry failure leaves no partial set behind.

## Commands

```bash
make image-lock VERSION=v1.2.3   # write the locks into bin/imagelocks/
make image-lock-check            # coverage check only: no registry lookups, no locks written
```

Both targets fetch the bundled subchart into `charts/` first, through `helm-deps`.
The generator itself reaches no registry under `--verify-only`; the dependency
fetch does.

`image-lock-check` is part of `make validate`, so a chart change that introduces an
unclassified image fails on the pull request rather than during a release.

## Output

Four files per release — two profiles by two platforms:

```text
bin/imagelocks/
  imagelock-kai-resource-management-v1.2.3-standard-linux-amd64.yaml
  imagelock-kai-resource-management-v1.2.3-standard-linux-arm64.yaml
  imagelock-kai-resource-management-v1.2.3-fips-linux-amd64.yaml
  imagelock-kai-resource-management-v1.2.3-fips-linux-arm64.yaml
```

The `fips` profile exists because `global.fipsMode` appends `-fips` to every image
tag, so it resolves to a different set of digests.

## Where the code lives

One package, unexported, with no API for anything else to depend on.

| File | Holds |
| --- | --- |
| `main.go` | Flags, validation, and the render → classify → resolve → write run |
| `chart.go` | Rendering, finding the images, deciding who owns each one |
| `registry.go` | Digest resolution, through go-containerregistry |
| `lock.go` | The ImageLock document, its file name, and writing the set |

## When a new image appears

A new service under the release registry is picked up automatically. An image under
some other registry fails `make image-lock-check` with its repository named, and the
fix is a decision rather than a code change: either it belongs to this release, and
its registry goes in the catalog in `chart.go`, or it belongs to another lock.

## The locks are read by something else

They feed the tooling that composes a platform-wide artifact lock from every
project's. That reader validates strictly and rejects the whole set on one bad
entry, so `lock_test.go` restates its rules — including the exact YAML key names,
checked against the encoded bytes rather than round-tripped through the struct that
wrote them, because a misspelling consistent on both sides would otherwise pass.
