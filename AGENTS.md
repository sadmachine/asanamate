# AGENTS.md

## Config keys

The config is TOML, decoded into the structs in `internal/config/config.go`.

- **Shape:** before adding a key, judge whether its concept will grow more
  settings. A growing concept gets its own sub-table, even with one key today
  (`[list.header]` holds `style`, `spacing`, `color`). A standalone setting
  stays a snake_case key in its parent table (`list.group_by`). When unsure,
  ask the user.
- **Every key change touches:** the struct field and its doc comment,
  `Default()`, `validate()` (errors name the dotted key, such as
  `list.header.style`), the template in `internal/setup/setup.go`, the README
  config table, and the config tests.
- **Released keys are a public contract:** `Load` rejects unknown keys, and
  `asanamate config update` (`Merge` in `internal/setup/update.go`) fails on
  keys the template lacks, so renaming, moving, or removing a released key, or
  narrowing its accepted values, breaks existing configs. Before the first
  release, change keys freely. After it, add keys and values rather than
  changing them, and make a breaking change only with the user's approval.
- **Approved breaking changes:** teach `Merge` to move the old key to the new
  one, mark the commit breaking (`feat!:` plus a `BREAKING CHANGE:` footer
  naming the old and new keys), and tell the user the next release tag needs a
  breaking version bump. Releases come only from `v*` tags; there is no version
  file. Under semver that means the minor version while on `v0`, the major from
  `v1` on. A `v2` or later tag also requires the module path in `go.mod` to end
  in `/v2`.

## Local config migration

When a task changes what lives in `~/.config/asanamate` (a config key or
value, an action key, a new file, or a new layout), finish the task, then ask
the user whether to migrate their local config to it. On yes:

- Copy each file you will change to a `.bak` sibling first.
- Bring `config.toml` and `actions/` onto the new shape, keeping every value
  the user set. `asanamate config update` handles new template keys in
  `config.toml`; everything else is a hand edit.
- Validate with the branch's build: `go run ./cmd/asanamate doctor` loads the
  config and actions and names the first bad file or key. Done when it runs
  clean; then report what changed and where the backups are.
