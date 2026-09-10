#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# Restate the copyright holder in copied CRD manifests using this repository's form.
#
# The CRDs are generated output, copied verbatim from the pinned API module, and that
# module still names the holder as "NVIDIA CORPORATION". Every file published from this
# repository must use the reviewed entity instead, so the copy is normalised on the way
# in and sync-crds-check normalises the module's files the same way before diffing. The
# invariant it enforces is therefore "identical to the pinned module apart from this
# line", not "byte-identical".
#
# Rewriting only the holder is deliberate: the licence itself is unchanged, and the
# manifests remain the API module's work. This becomes a no-op once that module adopts
# the same form, so it is safe to leave in place.
#
# Usage: hack/normalize-crd-header.sh <directory>
set -euo pipefail

DIR="${1:?usage: hack/normalize-crd-header.sh <directory>}"

OLD='^# Copyright ([0-9]{4}) NVIDIA CORPORATION$'
NEW='# Copyright \1 NVIDIA CORPORATION \& AFFILIATES. All rights reserved.'

shopt -s nullglob
files=("${DIR}"/*.yaml)
if [[ ${#files[@]} -eq 0 ]]; then
  echo "::error::no CRD manifests found in ${DIR}" >&2
  exit 1
fi

for file in "${files[@]}"; do
  # -i '' is BSD sed; GNU sed wants -i with no argument. Write to a temp file so the
  # script behaves the same on a developer Mac and in CI.
  sed -E "s|${OLD}|${NEW}|" "${file}" >"${file}.tmp"
  mv "${file}.tmp" "${file}"
done
