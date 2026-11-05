# 13: Move settings the TUI changes into settings.toml

## Goal

Every setting the TUI can change has one home: `settings.toml`, next to
`config.toml`. Both the user and the app edit it. Today these settings have a
default in `config.toml` and a hidden override in the state file, so editing
the config does nothing once the TUI has saved a value. After this change,
the file shows the value the app uses, whoever set it last.

The same release reshapes the `[theme]` table, which moves into the new
file, so that its keys say what they do.

## Decisions

These were agreed with the user on 2026-10-15.

- **A separate file, not app writes to `config.toml`.** `config.toml` is
  mostly comments. The Go TOML library loses comments when it rewrites a
  file, so the app never writes `config.toml`. `settings.toml` is short and
  has only a header comment, so the app can rewrite it safely. kitty does the
  same with `kitten themes`, which writes `current-theme.conf`.
- **No more state overrides for settings.** `state.Display` goes away. State
  keeps only what XDG calls state: history, seen tasks, per-project views,
  columns, saved views, and the current saved view.
- **Same key names, except `[theme]`.** A setting keeps its dotted name and
  only changes files. `list.separator` in `config.toml` becomes
  `list.separator` in `settings.toml`. The `Config` struct fields stay; they
  are filled from the other file.
- **`[theme]` moves as a whole.** `theme.dark` and `theme.light` only matter
  as the pair that `auto` picks from, so they move with `theme.name` and are
  not split across two files. `colors.*` stays in `config.toml`: it changes
  how themes look, not which one is used.
- **Clearer theme names.** Today the built-in themes `dark` and `light`
  share their names with the keys `theme.dark` and `theme.light`, so the
  default reads `dark = "dark"`. Nothing shows that the pair only applies to
  `auto` either. The new layout fixes both problems:

  ```toml
  [theme]
  name = "auto"          # a theme, or "auto" to follow the terminal background

  [theme.auto]           # used only when name = "auto"
  dark = "ansi-dark"
  light = "ansi-light"
  ```

  - The built-ins become `ansi-dark` and `ansi-light`. Their defining trait
    is that they use the terminal's own ANSI palette.
  - `base = "dark"` and `base = "light"` in theme files stay as they are.
    There the word names an appearance, not a theme.
- **Breaking, approved.** This moves released keys and renames released
  values, so the release is breaking: a minor bump before `v1.0.0`. Follow
  "Approved breaking changes" in `AGENTS.md`.

## Depends on

Spec 12, so that old configs get the startup migration prompt.

## Keys that move

| `config.toml` (today) | `settings.toml` | TUI control |
|---|---|---|
| `theme.name` | `theme.name` | settings `t`, theme picker |
| `theme.dark` | `theme.auto.dark` | none yet |
| `theme.light` | `theme.auto.light` | none yet |
| `list.separator` | `list.separator` | settings `s` |
| `list.header.spacing` | `list.header.spacing` | settings `h` |
| `list.selection.style` | `list.selection.style` | settings `l` |
| `list.refresh_interval` | `list.refresh_interval` | settings `i`, `R` |
| `reader.view` | `reader.view` | settings `v`, `reader_view` key |
| `layout.focus.dim` | `layout.focus.dim` | settings `d` |

Values that change in all three theme keys: `"dark"` becomes `"ansi-dark"`
and `"light"` becomes `"ansi-light"`.

Check `internal/tui/settings.go`, `internal/tui/refresh.go`, and
`internal/tui/keys.go` for any other value the TUI saves to `state.Display`
on `main` when you start. Any such value moves too; add it to this table.

## Context

- `internal/state/state.go`: `Display` holds the TUI's overrides.
  `internal/tui/model.go` (`New`) reads them over the config values.
  `internal/tui/settings.go` (`saveDisplay`), `internal/tui/refresh.go`
  (the `R` picker), and `internal/tui/theme.go` (`setTheme`) write them.
- `cmd/asanamate/main.go` (`runTUI`): applies the saved `display.theme`
  before `darkBackground`. This goes away.
- `internal/config/config.go`: `Load`, `Default`, `validate`.
- `internal/config/theme.go`:
  - the `Theme` struct and `ThemeAuto`
  - `builtinThemes`, embedded from `internal/config/themes/dark.toml` and
    `light.toml`
  - `loadThemes`, which reserves the built-in file names
  - `ThemeNames`, `WithTheme`, and `Palette`
- `internal/tui/theme.go`: the theme picker lists `ThemeNames`.
- `internal/setup/setup.go`: the config template.
  `internal/setup/update.go`: `Merge` rebuilds `config.toml` from the
  template and fails on keys the template lacks. `writePrivate` writes files
  with mode 0600.
- `internal/config/moves.go`: key moves and command moves. This spec adds
  value renames.
- Spec 12: `PendingUpdates` and `UpdateNeeded` report old configs, and the
  TUI offers `config update` at startup.

## Requirements

- **One key list.** Add `config.SettingKeys`, the dotted keys in the
  `settings.toml` column above. It is the single source for:
  - what `settings.toml` may hold
  - what `config.toml` must no longer hold
  - what `Merge` moves out
- **The file.** Add `config.SettingsPath(configPath)` for `settings.toml`
  next to the config.
  - A missing file means every setting has its default.
  - The file starts with this header:

    ```toml
    # Settings you can also change in asanamate (press s).
    # (Since then: "(s for visuals; comma for tools)".)
    # asanamate rewrites this file when you change a setting there. It keeps
    # every value, but not comments below this header.
    ```

  - `asanamate setup` writes the file with every setting at its default, so
    users can find it. `config update` writes it when missing.
- **Loading.** `Load` decodes `config.toml`, then decodes `settings.toml`
  over the same `Config`, then runs `validate` once.
  - Errors name the file and the dotted key, such as
    `settings.toml: list.selection.style must be ...`.
  - `settings.toml` rejects unknown keys, and also valid config keys that are
    not in `SettingKeys`, such as `workspace`.
  - A `SettingKeys` key in `config.toml` (old or new name) is a pending
    change that `PendingUpdates` reports (spec 12), for example
    "list.separator moved to settings.toml".
- **Theme layout.**
  - The `Theme` struct becomes `Name` plus `Auto struct{ Dark, Light }`.
  - Rename the embedded files to `ansi-dark.toml` and `ansi-light.toml`, and
    the `builtinThemes` keys with them. `base = "dark"` maps to `ansi-dark`
    and `base = "light"` to `ansi-light`.
  - Reserved theme file names: `auto`, `ansi-dark`, `ansi-light`, and the
    old `dark` and `light`. Reserving the old names stops a stray
    `dark.toml` from quietly taking over an old config's value.
  - `ThemeNames` lists `auto`, `ansi-dark`, `ansi-light`, then the files.
  - `"dark"` or `"light"` as a value of `theme.name` or `theme.auto.*`
    fails validation with a hint, such as
    `theme.name: "dark" is now "ansi-dark"`. That covers hand edits made
    after the upgrade.
  - `Palette`, `WithTheme`, and the theme picker follow the new struct.
  - The README Themes section shows the new layout, including a fixed
    preset (`name = "tokyo-night"`) and an `auto` pair.
- **Value renames as a change kind.** Add a value rename to the change list
  from spec 12: an exact value of a key becomes a new value. Register
  `"dark"` to `"ansi-dark"` and `"light"` to `"ansi-light"` for
  `theme.name`, `theme.auto.dark`, and `theme.auto.light`. Order the list so
  `theme.dark` and `theme.light` first move to `theme.auto.*`, then values
  are renamed, then the keys move to `settings.toml`. `PendingUpdates` and
  `Merge` both use the list.
- **Writing.** Add `config.SaveSetting(configPath, key string, value any) error`.
  - It reads `settings.toml` fresh on every call, sets one key, and writes
    the header plus the keys the file holds, in `SettingKeys` order.
  - Re-reading means a hand edit to another key while the app runs is kept.
  - Write atomically: a temp file in the same directory, then rename, with
    mode 0600.
  - It rejects keys not in `SettingKeys` and values that fail validation.
- **TUI.**
  - Each settings toggle, the `R` interval picker, and the theme picker call
    `SaveSetting` instead of writing `state.Display`. A save error goes to
    `m.status`, and the setting still applies for the session.
  - `New` reads start values from `Config` only.
  - Remove `saveDisplay`, the `cmp.Or(d.State.Display...)` fallbacks, and
    the saved-theme override in `runTUI`.
  - The app does not reload hand edits while running; they take effect at
    the next start or the next app write.
- **Migrating `config.toml` (the user's file, prompted through spec 12).**
  - `Merge` takes the `SettingKeys` (after renames) out of the user's config
    and returns them, and drops the keys from the template.
  - `Update` writes them into `settings.toml`. Values already in
    `settings.toml` win, and it backs up an existing `settings.toml` the same
    way as the config (`<file>.yyyy-mm-dd--hh-mm-ss.bak`).
  - The template in `internal/setup/setup.go` replaces the `[theme]` table
    and each other removed key with a one-line comment pointing to
    `settings.toml`. The `colors` comment that says "Values below are the
    dark theme's" names `ansi-dark`.
- **Migrating state (the app's file, silent).**
  - Rename `state.Display` to a legacy field that still decodes the
    `[display]` table. Mark it for removal in the release after next.
  - In `runTUI`, after the config loads and before `tui.New`: if the legacy
    table holds any value, write each one with `SaveSetting`. A state value
    wins over a value from `config.toml`, because the user was seeing it.
    Then clear the table, save state, and load the config again.
  - Map the old state names to keys: `separator` to `list.separator`,
    `header_spacing` to `list.header.spacing`, `selection_style` to
    `list.selection.style`, `reader_view` to `reader.view`,
    `refresh_interval` to `list.refresh_interval`, `focus_dim` to
    `layout.focus.dim`, and `theme` to `theme.name`. Apply the
    `dark`/`light` value rename to `theme`.
- **`doctor`** prints a `settings` row with the path, or "defaults (no
  file)".
- **Docs.**
  - README:
    - a `settings.toml` table, holding the moved rows from the config table
      in their new shape
    - the `s` row in Keys
    - the Themes section
    - the sentence about when config changes take effect
    - the `list.refresh_interval` text: `R` and settings save the interval;
      the template's old "for the current session only" was wrong
  - `docs/migrations.md`, under `## Unreleased`:
    - the key table and the theme value renames
    - what the startup prompt does
    - that settings saved in the TUI move over automatically
  - `AGENTS.md`, "Config keys": a key the TUI can change goes in
    `SettingKeys` and `settings.toml`, never in state. Every key change also
    touches the `settings.toml` template and table.
- **Commit** as `feat!:` with a `BREAKING CHANGE:` footer naming each moved
  key and its new file, and the renamed theme values.

## Out of scope

- Reloading `settings.toml` while the app runs.
- Picker actions that set the `auto` pair, such as "use as dark theme". Add
  this to `docs/deferred-ideas.md`.
- An `asanamate config settings` subcommand to open the file. Add it to
  `docs/deferred-ideas.md`.
- The default view (`default_filter`, `list.group_by`, `list.sort`,
  `list.fields`); that is spec 14.

## Acceptance

- Config tests:
  - defaults with no `settings.toml`
  - values read from `settings.toml`
  - validation errors that name `settings.toml` and the key
  - a non-setting key in `settings.toml` is rejected
  - a setting key in `config.toml` gives `UpdateNeeded`
  - `theme.name = "dark"` in `settings.toml` fails with the rename hint
  - `auto` picks `theme.auto.dark` or `theme.auto.light` by background
  - a `dark.toml` theme file is rejected as reserved
  - `SaveSetting` keeps a hand-edited key, writes in order, and rejects
    unknown keys and bad values
- Setup tests:
  - `Merge` moves every `SettingKeys` key out
  - `Merge` turns `theme.dark = "dark"` into `theme.auto.dark = "ansi-dark"`
    in `settings.toml`
  - `Update` writes `settings.toml`, keeps existing `settings.toml` values,
    and backs up both files
  - `setup` writes a default `settings.toml`
- TUI tests:
  - each settings toggle, `R`, and the theme picker write `settings.toml`
    and leave state untouched
  - the picker lists `ansi-dark` and `ansi-light`
  - a new `Model` built from the reloaded config starts with the saved
    values
- State migration test: a legacy `[display]` table moves into
  `settings.toml`, a state value beats a config value, `theme = "dark"`
  becomes `"ansi-dark"`, and the table is cleared.
- After migration, the local `~/.config/asanamate` passes `asanamate doctor`.
  Follow "Local config migration" in `AGENTS.md`.
- `go test ./...` and `go vet ./...` pass.
