#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# This script sets up a kind cluster for e2e testing with kai-resource-management.
# It can be run independently or called from run-e2e-kind.sh.

set -e

CLUSTER_NAME=${CLUSTER_NAME:-krm-e2e}

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
KIND_CONFIG=${KIND_CONFIG:-"${REPO_ROOT}/hack/kind-config.yaml"}
NAMESPACE=${NAMESPACE:-"kai-resource-management"}
VERSION=${VERSION:-"0.0.0"}

: ${KIND_K8S_TAG:="v1.34.0"}
: ${KIND_IMAGE:="kindest/node:${KIND_K8S_TAG}"}
: ${SERVICE_MONITOR_CRD:="https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.88.0/example/prometheus-operator-crd/monitoring.coreos.com_servicemonitors.yaml"}

CHART_DIR=""

cleanup() {
  if [[ -n "$CHART_DIR" ]]; then
    rm -rf "$CHART_DIR"
  fi
}

trap cleanup EXIT

# Parse named parameters
SKIP_BUILD=${SKIP_BUILD:-"false"}
SKIP_KRM_INSTALL=${SKIP_KRM_INSTALL:-"false"}

while [[ $# -gt 0 ]]; do
  case $1 in
    --skip-build)
      SKIP_BUILD="true"
      shift
      ;;
    --skip-krm-install)
      SKIP_KRM_INSTALL="true"
      shift
      ;;
    --kind-config)
      KIND_CONFIG="$2"
      shift 2
      ;;
    -h|--help)
      echo "Usage: $0 [--skip-build] [--skip-krm-install] [--kind-config <path>]"
      echo "  --skip-build: Load the images already tagged $VERSION instead of rebuilding"
      echo "  --skip-krm-install: Prepare the cluster without installing the chart"
      echo "  --kind-config: Kind config to use instead of hack/kind-config.yaml"
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

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
  echo "Deleting the existing $CLUSTER_NAME cluster..."
  kind delete cluster --name "$CLUSTER_NAME"
fi

echo "Creating kind cluster..."
kind create cluster --name "$CLUSTER_NAME" --image "$KIND_IMAGE" --config "$KIND_CONFIG"

# The chart renders ServiceMonitors, so the type must exist. The operator is not needed.
echo "Installing the ServiceMonitor CRD..."
kubectl apply --server-side -f "$SERVICE_MONITOR_CRD"

if [ "$SKIP_KRM_INSTALL" = "true" ]; then
  echo "Skipping the kai-resource-management install."
  exit 0
fi

if [ "$SKIP_BUILD" != "true" ]; then
  echo "Building images..."
  make build VERSION="$VERSION"
fi

echo "Loading images into kind..."
IMAGES=$(docker images --format '{{.Repository}}:{{.Tag}}' | grep ":${VERSION}\$" || true)
if [ -z "$IMAGES" ]; then
  echo "No images tagged $VERSION. Run without --skip-build." >&2
  exit 1
fi
for image in $IMAGES; do
  kind load docker-image --name "$CLUSTER_NAME" "$image"
done

echo "Installing kai-resource-management..."
CHART_DIR=$(mktemp -d)
helm dependency build ./deployments/kai-resource-management-chart >/dev/null
helm package ./deployments/kai-resource-management-chart -d "$CHART_DIR" >/dev/null
helm upgrade -i krm "${CHART_DIR}/kai-resource-management-${VERSION}.tgz" \
  -n "$NAMESPACE" --create-namespace \
  --values ./hack/e2e-values.yaml \
  --wait --timeout 10m
