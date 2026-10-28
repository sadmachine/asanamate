# 22: Hooks

## Goal

Users automate their own workflow without asanamate hardcoding it. A hook
runs a built-in prompt or an action when something happens to a ticket, for
tickets that match a filter, optionally only once per ticket.

Two cases drive the design:

- **Ask for a branch once.** When a ticket opens in the reader and it has no
  branch, ask for one. Whatever the answer, including `esc`, never ask again
  for that ticket.
- **React to new activity.** When a scan finds new related activity on a
  matching ticket, run a background action, such as opening a tmux window.

`time_tracking.prompt_after` is already a hardcoded hook: it opens the log
time form after an assign or complete. It becomes the first two hooks and the
key goes away.

## Decisions

These were agreed with the user on 2026-10-21.

- **The name is `hooks`.** It is the standard name for running your own code
  when something happens in a tool (git, tmux, Claude Code, Codex). "Hook"
  already means Claude Code and Codex hooks in asana-sessions and the README,
  so asanamate docs always qualify those ("Claude Code hook", "Codex hook")
  and `CONTEXT.md` defines **Hook** for asanamate.
- **A hook is `on`, `when`, `once`, and `run`.** `when` reuses the list
  filter language instead of a new condition syntax.
- **`ticket_open` is explicit.** It fires when the reader gains focus on a
  ticket: `Enter` (`open`), `2` (`focus_reader`), or `tab`/`l` (`next_pane`)
  landing on the reader. Moving the list selection never fires it. It is
  defined by the focus change, not by keys, so rebinding keys keeps it
  working.
- **No refresh or selection events.** Feeds already cover refresh, and
  firing on selection makes browsing run commands.
- **Replace `time_tracking.prompt_after` (breaking, approved).** It shipped
  in v0.3.1. The config update pipeline rewrites it as hooks, so the startup
  prompt and `asanamate config update` cover it. Pre-v1, so this makes the
  next release a minor bump.
- **Setup still never writes core config** (spec 20). The Harvest guide and
  `asanamate-hrvst setup` print the log time hooks for the user to paste,
  as they already do for `[time_tracking]`.
- **Hooks live in `config.toml`.** The TUI does not edit them, so they stay
  out of `settings.toml` and state. Only `once` records go to state.

## Depends on

Nothing. Specs 20 and 21 are merged; this spec edits their Harvest catalog
entry and setup output.

## Config

```toml
# Ask for a branch the first time an open ticket without one is focused.
[[hooks]]
on = "ticket_open"
when = "is:open branch:none"
once = true
run = "edit:branch"

# What time_tracking.prompt_after = ["assign", "complete"] becomes.
[[hooks]]
on = "assign"
when = "-assignee:@me -assignee:none"
run = "log_time"

[[hooks]]
on = "complete"
run = "log_time"

# Open a tmux window for new activity on tagged tickets, once per ticket.
[[hooks]]
on = "new_activity"
when = "assignee:@me tag:agent"
once = true
run = "action:tmux-open"
```

| Key | Default | Meaning |
|---|---|---|
| `hooks[].on` | required | `ticket_open`, `assign`, `complete`, or `new_activity` |
| `hooks[].when` | `""` (every ticket) | list filter query, checked against the event's ticket |
| `hooks[].once` | `false` | run at most once per ticket, remembered across restarts |
| `hooks[].run` | required | `log_time`, `edit:<item>`, or `action:<file>` |

### Events

All events are TUI only. CLI writes such as `asanamate complete` raise none,
as `prompt_after` never prompted after them.

- **`ticket_open`**: the reader gains focus on a ticket, or a ticket opens in
  the reader: `Enter` in Activity or the Feed panel, ticket history, ticket
  search, an Asana task link, or the jump menu. `when` is checked against the
  ticket as loaded. With the reader hidden (`z`), nothing opens and it does
  not fire. Opening the comment composer also focuses the reader, but does
  not fire it: you are already acting on that ticket.
- **`assign`**: a TUI assignment succeeded, from the edit menu, `Edit
  multiple`, or a comment draft's changes. `when` is checked against the
  ticket with the new assignee applied, so unassigning and assigning to
  yourself can be filtered out.
- **`complete`**: a TUI write completed a ticket. Reopening never fires it.
- **`new_activity`**: an Activity scan added a related event to the cache:
  not your own (`Mine`), not automatic, and not from the scan that seeded an
  empty cache (`FoundAt` is zero). Each added event fires once. `when` is
  checked against the event's ticket. With `activity.refresh_interval = "0"`
  scans run only when Activity opens, so this fires only then.

### Targets

- **`log_time`**: the log time form, as the `log_time` key (`t`) opens it.
  Needs `[time_tracking]`.
- **`edit:<item>`**: an edit menu item that asks for a value: `title`,
  `description`, `section`, `field`, `assign`, `due`, `add_project`,
  `remove_project`, or `branch`. `complete` and `multiple` are not targets:
  one writes without asking, the other is a menu.
- **`action:<file>`**: the action in `actions/<file>.toml`, run as if picked
  from the action menu: its repo, agent, form, and input steps, then its
  `mode`.

### Rules

- **Order.** Matching hooks run in config order. All matching background
  actions run. At most one interactive target opens per event: `log_time`,
  `edit:*`, or an action with a form, input, repo or agent choice, or
  `foreground` or `exit` mode. Later interactive hooks for that event do not
  run and are not marked `once`.
- **One write, one prompt.** A single write that raises both `assign` and
  `complete` (a draft or `Edit multiple`) opens the same target at most once,
  as `prompt_after` does today.
- **No loops.** An edit that a hook opened raises no hook events.
- **`once`.** Recorded when the target opens or the action starts, so `esc`
  counts as answered. Records are keyed by the hook's `on`, `when`, and `run`,
  so editing a hook starts it fresh. Nothing prunes them in this spec.
- **Busy.** An interactive target never opens over an open modal, input
  box, or comment composer. It does not run and is not marked `once`, so it
  asks next time.
- **`new_activity` runs nothing interactive.** Its targets must be actions
  with `mode = "background"` and no form (`form.fields` or `form.command`),
  `input`, `repo`, or `agent`. Prompts appearing for activity you did not
  open would interrupt work.
- **Loading details.** If a target needs ticket details that are still
  loading, it waits for them, and drops if focus moves to another ticket
  first.
- **Actions see the event.** `ASANAMATE_EVENT` holds the event name for
  hook runs and is empty from the action menu. For `new_activity` on a
  comment, the `ASANAMATE_COMMENT_*` variables describe that comment.

## Filter terms

Hooks need conditions the filter cannot express today. They are added to the
list filter too, since they are useful there.

| Term | Matches |
|---|---|
| `assignee:@me` | tickets assigned to you |
| `assignee:none` | unassigned tickets |
| `branch:any` | a branch saved for the ticket (`Set branch`) or a `branch_field` value |
| `branch:none` | neither, so the branch falls back to the ID field or title slug |

`assignee:` otherwise keeps matching names by substring. `@` never appears in
a name, so `@me` cannot clash with one. `none` follows `due:none` and
`agent:none`; a person whose name contains "none" now needs another
substring.

## Context

`main` moves fast, so these name functions rather than line numbers.

- `internal/config/config.go`: `TimeTracking.PromptAfter`, `PromptsAfter`,
  the `time_tracking.prompt_after` check in `validate()`, and
  `Action`/`loadActions`. Actions do not keep their file name today;
  `action:<file>` needs it. `Action.Form` is an `ActionForm`, either static
  `fields` or a `command` that returns them.
- `internal/config/updates.go`: the ordered `updates` pipeline that both
  `PendingUpdates` and `ApplyUpdates` run. `cloneTable` copies tables but
  shares arrays, so detection must not modify the user's `hooks` array in
  place.
- `internal/setup/update.go`: `Merge`. `headerLine` matches `[table]` only,
  and keys the template has no line for fail with "the template has no place
  for". It has never had to carry an array of tables.
- `internal/tui/keys.go`: `focusReaderPane`, which already calls
  `recordViewed`, is the one place the reader gains focus. Callers: `open`,
  `focus_reader`, `cyclePane` (for `next_pane`), the jump menu
  (`internal/tui/jumpmenu.go`), and the comment composer
  (`internal/tui/comment.go`), which must not raise the event.
- `internal/tui/history.go`: `showTicket` calls `focusList` then
  `focusReaderPane`, so it refocuses even when the reader had focus.
  Activity, the Feed panel, and loaded details (`internal/tui/model.go`) call
  it; ticket history, ticket search (`internal/tui/ticketsearch.go`), and
  Asana task links (`internal/tui/flow.go`) go through `openTicket`, which
  loads an unknown ticket first and shows it when it arrives.
- `logTime` comes from `toggleComplete` and `pickedUser`
  (`internal/tui/edit.go`), `draftLogTime` (`internal/tui/batch.go`), and
  `logAfter`/`c.logTime` in `internal/tui/comment.go`. `editDoneMsg.logTime`
  (`internal/tui/cmds.go`) reaches `editDone` (`internal/tui/model.go`),
  which opens the form.
- `internal/tui/edit.go`: the edit menu items (`openEditMenu`) and
  `pickedEdit`.
- `internal/tui/activity.go`: `gotActivity` merges scan results into the
  cache. `internal/activity/activity.go`: `Entry`, with `FoundAt` zero for
  seeding scans, and `NewSince`.
- `internal/filter/filter.go`: `Match(t, agentStates, today)`; `Parse` turns
  unknown keys into title words, and `project:` sections now match exactly,
  with `[a|b]` lists. Callers: the list filter in `internal/tui/model.go`
  and `runList` in `cmd/asanamate/main.go`, which passes no agent states.
- `internal/action/action.go`: `Branch` and `fieldBranch`.
- `internal/state/state.go`: `TaskBranches`; add `once` records beside it.
- `internal/extensions/catalog.go`: the Harvest entry's `Steps` and `Snippet`.
  `cmd/asanamate-hrvst/setup.go`: `setupHarvest`'s printed config.

## Requirements

### Config

- Add `Hooks []Hook` with `On`, `When`, `Once`, and `Run`, each with a doc
  comment. `Default()` has none.
- `validate()` names the entry, first entry `hooks[1]`:
  - `hooks[2].on: "refresh" must be one of ticket_open, assign, complete,
    new_activity`
  - `hooks[1].run` must be `log_time`, a listed `edit:` item, or
    `action:<file>` naming a loaded action.
  - `log_time` without `[time_tracking]` fails.
  - `new_activity` with an action that is not background-only fails, naming
    the action file.
- Keep each action's file name (without `.toml`) on `Action`.
- Remove `PromptAfter`, `PromptsAfter`, `PromptAssign`, `PromptComplete`,
  and their validation.

### Migration

- Add an update to the `updates` pipeline. For `time_tracking.prompt_after`,
  it reports `time_tracking.prompt_after was replaced by [[hooks]]` and
  appends, in this order, the `assign` hook for `"assign"` and the `complete`
  hook for `"complete"`, as in the example above. An empty list just
  disappears. Existing hooks stay first.
- `Merge` carries `[[hooks]]`: it writes the user's hooks as `[[hooks]]`
  blocks after the template's tables, keeping values and order, and its
  `contains` check covers arrays of tables.
- `docs/migrations.md`, under `## Unreleased` (add it above the newest
  release heading if missing): the before and after TOML, and that
  `asanamate config update` does it.

### TUI

- Raise events at one place each:
  - `ticket_open` from the reader focus change and the paths that open a
    ticket into the focused reader.
  - `assign` and `complete` from the code that sets `logTime` today, which
    instead builds the events for the write.
  - `new_activity` from `gotActivity` for entries the merge added.
- One runner matches hooks, applies the rules above, records `once` in
  state, and opens or runs the target. `editDone` loses its `logTime` path.
- `edit:<item>` reuses `pickedEdit`.
- `action:<file>` reuses the action menu's run path, setting
  `ASANAMATE_EVENT`.

### Filter

- `Match` and `Apply` take the extra inputs the new terms need: your user
  GID and each ticket's explicit branch. The TUI passes both. `asanamate
  list` fetches the user only when the query uses `assignee:@me`, and reads
  saved branches from state.
- Update the package comment and the README filter list.

### Setup and docs

- Template (`internal/setup/setup.go`): remove the `prompt_after` lines.
  Add a commented hooks section with the branch and log time examples, and
  under `[time_tracking]` point to it.
- Harvest catalog entry and `asanamate-hrvst setup` output: after the
  `[time_tracking]` snippet, print the two log time hooks as optional.
- README: a Hooks section (events, targets, rules, examples), config table
  rows for `hooks[].*`, the `prompt_after` row removed, and the time tracking
  paragraph that shows `prompt_after` rewritten to show the hooks. Qualify any bare "hook"
  that means a Claude Code or Codex hook.
- `docs/extensions.md`: `ASANAMATE_EVENT` in the action variables table.
- `CONTEXT.md`: add **Hook** ("a config entry that runs a built-in prompt or
  an action when an event happens to a ticket"), and under _Avoid_, bare
  "hook" for Claude Code or Codex hooks.
- `docs/specs/README.md`: add this spec's row (breaking).
- Commit as `feat!:` with a `BREAKING CHANGE:` footer:
  `time_tracking.prompt_after` is replaced by `[[hooks]]` entries with
  `run = "log_time"`.

## Out of scope

- Hooks for CLI writes, other edits (due, section, field), or refresh.
- Running hooks while the TUI is closed. Hooks fire only in a running TUI;
  two open TUIs both fire `new_activity` hooks.
- Pruning or resetting `once` records, and editing hooks in the TUI.
- Shell commands as targets other than through action files.

## Acceptance

- Config tests: hooks load; each `validate()` error above names its entry;
  `prompt_after` gives `UpdateNeeded`.
- Setup tests: `Merge` rewrites `prompt_after = ["assign", "complete"]` as
  the two hooks, keeps existing `[[hooks]]` before them in order, and
  round-trips a config with hooks unchanged.
- Filter tests for all four new terms, negated too, and that
  `assignee:<name>` still matches by substring.
- TUI tests:
  - The migrated hooks reproduce today's behavior: assigning to someone else
    and completing open the time form; unassigning, assigning to yourself,
    and reopening do not; a draft that assigns and completes opens it once.
  - `ticket_open` with the branch hook: `2`, `tab`, and `Enter` open the
    branch input on a branchless ticket; moving the list selection does not;
    after `esc`, refocusing the same ticket does not ask again, including
    after a restart. Opening a ticket from Activity, the Feed panel, ticket
    search, or an Asana task link fires it too. Starting a comment does not,
    and a hook skipped because a modal or the composer was open asks next
    time.
  - An edit opened by a hook raises no events.
  - `new_activity` runs a background action for an added related event, not
    for your own, automatic, or seeding-scan events.
- `asanamate list --filter "assignee:@me branch:none"` works.
- After migration, the local `~/.config/asanamate` passes `asanamate doctor`.
- `go test ./...` and `go vet ./...` pass.
