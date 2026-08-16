#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

# Verify every ServiceAccount this chart creates is granted the OpenShift SCC.
#
# The containers pin runAsUser 10000, which restricted-v2 rejects, so a ServiceAccount
# missing from the SCC means its pods never start on OpenShift. Hook ServiceAccounts
# are the ones to forget: they are declared in a different file from the SCC and
# nothing else references them, so nothing else notices.
#
# A chart unit test cannot express this. helm-unittest asserts against a declared list
# of templates and has no way to enumerate every rendered object and cross-check one
# against another, so it can catch an edit to the SCC but not a ServiceAccount added
# somewhere else.
#
# Usage: hack/scc-check.sh [chart-directory]
set -euo pipefail

CHART_DIR="${1:-deployments/kai-resource-management-chart}"
SCC_TEMPLATE="${CHART_DIR}/templates/rbac/scc.yaml"

# rbac.create gates the SCC itself, so force it on: this checks SCC coverage, not
# whether an installation happens to create RBAC.
rendered="$(helm template scc-check "${CHART_DIR}" --set openshift=true --set rbac.create=true)"

# Only this chart's own ServiceAccounts: the kai-scheduler subchart grants its own
# through its own SCC, and its objects are rendered from charts/ rather than templates/.
accounts="$(
  awk '/^# Source: /{ mine = ($0 ~ /^# Source: kai-resource-management\/templates\//) }
       mine && /^kind: ServiceAccount$/ { found = 1 }
       mine && found && /^  name: / { print $2; found = 0 }' <<<"${rendered}" | sort -u
)"

granted="$(
  awk '/^# Source: /{ mine = ($0 ~ /scc\.yaml$/) } mine' <<<"${rendered}" |
    grep -oE 'system:serviceaccount:[^:]+:[a-z0-9-]+' | sed 's/.*://' | sort -u || true
)"

status=0
while read -r account; do
  [[ -n "${account}" ]] || continue
  if ! grep -qx "${account}" <<<"${granted}"; then
    echo "::error::ServiceAccount ${account} is not granted the SCC in ${SCC_TEMPLATE}"
    status=1
  fi
done <<<"${accounts}"

exit "${status}"
