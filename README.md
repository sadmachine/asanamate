# asanamate

A terminal UI for reading Asana tickets and running your own commands on them.
Browse My Tasks or any project, read a ticket with its fields, comments, and
attachments, then press a key to start a coding agent in the ticket's repo with
the ticket as context. Agents and scripts report back with `asanamate comment`,
`move`, and `field`.

asanamate runs in any terminal and works well in tmux panes, windows, and popups.
macOS and Linux only.

## Install

```sh
brew install --cask sadmachine/tap/asanamate
# or
go install github.com/sadmachine/asanamate/cmd/asanamate@latest
```

Prebuilt binaries are attached to each [GitHub release](https://github.com/sadmachine/asanamate/releases).

## Setup

1. Create a personal access token at <https://app.asana.com/0/my-apps> and export it:
   `export ASANA_ACCESS_TOKEN=...`
2. Run `asanamate setup`. It checks the token, asks for your workspace and the
   directory that holds your git repositories, and writes
   `~/.config/asanamate/config.toml` with every default explained.
3. Run `asanamate`.

`asanamate config` opens the config file in `$VISUAL` or `$EDITOR` (falling
back to `vi`) and reports any errors in it after you save.

`asanamate config update` rewrites the config from the current template so it
picks up new settings and comments, keeping every value you set. The previous
file is saved as `config.toml.bak`; copy any comments of your own back from it.

## Layout

At 100 columns and wider, the ticket list and the reader sit side by side in
bordered panels; the focused one has an accent-colored border. At 160 columns
and wider, a views panel on the left lists My Tasks, recent projects, the
groupings, and running agents: press `0`, move with `j`/`k`, and open one
with `enter`. In a reader at least 90 columns wide, the cards view puts the
agents and subtasks beside the details card.

The bottom bar shows the mode (`NORMAL`, `READ`, `EDIT`, `FILTER`, or
`VIEWS`), the view, filter, grouping, ticket count, and agents, with the keys
for what has focus on the right. Messages take the keys' place until the next
key press.

## Keys

| Key | Action |
|---|---|
| `j`/`k`, arrows | move (list) or scroll (reader) |
| `g`/`G` | first/last ticket |
| `tab` | switch focus between the list and the reader; in the cards view, move into the reader and select its first editable row |
| `tab` / `shift+tab` (reader, cards view) | move between editable rows: assignee, project and My Tasks sections, settable custom fields, and Comments |
| `enter` (reader, row selected) | edit the selected row, skipping the `e` menu |
| `esc` (reader) | return focus to the list |
| `0` / `1` / `2` | focus the views panel (wide screens) / the list / the reader |
| `/` | edit the filter (Enter or Esc to finish) |
| `p` | switch project (recent first) |
| `enter`, `a` | run an action on the selected ticket |
| `e` | edit the selected ticket: add a comment, move it to a section (of a project, or of My Tasks when it is yours), set a custom field (text, number, date, single- or multi-select, people), or assign it |
| `f` | attachments: view images inline or open in the browser |
| `b` | group the list by a field (built-ins, `list.fields`, or a custom field on the loaded tickets) |
| `=` | fit the list pane to its content (also on project, grouping, and filter changes) |
| `v` | switch the reader between the cards and markdown views |
| `o` | open the ticket in the browser |
| `r` | reload |
| `?` | show every key |
| `q` | quit |

## Filtering

Space-separated terms, all of which must match:

- words match the title
- `section:`, `project:`, `assignee:`, `tag:` match names
- `is:open`, `is:done`
- `agent:any`, `agent:none`, `agent:<state>` match linked [agents](#agents-optional)
- `-term` negates a term; `"double quotes"` group words

Example: `is:open section:"in progress" -tag:blocked`. Set the startup filter
with `default_filter`.

## List-only mode and scripting

- `asanamate --no-preview` shows only the ticket list at full width. It is handy
  in a small popup: pick a ticket, press `enter`, and run an action.
- `asanamate list` prints tickets for other tools. The default is My Tasks with
  `default_filter` applied.
  - `--project <gid>` lists a project instead. The gid is the number after
    `/project/` in the project's URL.
  - `--filter "<query>"` overrides the filter. `--filter ""` shows everything.
  - `--format tsv` (default) prints `gid, section, due, title, url` separated
    by tabs. `--format jsonl` prints one JSON object per task.
- `asanamate show <gid>` prints one ticket as Markdown. `--format json` prints
  JSON instead.
- `asanamate doctor [<gid>]` shows the running agents and, for a ticket, why
  they link to it or not. See [Agents](#agents-optional).

Pick a ticket with fzf:

```sh
asanamate list | fzf --delimiter '\t' --with-nth 2,4 --preview 'asanamate show {1}' | cut -f1
```

## Configuration

`~/.config/asanamate/config.toml` (or `$XDG_CONFIG_HOME/asanamate/config.toml`):

| Key | Default | Meaning |
|---|---|---|
| `workspace` | set by setup | Asana workspace gid |
| `theme` | `"dark"` | reading pane style: `dark` or `light` |
| `accent_color` | `"4"` | accent for reader card headings, group headers, and the selection marker: an ANSI color number (`0`–`255`) or `#rrggbb` |
| `images` | `"auto"` | `auto`, `kitty` (force on), or `off` |
| `default_filter` | `"is:open"` | filter applied at startup |
| `confirm_writes` | `true` | write-back subcommands ask before writing |
| `list.layout` | `"single"` | `single` (one line per ticket) or `multi` (title, then fields on a second line) |
| `list.fields` | `["section", "due"]` | values shown with the title, in aligned columns (`single` layout); due dates show relative to today and are colored by urgency |
| `branch_field` | `""` | custom field holding the ticket's git branch (`$ASANAMATE_BRANCH`); empty uses the title slug |
| `agents.preset` / `agents.command` | unset (off) | opt-in agent tracking; see [Agents](#agents-optional) |
| `symbols` | `unicode` on UTF-8, else `ascii` | `unicode`, `nerd`, or `ascii` for ticket markers and agent states |
| `reduced_motion` | OS setting | `true` shows static agent symbols instead of the spinner |
| `list.separator` | `false` | frame each ticket with lines above and below; neighbours share one |
| `list.group_by` | `""` (ungrouped) | starting grouping: any `list.fields` name; `b` picks another |
| `list.header.style` | `"rule"` | group headers: `rule` (`── Label (n) ───`) or `bar` (reversed bar) |
| `list.header.spacing` | `0` | blank lines above and below each group header |
| `list.header.color` | `accent_color` | group header color, same format as `accent_color` |
| `list.selection.style` | `"marker"` | selected ticket: `marker` (bold title with a left `▌`) or `bar` (reversed row; agent badges swap colors) |
| `list.selection.color` | `accent_color` | selection marker color, same format as `accent_color` |
| `reader.view` | `"cards"` | reader's starting view: `cards` (details card, titled sections, one box per comment) or `markdown` (the rendered ticket Markdown); `v` switches |
| `reader.max_text_width` | `0` | cards view: wrap description and comment text at this many columns (words are kept whole); `0` wraps at the pane width |
| `repo_source.command` | lists repos in your setup directory | prints one repo path per line |

The title is always shown. `list.fields` picks the other columns and their
order: the built-ins `section`, `due`, `assignee`, `initials` (the assignee's
initials as a colored badge, handy in shared projects), `project`, `tags`, and
`completed` (open/done). Any other name is matched to a custom field, ignoring
case and surrounding spaces. A column shows only when some visible ticket has
a value for it.

`list.group_by` takes the same names and puts a `Value (count)` header above
each group. Groups keep Asana's order (first seen), with tickets missing
the value last. `due` groups into Overdue, Today, Tomorrow, Next 7 days, Later,
and No due date.

Custom fields are separate Asana objects, so two can share a name (for example
one per project). When that happens, asanamate uses the one attached to the
active project; otherwise the first with a value. Writes never guess:
`asanamate field` needs `--project` (actions pass it automatically). For example:

```toml
[list]
layout = "multi"
fields = ["section", "Status", "Branch Name", "due"]
```

To pick repos from sesh or zoxide instead:

```toml
[repo_source]
command = "sesh list -z"
```

## Actions

```toml
[[actions]]
name = "Start Claude"   # shown in the menu
key = "c"               # one character
mode = "background"     # foreground | background | exit
repo = true             # resolve the ticket's repo and run inside it
command = '''git switch "$ASANAMATE_BRANCH" 2>/dev/null || git switch -c "$ASANAMATE_BRANCH" &&
tmux new-window -c "$ASANAMATE_REPO" -n "$ASANAMATE_SLUG" -e "ASANAMATE_TICKET_MD=$ASANAMATE_TICKET_MD" -e "ASANAMATE_GID=$ASANAMATE_GID" 'claude "$(cat "$ASANAMATE_TICKET_MD")"' '''
confirm_writes = false  # optional per-action override
```

Commands run with `/bin/sh -c`. Ticket data is available only through
environment variables and files. Always reference the variables, and never
paste ticket text into the command, because ticket content is untrusted.

| Variable | Value |
|---|---|
| `ASANAMATE_GID`, `ASANAMATE_TITLE`, `ASANAMATE_URL` | task gid, name, permalink |
| `ASANAMATE_SLUG` | branch-friendly title, e.g. `fix-login-bug` |
| `ASANAMATE_COMPLETED`, `ASANAMATE_ASSIGNEE`, `ASANAMATE_DUE`, `ASANAMATE_TAGS` | task details |
| `ASANAMATE_MY_SECTION` | My Tasks section |
| `ASANAMATE_PROJECT`, `ASANAMATE_PROJECT_GID`, `ASANAMATE_SECTION` | active project and the ticket's section in it |
| `ASANAMATE_REPO` | resolved repo (`repo = true` actions) |
| `ASANAMATE_BRANCH` | the ticket's branch: `branch_field`'s value, or the title slug |
| `ASANAMATE_AGENT_STATE`, `ASANAMATE_AGENT_STATUS`, `ASANAMATE_AGENT_PATH`, `ASANAMATE_AGENT_TARGET`, `ASANAMATE_AGENT_TITLE` | the agent the action is about (the chosen one for `agent = true`, else the most urgent): normalized state, raw status, directory, jump id, title. Empty without agents |
| `ASANAMATE_AGENT_TARGETS` | every linked agent, one `target<TAB>state<TAB>path<TAB>title` line each |
| `ASANAMATE_TICKET_JSON`, `ASANAMATE_TICKET_MD` | full ticket as JSON / Markdown |
| `ASANAMATE_INPUT_FILE` | text typed into the `input` box, possibly empty. Empty path for actions without `input` |
| `ASANAMATE_FIELD_<NAME>` | custom field display values, e.g. `ASANAMATE_FIELD_BRANCH_NAME` |
| `ASANAMATE_CONFIRM_WRITES` | `1`/`0`, read by the write-back subcommands |

Modes:

- `foreground`: suspends the TUI, runs in the same terminal, and resumes.
- `background`: runs detached. Output goes to `~/.local/state/asanamate/actions.log`.
- `exit`: quits asanamate, then runs the command. Best for popups.

Input: set `input` to a title, and asanamate asks for free-form text right
before running the action. Enter adds a newline, ctrl+s runs, and esc cancels.
Leaving it empty is fine. Each ticket has one input file, so an action that
starts on the same ticket overwrites the text from the one before. For example, to put notes above the ticket in
Claude's first prompt:

```toml
[[actions]]
name = "Start Claude with notes"
key = "C"
mode = "background"
repo = true
input = "Notes for Claude"
command = '''git switch "$ASANAMATE_BRANCH" 2>/dev/null || git switch -c "$ASANAMATE_BRANCH" &&
p="$ASANAMATE_INPUT_FILE.prompt" &&
{ [ -s "$ASANAMATE_INPUT_FILE" ] && { cat "$ASANAMATE_INPUT_FILE"; printf '\n\nTicket information below.\n\n'; }; cat "$ASANAMATE_TICKET_MD"; } > "$p" &&
tmux new-window -c "$ASANAMATE_REPO" -n "$ASANAMATE_SLUG" -e "P=$p" -e "ASANAMATE_GID=$ASANAMATE_GID" 'claude "$(cat "$P")"' '''
```

**tmux note:** `tmux new-window` and `split-window` run their command in the
tmux server's environment, so `ASANAMATE_*` variables do not reach them. Pass
the ones you need with `-e NAME="$NAME"`, as in the example above.

The active project is the one picked for the repo when `repo = true`.
Otherwise it is the project you are viewing (if the ticket is in it), or the
ticket's only project.

Repo resolution: the first time a project's ticket runs a `repo = true` action,
you pick a repo from `repo_source.command` or type any path (Tab). asanamate
remembers it. Tickets in several projects always ask which project to use.

More examples:

```toml
[[actions]]
name = "Create branch and record it"
key = "b"
mode = "foreground"
repo = true
command = '''git switch -c "feature/$ASANAMATE_SLUG" && asanamate field "$ASANAMATE_GID" "Branch Name" "feature/$ASANAMATE_SLUG"'''

[[actions]]
name = "Jump to repo session"
key = "s"
mode = "exit"
repo = true
command = 'sesh connect "$ASANAMATE_REPO"'
```

## Agents (optional)

asanamate can show coding agents that another tool is running, such as
[ccmux](https://github.com/motherskitchenblr2/ccmux), next to their tickets.
It is off until you pick a source:

```toml
branch_field = "Branch Name"

[agents]
preset = "ccmux"   # reads `ccmux show --json` directly
```

Or use any tool with a command that prints `<path>\t<status>[\t<target>[\t<title>]]`
per agent, plus a mapping from its statuses to asanamate's states:

```toml
[agents]
command = '''my-agents --tsv'''

[agents.states]        # a status equal to a state name maps to it already
working   = ["running", "busy"]
waiting   = ["permission", "input"]
completed = ["done", "finished"]
idle      = ["sleeping"]   # anything unmapped is "unknown"
```

The sources run every 5 seconds. With the ccmux preset, a session that finished
a turn you have not looked at yet counts as `completed`, and an agent's title is
ccmux's session summary, else its first prompt (one line, cut to 60
characters), else its pane title.

**Linking.** A ticket matches every agent whose working directory has
`$ASANAMATE_BRANCH` checked out, in a repo linked to one of the ticket's
projects (worktrees count as their repo).

Agents missing from a ticket usually come down to one of these:

- `branch_field` is unset, or empty for the ticket, so the branch is the title
  slug rather than the branch you work on. When a set `branch_field` is
  empty, the reading pane warns, and actions whose command uses
  `$ASANAMATE_BRANCH` ask before running.
- The repo is on another branch or a detached HEAD. When a ticket has no
  agents, the reading pane lists the agents in its repos and the branch each
  one is on. Start agents with an action that switches to `$ASANAMATE_BRANCH`
  first, like the Start Claude example in [Actions](#actions).

`asanamate doctor [<gid>]` prints the agents asanamate sees, with their branches
and repos, and for a ticket, its branch, linked repos, and why each agent in
them does or does not match.

**Display.** Linked agents appear right-aligned on the ticket's title line,
most urgent first; more than four show as grouped counts (`⚠1 ◐3 ●2`). The
header sums them up, and tickets with a waiting agent get a yellow title. The
reading pane lists the ticket's agents with their titles and directories.

| State | unicode | ascii | Meaning |
|---|---|---|---|
| waiting | `⚠` | `(!)` | needs you (question or permission prompt) |
| working | spinner (`◐` with reduced motion) | `(~)` | busy |
| completed | `●` | `(+)` | finished, not yet looked at |
| idle | `○` | `(-)` | nothing happening |
| unknown | `?` | `(?)` | status not in the mapping |

`symbols = "unicode"` (default on UTF-8 locales), `"nerd"` (needs a Nerd Font),
or `"ascii"` (default otherwise) also sets the ticket markers (`□`/`✓`,
`[ ]`/`[x]`). Override single states with `[agents.symbols]`, e.g.
`waiting = "!"`. `reduced_motion = true` stops the spinner; unset, asanamate
follows the OS setting (macOS Reduce Motion, GNOME animations).

**Filtering.** `agent:any`, `agent:none`, and `agent:<state>`, for example
`agent:waiting` for "what needs me".

**Actions.** Every action gets the most urgent linked agent in
`ASANAMATE_AGENT_*` and all of them in `$ASANAMATE_AGENT_TARGETS`. An action
with `agent = true` needs one: it runs directly for a single agent, asks which
one (by title) when there are several, and refuses when there are none.

```toml
[[actions]]
name = "Start agent in a worktree"
key = "c"
mode = "background"
repo = true
command = '''ccmux spawn claude --cwd "$ASANAMATE_REPO" --worktree "$ASANAMATE_BRANCH" --detach --prompt "$(cat "$ASANAMATE_TICKET_MD")" &&
asanamate field --yes "$ASANAMATE_GID" "Branch Name" "$ASANAMATE_BRANCH"'''

[[actions]]
name = "Jump to agent"
key = "j"
mode = "exit"
agent = true
command = 'ccmux switch "$ASANAMATE_AGENT_TARGET"'
```

Ticket text becomes the agent's prompt, so anyone who can edit a ticket can
steer the agent. Be careful combining this with auto-approved tools.

## Writing back to Asana

```sh
asanamate comment [--yes] <gid> "Opened PR https://..."   # "-" reads stdin
asanamate move    [--yes] [--project <gid>] <gid> "In Review"
asanamate field   [--yes] [--project <gid>] <gid> "Branch Name" feature/fix-login
```

Each command asks on the terminal before writing unless `--yes` is given,
`ASANAMATE_CONFIRM_WRITES=0` is set, or `confirm_writes = false`. With no
terminal available (for example, a background action), a write that needs
confirmation is refused.

In a comment (from the CLI or the `e` menu), `@Full Name` mentions a
workspace user and notifies them, the same as in Asana. Matching ignores case
and picks the longest name that fits, so `@victoria andersen` tags Victoria
Andersen even if a Victoria also exists. An `@` inside a word, or one that
matches no one or two people with the same name, stays as plain text.

## tmux

```tmux
# Popup (pair with exit-mode actions)
bind-key A display-popup -E -w 90% -h 90% asanamate
# Quick-pick popup: list only
bind-key T display-popup -E -w 60% -h 60% "asanamate --no-preview"
# Pane (pair with background actions)
bind-key a split-window -h asanamate
# Needed for inline images inside tmux
set -g allow-passthrough on
```

## Files

- Config: `~/.config/asanamate/config.toml`
- State (repo links, recent projects): `~/.local/state/asanamate/state.toml`
- Ticket exports: `~/.local/state/asanamate/tickets/<gid>/`
- Background action log: `~/.local/state/asanamate/actions.log`

`$XDG_CONFIG_HOME` and `$XDG_STATE_HOME` are honored.
