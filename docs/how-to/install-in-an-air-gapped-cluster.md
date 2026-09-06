# Install in an air-gapped cluster

**Goal:** install KAI Resource Management on a cluster with no route to the public
internet, pulling every image from your own registry and knowing that what you mirrored
is exactly what the release was built from.

**You need:** a private container registry your cluster can pull from, a host that can
reach both that registry and `ghcr.io`, Helm 3, and a mirroring tool — the examples use
[`skopeo`](https://github.com/containers/skopeo), but `crane` or `regctl` work the same
way. `yq` is used to read the lock file.

## What a release gives you

Each release attaches an **image lock** to its GitHub Release: the images built from this
repository, each one pinned to the digest its tag pointed at when the release was
published. Mirroring by digest is what makes the copy reproducible — a tag can be moved,
a digest cannot.

The lock covers this repository's five images and no more. KAI Scheduler ships as a
subchart and publishes its own images, so mirroring it is a second step —
[KAI Scheduler's images](#kai-schedulers-images) below. Do both, or the install comes up
half mirrored.

There is one lock per profile and architecture, because they hold different digests:

```text
imagelock-kai-resource-management-vX.Y.Z-standard-linux-amd64.yaml
imagelock-kai-resource-management-vX.Y.Z-standard-linux-arm64.yaml
imagelock-kai-resource-management-vX.Y.Z-fips-linux-amd64.yaml
imagelock-kai-resource-management-vX.Y.Z-fips-linux-arm64.yaml
```

Pick `standard` unless you are installing the FIPS variant — see
[FIPS 140-3](../fips.md) — and pick the architecture of the nodes the workloads will run
on. If your cluster mixes architectures, mirror both files.

Each entry looks like this:

```yaml
- name: krm-operator
  image: ghcr.io/kai-scheduler/kai-resource-management/krm-operator@sha256:89e1aab8…
  source: ghcr.io/kai-scheduler/kai-resource-management/krm-operator:vX.Y.Z
  indexDigest: sha256:c6971f83…
```

`image` is what to copy. `source` is the tag the chart asks for: the chart references
images by tag and has no digest field, so the mirrored digest has to end up published
under that tag in your registry. `indexDigest` names the multi-arch index the platform
manifest came from, and is the same in both architecture files — that is how you can
confirm two files describe one release.

Three of the five appear in no template. The krm-operator creates the
nodepool-controller, project-controller and pod-group-assigner Deployments from the
KRMConfig, which travels as ConfigMap text — so a lock built by reading pod specs would
miss them. These are in the lock.

## 1. Fetch the release assets

On the host with internet access:

```bash
export VERSION=vX.Y.Z
export ARCH=amd64        # or arm64
export PROFILE=standard  # or fips

gh release download "$VERSION" --repo kai-scheduler/kai-resource-management \
  --pattern "kai-resource-management-$VERSION.tgz" \
  --pattern "imagelock-kai-resource-management-$VERSION-$PROFILE-linux-$ARCH.yaml"
```

Without the GitHub CLI, the same two files are plain downloads:

```bash
BASE="https://github.com/kai-scheduler/kai-resource-management/releases/download/$VERSION"
curl -LO "$BASE/kai-resource-management-$VERSION.tgz"
curl -LO "$BASE/imagelock-kai-resource-management-$VERSION-$PROFILE-linux-$ARCH.yaml"
```

## 2. Mirror this repository's images

Copy each digest into your registry under the tag the chart will ask for. Keeping the
source path means the chart needs one override per registry rather than one per image:

```bash
set -euo pipefail
export INTERNAL_REGISTRY=registry.internal.example

LOCK="imagelock-kai-resource-management-$VERSION-$PROFILE-linux-$ARCH.yaml"
yq -r '.spec.images[] | .image + " " + .source' "$LOCK" | while read -r digest_ref tag_ref; do
  skopeo copy "docker://$digest_ref" "docker://$INTERNAL_REGISTRY/$tag_ref"
done
```

`ghcr.io/kai-scheduler/kai-resource-management/krm-operator:vX.Y.Z` becomes
`$INTERNAL_REGISTRY/ghcr.io/kai-scheduler/kai-resource-management/krm-operator:vX.Y.Z`.
If your registry cannot hold paths that deep, flatten them however it requires — you will
just have more `--set` overrides to write in step 4.

Copy the chart archive and the lock file across to the air-gapped side as well. Keep the
lock: it is the record of what this installation is running.

## 3. Mirror KAI Scheduler's images

The chart bundles KAI Scheduler as a subchart, and it is not optional — installing this
chart installs it. Its images are built and published by the
[KAI Scheduler](https://github.com/kai-scheduler/KAI-Scheduler) project, which pins them
itself, so they are not restated in this repository's lock.

Read the version this release bundles out of the chart you just downloaded:

```bash
helm show chart ./kai-resource-management-$VERSION.tgz | grep -A2 'name: kai-scheduler'
```

Mirror that version's images from the KAI Scheduler project, under the same
"copy the digest, publish it under the source tag" rule as above. They live under
`ghcr.io/kai-scheduler/kai-scheduler/`, and there are more of them than you might expect:
its operator creates most of its workloads from a Config object rather than from a
template, so reading its pod specs is not enough. Follow that project's own air-gap
instructions for the authoritative list.

For the FIPS profile, mirror its `-fips` tags — KAI Scheduler applies the same suffix
this chart does.

## 4. Install from the private registry

KAI Scheduler is bundled as a subchart and keeps its own registry value, so there are two
overrides — one for this chart's images, one for the subchart's:

```bash
helm upgrade --install krm ./kai-resource-management-$VERSION.tgz \
  --namespace kai-resource-management --create-namespace \
  --set image.registry=$INTERNAL_REGISTRY/ghcr.io/kai-scheduler/kai-resource-management \
  --set kai-scheduler.global.registry=$INTERNAL_REGISTRY/ghcr.io/kai-scheduler/kai-scheduler \
  --wait --timeout 10m
```

If your registry needs credentials, create the pull secret in the install namespace and
name it for every pod the chart and the operators create:

```bash
kubectl create namespace kai-resource-management
kubectl create secret docker-registry internal-registry \
  --namespace kai-resource-management \
  --docker-server="$INTERNAL_REGISTRY" \
  --docker-username=… --docker-password=…
```

```bash
helm upgrade --install krm ./kai-resource-management-$VERSION.tgz \
  --namespace kai-resource-management --create-namespace \
  --set image.registry=$INTERNAL_REGISTRY/ghcr.io/kai-scheduler/kai-resource-management \
  --set kai-scheduler.global.registry=$INTERNAL_REGISTRY/ghcr.io/kai-scheduler/kai-scheduler \
  --set global.imagePullSecrets[0].name=internal-registry \
  --wait --timeout 10m
```

For the FIPS profile add `--set global.fipsMode=on --set kai-scheduler.global.fips=true`,
and mirror the `fips` lock rather than the `standard` one. The two switches must agree:
the chart and the subchart spell the same thing differently, and setting only one leaves
half the install on non-FIPS images.

Two more things the chart does that an offline cluster can trip on, neither specific to
air-gap but both easy to hit here:

- The chart renders `ServiceMonitor` objects. Without the Prometheus Operator's CRD they
  fail to apply; install with `--set serviceMonitor.create=false` if you have no
  Prometheus.
- OpenShift is normally auto-detected with a cluster lookup. Under `helm template` or a
  GitOps tool that renders offline, set `--set openshift=true` explicitly.

## 5. Verify

```bash
kubectl get pods -n kai-resource-management
```

Every pod should reach `Running`, and every image reference should name your registry:

```bash
kubectl get pods -n kai-resource-management \
  -o jsonpath='{range .items[*].spec.containers[*]}{.image}{"\n"}{end}' | sort -u
```

Anything still pointing at `ghcr.io` is an image the overrides missed, and it will stay
in `ImagePullBackOff` until it is mirrored — a KAI Scheduler image here means step 3 was
skipped or incomplete. From here, continue with the
[quickstart](../getting-started/quickstart.md) from its step 3.

## Upgrading

Each version has its own lock, and the digests change between versions even when a tag
does not. Repeat steps 1 to 3 for the new version before upgrading; the mirroring command
is idempotent, so re-running it over an already-mirrored image is cheap.

Check the bundled KAI Scheduler version each time. A release of this chart can move it,
and step 3 then has a different set of images to mirror.
