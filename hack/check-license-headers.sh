#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# Verify the exact wording of the NVIDIA copyright header.
#
# make license-check runs addlicense, which only reports whether *a* header exists. It
# never inspects the wording, and it silently skips any extension it has no comment
# style for, .tpl among them. Both gaps reached an external licensing review while
# license-check was passing: the headers named a superseded copyright entity, and the
# two Helm partials carried no header at all.
#
# Scope is every file that already declares an SPDX identifier in its header, plus
# every .tpl. Enforcing presence on the extensions addlicense does understand stays
# addlicense's job; this checks the text it cannot.
#
# Usage: hack/check-license-headers.sh
set -euo pipefail

COPYRIGHT='Copyright [0-9]{4} NVIDIA CORPORATION & AFFILIATES\. All rights reserved\.'
SPDX='SPDX-License-Identifier: Apache-2\.0'
# Lines addlicense emits from hack/license-header.txt, in Go and shell comment styles.
COPYRIGHT_LINE="^(//|#) ${COPYRIGHT}\$"
SPDX_LINE="^(//|#) ${SPDX}\$"

# Derived from christophebedard/dco-check and keeps that project's Apache header,
# attributed in NOTICE. Adding an NVIDIA copyright to it would misstate authorship.
SKIP='.github/scripts/dco_check.py'

# Deep enough for a shebang, a blank line and the two header lines.
HEADER_LINES=5

status=0
while read -r file; do
  [[ "${file}" == "${SKIP}" ]] && continue
  [[ -f "${file}" ]] || continue

  header="$(head -n "${HEADER_LINES}" "${file}" 2>/dev/null || true)"
  # In scope once either header line is present, not just the SPDX one: dropping a
  # single line must be an error rather than a way out of the check. A file with
  # neither line is addlicense's call, except .tpl, which addlicense cannot see.
  if [[ "${file}" != *.tpl ]] &&
    ! grep -qE "${SPDX_LINE}|^(//|#) Copyright [0-9]{4} NVIDIA" <<<"${header}"; then
    continue
  fi

  if ! grep -qE "${COPYRIGHT_LINE}" <<<"${header}"; then
    echo "::error file=${file}::missing or misworded copyright line; expected: Copyright <year> NVIDIA CORPORATION & AFFILIATES. All rights reserved."
    status=1
  fi
  if ! grep -qE "${SPDX_LINE}" <<<"${header}"; then
    echo "::error file=${file}::missing SPDX line; expected: SPDX-License-Identifier: Apache-2.0"
    status=1
  fi
done < <(git ls-files)

exit "${status}"
