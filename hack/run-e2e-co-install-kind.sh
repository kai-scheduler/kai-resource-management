#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# Builds a cluster with no chart on it and runs the co-install suite, which installs
# KAI Scheduler on its own and this tree's chart on top of it. Mirrors the
# e2e-co-install-tests job. Sets -e, unlike run-e2e-kind.sh.

set -e

CLUSTER_NAME=${CLUSTER_NAME:-krm-e2e}

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
VERSION=${VERSION:-"0.0.0"}
KAI_CHART=${KAI_CHART:-"oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler"}

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
      echo "  KAI_VERSION: KAI Scheduler chart version to install; defaults to the chart's pin"
      echo "  KAI_CHART: Registry to pull KAI Scheduler from"
      echo "  VERSION: Tag of the images and chart under test (default 0.0.0)"
      echo "  CLUSTER_NAME: Kind cluster name (default krm-e2e)"
      exit 0
      ;;
    *)
      echo "Unknown option $1"
      echo "Use --help for usage information"
      exit 1
      ;;
  esac
done

cd "$REPO_ROOT"

if [ -z "$KAI_VERSION" ]; then
  KAI_VERSION=$(hack/bump-kai-scheduler.sh pinned)
  KAI_VERSION=${KAI_VERSION#chart=}
fi

# The cluster only: the suite installs KAI Scheduler and this chart itself.
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

echo "Packaging the chart under test..."
CHART_DIR=$(mktemp -d)
helm dependency build ./deployments/kai-resource-management-chart >/dev/null
helm package ./deployments/kai-resource-management-chart -d "$CHART_DIR" \
  --app-version "$VERSION" --version "$VERSION" >/dev/null

# Absolute: ginkgo runs each suite from its own package directory.
export CO_INSTALL_CHART_PATH="${CHART_DIR}/kai-resource-management-${VERSION}.tgz"
export CO_INSTALL_VALUES_FILE="${REPO_ROOT}/hack/e2e-values.yaml"
export CO_INSTALL_IMAGE_TAG="$VERSION"
export CO_INSTALL_KAI_CHART="$KAI_CHART"
export CO_INSTALL_KAI_VERSION="$KAI_VERSION"

set +e
make -C ${REPO_ROOT} test-e2e-co-install
SUITES_STATUS=$?
set -e

if [ "$PRESERVE_CLUSTER" != "true" ]; then
    kind delete cluster --name $CLUSTER_NAME
fi

exit $SUITES_STATUS
