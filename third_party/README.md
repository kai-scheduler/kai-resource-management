# Third-party source

This directory is for small, stable pieces of external source that are clearer
to maintain locally than as a full dependency. It must not be used to avoid
dependency review, vulnerability scanning, or license obligations.

Prefer a normal Go module dependency when the upstream library is maintained,
security-sensitive, or likely to evolve.

## Required layout

Use a path that identifies the upstream owner and project:

```text
third_party/<owner>/<project>/...
```

Every copied project must include a README containing:

- Upstream project name and URL.
- Exact release, tag, or commit.
- Date copied.
- Files copied and why vendoring is preferable.
- Local modifications.
- Update procedure.
- Applicable license and retained copyright notices.

Retain upstream license files and source headers. Do not apply this repository's
NVIDIA header to unmodified third-party source.
