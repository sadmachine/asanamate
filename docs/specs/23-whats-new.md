# 23: Show what's new after an update

## Goal

After someone upgrades asanamate, the first launch shows a short, curated
list of what the new version adds, so people learn about features they would
otherwise never find. It is a highlight reel, not a changelog: a few entries
per release, each saying what the feature is and how to try it. Anyone who
doesn't want it can turn it off from the modal itself.

## Context

- `cmd/asanamate/main.go`: `startTUI` sets `Deps.Version` from
  `currentVersion()` (the GoReleaser ldflag, or the module version for
  `go install`; `dev` otherwise). `loadOrMigrate` runs the config update prompt
  before the TUI starts.
- `internal/updatecheck/updatecheck.go`: `stableVersion` and `compare` parse
  and order `vX.Y.Z` tags, and treat development and prerelease builds as not
  stable. Both are unexported today.
- `internal/state/state.go`: `state.toml`, written only by the app; it
  migrates silently on load.
- `internal/config/settings.go`: `SettingKeys` and `settings.toml` hold the
  settings the TUI can change; `Model.saveSetting` in
  `internal/tui/settings.go` writes them.
- `internal/tui/settings.go`: `openTools` is the comma "Settings & tools"
  menu, which already lists program settings and tools such as "Status
  history".
- `internal/tui/markdown_test.go`, `internal/tui/activity.go`: the TUI
  already renders Markdown for ticket text.
- `docs/migrations.md` and `.github/workflows/release.yml`: upgrade steps
  wait under `## Unreleased`, the heading is renamed to the tag at release,
  and the workflow copies that section to the top of the GitHub release
  notes.

## Decisions

These were agreed with the user on 2026-11-05.

- **A curated file in the repo is the source.** It is not GitHub release
  notes and not commit history. GoReleaser's changelog lists every commit,
  which is exactly what this feature avoids. The file is embedded in the
  binary, so it works offline and always matches the installed version.
- **One modal, no tabs.** A curated release has only a few entries; tabs
  would split a short list into emptier screens, and a "bug fixes" tab would
  pull the full changelog back in. Entries are grouped by version, newest
  first, with a "New" section and, only when needed, a "Changed" section for
  behavior changes that would surprise someone.
- **It opens by itself the first time, and the user can turn that off.** The
  modal offers "Got it" and "Don't show on startup". The second option
  closes it and turns the setting off.
- **The setting is a table.** `[whats_new]` with `on_startup = true`, so
  later what's-new settings have a home without moving a released key. It
  lives in `settings.toml` and `config.SettingKeys`, because the TUI changes
  it.
- **The last version seen is state.** `last_seen_version` in `state.toml`;
  only the app writes it.

## Requirements

- **Content file.** `whats-new.md` sits beside the Go file that embeds it
  (`go:embed` cannot reach parent directories); `internal/whatsnew/` is the
  default home. Format:

  ```markdown
  ## Unreleased

  ### New

  - **Fold group sections.** Collapse a group-by section to its header.
    Try it: press `z` on a section header.

  ### Changed

  - **Working agents use a line spinner.** ...
  ```

  `## vX.Y.Z` headings, newest first, each with `### New` and optional
  `### Changed`. An entry is a bold title, one or two sentences, and how to
  try it. Links are absolute, as in `docs/migrations.md`.
- **Which entries show.** The entries for versions newer than
  `last_seen_version` and no newer than the running version. A user who
  skipped releases sees each of them under its own heading. `## Unreleased`
  never shows in a released build.
- **When the modal opens.** On launch of the main TUI (`asanamate` with no
  subcommand), after the config update prompt, when all of these hold:
  - the running version is stable (same rule as `updatecheck`);
  - the running version is newer than `last_seen_version`;
  - `whats_new.on_startup` is true;
  - at least one entry falls in the range.
- **Recording the version.** Closing the modal either way stores the running
  version as `last_seen_version`. When the modal would not open (setting off,
  no entries in range), the app stores the version silently, so the modal
  never covers releases the user already ran. A downgrade leaves the stored
  version unchanged.
- **First run.** A fresh install (no `state.toml` yet) shows nothing and
  records the running version. An existing user upgrading to the first
  release with this feature has a `state.toml` but no `last_seen_version`;
  they see the running version's entries only.
- **Modal.** Scrollable, sized to fit narrow panes (tmux sidebar, popup).
  Enter or Esc means "Got it"; one key, shown in the footer, means "Don't
  show on startup". The footer links the full GitHub release for the running
  version. Rendered with the TUI's existing Markdown renderer.
- **Reopening.** The comma "Settings & tools" menu gains two rows: "What's
  new", which opens the same modal with every version up to the running one,
  and "Show what's new on startup", an on/off toggle for
  `whats_new.on_startup`.
- **CLI.** `asanamate whats-new` prints the same Markdown, every version up
  to the running one, to stdout. It never writes state.
- **Config key.** `whats_new.on_startup` (bool, default `true`) touches
  everything AGENTS.md lists: the struct field and doc comment, `Default()`,
  `validate()`, the setup template, `SettingKeys` and the settings template,
  the README config and settings tables, and the config tests.
- **Release notes.** The release workflow copies the tag's section of
  `whats-new.md` into the GitHub release notes under `## What's new`, above
  the existing `## Upgrading` section.
- **Maintenance rules in AGENTS.md.**
  - Every `feat` and every `fix` commit gets a what's-new decision: whoever
    writes it decides whether it earns an entry, and adds it under
    `## Unreleased` in the same commit. Both types matter, because a `feat`
    can be New or Changed and a `fix` can be Changed. Nothing reads commit
    types to generate entries. The commit type does not pick the section;
    what the user experiences does:
    - **New:** something the user can now do that they could not before (a
      command, key, view, or setting). Usually a `feat`.
    - **Changed:** something the user already relies on now behaves or looks
      different (a moved key, a new default, a reworked screen). This can be
      a `feat`, a `fix`, or a breaking change.
    - **No entry:** polish nobody would look for, fixes that restore intended
      behavior, and internal work.
  - Before tagging, compare `## Unreleased` with every `feat` and `fix`
    commit in `git log --oneline <last tag>..main` to catch a missed or
    misfiled entry.
  - The release steps rename `## Unreleased` in `whats-new.md` along with
    `docs/migrations.md`, and delete it when empty.

## Out of scope

- Fetching release notes or anything else over the network.
- A bug-fix list, refactors, or a full changelog in the modal.
- Tabs, images, or per-entry "seen" tracking.
- Showing the modal from `asanamate read` or any other subcommand.

## Acceptance

- Upgrading from v0.A to v0.B with entries for both versions in between
  opens the modal once, listing both, newest first; the next launch shows
  nothing.
- "Don't show on startup" sets `whats_new.on_startup = false` in
  `settings.toml`; the next upgrade opens no modal, but "What's new" in the
  comma menu still shows the entries.
- A fresh install and a `dev` build never open the modal.
- `asanamate doctor` accepts a config with `[whats_new]` and rejects a
  non-boolean `on_startup` with an error naming `whats_new.on_startup`.
- A tag's GitHub release shows its "What's new" section when the file has
  one.

## Calls made without the user

- The content file lives at `internal/whatsnew/whats-new.md`, because of
  the `go:embed` path limit.
- The upgrade into the first release with this feature shows that release's
  entries to existing users rather than nothing.
- Versions with no entries are recorded silently, with no empty modal.
- `asanamate read` never shows the modal.
- The `asanamate whats-new` CLI command is included.
- Fixes get no entry unless they change behavior users rely on; then they
  go under "Changed".
