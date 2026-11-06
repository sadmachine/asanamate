# 11: Feed panel and the feed protocol

**Depends on nothing.**

## Goal

A Feed panel under the ticket list shows, at a glance, what happened today
and what is coming up, across Asana, coding agents, local git work, and any
external tool. External tools add to it
through one generic feed protocol, so the panel is agnostic to Harvest,
ccmux, Hub, or any future plugin.

## Context

- `docs/extensions.md`: the existing extension points. Time tracking only has
  per-ticket operations (`form`, `log`, `summary`), project links only answer
  per-project lookups, and agent sources only list running agents. None can
  supply a day-wide feed, and adding a day operation to each would change one
  contract per kind of content.
- `internal/tui/activity.go`: the background Activity scan and its disk
  cache. It already knows today's related Asana events.
- `internal/tui/statusline.go:114` and `internal/tui/nav.go:156`: the footer
  and the Views panel already count linked agents by state, and list rows show
  each ticket's agent badge. `linkedAgents` keeps only agents linked to a
  loaded ticket, so agents on other branches never show in the TUI.
- `internal/tui/listrow.go` (`ticketAgents`) and `agents.MatchAll`: how an
  agent links to a ticket, by the ticket's branch (`action.Branch`) and its
  linked repos. Feed items and the git source link the same way.
- `internal/timetracking` and `internal/projectlinks`: the JSON-over-stdin
  protocol pattern, with fixture-script tests under `testdata/`. Copy it.
- `cmd/asanamate-hrvst`: the Harvest provider, a separate binary.

## Requirements

### Layout

- A bordered `[3] Feed` panel sits under the `[1] Tickets` panel, the same
  width. It is always present, in one of two heights:
  - **Collapsed:** its two border rows only. The top edge carries the
    sources' summaries, such as `[3] Feed · 3h 10m logged · 4 new`.
  - **Expanded:** `feed.height` rows. Items timed after now (meetings,
    reminders) come first, soonest first, under a `Next (n)` group header;
    the rest follow newest first under `Earlier (n)`. The headers use
    `list.header.style`, like the list's, and show only when there are
    upcoming items. Upcoming times show as `in 25m`.
- With `feed.expand = "auto"`, the panel expands when the list leaves at
  least `feed.height` empty rows, and collapses otherwise. `F` toggles it and
  overrides auto until the view changes. `3` focuses it, expanding it first.
- The height is decided when a view loads, when the terminal resizes, and on
  `F`, never on a background refresh, so the list does not jump.
- In the focused panel, `j`/`k` move, `enter` opens the item's ticket in the
  reader (or its `url` when it has no ticket), and `esc` returns to the list.

### Built-in sources

- **asana:** today's related events from the Activity cache: comments,
  assignments, completions, and due date changes on the user's tickets. Its
  summary is the count of new events, such as `4 new`.
- **agents:** no summary, since the footer already counts agents. Items are
  what the footer cannot show:
  - **State changes,** with when they happened: `mocpa · completed · 22m`.
    asanamate records the time it first sees each agent in each state. Agents
    already running at the first poll show no time, since asanamate did not
    see them change.
  - **Agents linked to no loaded ticket,** once each, so agents on other
    branches are visible somewhere: `cpa-tech · feature/php-hiraeth · idle ·
    no ticket`.
- **git:** forgotten local work on ticket branches. For each loaded ticket
  with a branch, asanamate finds the worktree checked out on it in the
  ticket's linked repos (`git worktree list --porcelain`), then reports:
  - **Uncommitted changes:** `3 uncommitted files · <ticket title>`, timed at
    the newest changed file's modification time, in the warn tone.
  - **Unpushed commits:** `2 unpushed commits · <ticket title>`, timed at
    the newest unpushed commit, in the warn tone. A branch with no upstream
    reports `not pushed` instead.

  Its summary counts affected branches, such as `2 unpushed`. It runs on
  `feed.refresh_interval`, with each git command bounded by the feed
  timeout. A worktree git cannot read is skipped, not reported.

### Feed protocol

Add a "Feeds" section to `docs/extensions.md` and a row to its table.

- **Config:** `feed.commands`, a list of commands. Each runs as a new process
  when the TUI starts, then every `feed.refresh_interval`, while the TUI is
  open. `asanamate doctor` runs each once.
- **Input:** one JSON object on stdin:
  `{"version":1,"operation":"feed","date":"2026-10-12"}`. `date` is today's
  local date. Compatibility rules match time tracking: providers may ignore
  `version` and must ignore unknown fields.
- **Output:** one JSON object on stdout:

  ```json
  {"source":"hrvst","summary":"3h 10m logged","items":[
    {"time":"2026-10-12T14:05:00-04:00","text":"1.5h · Fuse: Internal Review",
     "ticket_gid":"1218987517194396","tone":"ok"}]}
  ```

  - `source`: required, 1 to 8 characters; the panel's right column.
  - `summary`: optional, cut to 24 characters, joined into the top edge.
  - `icon`: optional, added after this spec; see `docs/extensions.md#feeds`.
  - `items`: required, may be empty. Each needs `time` (RFC 3339) and
    `text` (one line: control characters become spaces, cut to fit). A `time`
    after now is an upcoming item. `tone` is `ok`, `warn`, `error`, `muted`,
    or empty, and maps to theme roles, never raw colors.
  - Linking, all optional, so tools that do not know Asana can still link:
    `ticket_gid`; `url`, which links when it is an Asana task URL and
    otherwise is what `enter` opens; and `branch`, with an optional `repo`,
    which link like an agent: to the loaded ticket on that branch, limited to
    tickets linked to `repo` when it is set. The first that names a loaded
    ticket wins, in that order; with several tickets on one branch, the first
    in list order wins. An item that links to nothing still shows.
- **Exit status:** non-zero or invalid output marks that source failed: its
  summary shows `<source> failed` in the muted color, and its previous items
  stay. Other sources are unaffected.
- **Timeout:** 1 minute per run; the command is killed after that.
- **Test:** a fixture script under `internal/feed/testdata/` through the real
  code path, as the other protocols do.

### Harvest

- `asanamate-hrvst` answers the feed protocol too, telling the requests apart
  by `operation`, so feeds need no flag or task ID: one item per time entry
  logged on `date` (hours, Harvest project, and notes), linked to its Asana
  ticket the same way `summary` matches entries, and a summary of the day's
  total, such as `3h 10m logged`.
- ccmux and Hub need no change. Either can add a feed command later.

### Likely feeds

Not part of this task; they show the contract is general enough. Each fits
the item shape without changes:

- **Pull requests and CI:** review requests, review comments, failed runs,
  linked by `branch` and `repo`.
- **Deploys:** finished or failed deploys per site, linked by `branch`.
- **Calendar:** today's meetings as upcoming items, unlinked.
- **Messages:** Slack or email mentions carrying an Asana task link, linked by
  `url`.

### Config

New `[feed]` table. Follow the AGENTS.md key checklist.

| Key | Default | Values |
|---|---|---|
| `feed.expand` | `"auto"` | `auto` (expand when the list leaves room), `collapsed`, `expanded` |
| `feed.height` | `12` | rows when expanded, borders included, at least `3` |
| `feed.refresh_interval` | `"1m"` | as `activity.refresh_interval`; `"0"` runs feeds only at start and on reload (`r`) |
| `feed.commands` | `[]` | feed commands |

### Keys

`F` toggles the panel and `3` focuses it, through `internal/keymap/catalog.go`.

### Docs

README: a Feed panel section, the config rows, the keys, and the "What it
does" list. `docs/extensions.md`: the Feeds section.

## Out of scope

- Charts, tables, or any widget beyond a list of lines.
- Totals across sources; each source sums its own in `summary`.
- Writing anything from the panel.

## Acceptance

- Collapsed, the panel is two border rows under the list, with
  `[3] Feed` at the left of its top edge and the summaries and `F expand`
  at the right. Expanded, each row is the item's time (muted), its text in
  its tone with the ticket title in the plain text color, and its source
  (muted) at the right edge.
- A feed fixture's items and summary appear; a failing fixture shows
  `<source> failed` and keeps its previous items.
- Fixture items link by `ticket_gid`, by an Asana task `url`, and by
  `branch` with and without `repo`; an item with a non-Asana `url` opens it.
- An upcoming fixture item shows under `Next (1)` as `in <duration>`, and
  without upcoming items the panel shows no group headers.
- A ticket worktree with an uncommitted file and an unpushed commit shows
  both git items; a clean one shows none.
- `enter` on an item with `ticket_gid` opens that ticket in the reader.
- An agent outside every loaded ticket appears once as `no ticket`.
- A background refresh never changes the panel's height.
- `go test ./...` and `go vet ./...` pass.
