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
   `~/.config/asanamate/config.toml` with every default explained, plus a
   pager action and a Claude example in `~/.config/asanamate/actions/`. When
   Codex is installed, it also offers to add asanamate's agent status hook (see
   [Agent status](#agent-status)).
3. Run `asanamate`.

`asanamate config` opens the config file in `$VISUAL` or `$EDITOR` (falling
back to `vi`) and reports any errors in it after you save.

`asanamate config update` rewrites the config from the current template so it
picks up new settings and comments, keeping every value you set. The previous
file is saved as `config.toml.bak`; copy any comments of your own back from it.
Actions live in their own files, so updates never touch them.

## Layout

At 100 columns and wider, the ticket list and the reader sit side by side in
bordered panels; the focused one has an accent-colored border. The list widens
to fit full ticket titles, but leaves the reader at least 80 columns and cuts
titles that no longer fit. At 160 columns
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
| `j`/`k` | move in the list; in cards view, select the next or previous field, section, or comment; in markdown view, scroll |
| up/down arrows | move in the list or scroll the reader |
| `g`/`G` | first/last ticket; in the reader, jump to its top or bottom (in cards view, selecting the first or last field, section, or comment) |
| `ctrl+d`/`ctrl+u` | half page down/up in the list, or scroll the reader half a page |
| `ctrl+f`/`ctrl+b` (or pgdown/pgup) | page down/up in the list, or scroll the reader a page |
| `tab` / `shift+tab` | focus the next/previous pane: the views panel (wide screens), the list, and the reader; in the cards view, entering the reader selects its first editable row |
| `enter` (reader, editable row selected) | edit the selected row, skipping the `e` menu; Add comment opens the comment editor |
| `esc` (reader) | return focus to the list |
| `0` / `1` / `2` | focus the views panel (wide screens) / the list / the reader |
| `/` | edit the filter (available fields appear while editing; `?` opens the filter guide; Enter or Esc to finish) |
| `p` | switch project (recent first) |
| `H` | ticket history: the last 10 tickets focused in the reader, most recent first; picking one selects it (pinning it to the list when the view doesn't show it) and focuses the reader |
| `L` | repo links: link, relink, or unlink each project's repo (starts on the viewed project), or give the selected ticket its own repo that overrides its projects' links without changing them |
| `space`, `a` | run an action on the selected ticket |
| `e` | edit the selected ticket: add a comment, move it to a section (of a project, or of My Tasks when it is yours), add or remove a project, set a custom field (text, number, date, single- or multi-select, people), assign it, or set its branch (saved locally, overrides `branch_field`; empty removes it) |
| `C` | add a comment to the selected ticket, from the list or the reader |
| `d` / `m` / `A` | set the due date / move to a section / assign, skipping the `e` menu |
| `.` | run the last picked action again on the selected ticket |
| `t` | log completed time for the selected ticket (only when time tracking is configured) |
| `f` | attachments: view images inline (`j`/`k` step between them) or open in the browser |
| `b` | group the list by a field (built-ins, `list.fields`, or a custom field on the loaded tickets) |
| `=` | fit the list pane to its content (also on project, grouping, and filter changes) |
| `v` | switch the reader between the cards and markdown views |
| `V` | recall or manage saved views |
| `]` / `[` | apply the next/previous saved view, in name order |
| `ctrl+s` | save the current filter and grouping under a name |
| `s` | settings: separators, header spacing, reader view, and auto-update interval; changes persist between sessions |
| `o` | open the ticket in the browser |
| `c` | copy the selected ticket's link to the clipboard (requires terminal OSC 52 support) |
| `r` | reload |
| `R` | set the automatic reload interval (persists between sessions) |
| `?` | show every key |
| `q` | quit |

## Filtering

Space-separated terms, all of which must match:

- words match the title
- `section:`, `project:`, `assignee:`, `tag:` match names
- `project:<name>[<section>]` matches a section within that project, such as
  `project:web[backlog]` or `project:web["in progress"]`
- `is:open`, `is:done`
- `due:overdue`, `due:today`, `due:week` (the next 7 days, today included),
  `due:none`
- `agent:any`, `agent:none`, `agent:<state>` match linked [agents](#agents-optional)
- `-term` negates a term; `"double quotes"` group words

Example: `is:open section:"in progress" -tag:blocked`. Set the startup filter
with `default_filter`.

### Saved views

Press `ctrl+s`, type a friendly name, then press `ctrl+s` again to save the
current filter and grouping. Press `V` to search saved views by name and press
`enter` to apply one to the current project or My Tasks. Saved views are reusable
across projects and persist between sessions; they do not switch projects.
Changing a filter or grouping after recall leaves the saved view unchanged.

Save under the same name to replace a view, with confirmation. The `V` picker
also offers saving and deletion; deletion requires confirmation. These keys
work in narrow terminals and list-only mode too.

## List-only mode and scripting

- `asanamate --no-preview` shows only the ticket list at full width. It is handy
  in a small popup: pick a ticket, press `space` or `a`, and run an action.
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
| `default_filter` | `"is:open"` | filter applied at startup |
| `confirm_writes` | `true` | write-back subcommands ask before writing |
| `list.layout` | `"single"` | `single` (one line per ticket) or `multi` (title, then fields on a second line) |
| `list.refresh_interval` | `"30s"` | automatic list reload interval, at least `1s`; durations such as `"15s"` or `"1m"`; `R` or the `s` settings menu overrides it and the override persists, `ctrl+s` applies it; the statusline shows the interval, and an automatic reload marks the list border yellow instead of opening the loading modal |
| `list.fields` | `["section", "due"]` | values shown with the title, in aligned columns (`single` layout); due dates show relative to today and are colored by urgency |
| `branch_field` | `""` | custom field holding the ticket's git branch (`$ASANAMATE_BRANCH`); empty uses the ticket's ID field, else the title slug |
| `agents.preset` / `agents.command` | unset (off) | opt-in agent tracking; see [Agents](#agents-optional) |
| `symbols` | `unicode` on UTF-8, else `ascii` | `unicode`, `nerd`, or `ascii` for ticket markers and agent states |
| `reduced_motion` | OS setting | `true` shows static agent symbols instead of the spinner |
| `list.separator` | `false` | frame each ticket with lines above and below; neighbours share one; the `s` settings menu toggles it and the choice persists |
| `list.group_by` | `""` (ungrouped) | starting grouping: any `list.fields` name; `b` picks another |
| `list.header.style` | `"rule"` | group headers: `rule` (`── Label (n) ───`) or `bar` (reversed bar) |
| `list.header.spacing` | `false` | `true` adds a blank line above and below each group header; the `s` settings menu toggles it and the choice persists |
| `list.header.color` | `accent_color` | group header color, same format as `accent_color` |
| `list.pinned.color` | `"208"` (orange) | header color of the Pinned section, which tops the list with a ticket you edited or opened that the filter would hide; same format as `accent_color`, empty uses it |
| `list.selection.style` | `"marker"` | selected ticket: `marker` (bold title with a left `▌`) or `bar` (reversed row; agent badges swap colors) |
| `list.selection.color` | `accent_color` | selection marker color, same format as `accent_color` |
| `reader.view` | `"cards"` | reader's starting view: `cards` (details card, titled sections, one box per comment) or `markdown` (the rendered ticket Markdown); `v` switches and the choice persists |
| `images.mode` | `"auto"` | kitty graphics: `auto`, `kitty` (force on), or `off` |
| `images.inline` | `false` | cards view: draw images in descriptions and comments in place of their links (needs `images.mode` on and a terminal with kitty Unicode placeholders, such as kitty or Ghostty) |
| `reader.max_text_width` | `0` | cards view: wrap description and comment text at this many columns (words are kept whole); `0` wraps at the pane width |
| `repo_source.command` | lists repos in your setup directory | prints one repo path per line |
| `time_tracking.id` / `time_tracking.command` | unset (off) | provider ID for saved choices and command implementing the time tracking JSON protocol |

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

### Time tracking (optional)

To use installed `hrvst`, add:

```toml
[time_tracking]
id = "hrvst"
command = "asanamate time-provider hrvst --task-id YOUR_ENGINEERING_TASK_ID"
```

Set `--task-id` to numeric ID of your default Harvest task (Engineering in
your case). `hrvst alias list` can show that ID for an existing Engineering
alias. Aliases do not populate project picker. Project choices come from
`hrvst users project-assignments me`; entry creation uses Harvest's API with
the credentials in `~/.hrvst/config.json` so Asana link metadata is preserved.

Press `t` on a ticket. If ticket belongs to several Asana projects, choose one.
The form shows Harvest project, Engineering task, and decimal hours. Press
Enter to edit a field, Tab to move, and Ctrl+S to log. Project and task choices
are remembered per Asana project after a successful log; hours starts empty.
Harvest notes contain ticket title; external reference links to Asana ticket
and chosen Asana project.

Other trackers can implement same command protocol. Command runs through
`/bin/sh -c` and reads one JSON request from stdin. For `{"operation":"form"}`
return a form spec, such as:

```json
{"fields":[
  {"id":"project_id","label":"Project","type":"select","remember":true,
   "options":[{"id":"123","name":"Web"}]},
  {"id":"hours","label":"Hours","type":"hours"}
]}
```

Fields are required. IDs use snake_case. Select option IDs must be stable;
`remember` saves a select choice per Asana project. Time forms need exactly
one `hours` field. The `log` request contains `values` keyed by field ID and
an `asana` object with `task_gid`, `project_gid`, `title`, and `url`. Exit zero
only after creating entry. Failed logs are never retried automatically. Use a
different provider `id` when changing trackers so saved choices stay separate.

## Actions

Each action is its own file in `~/.config/asanamate/actions/`, next to
`config.toml`. Every `*.toml` file there loads in file name order, so prefixes
such as `10-` and `20-` set the menu order. Other files, such as
`claude-tmux.toml.example`, are ignored until renamed. A file holds one action,
with its keys at the top level:

```toml
# actions/claude.toml
name = "Start Claude"   # shown in the menu
key = "c"               # one character
mode = "background"     # foreground | background | exit
repo = true             # resolve the ticket's repo and run inside it
context = ""            # "" (everywhere) | "comment"; see below
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
| `ASANAMATE_WORKTREE` | the repo's worktree with `$ASANAMATE_BRANCH` checked out, the main one included; empty when none has it |
| `ASANAMATE_BRANCH` | the ticket's branch: the one saved for it, else `branch_field`'s value, else the ID field, else the title slug |
| `ASANAMATE_AGENT_STATE`, `ASANAMATE_AGENT_STATUS`, `ASANAMATE_AGENT_PATH`, `ASANAMATE_AGENT_TARGET`, `ASANAMATE_AGENT_TITLE` | the agent the action is about (the chosen one for `agent = true`, else the most urgent): normalized state, raw status, directory, jump id, title. Empty without agents |
| `ASANAMATE_AGENT_TARGETS` | every linked agent, one `target<TAB>state<TAB>path<TAB>title` line each |
| `ASANAMATE_COMMENT_GID`, `ASANAMATE_COMMENT_AUTHOR`, `ASANAMATE_COMMENT_AUTHOR_GID`, `ASANAMATE_COMMENT_DATE`, `ASANAMATE_COMMENT_TEXT` | the highlighted comment when the menu opened: story gid, author name and gid, `created_at` timestamp, and body as Markdown. Empty when no comment is highlighted |
| `ASANAMATE_TICKET_JSON`, `ASANAMATE_TICKET_MD` | full ticket as JSON / Markdown |
| `ASANAMATE_INPUT_FILE` | text typed into the `input` box, possibly empty. Empty path for actions without `input` |
| `ASANAMATE_PARAM_<ID>` | selected action form value, with its field ID uppercased |
| `ASANAMATE_FIELD_<NAME>` | custom field display values, e.g. `ASANAMATE_FIELD_BRANCH_NAME` |
| `ASANAMATE_CONFIRM_WRITES` | `1`/`0`, read by the write-back subcommands |

Modes:

- `foreground`: suspends the TUI, runs in the same terminal, and resumes.
- `background`: runs detached. Output goes to `~/.local/state/asanamate/actions.log`.
- `exit`: quits asanamate, then runs the command. Best for popups.

Context: an action with `context = "comment"` appears only when a comment is
highlighted in the cards view, listed before the other actions. Press `space`
or `a` on a comment to open the menu. Its key may repeat a key of an
everywhere action; the comment action wins while a comment is highlighted.

```toml
# actions/reply.toml
name = "Reply to comment"
key = "r"
context = "comment"
input = "Reply"
command = '''{ printf '> %s wrote:\n\n' "$ASANAMATE_COMMENT_AUTHOR"; printf '%s\n' "$ASANAMATE_COMMENT_TEXT" | sed 's/^/> /'; printf '\n'; cat "$ASANAMATE_INPUT_FILE"; } |
asanamate comment "$ASANAMATE_GID" -'''
```

Input: set `input` to a title, and asanamate asks for free-form text right
before running the action. Enter adds a newline, ctrl+s runs, and esc cancels.
Leaving it empty is fine. Each ticket has one input file, so an action that
starts on the same ticket overwrites the text from the one before. For example, to put notes above the ticket in
Claude's first prompt:

```toml
# actions/claude-notes.toml
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

Actions can use the same form spec with static fields in TOML. Answers reach
the command through `ASANAMATE_PARAM_<ID>`:

```toml
# actions/deploy.toml
name = "Deploy"
key = "D"
command = 'deploy --target "$ASANAMATE_PARAM_TARGET"'
[[form.fields]]
id = "target"
label = "Target"
type = "select"
remember = true
[[form.fields.options]]
id = "stage"
name = "Staging"
[[form.fields.options]]
id = "prod"
name = "Production"
```

Remembered action choices use the chosen Asana project and action key. They
are saved when the form is submitted. Existing `input` actions keep their text
box; an action can use both.

**tmux note:** `tmux new-window` and `split-window` run their command in the
tmux server's environment, so `ASANAMATE_*` variables do not reach them. Pass
the ones you need with `-e NAME="$NAME"`, as in the example above.

The active project is the one picked for the repo when `repo = true`.
Otherwise it is the project you are viewing (if the ticket is in it), or the
ticket's only project.

Repo resolution: the first time a project's ticket runs a `repo = true` action,
you pick a repo from `repo_source.command` or type any path (Tab). asanamate
remembers it. Tickets in several projects always ask which project to use.
A ticket given its own repo with `L` (**This ticket only**) uses that repo
instead, skips the project question, and leaves every project link as it is.
The reading pane's header shows the local repo such an action will run in,
when it will not ask: the ticket's own repo, else its only project's link.

More examples:

```toml
# actions/branch.toml
name = "Create branch and record it"
key = "b"
mode = "foreground"
repo = true
command = '''git switch -c "feature/$ASANAMATE_SLUG" && asanamate field "$ASANAMATE_GID" "Branch Name" "feature/$ASANAMATE_SLUG"'''
```

```toml
# actions/session.toml
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

### Agent status

ccmux can report a busy session as `idle`, Codex sessions most of all. With the
ccmux preset, an `idle` session therefore takes the state its agent last
reported itself, when that report is newer than ccmux's:

- **Claude Code** keeps a file per running session in
  `$CLAUDE_CONFIG_DIR/sessions/` (else `~/.claude/sessions/`). asanamate reads
  it as is; nothing to install.
- **Codex** reports through a hook. `asanamate setup` offers to add
  `asanamate hook codex` to `$CODEX_HOME/hooks.json` (else
  `~/.codex/hooks.json`), saving the old file as `hooks.json.bak`; run
  `asanamate setup hooks` to add it later. Codex asks you to trust the new
  hooks on its next start, and needs hooks on (`[features] hooks = true`). The
  hook writes one file per session in `$XDG_STATE_HOME/asanamate/agents/`
  (else `~/.local/state/asanamate/agents/`).
  Without it, Codex states fall back to the Codex pane title, and are
  inaccurate or wrong when the title format changes.

Any status other than `idle` from ccmux is kept as is.

**Linking.** A ticket matches every agent whose working directory has
`$ASANAMATE_BRANCH` checked out, in the ticket's own repo when set, else a
repo linked to one of the ticket's projects (worktrees count as their repo). A project shared across repos that
is linked to just one of them pulls that repo's agents into every ticket it
holds; press `L` to unlink it.

Agents missing from a ticket usually come down to one of these:

- `branch_field` is unset, or empty for the ticket, so the branch is the title
  slug rather than the branch you work on. When a set `branch_field` is
  empty, the reading pane warns, and actions whose command uses
  `$ASANAMATE_BRANCH` ask before running. A branch typed there, other than
  the fallback, is saved for the ticket and used from then on in place of
  `branch_field`. Set branch in the `e` menu edits or removes it.
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
# actions/agent.toml
name = "Start agent in a worktree"
key = "c"
mode = "background"
repo = true
command = '''ccmux spawn claude --cwd "$ASANAMATE_REPO" --worktree "$ASANAMATE_BRANCH" --detach --prompt "$(cat "$ASANAMATE_TICKET_MD")" &&
asanamate field --yes "$ASANAMATE_GID" "Branch Name" "$ASANAMATE_BRANCH"'''
```

```toml
# actions/jump.toml
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

Comments from the CLI or TUI accept Markdown: `**bold**`, `*italic*`,
`~~strikethrough~~`, inline code, fenced code blocks, links, lists, and blockquotes.
Put code fences on their own lines:

````markdown
Here are the values:

```json
{"enabled": true}
```
````

For multiline CLI comments, use stdin so the shell does not interpret backticks:

```sh
asanamate comment <gid> - <<'COMMENT'
**Example**

```json
{"enabled": true}
```
COMMENT
```

Code and link text do not expand `@Full Name` mentions. Raw HTML stays literal.
Asana comments do not support headings, images, or tables: headings become bold,
images retain their alt text, and table syntax stays text. Lists inside quotes
and code blocks or quotes inside lists are flattened to supported formatting.
Fenced code blocks preserve whitespace but do not add syntax highlighting.

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
- Actions: `~/.config/asanamate/actions/*.toml`
- State (repo links, saved branches, saved views, recent projects, ticket history): `~/.local/state/asanamate/state.toml`
- Ticket exports: `~/.local/state/asanamate/tickets/<gid>/`
- Background action log: `~/.local/state/asanamate/actions.log`

`$XDG_CONFIG_HOME` and `$XDG_STATE_HOME` are honored.
