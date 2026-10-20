# Changelog

## Unreleased

- Optional `asanamate agents -- <source> [args...]` decorator joins live agents
  to explicit conversation ticket links, with repo/branch fallback for missing
  identity or unavailable metadata. Conflicts suppress matching.

## 0.1.1

- Explicit unlink cancels deferred Claude links, including before the first
  transcript is saved. Prompt hooks, Stop hooks, and metadata changes share
  the session lock so an in-flight hook cannot restore a cancelled link.
- Standalone releases no longer replace the core asanamate release as GitHub's
  Latest release.

## 0.1.0

- Optional native Claude Code and Codex plugins, explicit link skills, and
  nonblocking prompt hooks with verified metadata writes.
- Native setup/removal and diagnostics for PATH, versions, registration and trust.
- Private expiring Claude pending links for first-prompt persistence.
- Independently tagged tarballs, checksums, package CI and pinned installs.
- `list`, `open`, `link`, `unlink`, `doctor`, `help`, and `version` for
  Claude Code and Codex conversations linked by `[asana:<GID>]` title markers.
- `asanamate list` and `asanamate open` adapters for asanamate's conversation
  protocol, version 1.
- Claude activity comes from Claude Code's live-session registry; `open`
  resumes inactive Claude conversations and refuses active or unknown ones.
