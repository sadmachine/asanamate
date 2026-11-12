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

## Groupings directory

**Now:** Rule groupings live in `config.toml` as `[[groupings]]` tables,
released in v0.4.5. The built-in `state` and `now` groupings are in
`builtinGroupings` (`internal/config/groupings.go`) and appear commented out
at the end of the generated `config.toml`. `asanamate config update`
rewrites `config.toml` from the template, so comments a user adds to a
grouping are lost.

**Idea:** Load groupings from `~/.config/asanamate/groupings/*.toml`, one
grouping per file named for the grouping (`now.toml`), like `actions/` and
`themes/`. Setup writes `state.toml.example` and `now.toml.example` with the
built-in sections; renaming one to `.toml` replaces that built-in.
`config update` never rewrites these files.

**Why deferred:** It needs a directory loader, duplicate-name checks, and
setup, README, and migration work. Do it before v1.0.0: removing
`[[groupings]]` from `config.toml` is a breaking change, which costs a minor
bump now and a major one after v1.0.0.

**Watch out for:** Decide whether `[[groupings]]` in `config.toml` stays
supported beside the directory (additive) or moves through the config update
pipeline (breaking, with a `docs/migrations.md` entry). Take the name from
the file name only, so a file and a `name` key cannot disagree. `doctor` must
name the bad grouping file.

## Homebrew formula for asana-sessions

**Now:** `asana-sessions` installs with `install.mjs` from its GitHub release
(`asana-sessions-release.yml` uploads the tarball, `install.mjs`, and
`checksums.txt`). Each install gets its own `<prefix>/lib/asana-sessions/install-*`
directory, and the `current` symlink points at the active one.
`bin/asana-sessions` links through `current`. Claude Code reads plugin files
from the registered marketplace folder for as long as a session runs, so
`registeredPackage` (`packages/asana-sessions/src/setup.js`) registers the
Claude plugin through `current`. Codex copies plugins into its own cache, so
it registers the real directory. The installer runs `setup --refresh` after
each install, then prunes old install directories. Core asanamate already
builds a Homebrew cask (`homebrew_casks` in `.goreleaser.yaml`), but
`skip_upload` stays on until `sadmachine/homebrew-tap` and
`HOMEBREW_TAP_TOKEN` exist.

**Idea:** Ship `asana-sessions` as a formula in the same tap. Homebrew
replaces the installer: `Cellar/asana-sessions/<version>` is the install
directory, `opt/asana-sessions` is the stable link, `brew cleanup` is the
prune, and `brew uninstall` removes it. The work:

- Create `sadmachine/homebrew-tap` and the `HOMEBREW_TAP_TOKEN` secret, then
  drop `skip_upload` for the core cask.
- Write the formula (`Formula/asana-sessions.rb`). Point it at the release
  tarball and its sha256, add `depends_on "node"`, unpack into `libexec`, and
  run `npm ci --omit=dev` there so the shipped shrinkwrap stays in force.
  Link `libexec/bin/asana-sessions.js` into `bin`. Do not use
  `std_npm_args`: a global tarball install can re-resolve dependencies.
- Have `asana-sessions-release.yml` update the formula's `url` and `sha256`
  in the tap after it publishes the release.
- In `registeredPackage`, also accept Homebrew's stable link. Node resolves
  the real path, so the package sees `<brew prefix>/Cellar/asana-sessions/<version>/libexec`.
  Use `<brew prefix>/opt/asana-sessions/libexec` when it resolves to the
  package. Without this, setup registers the Cellar path, and the
  `brew cleanup` after an upgrade breaks open sessions the same way the
  installer's prune did. Add a setup test like the `current` one.
- Formulas must not write to the user's home, and `post_install` is
  sandboxed, so the formula cannot run `setup --refresh`. Print
  `asana-sessions setup --refresh` (and `asana-sessions remove` before
  `brew uninstall`) in `caveats`. Open sessions keep working without the
  refresh because Claude reads through `opt`; `doctor` shows `stale` until
  the user runs it.
- Document Homebrew as the main install in the package README and keep
  `install.mjs` for machines without Homebrew. Update `RELEASING.md` so the
  release checks also install with `brew install` from the tap.

**Why deferred:** The tap does not exist yet. The `current` link fixes the
installer, and the Homebrew path detection should be written once a real
Cellar layout can be tested.

**Watch out for:** Someone moving from `install.mjs` to Homebrew has both
`~/.local/bin/asana-sessions` and the brew one. Whichever comes first on the
provider's PATH runs the hooks, and `setup` warns when that is another
installation. The migration notes should say to run `asana-sessions remove`,
then delete `~/.local/bin/asana-sessions` and `~/.local/lib/asana-sessions`.
A cask would run install hooks that can write to the home directory, but
Homebrew gives casks no `opt` link, so a cask would need its own stable path.
