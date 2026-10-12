#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# Report which sub-chart versions a build of the chart contains, as
# `<chart>: <version>` lines without a leading v.
#
# Usage:
#   hack/subchart-versions.sh locked           # versions in the chart's Chart.lock
#   hack/subchart-versions.sh resolve          # re-resolve the ranges in Chart.yaml, then as `locked`
#   hack/subchart-versions.sh previous <tag>   # versions bundled in an earlier release
#
# `locked` reads the lock `helm dependency build` leaves behind. The version
# inside a downloaded sub-chart still carries the v of its release tag, so it is
# not used. `previous` reads the subcharts.yaml a release publishes, and for a
# release that predates it, the sub-charts bundled in its published chart.
set -euo pipefail

KRM_REPO=kai-scheduler/kai-resource-management
CHART_OCI=oci://ghcr.io/kai-scheduler/kai-resource-management/kai-resource-management
CHART_DIR="${CHART_DIR:-deployments/kai-resource-management-chart}"
ASSET=subcharts.yaml

usage() {
  echo "usage: $0 locked | resolve | previous <tag>" >&2
  exit 1
}

locked() {
  local lock="$CHART_DIR/Chart.lock"
  [ -f "$lock" ] || {
    echo "No $lock; run helm dependency build first." >&2
    exit 1
  }
  awk '
    /^dependencies:/ {inside = 1; next}
    /^[a-z]/ {inside = 0}
    inside && /^- name:/ {name = $3}
    inside && /^  version:/ {
      version = $2
      gsub(/["'\'']/, "", version)
      sub(/^v/, "", version)
      print name ": " version
    }' "$lock" | sort
}

resolve() {
  helm dependency update "$CHART_DIR" >/dev/null
  locked
}

# A release published before subcharts.yaml existed has no asset, but its chart
# carries the sub-charts it was built with.
chart_version() {
  awk '
    /^name:/ {name = $2}
    /^version:/ {version = $2}
    END {
      gsub(/["'\'']/, "", version)
      sub(/^v/, "", version)
      print name ": " version
    }'
}

bundled() (
  local tag="$1" tmp entry
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  helm pull "$CHART_OCI" --version "$tag" --destination "$tmp" >/dev/null
  tar -xzf "$tmp"/*.tgz -C "$tmp"
  for entry in "$tmp"/kai-resource-management/charts/*/; do
    chart_version <"${entry}Chart.yaml"
  done | sort
)

previous() {
  local tag="$1" assets
  assets="$(gh release view "$tag" --repo "$KRM_REPO" --json assets --jq '.assets[].name')"
  if grep -qx "$ASSET" <<<"$assets"; then
    gh release download "$tag" --repo "$KRM_REPO" --pattern "$ASSET" --output -
  else
    bundled "$tag"
  fi
}

[ $# -ge 1 ] || usage
case "$1" in
  locked) locked ;;
  resolve) resolve ;;
  previous)
    [ $# -eq 2 ] || usage
    previous "$2"
    ;;
  *) usage ;;
esac
