# FIPS 140-3

FIPS 140-3 is the US federal standard for cryptographic modules. Regulated
deployments are required to run software whose cryptography is performed by a
module validated under NIST's Cryptographic Module Validation Program.

KAI Resource Management supports this through the Go toolchain's native FIPS
140-3 support, which has two independent halves. Both must be right, and an
install missing either one still comes up healthy — which is why the chart
refuses ambiguous configurations rather than rendering them.

| Half | Mechanism | Chosen by |
| --- | --- | --- |
| Build | `GOFIPS140=v1.0.0` embeds the validated [Go Cryptographic Module](https://go.dev/doc/security/fips140#the-go-cryptographic-module) and routes `crypto/*` through it | the `-fips` image tag |
| Run time | `GODEBUG=fips140=<mode>` selects how strictly that module is used | `global.fipsMode` |

A binary built without `GOFIPS140` can never be compliant, whatever `GODEBUG`
says. The images are otherwise identical: same base image, same sources, same
version.

## Modes

`global.fipsMode` takes one of three values, and drives both halves at once.

| Mode | Images | Behaviour |
| --- | --- | --- |
| `off` (default) | regular | Ordinary crypto paths. Not compliant. |
| `on` | `-fips` | Approved algorithms are served by the validated module, which runs its mandated self-tests at startup. Non-approved algorithms still work, outside the validated boundary. |
| `only` | `-fips` | As `on`, and any use of a non-approved algorithm returns an error or panics. |

Note that `on` is what a FIPS-built binary already does by default, so the
practical reasons to set the mode explicitly are to reach `only`, or to run a
`-fips` image with FIPS switched back off.

Choose `only` when compliance has to be *demonstrated*: nothing outside the
validated boundary is reachable, so a dependency that quietly reaches for a
non-approved algorithm fails loudly instead of doing it silently. Accept the
trade — that failure is a run-time one, so a code path exercised rarely can take
a controller down long after install. `on` is the safer default for most
deployments.

## Installing

```sh
helm upgrade --install krm oci://ghcr.io/kai-scheduler/kai-resource-management/kai-resource-management \
  -n kai-resource-management --create-namespace \
  --set global.fipsMode=on \
  --set kai-scheduler.global.fips=true
```

The mode appends `-fips` to every resolved image tag — whether that tag comes
from a per-component `<component>.image.tag`, from `image.tag`, or from the
chart version — so FIPS selection is orthogonal to version pinning.

An unrecognised value fails the render rather than installing without FIPS.
A bool is accepted for the common case: `--set global.fipsMode=true` means `on`.

### Why the second flag

`kai-scheduler.global.fips` is a temporary duplicate. The bundled KAI Scheduler
release predates `fipsMode` and reads a boolean `global.fips`; Helm shares
`global.*` into subcharts verbatim rather than deriving one key from another, so
it cannot be inferred. Setting only one of the two would put KRM on FIPS images
and the scheduler on ordinary ones, so the chart refuses to render until both
agree. Both the duplicate and the check disappear when the pinned scheduler
understands `fipsMode`.

## Coverage and limits

Read this before treating an install as compliant.

- **`GODEBUG` reaches KRM's own services only** — `krm-operator`,
  `nodepool-controller`, `pod-group-assigner`, and `project-controller` via the
  operator. The bundled KAI Scheduler components get FIPS *images* but no
  `GODEBUG`, because KAI Scheduler implements no run-time half. They therefore
  run at the build default of `on` and cannot be put into `only`. An install at
  `fipsMode=only` is strict for KRM and not for the scheduler.
- **The `helm-hooks` image is tagged `-fips` but contains no FIPS-built binary.**
  It is Alpine plus an upstream `kubectl`, used by the CRD upgrader and the
  KRMConfig deployer/cleanup Jobs. It takes the suffix only to keep one tag
  scheme across the release; treat the tag as a version marker there, not as a
  claim about its contents. Those Jobs deliberately carry no `GODEBUG`.
- **FIPS is about the cryptographic module, not about the workloads KRM
  schedules.** It says nothing about the containers users run.

## Building locally

```sh
make build FIPS=1
```

This compiles every service with `GOFIPS140=v1.0.0` and tags the images
`<version>-fips`. Nothing else changes: the module ships inside the Go
toolchain and is pure Go, so neither the builder image nor the runtime base
image needs a FIPS variant, and both `linux/amd64` and `linux/arm64` are built
as usual.

Confirm a binary really is FIPS-enabled by reading its build info:

```sh
go version -m bin/krm-operator-amd64 | grep GOFIPS140
```

The toolchain stamps a resolved version with a content hash, so expect
`GOFIPS140=v1.0.0-<hash>` rather than a bare `v1.0.0`.

Releases publish these images from the `build-and-push-fips` job, which runs the
same command and applies the check above to every binary it produced.

Each release also carries a `fips` [image lock](how-to/install-in-an-air-gapped-cluster.md)
alongside the `standard` one. Mirror that file, not the standard one, when
installing the FIPS variant into an air-gapped cluster.
