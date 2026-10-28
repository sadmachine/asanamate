# Deferred ideas

Improvements found during the 2026-09-28 code review that were left out of the
cleanup. Each one either adds a feature or rewrites behavior, so it needs its
own design pass. They are recorded here so they can be picked up later.

## Cheaper project list

**Now:** `Client.Projects` loads every unarchived project in the workspace
with its member list. The TUI keeps the ones the user belongs to for project
navigation, caching them in state, and offers all of them in the Add to
project picker. In large workspaces this is slow and moves a lot of data.
The cache lets navigation open at once, and the load runs in the background
once per session, but the request costs the same.

**Idea:** Asana has no server-side filter for the user's own projects:
`GET /projects` has no member filter, and `GET /memberships` needs a project
`parent` for user memberships. A team-based list
(`/users/{gid}/team_memberships`, then `GET /memberships` with each team and
`resource_subtype=project_membership`) moves far less data for navigation.
The Add to project picker still needs every accessible project, so it would
keep a full load, made only when the picker opens.

**Watch out for:** A team-based list includes team projects the user is not a
member of and misses projects shared with the user directly, which changes
what project navigation shows.

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

## Settings follow-ups

- Add theme picker actions to use the highlighted theme as the dark or light
  member of `theme.auto`.
- Add `asanamate config settings` to open `settings.toml` in the user's editor.

## Views panel layout

**Now:** The views panel shows whenever the reader sits beside the list in a
terminal at least 160 columns wide (`showNav` in `internal/tui/nav.go`). It
takes 26 columns from the reader, so `layout.list_width` leaves the reader
less room at 160 columns and up than just below.

**Idea:** Add `layout.views` (bool, default `true`) to the `[layout]` table,
and a session toggle key next to `toggle_reader`. With it off, the list and
reader split the full width.

**Watch out for:** `readerShown` in `internal/tui/model.go` subtracts the
panel's width itself to avoid calling `showNav`, which depends on it; a new
setting must change both. Focus must leave the panel when it hides, as
`layout()` already does on resize.

## Recheck the reactions write

**Now:** `asana.Client.SetReaction` reacts to a comment with
`PUT /stories/{gid}` and `{"reactions": [{"emoji": ..., "reacted": ...}]}`.
Asana's OpenAPI spec does not list this field: as of 2026-10-08, `StoryRequest`
accepts only `text`, `html_text`, `is_pinned`, and `sticker_name`. It was found
by testing, after Asana's forum post "[Upcoming] Adding emoji reactions to the
API" described an experimental rollout. Asana ignores unknown fields with a
200, so `writeback.Service.React` reads the comment back and returns
`ErrReactionIgnored` when nothing changed.

**Idea:** Check the Asana API reference and changelog for a documented
reactions write (a field on story update or a `/reactions` endpoint). If one
exists, switch `SetReaction` to it. If reactions now fail with
`ErrReactionIgnored`, find the new request shape or remove the feature.

**Watch out for:** The `reaction_summary` reads in `Client.Comments` and
`Client.Story` may change with it.

## Known issues found during the review

These were already broken before the cleanup:

- `TestRunBackgroundStartsNewSession` in `internal/action` fails under
  `go test -race`. It passes without `-race`. The race is between
  `action.RunBackground` and the test.
