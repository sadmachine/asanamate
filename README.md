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

## Keys

| Key | Action |
|---|---|
| `j`/`k`, arrows | move (list) or scroll (reader) |
| `g`/`G` | first/last ticket |
| `tab` | switch focus between the list and the reader |
| `/` | edit the filter (Enter or Esc to finish) |
| `p` | switch project (recent first) |
| `enter`, `a` | run an action on the selected ticket |
| `f` | attachments: view images inline or open in the browser |
| `o` | open the ticket in the browser |
| `r` | reload |
| `q` | quit |

## Filtering

Space-separated terms, all of which must match:

- words match the title
- `section:`, `project:`, `assignee:`, `tag:` match names
- `is:open`, `is:done`
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
| `images` | `"auto"` | `auto`, `kitty` (force on), or `off` |
| `default_filter` | `"is:open"` | filter applied at startup |
| `confirm_writes` | `true` | write-back subcommands ask before writing |
| `list.layout` | `"single"` | `single` (one line per ticket) or `multi` (title, then fields on a second line) |
| `list.fields` | `["section"]` | values shown with the title |
| `branch_field` | `""` | custom field holding the ticket's git branch (`$ASANAMATE_BRANCH`); empty uses the title slug |
| `agents.command` | unset (off) | opt-in agent tracking; see [Agents](#agents-optional) |
| `list.separator` | `false` | frame each ticket with lines above and below; neighbours share one |
| `repo_source.command` | lists repos in your setup directory | prints one repo path per line |

The title is always shown. `list.fields` accepts the built-ins `section`,
`due`, `assignee`, `project`, `tags`, `completed` (open/done), and `agent`
(when [agents](#agents-optional) are on). Any other name is matched to a custom
field, ignoring case and surrounding spaces.

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
command = '''tmux new-window -c "$ASANAMATE_REPO" -n "$ASANAMATE_SLUG" -e "ASANAMATE_TICKET_MD=$ASANAMATE_TICKET_MD" -e "ASANAMATE_GID=$ASANAMATE_GID" 'claude "$(cat "$ASANAMATE_TICKET_MD")"' '''
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
| `ASANAMATE_AGENT_STATUS`, `ASANAMATE_AGENT_PATH`, `ASANAMATE_AGENT_TARGET` | the linked running agent, when [agents](#agents-optional) are on (empty otherwise) |
| `ASANAMATE_TICKET_JSON`, `ASANAMATE_TICKET_MD` | full ticket as JSON / Markdown |
| `ASANAMATE_FIELD_<NAME>` | custom field display values, e.g. `ASANAMATE_FIELD_BRANCH_NAME` |
| `ASANAMATE_CONFIRM_WRITES` | `1`/`0`, read by the write-back subcommands |

Modes:

- `foreground`: suspends the TUI, runs in the same terminal, and resumes.
- `background`: runs detached. Output goes to `~/.local/state/asanamate/actions.log`.
- `exit`: quits asanamate, then runs the command. Best for popups.

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

asanamate can link tickets to coding agents that another tool is running, such
as [ccmux](https://github.com/motherskitchenblr2/ccmux), agent-deck, or dmux. It
is off until you set a command:

```toml
branch_field = "Branch Name"

[agents]
# Prints "<path>\t<status>[\t<target>]" per running agent.
command = '''ccmux show --json | jq -r '.[] | "\(.cwd)\t\(.status)\t\(.sessionId)"' '''
```

Check the field names against your tool's output (`ccmux show --json`); the
line format is all asanamate relies on. The command runs every 5 seconds.

The link is the git branch: a ticket matches an agent whose working directory
has `$ASANAMATE_BRANCH` checked out, in a repo linked to one of the ticket's
projects (worktrees count as their repo). Linked tickets get the `agent` list
field and the `ASANAMATE_AGENT_*` variables, so actions can start and jump to
agents:

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
command = '[ -n "$ASANAMATE_AGENT_TARGET" ] && ccmux switch "$ASANAMATE_AGENT_TARGET"'
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
