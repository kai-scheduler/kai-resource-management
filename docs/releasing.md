# Releasing

This document is for maintainers. It describes how a version of KAI Resource
Management is released and what each release publishes.

## Versioning

Releases use semantic versioning with a leading `v`, for example `v0.1.0`.

- Minor and major releases are prepared from `main`.
- Patch releases are prepared from the matching `v*.*` release branch.

One version is used for everything a release produces: the Helm chart version,
the chart `appVersion`, and the tag of every controller image. The chart
resolves component image tags from `appVersion`, so the chart and the images it
references cannot drift apart.

## What a release publishes

A version tag publishes to GitHub Container Registry under
`ghcr.io/kai-scheduler/kai-resource-management`:

- `nodepool-controller`, `project-controller` and `pod-group-assigner` images,
  built for `linux/amd64` and `linux/arm64`.
- The `kai-resource-management` Helm chart, pushed as an OCI artifact.

The packaged chart is also attached to the GitHub Release as an asset.

GHCR is the authoritative registry for this project. No other registry mirrors
these artifacts.

## Prerequisites

- A repository secret named `RELEASE_BOT_TOKEN` containing a personal access
  token with `contents: write` and `pull-requests: write`. The default
  `GITHUB_TOKEN` cannot be used: tags it pushes do not trigger other workflows,
  so artifact publishing would never run.
- The account owning that token must be listed in
  [`MAINTAINERS.md`](../MAINTAINERS.md), because the release pull request edits
  `CHANGELOG.md` and only maintainers may do so.
- A repository label named `skip-changelog`. The release pull request applies
  it, and creating the pull request fails if the label does not exist.

## Releasing

### 1. Prepare the changelog

In the Actions tab, run the **Release — Prepare Changelog** workflow. Select the
branch to release from and enter the version, for example `v0.1.0`.

The workflow folds the pending fragments in `.changes/unreleased/` into
`CHANGELOG.md` as a new `## [v0.1.0]` section, clears the fragments, and opens a
`release/prepare-v0.1.0` pull request against the selected branch.

Preview the section beforehand without changing anything:

```bash
make changelog-preview VERSION=v0.1.0
```

### 2. Review and merge

Review the new changelog section for accuracy. `CHANGELOG.md` is the source of
truth for the release notes, so correct it in this pull request rather than
after the release.

Merging the pull request runs **Release — Tag & Publish**, which verifies that
the top changelog version matches the branch version and that the tag does not
already exist, then creates the tag and the GitHub Release.

### 3. Artifacts publish automatically

Pushing the tag runs **Upload artifacts to GitHub Container Registry**, which
builds and pushes the controller images and the chart, and attaches the chart to
the GitHub Release.

## Releasing without the automation

A release can be produced entirely by hand, which is also the fallback if the
automation fails partway through:

1. Run `make changelog-release VERSION=v0.1.0` locally and merge the resulting
   change through a normal pull request.
2. Create the release in the GitHub Releases interface, using the new
   `CHANGELOG.md` section as the notes.

Publishing the release creates the tag, which publishes the artifacts exactly as
above. This path does not need `RELEASE_BOT_TOKEN`.

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
