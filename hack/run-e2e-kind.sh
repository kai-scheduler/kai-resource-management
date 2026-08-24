#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

CLUSTER_NAME=${CLUSTER_NAME:-krm-e2e}

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..

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
      echo "  --skip-build: Load the images already built instead of rebuilding"
      echo "  --preserve-cluster: Keep the kind cluster after running the test suites"
      exit 0
      ;;
    *)
      echo "Unknown option $1"
      echo "Use --help for usage information"
      exit 1
      ;;
  esac
done

# Build setup script arguments
SETUP_ARGS=""
if [ "$SKIP_BUILD" = "true" ]; then
    SETUP_ARGS="$SETUP_ARGS --skip-build"
fi

# Run the cluster setup script
CLUSTER_NAME=$CLUSTER_NAME ${REPO_ROOT}/hack/setup-e2e-cluster.sh $SETUP_ARGS

make -C ${REPO_ROOT} test-e2e
SUITES_STATUS=$?

if [ "$PRESERVE_CLUSTER" != "true" ]; then
    kind delete cluster --name $CLUSTER_NAME
fi

exit $SUITES_STATUS
