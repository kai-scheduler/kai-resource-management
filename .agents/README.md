# Shared agent assets

Repository-owned agent skills and supporting assets belong under
`.agents/skills/`.

The `.agents/` tree is the source of truth for assets shared across agent
harnesses. Harness-specific directories such as `.claude/` and `.codex/` are
reserved for local state and remain ignored.

Keep general repository instructions in the root `AGENTS.md`. Add a shared
skill only when it captures a focused, repeatable workflow that benefits from
its own instructions or scripts.
