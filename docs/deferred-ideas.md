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

**Now:** `internal/jsoncmd` runs every captured command (providers, sources,
`agents.List`, and `repo.Candidates`) with `/bin/sh -c`, bounded output, and
process-group cleanup, and `internal/editor` builds every editor launch.
`action.Command` still builds `/bin/sh -c <command>` itself, since actions
run in the terminal without capture.

**Idea:** Add one helper that builds the terminal command too.

**Why deferred:** It is a one-line duplicate in one place, and a helper for
it costs more than it saves. Revisit if shell handling grows, for example a configurable
shell or Windows support.

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

**Now:** `[keys]` covers most key handlers. These keys are fixed: the item
keys of the edit menu (`internal/tui/edit.go`), the settings and tools menus
(`internal/tui/settings.go`), and the connection status menu's `r`
(`internal/tui/connection.go`); the comment composer footer's `tab`,
`shift+tab`, and `enter` (`internal/tui/comment.go`); and the multi-select
picker's `ctrl+r`, `ctrl+up`, and `ctrl+down` (`internal/tui/picker.go`);
and the What's new screen's `d` (`internal/tui/whatsnew.go`).

**Idea:** Add `[keys.edit_menu]` and `[keys.settings_menu]` scopes, one name
per menu item, and scopes or names for the other fixed keys.

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

## Places panel layout

**Now:** The Places panel shows in a terminal at least 160 columns wide
(`showNav` in `internal/tui/nav.go`) unless `toggle_sidebar` (`Z`) hid it for
the session. It takes 26 columns from the reader, so `layout.list_width`
leaves the reader less room at 160 columns and up than just below.

**Idea:** Add `layout.views` (bool, default `true`) to the `[layout]` table,
so the panel can start hidden; `toggle_sidebar` would then flip the saved
choice for the session.

**Watch out for:** `navHidden` is session state today; a setting must seed
it at startup and, if the toggle should persist, belong in `settings.toml`
(see `config.SettingKeys`).

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

**Watch out for:** The `reaction_summary` reads in `Client.Stories`
(`internal/asana/activity.go`) and `Client.Story` may change with it.

## Drag the pane divider

**Now:** With `mouse.enabled`, the mouse scrolls, focuses panes, selects
rows, runs statusline hints, and drags to copy text inside a pane, but
cannot resize panes. The list's width
comes from `layout.list_width`, or else from `paneWidths`, which fits ticket
titles and resizes on focus with `layout.focus.resize`. Mouse hit areas are
recorded while drawing (`mouseHits` in `internal/tui/mouse.go`).

**Idea:** Drag the border between the list and the reader to set the list
width. Press on the border, track `tea.MouseMotionMsg` while the button is
held (cell motion mode already reports drags), and set the width on release.

**Watch out for:** Decide whether a dragged width lasts for the session or is
saved, and how it overrides `layout.list_width` and the focus resize; the
reader's minimum width and the Places panel still bound it. Wrapped lines in
the reader rerender at every new width, so redraw on release, not on each
motion event. A press on a panel border starts no text drag today, so the
divider drag can claim it.

## Model state and package-level styles

**Now:** `tui.Model` has about 90 fields. The active palette's styles
(`internal/tui/styles.go`) and the key bindings (`keys` and `typeFirst` in
`internal/tui/hints.go`) are package variables that `New` sets, documented as
safe because a process runs one `Model`; a few styles, such as
`accentStyle`, live on `Model` instead. Tests that change the palette or keys
therefore depend on order and cannot run in parallel.

**Idea:** Move styles and bindings into one struct owned by `Model`, and
group related `Model` fields (loading, edit flow, layout) into named structs.

**Why deferred:** It touches nearly every file in `internal/tui` and changes
no behavior, so it is a large diff with no user-visible gain. Revisit when a
second `Model` per process is needed, or when parallel TUI tests are worth it.

## Pull requests in the Feed panel, list rows, and statusline

**Now:** Pull request providers (`pull_requests.commands`) feed the reader's
Pull requests section and the `pr:`, `checks:`, and `review:` filter terms.
The Feed panel shows only `pull requests failed` in its summary when a
provider fails.

**Idea:** Three small follow-ups on the same data: a pull request source in
the Feed panel (such as checks failing or changes requested on a loaded
ticket), a badge on list rows with an open pull request, and a statusline
count of pull requests that need you.

**Why deferred:** Spec 24 kept to the protocol, the adapters, filtering, and
the reader. Each follow-up is small once the data exists, and needs its own
choice of what is worth showing.
