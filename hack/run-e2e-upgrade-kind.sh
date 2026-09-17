#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# Installs the newest published release then upgrades to a chart built from this
# tree, mirroring the e2e-upgrade-tests job. Sets -e, unlike run-e2e-kind.sh.

set -e

CLUSTER_NAME=${CLUSTER_NAME:-krm-e2e}

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
NAMESPACE=${NAMESPACE:-"kai-resource-management"}
VERSION=${VERSION:-"0.0.0"}

# Overridable: a scratch registry is the only way to exercise this before a release.
CHART_OCI=${CHART_OCI:-"oci://ghcr.io/kai-scheduler/kai-resource-management/kai-resource-management"}
RELEASES_API=${RELEASES_API:-"https://api.github.com/repos/kai-scheduler/kai-resource-management/releases?per_page=100"}

CHART_DIR=""

cleanup() {
  if [ -n "$CHART_DIR" ]; then
    rm -rf "$CHART_DIR"
  fi
}

trap cleanup EXIT

# Parse named parameters
SKIP_BUILD="false"
PRESERVE_CLUSTER="false"

while [[ $# -gt 0 ]]; do
  case $1 in
    --skip-build)
      SKIP_BUILD="true"
      shift
      ;;
    --preserve-cluster)
      PRESERVE_CLUSTER="true"
      shift
      ;;
    -h|--help)
      echo "Usage: $0 [--skip-build] [--preserve-cluster]"
      echo "  --skip-build: Load the images already tagged \$VERSION instead of rebuilding"
      echo "  --preserve-cluster: Keep the kind cluster after running the suite"
      echo ""
      echo "Environment variables:"
      echo "  UPGRADE_FROM_VERSION: Release to upgrade from; resolved from GitHub when unset"
      echo "  VERSION: Tag of the images and chart to upgrade to (default 0.0.0)"
      echo "  CLUSTER_NAME: Kind cluster name (default krm-e2e)"
      echo "  CHART_OCI: Registry to pull the release to upgrade from"
      exit 0
      ;;
    *)
      echo "Unknown option $1"
      echo "Use --help for usage information"
      exit 1
      ;;
  esac
done

# Echoes the newest release whose chart is published, or nothing when there is no
# release to upgrade from; charts lag their release, so candidates are probed.
# Returns non-zero only when the lookup itself failed, which is not a skip. The
# failures are reported explicitly because set -e does not reach into the command
# substitution the caller invokes this from.
resolve_upgrade_from_version() {
  local branch major minor pattern response tags candidates candidate error

  branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "main")

  if [[ "$branch" =~ v([0-9]+)\.([0-9]+) ]]; then
    major="${BASH_REMATCH[1]}"
    minor="${BASH_REMATCH[2]}"
    if [ "$minor" -eq 0 ]; then
      return 0
    fi
    pattern="^v${major}\.$((minor - 1))\.[0-9]+$"
  else
    pattern='^v[0-9]+\.[0-9]+\.[0-9]+$'
  fi

  # One pipeline would make a failed request or a jq error indistinguishable from
  # "no matching release", so each stage that can fail is checked on its own.
  if ! response=$(curl -sf "$RELEASES_API"); then
    echo "Failed to list releases from $RELEASES_API." >&2
    return 1
  fi
  if ! tags=$(printf '%s\n' "$response" | jq -r '.[].tag_name'); then
    echo "Failed to read release tags from $RELEASES_API." >&2
    return 1
  fi
  candidates=$(printf '%s\n' "$tags" | grep -E "$pattern" | sort -rV || true)
  if [ -z "$candidates" ]; then
    return 0
  fi

  while IFS= read -r candidate; do
    if error=$(helm show chart "$CHART_OCI" --version "$candidate" 2>&1 >/dev/null); then
      echo "$candidate"
      return 0
    fi
    if [[ "$error" != *"not found"* ]]; then
      echo "Querying the chart for $candidate failed: $error" >&2
      return 1
    fi
  done <<< "$candidates"

  return 0
}

if [ -z "$UPGRADE_FROM_VERSION" ]; then
  echo "Resolving the version to upgrade from..."
  UPGRADE_FROM_VERSION=$(resolve_upgrade_from_version) || exit 1
fi

if [ -z "$UPGRADE_FROM_VERSION" ]; then
  echo "No published release has a chart to upgrade from. Skipping the upgrade tests."
  exit 0
fi

echo "Upgrading from $UPGRADE_FROM_VERSION to $VERSION"

cd "$REPO_ROOT"

# The cluster only: what gets installed below is the previous release, not this tree.
CLUSTER_NAME=$CLUSTER_NAME ${REPO_ROOT}/hack/setup-e2e-cluster.sh --skip-krm-install

if [ "$SKIP_BUILD" != "true" ]; then
  echo "Building images..."
  make build VERSION="$VERSION"
fi

# No registry the cluster can reach, so the images must be on the nodes first.
echo "Loading images into kind..."
IMAGES=$(docker images --format '{{.Repository}}:{{.Tag}}' | grep ":${VERSION}\$" || true)
if [ -z "$IMAGES" ]; then
  echo "No images tagged $VERSION. Run without --skip-build." >&2
  exit 1
fi
for image in $IMAGES; do
  kind load docker-image --name "$CLUSTER_NAME" "$image"
done

# No --set image.tag: a published chart's appVersion is the release it shipped as.
echo "Installing kai-resource-management $UPGRADE_FROM_VERSION..."
helm install krm "$CHART_OCI" --version "$UPGRADE_FROM_VERSION" \
  -n "$NAMESPACE" --create-namespace \
  --values ./hack/e2e-values.yaml \
  --wait --timeout 10m

echo "Packaging the chart to upgrade to..."
CHART_DIR=$(mktemp -d)
helm dependency build ./deployments/kai-resource-management-chart >/dev/null
helm package ./deployments/kai-resource-management-chart -d "$CHART_DIR" \
  --app-version "$VERSION" --version "$VERSION" >/dev/null

# Absolute: ginkgo runs each suite from its own package directory.
export UPGRADE_CHART_PATH="${CHART_DIR}/kai-resource-management-${VERSION}.tgz"
export UPGRADE_VALUES_FILE="${REPO_ROOT}/hack/e2e-values.yaml"
export UPGRADE_IMAGE_TAG="$VERSION"

set +e
make -C ${REPO_ROOT} test-e2e-upgrade
SUITES_STATUS=$?
set -e

if [ "$PRESERVE_CLUSTER" != "true" ]; then
    kind delete cluster --name $CLUSTER_NAME
fi

exit $SUITES_STATUS
