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

Each minor version matches the KAI Scheduler minor it bundles: `v0.18` bundles
KAI Scheduler `v0.18.*`. Patch numbers are independent.

One version is used for everything a release produces: the Helm chart version,
the chart `appVersion`, and the tag of every controller image. The chart
resolves component image tags from `appVersion`, so the chart and the images it
references cannot drift apart.

The chart is also tagged in the registry without the `v`, for example `0.18.5`
next to `v0.18.5`. Both tags point at the same chart. The un-prefixed one exists
because Helm cannot resolve a version range such as `~0.18.0` against tags that
start with `v`. Git tags keep the `v`, which Go modules require.

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

The packaged chart is also attached to the GitHub Release as an asset, together
with `subcharts.yaml`, which lists the sub-chart versions that chart was built
with, for example `kai-scheduler: 0.18.5`.

GHCR is the authoritative registry for this project. No other registry mirrors
these artifacts.

## Prerequisites

These are configured; they are listed so the requirements are not lost.

- A repository secret named `KAIBOT_TOKEN` holding a token for the shared
  `KaiPilotBot` account, with `contents: write`, `pull-requests: write` and
  `actions: write`; the last lets the scheduled release workflow start
  **Release — Prepare Changelog**. The default `GITHUB_TOKEN` cannot be used:
  tags it pushes do not trigger other workflows, so artifact publishing would
  never run.
- `KaiPilotBot` must have write access to this repository, because the release
  pull request is pushed and opened as that account. The account is authorized
  to edit `CHANGELOG.md` by name in `validate-changelog.yaml`, so it is
  deliberately not listed in [`MAINTAINERS.md`](../MAINTAINERS.md), which is a
  roster of people.
- The `skip-changelog` and `dependencies` labels. The release pull request
  applies `skip-changelog`, the KAI Scheduler bump pull requests apply
  `dependencies`, and creating a pull request fails if its label does not
  exist.
- Auto-merge enabled on the repository, and GitHub Actions allowed to approve
  pull requests. The KAI Scheduler bump on `main` is approved by
  `github-actions` and merges itself once its checks pass, and so does every
  release changelog pull request.

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
`v0.1.0`. On any other ref, `main` included, the workflow stops with an error.

The workflow folds the pending fragments in `.changes/unreleased/` into
`CHANGELOG.md` as a new `## [v0.1.0]` section, clears the fragments, and opens a
`release/prepare-v0.1.0` pull request against the selected branch.

When the chart declares KAI Scheduler as a version range and it now resolves to
a newer version than the previous release bundled, the workflow adds a
`Bundle KAI Scheduler <version>` fragment itself. A release with no fragments at
all gets a `Maintenance release` one, so its notes are never empty.

Preview the section beforehand without changing anything:

```bash
make changelog-preview VERSION=v0.1.0
```

### 3. Review and merge

The pull request is approved by `github-actions` and merged by `KaiPilotBot`
once its checks pass, as long as it changes nothing but `CHANGELOG.md` and the
changelog fragments. Anything pushed on top of the bot's commit, or any other
file in the pull request, leaves it for a maintainer.

Its checks are the full pull request CI, e2e and upgrade e2e included, even
though it changes only the changelog. They build the chart the same way the
release does, so they test the KAI Scheduler version the release will bundle.

`CHANGELOG.md` is the source of truth for the release notes, so check the new
section for accuracy while the checks run, and correct it in the pull request
rather than after the release.

Two limits of the automatic merge are accepted. The approval is not dismissed by
a later push, so someone with write access could add a change while the checks
run, as with the **Bump KAI Scheduler** pull requests. And a version range is
resolved more than once: when the changelog is prepared, when the pull request
checks build the chart, and when the chart is built after the merge. If KAI
Scheduler publishes another patch in between, the release bundles a version the
e2e tests did not run against, and the `Bundle KAI Scheduler` entry may name an
earlier one. `subcharts.yaml` always records the version the chart carries.

Merging the pull request runs **Release — Tag & Publish**, which verifies that
the top changelog version matches the branch version and that the tag does not
already exist, then creates the tag and the GitHub Release.

The release is marked **Latest** only when its version is the highest release
tag. A patch to an older minor, for example `v0.18.6` after `v0.19.0` exists,
is published without taking **Latest** from the newer minor.

### 4. Artifacts publish automatically

Pushing the tag runs **Upload artifacts to GitHub Container Registry**, which
builds and pushes the controller images and the chart, adds the chart tag
without the `v`, and attaches the chart and `subcharts.yaml` to the GitHub
Release.

## Scheduled patch releases

**Release — Scheduled Patch Releases** runs on Sunday and Wednesday at 23:00 UTC
and can be started from the Actions tab, with **dry-run** to see what it would
do. For each `v<major>.<minor>` branch it starts the next patch release, for
example `v0.18.4` after `v0.18.3`, when the KAI Scheduler version range in the
chart now resolves to a newer version than the `subcharts.yaml` of the latest
release records.

Commits on the branch do not start a release by themselves. They ship with the
next KAI Scheduler patch, or sooner if a maintainer releases the branch by hand.

It also does nothing when the next tag or a `release/prepare-*` pull request for
it already exists, or when the branch has no release tag yet.

A branch takes part only while its chart declares KAI Scheduler as a version
range, for example `version: "~0.18.0"` in `Chart.yaml`. A branch that pins an
exact version is left to **Bump KAI Scheduler** and to manual releases, and
`main` is never released this way.

The workflow runs **Release — Prepare Changelog** on the branch, so the release
continues exactly as a manual one does in steps 2 to 4. The release notes come
from fragments that were already reviewed in their own pull requests.

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

Every merge to `main` publishes a development build of each image and of the
chart, versioned `0.0.0-<short-sha>`. These are untested and unsupported, carry
no upgrade guarantee, and exist to try an unreleased fix — not to run in
production. `0.0.0` sorts below every real release, so an install that does not
name a version never selects one.

Every push to a `v<x>.<y>` release branch publishes the same kind of build,
versioned `<x>.<y>-dev-<short-sha>`, for example `0.18-dev-1db3d56` from `v0.18`.
It carries no patch number, so it cannot be mistaken for a `v0.18.z` release, and
as a pre-release it is likewise never selected by an install that does not name a
version.

No FIPS variant is built from a branch, and no GitHub Release exists to attach
the chart to. Both remain tag-only.

Development versions are pruned weekly by **Clean up development packages**,
which keeps the 30 most recent per package, counting both kinds together. It
ignores every version that does not begin with `0.0.0-` or `<x>.<y>-dev-`, so a
release can never be selected for deletion.
Anything older than those 30 is removed: do not pin to a development version you
need to keep.

Pull requests build the controller images and package the chart to verify that
both still build, and cannot publish them: the workflow requests no registry
write permission and performs no registry login.

## Keeping KAI Scheduler current

**Bump KAI Scheduler** runs daily and, as `KaiPilotBot`, opens one pull request
for `main` and one for each `v<major>.<minor>` release branch that is behind:

- `main` follows KAI Scheduler's own development builds: the chart dependency
  moves to the newest published `0.0.0-<short-sha>`. The Go API types come from
  `github.com/kai-scheduler/api` and are bumped separately.
- Each `v<major>.<minor>` branch moves to the newest KAI Scheduler release of
  the same minor, for example `v0.18.2` on `v0.18`, with a `Changed` changelog
  fragment.

A branch whose chart declares a version range, such as `~0.18.0`, has nothing to
bump: the range resolves to the newest matching release when the chart is built,
and the bot leaves it alone. See
[Scheduled patch releases](#scheduled-patch-releases) for how a newer version
there starts a release.

Each pull request is labeled `dependencies` and merges itself once its checks
pass. Only the bot's own commit merges this way, and only when it changes
nothing but the version pins and their changelog fragment. Anything pushed on
top of it waits for a maintainer, and the bot stops updating that pull request
until it is merged or closed.

Merging a bump does not release anything; it is included in the next release of
that branch. A branch that is already current gets no pull request, and an open
one is updated in place when a newer version appears.

Run the workflow from the Actions tab to bump sooner, with **dry-run** to see
what it would change. The same bump can be made by hand:

```bash
make bump-kai-scheduler KAI_VERSION=v0.18.2        # a release: chart only
make bump-kai-scheduler KAI_VERSION=0.0.0-1db3d56  # a main build: chart only
```

On a release branch, also add the changelog fragment:
`make changelog KIND=Changed BODY="Bundle KAI Scheduler v0.18.2"`.

## Reading the Actions tab

**Release — Tag & Publish** appears for every pull request closed against `main`
or a `v*.*` branch, and is skipped unless the pull request was merged and came
from a `release/prepare-*` branch. GitHub can only filter this trigger on the
target branch, so the remaining condition is evaluated in the job itself. A
skipped entry is expected and consumes no runner time.

**Release — Scheduled Patch Releases** has one entry per `v*.*` branch. Most
runs end within seconds with a message that the branch is up to date or pins an
exact version.
