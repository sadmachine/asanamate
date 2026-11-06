# Changelog

## 0.1.4

- `asanamate list` reads generated Claude titles concurrently, backward from
  the end of each transcript, within the listing's 8-second deadline. A
  title that is not found in time leaves the saved title in the listing.
- Lookups omit a `cwd` that is relative or contains NUL, which asanamate
  rejects along with the whole response.
- Codex reply lines and the asanamate snapshot are limited as they are read,
  so an oversize reply or snapshot no longer has to fit in memory first.
- In `asanamate agents --`, one provider's 6-second lookup timeout no longer
  stops the other provider's lookup.
- Codex's PostToolUse hook starts the helper only while a Codex link is
  pending. A pending Claude link or a leftover temporary file used to start
  Node on every Codex tool call. When Codex ran hooks through zsh, the old
  guard also failed with `no matches found` on every tool call while nothing
  was pending. The hook's definition changed, so review and trust it again in
  Codex `/hooks` after upgrading.
- `ASANA_SESSIONS_TASK` links its task once per conversation. An `unlink`
  or `link --replace` in that conversation is no longer undone or flagged as
  a conflict on every later prompt.
- A hook that hits its deadline no longer blocks other writes to the same
  conversation for 30 seconds: locks record their owner process and free at
  once when it exits, and a slow holder no longer releases another's lock.
- A relative `XDG_STATE_HOME` is ignored, as the XDG spec requires, instead
  of writing state into the project directory.
- The installer removes install directories older than the previous one once
  plugins refresh. Each upgrade used to leave its whole directory behind.
- Update the shipped `body-parser` (2.4.0) and `fast-uri` (3.1.9)
  dependencies to fix published vulnerabilities.

## 0.1.3

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
