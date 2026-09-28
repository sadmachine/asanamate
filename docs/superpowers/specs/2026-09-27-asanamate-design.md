# asanamate Design

Date: 2026-09-27
Status: Draft for review

## Purpose

asanamate is a terminal UI for reading Asana tickets and running user-defined
actions against them. A typical action starts a coding agent in the ticket's git
repository with the ticket as context. The agent or script can then report back
to Asana through asanamate's write-back subcommands.

asanamate runs standalone in any terminal. It fits tmux panes, windows, and
popups, but it does not depend on tmux.

## Goals

- Browse "My Tasks" (the default view) or any project the user is a member of.
- Filter the ticket list live. The config file sets the default filter.
- Read a ticket: title, metadata, custom fields, description, subtasks,
  dependencies, attachments, and comments.
- Run configured actions on the selected ticket. Actions receive ticket data
  safely, through environment variables and files.
- Map Asana projects to local git repositories, remember the mapping, and ask
  when the mapping is unknown or ambiguous.
- Update Asana from scripts: comment, move to a section, and set a custom field.
  Updates ask for confirmation by default.
- Show image attachments inline on terminals that support the kitty graphics
  protocol. Open everything else in the browser.
- Offer a list-only mode for quick picking: a TUI without the reading pane, and
  a structured `list` output that tools like fzf can read.
- Be reusable by other people: guided setup, documented defaults, no
  author-specific values.
- Install through Homebrew, `go install`, or prebuilt release binaries.

## Non-goals (v1)

- Outcome tracking (for example, which tickets have a running agent or open PR).
- Multiple Asana workspaces. Setup picks one workspace.
- Filtering on custom fields. Custom fields are for display only.
- Editing tickets inside the TUI. All writes go through the write-back
  subcommands, so scripts and agents share one path.
- Windows. Background actions use Unix process groups.
- npm distribution. A Go binary on npm needs a per-platform package or a
  postinstall download script. Both add release work with little gain over
  Homebrew and `go install`. We can revisit this later.
- OAuth. Authentication uses a personal access token.

## Technology

- Go 1.26. The result is a single static binary with fast startup, which suits
  tmux popups.
- Bubble Tea v2 (`charm.land/bubbletea/v2`), Bubbles v2 (`viewport`,
  `textinput`), Lip Gloss v2, and Glamour v2 for Markdown rendering.
- `github.com/BurntSushi/toml` for config and state files.
- `github.com/JohannesKaufmann/html-to-markdown/v2` converts Asana rich text
  (`html_notes`, `html_text`) to Markdown.
- Everything else uses the Go standard library: `net/http`, `os/exec`, and
  `image` for PNG, JPEG, and GIF decoding.
- GoReleaser v2 handles releases and the Homebrew tap.

## Authentication and setup

- The token is read from the `ASANA_ACCESS_TOKEN` environment variable. The
  token is never written to disk or logs.
- `asanamate setup` runs an interactive setup:
  1. If a config file exists, it asks before overwriting it. The default answer
     is no.
  2. It checks the token with `GET /users/me` and prints the user's name.
  3. It asks the user to pick a workspace. If only one workspace exists, it
     selects that one automatically.
  4. It asks for the directory that contains the user's git repositories. The
     default is `~/code`. The directory must exist. This directory feeds the
     default repo-listing command.
  5. It writes `config.toml` with mode 0600. Comments in the file explain every
     default. The file includes one working example action and one commented
     tmux + Claude example.
  6. It prints where the config and state files are and how to change the repo
     source, for example to `sesh list -z`.

## Files

- Config: `$XDG_CONFIG_HOME/asanamate/config.toml`, else
  `~/.config/asanamate/config.toml`. The user edits this file. asanamate writes
  it only during setup.
- State: `$XDG_STATE_HOME/asanamate/state.toml`, else
  `~/.local/state/asanamate/state.toml`. asanamate owns this file. It holds the
  project-to-repo links and the recent projects list. Writes are atomic
  (temporary file + rename) and use mode 0600.
- Ticket files: `<state dir>/tickets/<gid>/ticket.json` and `ticket.md`. Every
  action run rewrites both. They live in a stable location because background
  actions (for example, a new tmux window) may read them after asanamate has
  moved on.
- Action log: `<state dir>/actions.log`. Background action output is appended
  here.

## Config reference

```toml
workspace = "65277123677205"   # required; set by setup
theme = "dark"                 # glamour style: "dark" or "light"
images = "auto"                # "auto", "kitty" (force on), or "off"
default_filter = "is:open"     # initial filter query
confirm_writes = true          # write-back subcommands ask before writing

[repo_source]
# Prints one repository path per line. `~` is expanded.
command = '''find '/Users/me/code' -mindepth 2 -maxdepth 2 -name .git -exec dirname {} \;'''

[[actions]]
name = "Start Claude"          # required
key = "c"                      # required; one character; unique
mode = "background"            # "foreground" (default), "background", or "exit"
repo = true                    # resolve the ticket's repo first; sets cwd
command = '''...'''            # required; run with /bin/sh -c
confirm_writes = false         # optional; overrides the global value for this action
```

Unknown keys are errors, so typos are reported instead of ignored.

## Ticket list and filtering

- The default view is My Tasks: `GET /users/me/user_task_list?workspace=...`,
  then `GET /user_task_lists/{gid}/tasks`.
- The project view uses `GET /projects/{gid}/tasks`.
- Both views fetch incomplete tasks plus tasks completed in the last 7 days
  (`completed_since`), so `is:done` has results.
- The filter query is space-separated terms, all of which must match:
  - bare word: case-insensitive substring of the title
  - `section:x`: the My Tasks section or any project section contains x
  - `project:x`, `assignee:x`, `tag:x`: substring match on names
  - `is:open`, `is:done`
  - a leading `-` negates a term, for example `-section:done`
  - double quotes group words, for example `section:"in progress"`
- `/` edits the filter. The list updates on every keystroke.
- `[list]` configures each row. The title is always shown. `layout = "single"`
  (default) puts the fields after the title on one line; `"multi"` puts them on
  a second line. `fields` (default `["section"]`) lists the values to show, in
  order, joined by ` · `. Built-ins: `section`, `due` (shown as `due
  YYYY-MM-DD`), `assignee`, `project`, `tags`, `completed` (open/done). Any
  other name matches a custom field by name (case-insensitive, trimmed); the
  first matching field with a value wins. Empty values are skipped. List
  fetches include custom field display values for this. `separator = true`
  (default false) frames each ticket with dim lines above and
  below, in either layout; neighbouring tickets share one line.

## Project picker

- `p` opens a picker. It lists "My Tasks", then recently opened projects (most
  recent first), then all other member projects sorted by name.
- Member projects come from `GET /projects?workspace=...&archived=false` with
  `members.gid`, filtered to projects where the current user is a member.
  (Checked on 2026-09-27: 205 visible projects, 10 memberships, 3 pages.)
- "Recent" means recently opened in asanamate. Asana has no public recents
  endpoint. The list keeps at most 20 entries.
- The picker filters as the user types (case-insensitive; every typed word must
  appear in the name) and keeps the original order. It uses a built-in picker,
  not fzf, because running fzf from a TUI in a tmux popup is fragile.

## Reading pane

The detail view makes four API calls in parallel: the task (with
`html_notes`, custom fields, dependencies, dependents, and parent), comments
(stories with `type == "comment"`), attachments, and subtasks. The ticket is
rendered as Markdown through Glamour. `ticket.md` uses the same Markdown, so the
pane and agents see the same content.

Detail loads 200 ms after the selection stops moving, and results are cached
for the session. A response for a ticket that is no longer selected is cached
but not shown. `r` reloads the list and clears the cache.

All Asana text has control characters removed before it is displayed or passed
to actions. Ticket content cannot inject terminal escape sequences.

## Actions

- `a` opens the action menu for the selected ticket. Pressing an action's key
  runs it. Arrow keys and Enter also work.
- Ticket data is passed as environment variables, never interpolated into the
  command string. Ticket text is untrusted, and interpolation would allow shell
  injection. Existing `ASANAMATE_*` variables from the parent environment are
  removed first.

| Variable | Value |
|---|---|
| `ASANAMATE_GID` | task gid |
| `ASANAMATE_TITLE` | task name |
| `ASANAMATE_URL` | permalink |
| `ASANAMATE_SLUG` | lowercase, dash-separated title, at most 50 characters (for branch names) |
| `ASANAMATE_COMPLETED` | `true` / `false` |
| `ASANAMATE_ASSIGNEE` | assignee name |
| `ASANAMATE_DUE` | due date `YYYY-MM-DD` |
| `ASANAMATE_TAGS` | comma-separated tag names |
| `ASANAMATE_MY_SECTION` | My Tasks section |
| `ASANAMATE_PROJECT`, `ASANAMATE_PROJECT_GID` | active project (see below) |
| `ASANAMATE_SECTION` | the ticket's section in the active project |
| `ASANAMATE_REPO` | resolved repo path (repo actions only) |
| `ASANAMATE_TICKET_JSON` | path to the full ticket as JSON |
| `ASANAMATE_TICKET_MD` | path to the ticket as Markdown |
| `ASANAMATE_CONFIRM_WRITES` | `1` or `0`; read by the write-back subcommands |
| `ASANAMATE_FIELD_<NAME>` | display value of each custom field; the name is uppercased, and each run of other characters becomes `_` |

- Active project: for repo actions, the project chosen during repo resolution.
  For other actions, the viewed project if the ticket belongs to it, otherwise
  the ticket's only project, otherwise empty.
- Modes:
  - `foreground`: the TUI suspends, the command runs in the same terminal, and
    the TUI resumes when it exits.
  - `background`: the command runs detached in its own process group, and its
    output goes to `actions.log`. The status line reports the exit status. The
    command keeps running if asanamate quits.
  - `exit`: asanamate quits, then runs the command in the same terminal and
    exits with the command's exit code. This mode suits tmux popups.
- Recommended setup: run asanamate in a long-lived pane or window, and use
  `background` actions that open tmux windows. A `display-popup` closes when its
  process exits, so a popup should use `exit` mode for handoffs.
- tmux gotcha (documented in the README): `tmux new-window` runs its command in
  the tmux server's environment, so `ASANAMATE_*` variables do not reach it.
  Pass them with `-e`.

## Repo resolution

Only actions with `repo = true` resolve a repo.

1. Choose the project:
   - no projects: skip; pick a repo every time and do not save it
   - one project: use it
   - several projects: always ask which one
2. If `state.repos[project]` exists and is still a git repository, use it.
   Otherwise, show a notice and continue to step 3.
3. Open the repo picker. Candidates come from `repo_source.command`. The user
   can pick a candidate, or type any path and press Tab (or Enter when nothing
   matches). The path is validated with `git -C <path> rev-parse
   --show-toplevel`. Invalid paths show an error and the picker stays open.
4. Save the repo top-level path for the project (if a project was chosen), then
   run the action with `cwd` set to the repo.

## Write-back subcommands

```
asanamate comment [--yes] <gid> <text | ->          # "-" reads the text from stdin
asanamate move    [--yes] [--project <gid>] <gid> <section name>
asanamate field   [--yes] <gid> <field name> <value> # empty value clears the field
```

- Confirmation is needed unless `--yes` is passed. Otherwise
  `ASANAMATE_CONFIRM_WRITES` (`1`/`0`) decides, and if that is unset,
  `confirm_writes` from the config decides.
- The prompt reads from `/dev/tty`. With no terminal (for example, a background
  action), the command refuses to write and suggests `--yes`.
- `move` matches the section name case-insensitively. It needs `--project` when
  the task belongs to several projects. Errors list the valid choices.
- `field` matches the field name case-insensitively, ignoring surrounding
  spaces. It supports text, number, and enum (matched by option name) fields.
  Other types are rejected.
- The task gid must be numeric.

## Attachments and images

- The reading pane lists attachments as numbered links (`permanent_url`,
  falling back to `view_url`).
- `f` opens the attachments picker. For an image (png/jpg/jpeg/gif hosted by
  Asana) on a supported terminal, asanamate fetches a fresh `download_url`,
  downloads it (https only, at most 20 MB, and without the Asana token, because
  the URL points to a signed storage host), re-encodes it as PNG, and shows it
  full screen with the kitty graphics protocol until Enter is pressed.
  Everything else opens in the browser.
- If loading an image fails, asanamate shows the error and opens the link in
  the browser.
- Detection when `images = "auto"`: the outer terminal must support the kitty
  protocol (`KITTY_WINDOW_ID`, `GHOSTTY_RESOURCES_DIR`, `TERM_PROGRAM` of
  ghostty/WezTerm, or `TERM` containing kitty/ghostty). Inside tmux,
  `allow-passthrough` must also be `on` or `all`. Inside tmux, escape sequences
  are wrapped in the tmux passthrough envelope.
- Image display is a full-screen view, not inline in the reading pane. Bubble
  Tea's renderer and tmux scrolling make inline kitty images unreliable.
- The browser opener only accepts http(s) URLs (`open` on macOS, `xdg-open` on
  Linux).

## Key bindings

| Key | Action |
|---|---|
| `j`/`k`, arrows | move the selection (list) or scroll (reader) |
| `enter` | action menu (same as `a`) |
| `g`/`G` | first/last ticket |
| `tab` | switch focus between the list and the reader |
| `/` | edit the filter (Enter/Esc to finish) |
| `p` | project picker |
| `a` | action menu |
| `f` | attachments |
| `o` | open the ticket in the browser |
| `r` | reload |
| `q`, `ctrl+c` | quit |

Below 100 columns, only the focused pane is shown.

If `a` or `enter` is pressed before the ticket's details have loaded, the
details are fetched and the action menu opens when they arrive. Actions need
the full ticket.

## List-only mode and scripting output

- `asanamate --no-preview` starts the TUI with no reading pane. The list uses
  the full width, and `tab` does nothing. Details are still fetched in the
  background for actions, but they are not rendered. Everything else works as
  in the normal TUI. This mode suits a small tmux popup for quick picking:
  select a ticket, press `enter`, and choose an action.
- `asanamate list [--project <gid>] [--filter <query>] [--format tsv|jsonl]`
  prints tickets to stdout and exits. By default it lists My Tasks, filtered by
  `default_filter`; `--filter ""` lists everything that was fetched. It fetches
  the same set as the TUI.
  - `tsv` (default) prints one line per ticket with columns `gid`, `section`,
    `due`, `title`, `url`. Tabs, newlines, and control characters in values
    become spaces, so every line has exactly 5 columns. `section` is the My
    Tasks section, or the project section when `--project` is given.
  - `jsonl` prints one JSON object per line with the task fields as returned by
    the list endpoint (the same shape as in `ticket.json`).
- `asanamate show [--format md|json] <gid>` prints one ticket as Markdown (the
  same text as `ticket.md`) or JSON (the same as `ticket.json`). It is meant
  for `fzf --preview`.
- Example: `asanamate list | fzf --delimiter '\t' --with-nth 2,4 --preview
  'asanamate show {1}' | cut -f1` prints the gid of the chosen ticket.
- A project gid is the number after `/project/` in an Asana project URL.

## Errors and limits

- Asana API errors show the status and Asana's message in the status line.
- On HTTP 429, the client waits for `Retry-After` and retries once, if the wait
  is 10 s or less.
- The HTTP timeout is 30 s. List pagination uses `limit=100` and `offset`.
- If two asanamate instances write state at the same time, the last write wins.
  This is acceptable for per-user preferences.

## Distribution

- `go install github.com/sadmachine/asanamate/cmd/asanamate@latest`
- GitHub Releases: GoReleaser builds darwin and linux binaries for amd64 and
  arm64 when a `v*` tag is pushed.
- Homebrew: GoReleaser publishes a cask to `sadmachine/homebrew-tap`. It needs
  a tap repository and a `HOMEBREW_TAP_TOKEN` secret (manual, one-time setup).
  The cask removes the macOS quarantine attribute because the binaries are not
  notarized.

## Testing

- Unit tests for config, state, filter, ticket Markdown, action env and files
  (including a shell-injection test), repo resolution, write-back logic, kitty
  encoding and detection, the browser URL guard, setup, and the `list`/`show`
  output formats.
- An Asana client test against `httptest` servers, using response shapes
  checked against the live API on 2026-09-27.
- TUI: model tests for the picker, filtering, and repo-resolution flow. Layout
  and rendering are checked manually.
