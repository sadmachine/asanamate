# Deferred ideas

Improvements found during the 2026-09-28 code review that were left out of the
cleanup. Each one either adds a feature or rewrites behavior, so it needs its
own design pass. They are recorded here so they can be picked up later.

## Action run steps

**Now:** `pendingRun` in `internal/tui/flow.go` tracks progress with four
flags (`projectChosen`, `awaitingFields`, `inputDone`, `formDone`) and an
empty `branch` string.
`continueRun` and `execute` re-enter each other until every flag is settled.
The flow works, but each new step adds another flag and another re-entry
point.

**Idea:** Model the run as an ordered list of steps: pick agent, pick project,
resolve repo, collect input, fill the action form, load project fields, ask
for a branch when the branch field is empty. Each step either finishes right
away or opens a modal and resumes when it is answered.

**Watch out for:** This is the most intricate flow in the TUI. The tests in
`flow_test.go` and `agents_test.go` cover it and should pass unchanged.

## Multi-enum and people fields from the CLI

**Now:** The TUI can set `multi_enum` and `people` custom fields
(`pickedValues` in `internal/tui/edit.go`). `asanamate field` goes through
`writeback.FieldValue`, which only supports text, number, enum, and date.

**Idea:** Teach `FieldValue` to accept a comma-separated list of option names
or people names and resolve them to gids. Then the CLI and TUI share one
conversion.

**Watch out for:** People names need a workspace user lookup and a rule for
ambiguous names. `CommentHTML` already refuses two users with the same name.
Clearing the field with `""` must keep working.

## Cheaper project list

**Now:** `Client.MemberProjects` loads every unarchived project in the
workspace with its member list, then keeps the ones the user belongs to. In
large workspaces this is slow and moves a lot of data.

**Idea:** Check whether the Asana API has a server-side way to list only the
user's projects, for example through team memberships
(`/users/{gid}/team_memberships`, then `/teams/{gid}/projects`), and compare
the cost.

**Watch out for:** A team-based list may include projects the user can see
but is not a member of, which changes what the project picker shows.

## Shared shell runner

**Now:** `agents.List`, `action.Command`, `repo.Candidates`, and the
`timetracking` provider each build `/bin/sh -c <command>`.

**Idea:** Add one helper that builds the command.

**Why deferred:** It is a one-line duplicate in four places, and a new
package for it costs more than it saves. Revisit if shell handling grows, for
example a configurable shell or Windows support.

## Key sequences and a which-key popup

**Now:** v0.2.0 key bindings take single keys only. The `[keys]` format
reserves a space between two keys (`"g g"`, `"space e"`) for sequences and
rejects it with an error.

**Idea:** Support sequences and a leader key, with a which-key popup that
lists the keys that can follow a pressed prefix. The action menu and the edit
menu already behave like two-key sequences, so they could become the popup
itself. A sample chord layout groups keys by prefix: `g` go, `e` edit,
`v` view, `y` copy, `space` actions, `]`/`[` next and previous.

**Watch out for:** A key that is both a command and a prefix needs a timeout.
Avoid that by never letting a prefix key run a command alone; then the popup
opens at once and esc cancels. Popup group titles would need a new table,
such as `[keys.groups]`. Validation must reject a binding that is a prefix of
another.

## Configurable edit and settings menu keys

**Now:** `[keys]` covers every key handler except the item keys of the edit
menu (`internal/tui/edit.go`) and the settings menu
(`internal/tui/settings.go`), which are fixed.

**Idea:** Add `[keys.edit_menu]` and `[keys.settings_menu]` scopes, one name
per menu item.

**Watch out for:** These are key-select pickers, so item keys win over
`[keys.picker]` navigation. Validation must reject an item key that collides
with another item in the same menu.

## Reader viewport keys

**Now:** Keys not bound in `[keys.main]` reach the reading pane's bubbles
viewport, which uses its own default keymap (`h`/`l` and `left`/`right`
scroll sideways). Most of its other keys are shadowed by `[keys.main]`.

**Idea:** Add a `[keys.reader]` scope that sets the viewport's `KeyMap`, and
drop the keys `[keys.main]` already shadows.

**Watch out for:** `[keys.main]` movement bindings already scroll the reader
when it has focus. Two scopes must not both claim a key while the reader has
focus.

## Picker completion

**Now:** In a picker that accepts typed values (the repo picker), `ctrl+s`
uses the search text and a `Use "<text>"` row offers the same thing.

**Idea:** A `complete` binding (right arrow while the cursor is at the end of
the search text, as in fish) copies the highlighted item into the search
field, so a typed value can start from a match, such as a repo path plus a
subdirectory.

**Watch out for:** Right arrow moves the text cursor while searching. It may
only complete at the end of the text. Only the repo picker benefits today.

## Glamour style files

**Now:** `[colors.markdown]` sets a fixed set of glamour roles (text,
headings, links, code, quotes, rules) on top of the built-in dark or light
glamour style.

**Idea:** Let a theme point to a full glamour JSON style file, such as
`markdown_style = "tokyo-night.json"`, for complete control of the reading
pane's Markdown.

**Watch out for:** `newMarkdownRenderer` overrides several code block fields
after loading the style. Those overrides must still apply on top of a style
file. `[colors.markdown]` would then apply on top of the file.

## Image viewer keys

**Now:** The kitty image viewer (`Viewer.Run` in `internal/kitty/kitty.go`)
reads raw bytes from the terminal, outside Bubble Tea. Its keys are fixed:
`j` and `k` step between images; enter, `q`, esc, and ctrl+c close it.
`[keys]` does not reach it.

**Idea:** Add a `[keys.viewer]` scope and pass the resolved keys to
`NewViewer`.

**Watch out for:** The viewer matches single bytes. Multi-byte keys (arrows,
`shift+tab`, kitty protocol sequences) need a decoder, such as ultraviolet's,
before they can be bound there.

## Saved display state overrides config

**Now:** Toggles from the settings menu and a few keys (separator, header
spacing, reader view, refresh interval) save to `state.Display`, and saved
values win over `config.toml` (`internal/tui/model.go`, `New`). After a
toggle, editing those keys in `config.toml` does nothing, and nothing tells
the user why.

**Idea:** Pick one rule. Either a config change resets the saved value (store
the config value next to it and compare at startup), or settings changes write
to `config.toml` itself, or `asanamate doctor` reports which config keys are
overridden by saved state.

**Watch out for:** Writing to `config.toml` from the TUI must keep comments
and the user's layout, which `Merge` already knows how to do.

## Known issues found during the review

These were already broken before the cleanup:

- `TestRunBackgroundStartsNewSession` in `internal/action` fails under
  `go test -race`. It passes without `-race`. The race is between
  `action.RunBackground` and the test.
