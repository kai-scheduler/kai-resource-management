<!-- Copyright 2026 NVIDIA CORPORATION -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# imagelock — air-gap image lock generator

For every tagged release this command writes a digest-pinned list of the container
images an install needs, so an air-gapped cluster can mirror exactly what that
release was built from. The files are attached to the GitHub Release as assets;
nothing in the chart changes.

This page is for developers working on the generator. The operator-facing guide —
downloading a lock, mirroring it, installing from a private registry — is
[Install in an air-gapped cluster](../../docs/how-to/install-in-an-air-gapped-cluster.md).

## Scope

The lock covers **this repository's images only**. The bundled kai-scheduler
subchart publishes and pins its own, so its images are recognised, counted and left
alone. Ownership follows the registry, not a list of names: anything under the
release registry is locked, anything under `ghcr.io/kai-scheduler/kai-scheduler` is
skipped, and anything under neither stops the run — a third-party image reaches an
air-gapped site only if someone puts it there on purpose.

## What it does

1. Renders the chart with `helm template`, once per profile.
2. Collects every image the install would run. Both spellings count: a pod spec's
   `image: repo:tag`, and the `{name, repository, tag}` objects inside the KRMConfig
   and KAI Config, which travel as ConfigMap text and name images that appear in no
   pod spec at all.
3. Classifies each one against the registry catalog above.
4. Resolves each locked tag to the multi-arch index digest and to the per-platform
   manifest digest inside it.
5. Writes one ImageLock document per profile and platform.

Nothing is written until every digest is in hand, so a registry failure leaves no
partial set behind.

## Commands

```bash
make image-lock VERSION=v1.2.3   # write the locks into bin/imagelocks/
make image-lock-check            # offline coverage check; no network, no files
```

`image-lock-check` is part of `make validate`, so a chart change that introduces an
image nobody has classified fails on the pull request rather than during a release.

## Output

Four files per release — two profiles by two platforms:

```text
bin/imagelocks/
  imagelock-kai-resource-management-v1.2.3-standard-linux-amd64.yaml
  imagelock-kai-resource-management-v1.2.3-standard-linux-arm64.yaml
  imagelock-kai-resource-management-v1.2.3-fips-linux-amd64.yaml
  imagelock-kai-resource-management-v1.2.3-fips-linux-arm64.yaml
```

The `fips` profile exists because the chart's `global.fipsMode` appends `-fips` to
every image tag, so it resolves to a different set of digests.

## Where the code lives

Everything is in this one package, unexported, with no API for anything else to
depend on.

| File | Holds |
| --- | --- |
| `main.go` | Flags, validation, and the render → classify → resolve → write run |
| `chart.go` | Rendering the chart, finding the images, deciding who owns each one |
| `registry.go` | Digest resolution, through go-containerregistry |
| `lock.go` | The ImageLock document, its file name, and writing the set |

## When a new image appears

A new service published under the release registry is picked up automatically — it
needs no change here. An image under some other registry fails
`make image-lock-check` with its repository named, and the fix is a decision, not a
code change: either it belongs to this release, and its registry goes in the
catalog in `chart.go`, or it belongs to another project's lock.

## The locks are read by something else

They are consumed by the tooling that composes a platform-wide artifact lock out of
every project's. That reader validates strictly and rejects the whole set on one bad
entry, so `lock_test.go` restates its rules — including the exact YAML key names,
checked against the encoded bytes rather than round-tripped through the struct that
wrote them, because a misspelling consistent on both sides would otherwise pass.
