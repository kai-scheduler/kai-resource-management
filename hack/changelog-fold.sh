#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# Fold pending changie fragments into CHANGELOG.md as a new released version section,
# then clear the fragments. CHANGELOG.md is the source of truth.
#
# Usage: hack/changelog-fold.sh vX.Y.Z
#   CHANGIE=/path/to/changie hack/changelog-fold.sh v0.1.0   # override the binary
set -euo pipefail

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  echo "usage: $0 vX.Y.Z" >&2
  exit 1
fi

CHANGIE="${CHANGIE:-bin/changie}"
if [ ! -x "$CHANGIE" ]; then
  echo "changie not found at '$CHANGIE' — run: make changie" >&2
  exit 1
fi

CHANGELOG="CHANGELOG.md"

# Render the version section only (no header, no disk mutation, fragments untouched).
section="$("$CHANGIE" batch "$VERSION" --dry-run)"
if [ -z "$section" ]; then
  echo "no unreleased fragments to release for $VERSION" >&2
  exit 1
fi

# Insert the section before the first existing "## [" heading (one blank line between).
# Falls back to appending after the header block if the changelog has no versions yet.
SECTION="$section" awk '
  BEGIN { sec = ENVIRON["SECTION"] }
  /^## \[/ && !ins { print sec "\n"; ins = 1 }
  { print }
  END { if (!ins) print "\n" sec }
' "$CHANGELOG" > "$CHANGELOG.tmp"
mv "$CHANGELOG.tmp" "$CHANGELOG"

# Clear the folded fragments (keep the directory + .gitkeep).
find .changes/unreleased -maxdepth 1 -type f -name '*.yaml' -delete

echo "Folded $(basename "$VERSION") into $CHANGELOG and cleared fragments."
