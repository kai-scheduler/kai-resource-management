# Releasing

This document is for maintainers. It describes how a version of KAI Resource
Management is released and what each release publishes.

## Versioning

Releases use semantic versioning with a leading `v`, for example `v0.1.0`.

Every release is tagged from a `v<major>.<minor>` release branch, never from
`main` directly:

- A new minor or major release starts by cutting `v<major>.<minor>` from `main`,
  for example `v0.1`. The `v0.1.0` tag is created on that branch.
- Patch releases reuse the same branch. Merge the fixes into it, then release
  `v0.1.1`, `v0.1.2` and so on from there.

One version is used for everything a release produces: the Helm chart version,
the chart `appVersion`, and the tag of every controller image. The chart
resolves component image tags from `appVersion`, so the chart and the images it
references cannot drift apart.

## What a release publishes

A version tag publishes to GitHub Container Registry under
`ghcr.io/kai-scheduler/kai-resource-management`:

- `nodepool-controller`, `project-controller`, `pod-group-assigner`,
  `krm-operator` and `helm-hooks` images, built for `linux/amd64` and
  `linux/arm64`.
- A `<version>-fips` variant of each of those images, built against the
  validated Go cryptographic module and selected by `global.fipsMode`. See
  [FIPS 140-3](fips.md).
- The `kai-resource-management` Helm chart, pushed as an OCI artifact. One chart
  serves both image variants.

The GitHub Release carries the packaged chart as an asset, alongside four
**image locks** — one per profile and architecture:

```text
imagelock-kai-resource-management-<version>-standard-linux-amd64.yaml
imagelock-kai-resource-management-<version>-standard-linux-arm64.yaml
imagelock-kai-resource-management-<version>-fips-linux-amd64.yaml
imagelock-kai-resource-management-<version>-fips-linux-arm64.yaml
```

Each lists the five images built from this repository — including the three the
krm-operator creates from the KRMConfig rather than the chart from a template —
pinned to the digest its tag resolved to at release time.

The bundled KAI Scheduler is deliberately not restated there: that project
publishes and pins its own images, and duplicating its digests would create two
records of one release that disagree as soon as a tag is re-pushed. An
air-gapped site mirrors both, and a lock that spans the whole platform is
composed from the per-project ones. See
[Install in an air-gapped cluster](how-to/install-in-an-air-gapped-cluster.md),
and [`build/imagelock/README.md`](../build/imagelock/README.md) for the generator.

GHCR is the authoritative registry for this project. No other registry mirrors
these artifacts.

## Prerequisites

These are configured; they are listed so the requirements are not lost.

- A repository secret named `KAIBOT_TOKEN` holding a token for the shared
  `KaiPilotBot` account, with `contents: write` and `pull-requests: write`. The
  default `GITHUB_TOKEN` cannot be used: tags it pushes do not trigger other
  workflows, so artifact publishing would never run.
- `KaiPilotBot` must have write access to this repository, because the release
  pull request is pushed and opened as that account. The account is authorized
  to edit `CHANGELOG.md` by name in `validate-changelog.yaml`, so it is
  deliberately not listed in [`MAINTAINERS.md`](../MAINTAINERS.md), which is a
  roster of people.
- The `skip-changelog` and `dependencies` labels. The release pull request
  applies `skip-changelog`, and creating the pull request fails if the label
  does not exist.

## Releasing

### 1. Make sure the release branch exists

For a new minor or major release, cut `v<major>.<minor>` from `main`, for
example `v0.1`.

For a patch release the branch already exists. Merge the fixes into it first;
do not re-cut it from `main`, which has moved on and would pull unreleased work
into the patch.

Everything below happens on that branch, not on `main`.

### 2. Prepare the changelog

In the Actions tab, run the **Release — Prepare Changelog** workflow. Select the
`v<major>.<minor>` release branch and enter the full version, for example
`v0.1.0`.

The workflow folds the pending fragments in `.changes/unreleased/` into
`CHANGELOG.md` as a new `## [v0.1.0]` section, clears the fragments, and opens a
`release/prepare-v0.1.0` pull request against the selected branch.

Preview the section beforehand without changing anything:

```bash
make changelog-preview VERSION=v0.1.0
```

### 3. Review and merge

Review the new changelog section for accuracy. `CHANGELOG.md` is the source of
truth for the release notes, so correct it in this pull request rather than
after the release.

Merging the pull request runs **Release — Tag & Publish**, which verifies that
the top changelog version matches the branch version and that the tag does not
already exist, then creates the tag and the GitHub Release.

### 4. Artifacts publish automatically

Pushing the tag runs **Upload artifacts to GitHub Container Registry**, which
builds and pushes the controller images and the chart, and attaches the chart to
the GitHub Release. A third job then generates the image locks and attaches those
too — it needs both image sets to exist, so it runs after the other two.

The lock job runs after the release is already published, so a failure there does
not hold anything back. Re-run the job, or generate the locks by hand from a
checkout of the tag:

```bash
make image-lock VERSION=v0.1.0
gh release upload v0.1.0 ./bin/imagelocks/*.yaml --clobber
```

## Releasing without the automation

A release can be produced entirely by hand, which is also the fallback if the
automation fails partway through:

1. Run `make changelog-release VERSION=v0.1.0` locally and merge the resulting
   change through a normal pull request.
2. Create the release in the GitHub Releases interface, using the new
   `CHANGELOG.md` section as the notes.

Publishing the release creates the tag, which publishes the artifacts exactly as
above. This path does not need `KAIBOT_TOKEN`.

## Branches other than tags

Pushes to `main` are validated but publish nothing. Only version tags publish
artifacts.

Pull requests build the controller images and package the chart to verify that
both still build, and cannot publish them: the workflow requests no registry
write permission and performs no registry login.

## Reading the Actions tab

**Release — Tag & Publish** appears for every pull request closed against `main`
or a `v*.*` branch, and is skipped unless the pull request was merged and came
from a `release/prepare-*` branch. GitHub can only filter this trigger on the
target branch, so the remaining condition is evaluated in the job itself. A
skipped entry is expected and consumes no runner time.
