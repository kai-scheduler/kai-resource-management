#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# Pin the bundled KAI Scheduler, or work out what it should be pinned to.
#
# Usage:
#   hack/bump-kai-scheduler.sh set <vX.Y.Z | 0.0.0-<sha>>
#   hack/bump-kai-scheduler.sh set-chart <vX.Y.Z | 0.0.0-<sha>>
#   hack/bump-kai-scheduler.sh set-go <vX.Y.Z>
#   hack/bump-kai-scheduler.sh plan [vX.Y]
#   hack/bump-kai-scheduler.sh pinned
#
# `set` pins the chart dependency, and the Go module too for a release; a main
# build has no module version to pin. `plan` prints chart=<version> and
# go=<version> for the pins the checkout is behind on, each empty when it is up
# to date; `pinned` prints the current pins the same way. Without a line, plan
# follows KAI's main builds for the chart and its newest release for the module;
# with one, the newest release of that line for both.
set -euo pipefail

KAI_REPO=kai-scheduler/KAI-Scheduler
KAI_CHART=oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler
KAI_MODULE=github.com/kai-scheduler/KAI-scheduler
CHART_FILE=deployments/kai-resource-management-chart/Chart.yaml
GO="${GO:-go}"

usage() {
  echo "usage: $0 set|set-chart|set-go <version> | plan [vX.Y] | pinned" >&2
  exit 1
}

is_release() { [[ "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; }
is_main_build() { [[ "$1" =~ ^0\.0\.0-[0-9a-f]{7,}$ ]]; }

# True when $1 is a strictly higher release than $2.
is_newer() {
  [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "${1#v}" "${2#v}" | sort -V | tail -n1)" = "${1#v}" ]
}

pinned_chart() {
  awk '/^ *- name: kai-scheduler$/ {found = 1; next}
       found && /^ *version:/ {gsub(/["'\'' ]/, "", $2); print $2; exit}' "$CHART_FILE"
}

pinned_go() {
  awk -v module="$KAI_MODULE" '$1 == module {print $2; exit}' go.mod
}

# A tag or commit exists before its chart does: KAI publishes it from a
# workflow that takes several minutes after the push.
is_chart_published() {
  helm show chart "$KAI_CHART" --version "$1" >/dev/null 2>&1
}

latest_main_build() {
  local sha version
  # KAI's CI checks out a single commit, where `git rev-parse --short` always
  # yields the 7-character minimum.
  for sha in $(gh api "repos/$KAI_REPO/commits?sha=main&per_page=20" --jq '.[].sha'); do
    version="0.0.0-${sha:0:7}"
    if is_chart_published "$version"; then
      echo "$version"
      return
    fi
  done
  echo "No published chart among the last 20 KAI Scheduler main commits" >&2
  exit 1
}

# Releases rather than tags: a tag that was retracted never got a release.
latest_release() {
  local line="$1" pattern tag
  if [ -n "$line" ]; then
    pattern="^${line//./\\.}\\.[0-9]+$"
  else
    pattern='^v[0-9]+\.[0-9]+\.[0-9]+$'
  fi
  for tag in $(gh release list --repo "$KAI_REPO" --exclude-drafts --exclude-pre-releases \
      --limit 200 --json tagName --jq '.[].tagName' | { grep -E "$pattern" || true; } | sort -rV); do
    if is_chart_published "$tag"; then
      echo "$tag"
      return
    fi
  done
}

set_chart() {
  local version="$1"
  is_release "$version" || is_main_build "$version" || {
    echo "Not a KAI Scheduler release or main build: $version" >&2
    exit 1
  }
  awk -v version="$version" '
    /^ *- name: kai-scheduler$/ {found = 1}
    found && /^ *version:/ {sub(/version:.*/, "version: \"" version "\""); found = 0}
    {print}' "$CHART_FILE" >"$CHART_FILE.tmp"
  mv "$CHART_FILE.tmp" "$CHART_FILE"
}

set_go() {
  local version="$1"
  is_release "$version" || {
    echo "Not a KAI Scheduler release: $version" >&2
    exit 1
  }
  "$GO" get "$KAI_MODULE@$version"
  "$GO" mod tidy
  python3 hack/gen-notice.py
}

plan() {
  local line="${1:-}" chart_pin go_pin release chart="" go=""
  chart_pin="$(pinned_chart)"
  go_pin="$(pinned_go)"

  if [ -z "$line" ]; then
    local build
    build="$(latest_main_build)"
    [ "$build" = "$chart_pin" ] || chart="$build"
    release="$(latest_release "")"
  else
    [[ "$line" =~ ^v[0-9]+\.[0-9]+$ ]] || {
      echo "Not a release line: $line" >&2
      exit 1
    }
    release="$(latest_release "$line")"
    if [ -n "$release" ] && is_newer "$release" "$chart_pin"; then
      chart="$release"
    fi
  fi

  if [ -n "$release" ] && is_newer "$release" "$go_pin"; then
    go="$release"
  fi
  echo "chart=$chart"
  echo "go=$go"
}

[ $# -ge 1 ] || usage
command="$1"
shift
case "$command" in
  set)
    [ $# -eq 1 ] || usage
    set_chart "$1"
    if is_release "$1"; then
      set_go "$1"
    fi
    ;;
  set-chart)
    [ $# -eq 1 ] || usage
    set_chart "$1"
    ;;
  set-go)
    [ $# -eq 1 ] || usage
    set_go "$1"
    ;;
  plan)
    [ $# -le 1 ] || usage
    plan "${1:-}"
    ;;
  pinned)
    [ $# -eq 0 ] || usage
    echo "chart=$(pinned_chart)"
    echo "go=$(pinned_go)"
    ;;
  *)
    usage
    ;;
esac
