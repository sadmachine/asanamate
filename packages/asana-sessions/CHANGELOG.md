# Changelog

## Unreleased

- The installer refreshes registered plugins after each install with the new
  `setup --refresh`, so hooks no longer stay on the previous version until
  setup is rerun by hand. Unregistered providers stay unregistered, and an
  install that is not the `asana-sessions` on `PATH` leaves plugins alone.
- Codex prompt hooks defer linking unnamed threads, preserving Codex's
  generated title instead of preventing it with the `Conversation` fallback.
  The first tool call after Codex generates the title completes the link,
  through a new PostToolUse hook that starts the helper only while a link is
  pending; a new Stop hook completes it otherwise. Named threads still link
  immediately. Review the prompt, PostToolUse, and Stop hooks in Codex
  `/hooks` after upgrading.
- A Claude conversation linked before Claude Code generated its title no
  longer stays `Conversation [asana:<GID>]`: `list` shows the generated title,
  and the Stop hook renames the conversation to it once one exists.

## 0.1.2

- Install packed releases with the shared `install.mjs` installer, which uses
  `npm ci` to preserve every shrinkwrapped dependency. Failed upgrades retain
  the current executable; release assets include the installer and its checksum.
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
