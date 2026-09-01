#!/usr/bin/env python3
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

"""Regenerate the third-party attribution in NOTICE from the linked dependency set.

NOTICE must describe what the published images actually carry, so the set it lists is
the union of `go list -deps` over every cmd/ main for every released platform -- not
go.mod, which also names test-only and build-only modules that reach no binary.

Attribution is derived from the module cache, but two things resist derivation: a
module can ship no copyright statement at all, and a module can be split-licensed. The
OVERRIDES table below carries those cases as reviewed data. Everything else is
recomputed on every run, so a removed import loses its entry and a bumped version that
changes license is rewritten.
"""

import argparse
import collections
import difflib
import os
import re
import subprocess
import sys

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
NOTICE = os.path.join(REPO, "NOTICE")

PLATFORMS = [("linux", "amd64"), ("linux", "arm64")]

MARK = "## The following components are included in this product:"
SUMMARY_HEAD = (
    "This project also includes software components licensed under various "
    "open-source licenses. The following licenses apply to the components used "
    "in this project:"
)
SUMMARY_TAIL = "These licenses apply to the components used in this project"

# SPDX identifier -> (heading in the summary, display name used in entries).
# Order fixes the order of the summary sections.
LICENSES = [
    ("MIT", "MIT License", "MIT License"),
    ("Apache-2.0", "Apache License", "Apache License 2.0"),
    ("BSD-3-Clause", "BSD Licenses", 'BSD 3-Clause "New" or "Revised" License'),
    ("BSD-2-Clause", "BSD Licenses", 'BSD 2-Clause "Simplified" License'),
    ("ISC", "ISC License", "ISC License"),
    ("MPL-2.0", "Mozilla Public License (MPL)", "Mozilla Public License 2.0"),
]
DISPLAY = {spdx: name for spdx, _, name in LICENSES}

# Reviewed data for what cannot be derived. `license` overrides classification;
# `body` replaces the generated licence and copyright lines outright.
OVERRIDES = {
    # Ships no copyright statement anywhere in the distributed module.
    "cel.dev/expr": {"copyright": "Copyright Google LLC"},
    "github.com/hashicorp/errwrap": {"copyright": "Copyright HashiCorp, Inc."},
    "github.com/hashicorp/go-multierror": {"copyright": "Copyright HashiCorp, Inc."},
    "github.com/moby/sys/mountinfo": {"copyright": "Copyright The Moby Authors"},
    "github.com/opencontainers/selinux": {"copyright": "Copyright The opencontainers/selinux Authors"},
    "github.com/modern-go/concurrent": {"copyright": "Copyright 2018 Modern Go Programming"},
    "github.com/modern-go/reflect2": {"copyright": "Copyright 2018 Modern Go Programming"},
    # Split-licensed per file: the linked packages span both licences.
    "github.com/cyphar/filepath-securejoin": {
        "body": [
            'Licensed under the Mozilla Public License 2.0 and the BSD 3-Clause "New" or "Revised" License',
            "(SPDX-License-Identifier: BSD-3-Clause AND MPL-2.0; both apply, see the module's COPYING.md)",
            "Copyright (C) 2024-2025 Aleksa Sarai <cyphar@cyphar.com>; Copyright (C) 2024-2025 SUSE LLC (MPL-2.0 portions)",
            "Copyright (C) 2014-2015 Docker Inc & Go Authors; Copyright (C) 2017-2024 SUSE LLC (BSD-3-Clause portions)",
        ]
    },
    # The LICENSE file names only the fork's own copyright; the vendored code differs.
    "go.yaml.in/yaml/v2": {
        "license": "Apache-2.0",
        "copyright": "Copyright 2011-2016 Canonical Ltd. (includes libyaml portions under the MIT License, Copyright (c) 2006-2010 Kirill Simonov)",
    },
    "go.yaml.in/yaml/v3": {"copyright": "Copyright (c) 2006-2010 Kirill Simonov"},
    "gopkg.in/yaml.v3": {
        "copyright": "Copyright (c) 2006-2010 Kirill Simonov; Copyright (c) 2011-2019 Canonical Ltd."
    },
    "sigs.k8s.io/json": {
        "copyright": "Copyright The Kubernetes Authors. (includes portions under the BSD 3-Clause License, Copyright 2010 The Go Authors)"
    },
    # LICENSE carries only the Apache boilerplate; copyright taken from the project.
    "github.com/go-logr/logr": {"copyright": "Copyright 2021 The logr Authors."},
    "github.com/go-logr/zapr": {"copyright": "Copyright 2023 The logr Authors."},
    "github.com/gogo/protobuf": {"copyright": "Copyright (c) 2013, The GoGo Authors. All rights reserved."},
    "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring": {
        "copyright": "Copyright 2018 The prometheus-operator Authors"
    },
    "gomodules.xyz/jsonpatch/v2": {"copyright": "Copyright (c) 2015 The Authors"},
    "k8s.io/kube-openapi": {"copyright": "Copyright The Kubernetes Authors."},
    "github.com/kai-scheduler/api": {"copyright": "Copyright 2025 NVIDIA CORPORATION"},
    # Upstream's own NOTICE opens "Copyright Copyright 2025 NVIDIA CORPORATION".
    "github.com/kai-scheduler/KAI-scheduler": {"copyright": "Copyright 2025 NVIDIA CORPORATION"},
}

# Apache-2.0 and MIT templates both carry a bracketed placeholder rather than a real
# holder. Every bracket style is in the wild, including {yyyy}.
PLACEHOLDER = re.compile(r"copyright\s*[\[\(<{]\s*(yyyy|year|name)", re.I)
COPYRIGHT = re.compile(r"^\s*(?:#|//|\*|;)?\s*(Copyright\b.*)$")
LICENSE_FILE = re.compile(r"^(LICEN[CS]E|COPYING)", re.I)
# Licences covering a module's documentation rather than its code. Extensions are not
# excluded in general: license.md and LICENSE.md are the only licence a module ships.
IGNORED_LICENSE_FILE = re.compile(r"\.docs$", re.I)

# Source files scanned per module when falling back to header copyrights.
# k8s.io/kubernetes alone ships over 5000, and reading them all costs a minute.
SOURCE_SCAN_LIMIT = 200


def run(args, **kw):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kw).stdout


def binaries():
    cmd = os.path.join(REPO, "cmd")
    return sorted(
        d for d in os.listdir(cmd) if os.path.exists(os.path.join(cmd, d, "main.go"))
    )


def closure():
    """module path -> (version, directory), for every module linked into any binary."""
    mods = {}
    fmt = "{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}"
    for name in binaries():
        for goos, goarch in PLATFORMS:
            env = dict(os.environ, GOOS=goos, GOARCH=goarch)
            go = os.environ.get("GO", "go")
            out = run([go, "list", "-deps", "-f", fmt, f"./cmd/{name}"], cwd=REPO, env=env)
            for line in out.splitlines():
                if not line.strip():
                    continue
                path, version, directory = line.split("|", 2)
                mods[path] = (version, directory)
    mods.pop("github.com/kai-scheduler/kai-resource-management", None)
    return mods


def license_files(directory):
    try:
        names = sorted(os.listdir(directory))
    except OSError:
        return []
    return [
        os.path.join(directory, n)
        for n in names
        if LICENSE_FILE.match(n)
        and not IGNORED_LICENSE_FILE.search(n)
        and os.path.isfile(os.path.join(directory, n))
    ]


def classify(path):
    head = "\n".join(open(path, encoding="utf-8", errors="replace").read().splitlines()[:40])
    if "Mozilla Public License" in head:
        return "MPL-2.0"
    if "Apache License" in head:
        return "Apache-2.0"
    if re.search(r"GNU (GENERAL|LESSER|LIBRARY)", head, re.I):
        return "GPL-family"
    if "MIT License" in head or "Permission is hereby granted, free of charge" in head:
        return "MIT"
    if "Redistribution and use in source and binary forms" in head:
        # The third BSD clause is a no-endorsement clause; its wording varies far more
        # than "Neither the name of", which is why the whole phrase is not matched.
        return "BSD-3-Clause" if re.search(r"endorse or promote", head, re.I) else "BSD-2-Clause"
    if "Permission to use, copy, modify, and/or distribute" in head:
        return "ISC"
    return None


def first_copyright(path):
    for line in open(path, encoding="utf-8", errors="replace"):
        if "Copyright" not in line or PLACEHOLDER.search(line):
            continue
        m = COPYRIGHT.match(line)
        if m:
            return m.group(1).strip()
    return None


def copyright_for(directory):
    """Best-effort copyright, preferring the most authoritative source available."""
    for name in ("NOTICE", "NOTICE.txt", "NOTICE.md"):
        path = os.path.join(directory, name)
        if os.path.exists(path):
            found = first_copyright(path)
            if found:
                return found, "NOTICE"
    for path in license_files(directory):
        found = first_copyright(path)
        if found:
            return found, "LICENSE"
    # Fall back to the most common source-file header, which is how projects whose
    # LICENSE is bare Apache boilerplate still state a holder.
    seen = []
    scanned = 0
    for root, dirs, files in os.walk(directory):
        dirs[:] = sorted(d for d in dirs if not d.startswith((".", "_")) and d != "testdata")
        for name in sorted(files):
            if not name.endswith(".go"):
                continue
            scanned += 1
            try:
                with open(os.path.join(root, name), encoding="utf-8", errors="replace") as fh:
                    for _ in range(12):
                        line = fh.readline()
                        if not line:
                            break
                        if "Copyright" in line and not PLACEHOLDER.search(line):
                            m = COPYRIGHT.match(line)
                            if m:
                                seen.append(m.group(1).strip())
                            break
            except OSError:
                pass
        if scanned >= SOURCE_SCAN_LIMIT:
            break
    if seen:
        counts = collections.Counter(seen)
        return min(counts, key=lambda text: (-counts[text], text)), "source header"
    return None, None


def entries(mods):
    """(text block, spdx, provenance) per module, sorted case-insensitively by path."""
    out, problems = [], []
    for path in sorted(mods, key=str.lower):
        _, directory = mods[path]
        override = OVERRIDES.get(path, {})
        if "body" in override:
            out.append(("\n".join([path] + override["body"]), "MPL-2.0", "override"))
            continue

        files = license_files(directory)
        spdx = override.get("license") or (classify(files[0]) if files else None)
        if spdx is None:
            problems.append(f"{path}: cannot identify a license (files: {[os.path.basename(f) for f in files] or 'none'})")
            continue
        if spdx == "GPL-family":
            problems.append(f"{path}: classified {spdx}; no copyleft of this kind may be linked")
            continue
        if len(files) > 1 and "license" not in override and "body" not in override:
            problems.append(
                f"{path}: ships {len(files)} license files "
                f"({', '.join(os.path.basename(f) for f in files)}); add an OVERRIDES entry"
            )

        if "copyright" in override:
            holder, source = override["copyright"], "override"
        else:
            holder, source = copyright_for(directory)
        if not holder:
            problems.append(f"{path}: no copyright statement found; add an OVERRIDES entry")
            continue
        out.append((f"{path}\nLicensed under the {DISPLAY[spdx]}\n{holder}", spdx, source))
    return out, problems


def summary(present):
    lines = [SUMMARY_HEAD, ""]
    for heading in dict.fromkeys(h for s, h, _ in LICENSES if s in present):
        lines.append(f"## {heading}")
        lines.append("")
        for spdx, h, name in LICENSES:
            if h == heading and spdx in present:
                lines.append(f"- {name}")
        lines.append("")
    return "\n".join(lines)


class TemplateError(Exception):
    """The hand-written parts of NOTICE no longer match what the generator splits on."""


def render(current, blocks, present):
    # Only the summary and the component list are generated. The prose between them
    # carries the written offer of source, so a missing marker must stop the run
    # rather than silently drop it.
    for marker in (SUMMARY_HEAD, SUMMARY_TAIL, MARK):
        if marker not in current:
            raise TemplateError(f"NOTICE is missing the marker text: {marker!r}")
    head, _, rest = current.partition(SUMMARY_HEAD)
    _, _, after = rest.partition(SUMMARY_TAIL)
    middle, _, _ = after.partition(MARK)
    return (
        head
        + summary(present)
        + SUMMARY_TAIL
        + middle
        + MARK
        + "\n\n"
        + "\n\n".join(b for b, _, _ in blocks)
        + "\n"
    )


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--check", action="store_true", help="fail if NOTICE is out of date; write nothing")
    ap.add_argument("--report", action="store_true", help="print where each attribution came from")
    args = ap.parse_args()

    mods = closure()
    blocks, problems = entries(mods)
    present = {spdx for _, spdx, _ in blocks}
    current = open(NOTICE, encoding="utf-8").read()
    try:
        updated = render(current, blocks, present)
    except TemplateError as err:
        print(f"::error::{err}", file=sys.stderr)
        return 1

    if args.report:
        by_source = collections.Counter(src for _, _, src in blocks)
        by_license = collections.Counter(spdx for _, spdx, _ in blocks)
        print(f"binaries: {', '.join(binaries())}")
        print(f"modules linked: {len(blocks)}")
        print("licenses: " + ", ".join(f"{k} x{v}" for k, v in sorted(by_license.items())))
        print("attribution from: " + ", ".join(f"{k} x{v}" for k, v in sorted(by_source.items())))

    for p in problems:
        print(f"::error::{p}", file=sys.stderr)
    if problems:
        print(
            f"::error::{len(problems)} module(s) could not be attributed; NOTICE not written.",
            file=sys.stderr,
        )
        return 1

    if args.check:
        if updated != current:
            diff = difflib.unified_diff(
                current.splitlines(True), updated.splitlines(True), "NOTICE", "NOTICE (regenerated)"
            )
            sys.stderr.writelines(diff)
            print(
                "::error::NOTICE does not match the linked dependency set. "
                "Run 'make notice' and commit the result.",
                file=sys.stderr,
            )
            return 1
        print(f"NOTICE is up to date ({len(blocks)} modules).")
        return 0

    if updated != current:
        open(NOTICE, "w", encoding="utf-8").write(updated)
        print(f"NOTICE updated ({len(blocks)} modules).")
    else:
        print(f"NOTICE already up to date ({len(blocks)} modules).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
