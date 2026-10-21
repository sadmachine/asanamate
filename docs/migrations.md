# Migrations

Steps to upgrade from the previous release, newest first. The release
workflow copies a version's section to the top of its GitHub release notes,
so links here are absolute. Steps for changes not yet released wait under
`## Unreleased`, which becomes the version's heading when it is tagged.

## Unreleased

The Extensions guide now uses the standalone companion's `install.mjs` installer
instead of npm's global tarball install, preserving its shrinkwrapped dependencies.
Existing configuration is unchanged. See the
[companion installation guide](https://github.com/sadmachine/asanamate/blob/main/packages/asana-sessions/README.md#install).

## v0.3.2

Visual settings remain under `s`; program settings and tools open with `,`.
Optional integrations are now listed under **Settings & tools > Extensions** and
`asanamate extensions`. The previous `asanamate addons` command remains an
alias. Guides include per-contract references, implementation instructions,
and the separately installed conversation-history helper. No config migration
is required. See [Extensions](https://github.com/sadmachine/asanamate#extensions).

Harvest now supports native personal access tokens and guided
`asanamate-hrvst setup`, without Node.js, npm, or hrvst-cli. The optional
`harvest.toml` beside core config stores only account/task defaults. Existing
legacy logins, explicit `--task-id` commands, and provider IDs remain supported;
no migration is required. See [native setup](https://github.com/sadmachine/asanamate#time-tracking-optional).

## v0.3.0

This breaking release moves settings into `settings.toml` next to `config.toml`.
The TUI offers migration at startup on a terminal; scripts report the pending
changes. You can also run `asanamate config update`. It keeps values, resets
comments to the current templates, and saves timestamped backups of files it
rewrites. Existing `settings.toml` values win over values from `config.toml`.
Settings last saved in the TUI migrate silently from state and take precedence,
because those are the values you were seeing.

| Old key in config.toml | New key in settings.toml |
|---|---|
| `theme.name` | `theme.name` |
| `theme.dark` | `theme.auto.dark` |
| `theme.light` | `theme.auto.light` |
| `list.separator` | `list.separator` |
| `list.header.spacing` | `list.header.spacing` |
| `list.selection.style` | `list.selection.style` |
| `list.refresh_interval` | `list.refresh_interval` |
| `reader.view` | `reader.view` |
| `layout.focus.dim` | `layout.focus.dim` |

In all three theme keys, `"dark"` becomes `"ansi-dark"` and `"light"` becomes
`"ansi-light"`. Theme files keep `base = "dark"` or `base = "light"`.
See [Themes](https://github.com/sadmachine/asanamate#themes).

The starting view stays in `config.toml`, grouped under `list.default_view`.
Per-project views and columns remain in state and keep their saved values.

| Old key | New key in config.toml |
|---|---|
| `default_filter` | `list.default_view.filter` |
| `list.group_by` | `list.default_view.group_by` |
| `list.sort.by` | `list.default_view.sort.by` |
| `list.sort.direction` | `list.default_view.sort.direction` |

`--no-preview` is removed; use `--hide-preview`.
The hidden `asanamate hook` subcommand is removed. If `asanamate doctor` on
v0.2.x warned about `codex_hook`, run `asanamate-ccmux setup` **before upgrading**.
Otherwise Codex keeps calling a command that no longer exists. The adapter's
`asanamate-ccmux hook codex` command is unchanged; see
[Agents](https://github.com/sadmachine/asanamate#agents-optional).

## v0.2.1

New default keys: `i` jumps to the next NEW ticket in the list, and `ctrl+d`
saves a comment draft and closes the box. If your `[keys]` already binds `i`
in `keys.main` or `ctrl+d` in `keys.input` to another binding, asanamate
names the clash and stops; move that binding to a free key, or rebind
`keys.main.next_new` or `keys.input.save_draft`.

## v0.2.0

v0.2.0 moves the color settings. The updater also migrates the earlier
`display`, `list.filter`, and `repo` settings. Run `asanamate config update` to rewrite
your config; it keeps your values and saves the old file as a timestamped
`config.toml.<time>.bak`. Until then, asanamate names the first moved key and stops.

| v0.1.0 | v0.2.0 |
|---|---|
| `theme` | `theme.name` |
| `accent_color` | `colors.accent` |
| `list.header.color` | `colors.header` |
| `list.pinned.color` | `colors.pinned` |
| `list.viewing.color` | `colors.viewing` |
| `list.selection.color` | `colors.selection` |
| `display.symbols` | `symbols` |
| `display.reduced_motion` | `reduced_motion` |
| `list.filter` | `default_filter` |
| `repo.root` | `repo_source.root` |
| `repo.command` | `repo_source.command` |
| `repo.branch_field` | `branch_field` |

The Harvest time tracking provider moved out of asanamate into its own
binary, `asanamate-hrvst`, in the same release archive (see [Install](https://github.com/sadmachine/asanamate#install)).
Install it, then run `asanamate config update`: it rewrites
`time_tracking.command = "asanamate time-provider hrvst --task-id <id>"` to
`"asanamate-hrvst --task-id <id>"`, keeping the flags. Until then, asanamate
names the old command and stops. `asanamate doctor` no longer checks hrvst;
run `asanamate-hrvst check` instead.

Default keys changed too: `enter` in the list opens the reader; `c` adds a
comment (was `C`); `y` copies the link (was `c`); `Y` copies the targeted
comment (was `y`); `@` assigns (was `A`); `A` opens the action builder (was
`ctrl+a`); `ctrl+/` opens the filter guide (was `?`), and `esc` in the filter
restores the previous filter. Pickers start in browse mode; press `/` to
search, or set `picker.type_first = true`.

The ccmux agent source moved out of asanamate into the `asanamate-ccmux`
adapter, which ships in the same archive. Install it next to `asanamate` (see
[Agents](https://github.com/sadmachine/asanamate#agents-optional)); `asanamate config update` rewrites the preset:

| v0.1.0 | v0.2.0 |
|---|---|
| `agents.preset = "ccmux"` | `agents.command = "asanamate-ccmux"` |
| `asanamate setup hooks` | `asanamate-ccmux setup` |
| `asanamate hook codex` (Codex hook) | `asanamate-ccmux hook codex` |

Codex hooks that run `asanamate hook codex` keep working in v0.2.0, and
`doctor` warns about them in its `codex_hook` row; v0.3.0 drops that command.
Run `asanamate-ccmux setup` to replace the hook.

New installs get the pager action as `actions/pager.toml.example`, inactive
until renamed. An existing `actions/pager.toml` is left as it is.
