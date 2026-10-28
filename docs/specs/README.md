# Roadmap specs

asanamate aims to be a full-featured, extensible Asana TUI. It should cover
the Asana work an average user does every day, run well in any terminal layout
(full screen, tmux pane, sidebar, or popup), and let external commands extend
it. The CLI should offer every write the TUI offers, so scripts and agents can
do what a person can.

Each file below is one self-contained task, numbered in the order to run
them. A task can start once the tasks in its "Depends on" column are merged
into `main`; tasks with no dependency between them (such as 02, 03, and 04)
can run in parallel, each on its own branch.

| Spec | Summary | Depends on | Breaking |
|---|---|---|---|
| [01-cli-parity](01-cli-parity.md) (done) | `assign`, `due`, and `project` subcommands; route TUI writes through `writeback` | none | no |
| [02-complete-tickets](02-complete-tickets.md) (done) | Mark tickets complete or reopen them, in the TUI and the CLI | 01 | no |
| [03-create-tickets](03-create-tickets.md) (done) | Create tickets and subtasks, in the TUI and the CLI | 01 | no |
| [04-layout-config](04-layout-config.md) (done) | `[layout]` table and a reader toggle key | none | no |
| [05-extension-protocols](05-extension-protocols.md) (done) | Document and test the external command protocols as the extension contract | none | no |
| [06-extract-hrvst](06-extract-hrvst.md) (done) | Move the Harvest provider out of the main binary | 05 | yes |
| [07-extract-agent-adapter](07-extract-agent-adapter.md) (done) | Move the ccmux, Claude, and Codex agent code into an adapter | 05 | yes |
| [08-readme-mission](08-readme-mission.md) (done) | Rewrite the README intro around the project's goal and scope | 02, 03, 05 | no |
| [09-activity-background-cache](09-activity-background-cache.md) (done) | Scan Activity in the background, cache it on disk, and show a new-event count | none | no |
| [10-portability-release](10-portability-release.md) (done, Homebrew pending) | Released as v0.2.0. **Homebrew is still to do** (plan Task 6); see the [implementation plan](10-portability-release-plan.md) | current main baseline | no |
| [11-feed-panel](11-feed-panel.md) (done) | Feed panel under the list, fed by Asana, agents, local git, and a generic feed protocol | none | no |
| [12-startup-config-migration](12-startup-config-migration.md) (done) | The TUI offers `config update` at startup when the config uses moved settings | none | no |
| [13-settings-file](13-settings-file.md) (done) | Settings the TUI changes move from `config.toml` and state into `settings.toml`; `[theme.auto]` and `ansi-dark`/`ansi-light` | 12 | yes |
| [14-default-view](14-default-view.md) (done) | `default_filter`, `list.group_by`, and `list.sort` move to `[list.default_view]` | 12, after 13 | yes |
| [15-remove-deprecated](15-remove-deprecated.md) (done) | Remove the hidden `hook` subcommand and `--no-preview` | none | yes |
| [16-conversation-history](16-conversation-history.md) (done) | Generic ticket conversation source, metadata, config, and open contract | none | no |
| [17-conversation-companion](17-conversation-companion.md) (implemented, gate pending) | Standalone `asana-sessions` package/CLI for native history, linking, and resumption | 16's adapter contract only; provider feasibility gate first | no |
| [18-conversation-plugins](18-conversation-plugins.md) (implemented, release gate pending) | `asana-sessions` native hook plugins, skill, installation, and independent releases | 17 | no |
| [19-conversation-ui](19-conversation-ui.md) (done) | Native ticket conversation picker, CLI, foreground opening, and end-to-end proof | 16, 17 | no |
| [20-extension-discovery](20-extension-discovery.md) (done) | Shared CLI/TUI extension catalog, guided setup, contract documentation, and readiness checks | none | no |
| [21-native-harvest](21-native-harvest.md) (implemented, native live gate pending) | Direct Harvest API, independent credentials, task selection, and compatible legacy setup | existing protocols; independent of 20 | no |
| [22-hooks](22-hooks.md) | `[[hooks]]` run a prompt or action on ticket open, assign, complete, or new activity; replaces `time_tracking.prompt_after` | none | yes |

Specs 12 to 15 are complete and shipped together in v0.3.0.

Specs 16 to 19 describe a separate conversation feature. All four are
implemented; CI checks a packed `asana-sessions` against core's CLI. The
remaining gates are spec 18's exact-main CI run and standalone release
checks. The standalone
`asana-sessions` package lives in
`packages/asana-sessions/` in this repository while it is built out; moving
to its own repository is a later decision. It has its own CLI, README,
tests, package version, and releases, and works without asanamate.
Specs 17–18 can ship before the core integration in 16/19, after their
provider feasibility and standalone acceptance checks. The integrated
feature requires all four acceptance lists. Spec 16 owns shared terms,
the portable ticket marker, core configuration, and extension contracts.
The optional Node package/plugins add no runtime dependency to the main Go
binary. Only a thin adapter knows asanamate's protocol and environment;
core/package compatibility is not tied to matching release numbers.

Specs 20 and 21 can ship independently. When both are present, 21 updates
the Harvest catalog entry from 20 to use native setup and checks.
Spec 20 also covers discovery of specs 16–19's conversation extension,
with availability following the independent core, helper, and hook gates.

## Rules for every task

- **Read `AGENTS.md` first.** Its config key rules apply to every new key:
  struct field and doc comment, `Default()`, `validate()`, the template in
  `internal/setup/setup.go`, the README config table, and the config tests.
- **New key bindings** go in `internal/keymap/catalog.go`, so they are
  configurable, show in `?` help, and pass `TestKeyBindingsAreUniqueAndDocumented`.
  Each spec sets its default keys, and no two specs share one: `x` complete
  (02), `n` create and `N` create subtask (03), `z` toggle reader (04). Use
  them as given; if one is taken on `main` when you start, stop and ask the
  user.
- **New writes** go through `writeback.Service`, so the TUI and the CLI share
  one implementation (01 sets this up).
- **Shared files.** Several tasks add a case to the subcommand switch in
  `cmd/asanamate/main.go`, a line to its `usage` text, and rows to the README
  Keys table. Expect small merge conflicts there; keep each addition to its
  own lines to make them easy to resolve.
- **Tests.** The packages you touch have tests. Add tests next to the existing
  ones, and run `go test ./...` and `go vet ./...` before you finish. CI also
  runs staticcheck and the race detector.
- **Docs.** Update the README section for every user-visible change, and the
  "What it does" list when a feature is added or removed.
- **Breaking changes** (06, 07) need the user's approval before you start.
  Follow "Approved breaking changes" in `AGENTS.md`.
- **Done** means the acceptance list in the spec passes. Then mark the task
  done in the table above.

## Open questions

- **Action builder or schema.** The action builder (`A`) guides users through
  action files. A JSON Schema for action files would give completion and docs
  in any TOML editor with less code to maintain. No decision yet; no spec.

Spec 21 has passed local protocol, setup safety, and failure-path tests, plus a
read-only check against an untouched real legacy login. Fresh native setup still
needs manual verification with a personal access token. No live time entry has
been created for validation.
