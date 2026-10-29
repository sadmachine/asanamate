// Package setup creates the asanamate config file interactively.
package setup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/keymap"
	"github.com/sadmachine/asanamate/internal/prompt"
	"github.com/sadmachine/asanamate/internal/repo"
	"github.com/sadmachine/asanamate/internal/ticket"
)

const (
	defaultRepoRoot = "~/code"
	// skipRepoRoot answers the repo directory prompt to set none.
	skipRepoRoot = "-"
)

// Options wires setup to its input, output, and file locations.
type Options struct {
	In         *bufio.Reader
	Out        io.Writer
	Client     *asana.Client
	ConfigPath string
	StatePath  string
}

// Run asks for the workspace and an optional repo directory, then writes the
// config file.
func Run(ctx context.Context, o Options) error {
	if _, err := os.Stat(o.ConfigPath); err == nil {
		ok, err := prompt.Confirm(o.In, o.Out, o.ConfigPath+" already exists. Overwrite it?")
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("setup cancelled; existing config kept")
		}
	}
	me, err := o.Client.Me(ctx)
	if err != nil {
		return fmt.Errorf("could not authenticate with %s: %w", config.TokenEnv, err)
	}
	fmt.Fprintf(o.Out, "Authenticated as %s.\n", ticket.Clean(me.Name))
	workspace, err := chooseWorkspace(o, me.Workspaces)
	if err != nil {
		return err
	}
	root, err := chooseRepoRoot(o)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(o.ConfigPath), 0o700); err != nil {
		return err
	}
	if err := writePrivate(o.ConfigPath, []byte(Render(workspace, root))); err != nil {
		return err
	}
	if _, err := os.Stat(config.SettingsPath(o.ConfigPath)); errors.Is(err, os.ErrNotExist) {
		if err := config.WriteSettings(o.ConfigPath, config.DefaultSettings()); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	actions := config.ActionsDir(o.ConfigPath)
	if err := writeFiles(actions, actionFiles); err != nil {
		return err
	}
	if err := writeFiles(config.ThemesDir(o.ConfigPath), themeFiles); err != nil {
		return err
	}
	repos := "type a repo path when an action asks for one.\n    Set [repo_source] root to pick from a directory instead."
	if root != "" {
		repos = "git repositories directly inside " + root + "."
	}
	fmt.Fprintf(o.Out, summary, o.ConfigPath, repos, actions, o.StatePath)
	return nil
}

func chooseWorkspace(o Options, workspaces []asana.Ref) (asana.Ref, error) {
	switch len(workspaces) {
	case 0:
		return asana.Ref{}, errors.New("your Asana account has no workspaces")
	case 1:
		fmt.Fprintf(o.Out, "Using workspace %s.\n", ticket.Clean(workspaces[0].Name))
		return workspaces[0], nil
	}
	fmt.Fprintln(o.Out, "Workspaces:")
	for i, w := range workspaces {
		fmt.Fprintf(o.Out, "  %d) %s\n", i+1, ticket.Clean(w.Name))
	}
	for {
		answer, err := prompt.Line(o.In, o.Out, "Workspace number", "1")
		if err != nil {
			return asana.Ref{}, err
		}
		if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(workspaces) {
			return workspaces[n-1], nil
		}
		fmt.Fprintln(o.Out, "Enter a number from the list.")
	}
}

// chooseRepoRoot returns the repo directory, with the home directory as "~",
// or "" when the user skips it.
func chooseRepoRoot(o Options) (string, error) {
	def := skipRepoRoot
	if info, err := os.Stat(repo.ExpandHome(defaultRepoRoot)); err == nil && info.IsDir() {
		def = defaultRepoRoot
	}
	for {
		answer, err := prompt.Line(o.In, o.Out, "Directory that contains your git repositories ("+skipRepoRoot+" to skip)", def)
		if err != nil {
			return "", err
		}
		if answer == skipRepoRoot {
			return "", nil
		}
		dir, err := filepath.Abs(repo.ExpandHome(answer))
		if err != nil {
			return "", err
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			fmt.Fprintf(o.Out, "%s is not a directory.\n", dir)
			continue
		}
		return repo.CollapseHome(dir), nil
	}
}

// Pager is the program the pager action runs: $PAGER's first word, else less.
func Pager() string {
	if f := strings.Fields(os.Getenv("PAGER")); len(f) > 0 {
		return f[0]
	}
	return "less"
}

// warning highlights the heading of a setup warning. lipgloss.Fprintln drops
// the color when the output is not a terminal or NO_COLOR is set.
var warning = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Yellow)

// Warn prints a setup warning: a highlighted heading, then body.
func Warn(out io.Writer, heading, body string) {
	lipgloss.Fprintln(out, "\n"+warning.Render(heading)+"\n"+body)
}

// WriteWithBackup writes data to path, readable only by you, after saving
// old, the file's previous contents, to a timestamped backup (see
// backupPath) when old is not nil. It returns the backup's path, else "".
func WriteWithBackup(path string, old, data []byte) (string, error) {
	backup := ""
	if old != nil {
		backup = backupPath(path)
		if err := writePrivate(backup, old); err != nil {
			return "", err
		}
	}
	return backup, writePrivate(path, data)
}

// Render returns the commented config file for a workspace and repo
// directory; an empty repoRoot leaves repo discovery off.
func Render(workspace asana.Ref, repoRoot string) string {
	name := strings.Join(strings.Fields(ticket.Clean(workspace.Name)), " ")
	root, _ := formatValue(repoRoot) // a string always encodes
	return fmt.Sprintf(configTemplate, name, workspace.GID, root) + keysTemplate()
}

// keysTemplate is the [keys] section: every binding, commented out with its
// default keys, generated from the keymap catalog.
func keysTemplate() string {
	var b strings.Builder
	b.WriteString(`
[keys]
# Key bindings. Each name takes a list of keys: a character ("j", "G", "?")
# or a key name with modifiers ("ctrl+d", "shift+tab", "enter", "space",
# "pgdown"). An empty list unbinds. ctrl+c quits after protecting comment input.
# Action keys stay in each actions/*.toml file.
`)
	for _, s := range keymap.Catalog() {
		fmt.Fprintf(&b, "\n[keys.%s]\n# %s\n", s.Name, s.Doc)
		for _, k := range s.Bindings {
			quoted := make([]string, len(k.Keys))
			for i, key := range k.Keys {
				quoted[i] = strconv.Quote(key)
			}
			fmt.Fprintf(&b, "# %s = [%s]  # %s\n", k.Name, strings.Join(quoted, ", "), k.Desc)
		}
	}
	return b.String()
}

const summary = `
Wrote %s.

Defaults:
  - View: My Tasks, filtered by "is:open". Press p to switch projects, / to filter.
  - Repo picker: %s
    Set [repo_source] command to use sesh, zoxide, or anything else.
  - Writes to Asana ask for confirmation (confirm_writes = true).
  - Actions: one file each in %s. Rename a .example file to enable it.
  - Themes: "auto" follows the terminal background. Edit the installed presets
    in themes/, or copy the example to make your own.
  - Agent status and time tracking are off. Run asanamate extensions or open
    Settings & tools > Extensions (comma, then o) to discover optional integrations and setup guides.
  - Repo links and recent projects are stored in %s.

Run asanamate to start.
`

const configTemplate = `# asanamate configuration.
# Reference: https://github.com/sadmachine/asanamate#configuration

# Asana workspace: %s
workspace = %q

# Ask before "asanamate comment/move/field" writes to Asana.
# Override per action with confirm_writes, or per call with --yes.
confirm_writes = true

# Custom field holding a ticket's git branch, exposed to actions as
# $ASANAMATE_BRANCH. Empty uses the ID field, else the title slug.
# Example: "Branch Name".
branch_field = ""

# Symbols for ticket markers and agent states: "unicode", "nerd" (needs a Nerd
# Font), or "ascii". Unset: unicode on UTF-8 locales, else ascii.
# symbols = "unicode"
# Static agent symbols instead of the spinner. Unset: follow the OS setting.
# reduced_motion = true

# Theme selection and the auto pair live in settings.toml.

[colors]
# Overrides on top of the active theme. Each value is a color (an ANSI number
# 0-255, which follows your terminal's scheme, or "#rrggbb") or a style table:
#   { fg = "4", bg = "#1a1b26", bold = true, italic = false,
#     underline = false, faint = false, reverse = false }
# A bare color sets fg only; fields left out keep the theme's value.
# Values below are ansi-dark's.
# Reader headings, the focused panel border, NORMAL and VIEWS pills.
# accent = { fg = "4", bold = true }
# Unfocused panel and card borders.
# border = "8"
# Secondary text: distant due dates, list fields, and tickets in the Later
# and No due date groups.
# muted = { faint = true }
# Bar selection and the views panel cursor.
# highlight = { reverse = true }
# Status messages and due-date urgency (today: warn, overdue: error), also
# on the Today and Overdue group headers.
# Positive time logged today uses ok; applied filter text uses warn.
# ok = "2"
# warn = "3"
# error = "1"
# Working coding agents.
# working = "14"
# Group headers and the selection marker; unset uses accent.
# header = { fg = "4", bold = true }
# selection = { fg = "4", bold = true }
# Pinned (P) and Viewing section headers.
# pinned = { fg = "208", bold = true }
# viewing = { fg = "5", bold = true }
# Comment reactions you made, drawn as pills.
# reacted = { bg = "236" }
# NEW badge on My Tasks tickets changed since you last viewed them.
# new = { fg = "15", bg = "4", bold = true }
# +N more badge on Activity events in a row on one ticket.
# more = { fg = "0", bg = "208", bold = true }
# Comment authors, their dots and gutters, cycle through these.
# authors = ["4", "5", "6", "2", "3", "1"]
# Git branch names; unset uses viewing foreground.
# branch = "5"
# Leading [TAG] in ticket titles; unset uses authors. [] disables tag colors.
# title_tags = ["4", "5", "6", "2", "3", "1"]

[colors.asana]
# Project markers and enum options use these hue families. Unset values
# use error, pinned, warn, ok, working, accent, viewing, viewing, and muted.
# red = "1"
# orange = "208"
# yellow = "3"
# green = "2"
# teal = "14"
# blue = "4"
# purple = "5"
# pink = "5"
# gray = "8"

[colors.mode]
# Statusline mode pills. A pill without bg is drawn reversed.
# Unset normal uses accent; unset filter uses warn.
# normal = { fg = "4", bold = true }
# filter = "3"
# read = "6"
# edit = "5"

[colors.markdown]
# Reading pane Markdown, on top of the theme's glamour style. Unset by default,
# except code.
# text = ""
# heading = ""
# h1 = { fg = "228", bg = "63", bold = true }
# link = ""
# code = "2"
# code_block = ""
# quote = ""
# rule = ""

[layout]
# Start with the reading pane beside the list; z shows or hides it for the
# session. --hide-preview starts with it hidden.
reader = true
# Fix the list pane at this many columns, borders included, and give the
# reader the rest; the reader hides when that leaves it too little room.
# 0 sizes the list automatically (= fits it to its content).
list_width = 0

[layout.focus]
# Pane dimming lives in settings.toml (layout.focus.dim).
# Give the focused pane the room: the list fits its titles while it has
# focus, and shrinks to short titles while the reader has it.
resize = true

[list]
# Auto-update interval lives in settings.toml (list.refresh_interval); R and Settings & tools save it.
# "single": one line per ticket. "multi": title on line one, fields on line two.
layout = "single"
# Values shown with the title (the title is always shown), in this order.
# Built-ins: section, due, assignee, initials (the assignee's, as a badge),
# project, tags, completed. Any other name is matched to a custom field, for
# example "Status" or "Branch Name".
# Projects whose columns were set in the TUI keep their own.
fields = ["section", "due"]
# Separators live in settings.toml (list.separator).

[list.default_view]
# The view a project starts with, until you change its grouping (b), sort
# (B), or filter (/) there; asanamate then remembers that project's own view.
filter = "is:open"
group_by = ""

[list.default_view.sort]
by = ""
direction = "asc"

[list.header]
# Group headers: "rule" (── Label (n) ───) or "bar" (reversed bar).
style = "rule"
# Header spacing lives in settings.toml (list.header.spacing).

# Selection style lives in settings.toml (list.selection.style).

[list.new]
# My Tasks tickets changed since you last viewed them carry a NEW badge.
# List them first, under a New header; a ticket you open stays there until
# you move away.
section = false

[reader]
# Reading pane view lives in settings.toml (reader.view).
# Wrap description and comment text in the cards view, and the comment
# editor, at this many columns; 0 wraps at the pane width.
max_text_width = 0

[reader.comments]
# How many of the newest comments, or activity entries in the All tab, the
# cards view lists; Show all lists the rest. 0 lists them all.
latest = 3
# Lines of text kept of each listed comment but the newest, ending in
# "see more"; enter shows one in full. 0 shows every comment in full.
preview_lines = 2

[images]
# Kitty graphics: "auto" detects kitty-protocol terminals, "kitty" forces on,
# "off" disables.
mode = "auto"
# Draw images in descriptions and comments in the cards view instead of links.
inline = false

[picker]
# Open pickers in search mode, so typing filters at once. esc leaves search
# for browse mode (j/k to move); a second esc closes the picker.
type_first = false

[activity]
# Scan Activity (I) in the background at this interval (at least 1s), so it
# opens ready and the views panel counts new related events. "0" turns
# background scans off; Activity then scans when you open it.
refresh_interval = "2m"

[feed]
# The Feed panel under the ticket list: Asana activity, agents, unpushed
# git work, and feed commands. "auto" expands it when the list leaves room,
# "collapsed" keeps it to its border, "expanded" keeps it open. F toggles it.
expand = "auto"
# Rows when expanded, borders included; at least 3.
height = 12
# Run feeds and the git check at this interval (at least 1s); "0" runs them
# only at start and on reload (r).
refresh_interval = "1m"
# Feed commands (see docs/extensions.md#feeds); for today's Harvest time
# entries, add "asanamate-hrvst".
commands = []

[mouse]
# The wheel scrolls the pane under the pointer; a click focuses a pane and
# selects a row; a double click runs enter; clicking a statusline hint runs
# its key; clicking a link in the reader opens it. Hold shift (option in
# iTerm2) while dragging to select text, or set false to leave the mouse to
# the terminal.
enabled = true

# Optional: show running coding agents next to their tickets. Off unless
# command is set. Explicit task GIDs from the source take precedence; otherwise
# tickets match agents on their branch in linked repos. See the README for
# optional conversation links and agent actions.
# The command prints one line per agent,
#   path<TAB>status[<TAB>target[<TAB>title]]
# such as the ccmux adapter, asanamate-ccmux, or your own tool.
# [agents]
# command = "asanamate-ccmux"
# Optional: focus an exact running conversation instead of resuming another
# copy. Runs without terminal input and keeps asanamate open. Other agent
# sources can use their own focus command with the same target variable.
# focus = 'ccmux switch "$ASANAMATE_AGENT_TARGET"'
#
# Map the tool's raw statuses onto asanamate's states. A status equal to a
# state name already maps to it; anything unmapped shows as "unknown".
# [agents.states]
# working   = ["working", "running", "busy"]
# waiting   = ["waiting", "permission", "input"]
# completed = ["done", "finished"]
# idle      = ["idle"]
#
# Override the symbol for any state (working, waiting, completed, idle,
# unknown). Overriding working turns the spinner off.
# [agents.symbols]
# waiting = "!"

# Optional saved Claude Code and Codex conversations for a ticket: the w
# modal's saved rows and "asanamate conversations". Both must be set
# together; empty commands leave the feature off. The commands below need the
# separately installed asana-sessions package. See README "Conversations" and
# docs/extensions.md for the protocol and provider safety requirements.
[conversations]
# command = "asana-sessions asanamate list"
# open = "asana-sessions asanamate open"

# Optional completed time tracking. The command reads a JSON request from
# stdin and writes a form spec as JSON. See README for the protocol.
[time_tracking]
# id = "hrvst"
# Run asanamate-hrvst setup first. --task-id ID remains an explicit override.
# command = "asanamate-hrvst"
# To open the log time form after assigning or completing a ticket, add the
# log_time hooks at the end of this file.

# Optional: suggest each Asana project's git repository and time tracking
# project from another system. The command reads a JSON request from stdin and
# writes the project's links as JSON (docs/extensions.md). imarc staff can use
# asanamate-hub for Hub; see github.com/imarc/asanamate-hub.
[project_links]
# command = "asanamate-hub"


[repo_source]
# Directory whose git repositories (its direct children) fill the repo picker.
# Empty: type a repo path when an action asks for one.
root = %s
# Or a command printing one repo path per line; it replaces root when set.
# Examples: "sesh list -z", "zoxide query -l".
# command = ""

# Optional hooks run a built-in prompt or an action when something happens to
# a ticket, in this order. See README "Hooks".
#   on: "ticket_open" (the reader focuses a ticket), "assign" or "complete"
#     (TUI edits), or "new_activity" (an Activity scan finds a related event).
#   when: a list filter query the ticket must match; empty matches every one.
#   once = true: run at most once per ticket.
#   run: "log_time", "edit:<item>" (title, description, section, field,
#     assign, due, add_project, remove_project, branch), or "action:<file>"
#     (actions/<file>.toml; new_activity runs background actions only).
#
# Ask for a branch the first time you open a ticket without one:
# [[hooks]]
# on = "ticket_open"
# when = "is:open branch:none"
# once = true
# run = "edit:branch"
#
# Log time after handing a ticket to someone else or completing it, once
# time tracking is set up:
# [[hooks]]
# on = "assign"
# when = "-assignee:@me -assignee:none"
# run = "log_time"
#
# [[hooks]]
# on = "complete"
# run = "log_time"
`

// actionFiles are written to the actions directory by setup. The .example
// files stay inactive until renamed to .toml, so a fresh install needs no
// pager or agent tools.
var actionFiles = []struct{ name, body string }{
	{"pager.toml.example", `# Rename to pager.toml to enable. Needs $PAGER, else less.
# One action per file. Every *.toml file here is an action, in file name order.
# Commands run with /bin/sh -c. Ticket data arrives in ASANAMATE_* environment
# variables and in the files $ASANAMATE_TICKET_JSON and $ASANAMATE_TICKET_MD.
# Never paste ticket text into the command; always use the variables.
# Optional [form] fields provide select, bool, number, string, hours, or date
# controls, each with an optional default. Values reach the
# command as $ASANAMATE_PARAM_<ID>. See README for an example.
# For dynamic controls, set [form] command = "your-form-command" instead of
# form.fields. It reads a JSON ticket request and returns {"fields":[...]}.
# mode: "foreground" (suspend the TUI), "background" (detached, logged), or
# "exit" (quit asanamate, then run). repo = true resolves the ticket's repo first.
# context = "comment" shows the action only on a highlighted comment, first in
# the menu, with the comment in $ASANAMATE_COMMENT_*.
name = "View ticket in pager"
key = "v"
mode = "foreground"
command = '${PAGER:-less} "$ASANAMATE_TICKET_MD"'
`},
	{"claude-tmux.toml.example", `# Rename to claude-tmux.toml to enable.
name = "Start Claude in a new tmux window"
key = "c"
mode = "background"
repo = true
# Switch to the ticket's branch first so the agent links to the ticket.
# tmux new-window does not inherit this environment; pass variables with -e.
command = '''git switch "$ASANAMATE_BRANCH" 2>/dev/null || git switch -c "$ASANAMATE_BRANCH" &&
tmux new-window -c "$ASANAMATE_REPO" -n "$ASANAMATE_SLUG" -e "ASANAMATE_TICKET_MD=$ASANAMATE_TICKET_MD" -e "ASANAMATE_GID=$ASANAMATE_GID" -e "ASANA_SESSIONS_TASK=$ASANAMATE_GID" 'claude "$(cat "$ASANAMATE_TICKET_MD")"' '''
`},
}

// themeFiles are written to the themes directory by setup. The .example file
// stays inactive until renamed to .toml and named in [theme].
var themeFiles = []struct{ name, body string }{
	{"example.toml.example", `# Rename to <name>.toml and set [theme] name = "<name>" in config.toml.
# Built-in theme to start from: "dark" or "light". It sets every color this
# file leaves out, and the base Markdown style. Keys match config.toml's
# [colors] tables.
base = "dark"

[colors]
accent = "#7aa2f7"
border = "#3b4261"
muted = "#565f89"
pinned = "#ff9e64"
selection = { fg = "#bb9af7", bold = true }
authors = ["#7aa2f7", "#bb9af7", "#7dcfff", "#9ece6a", "#e0af68"]

[colors.mode]
normal = { fg = "#1a1b26", bg = "#7aa2f7", bold = true }
read = { fg = "#1a1b26", bg = "#7dcfff", bold = true }

[colors.markdown]
heading = { fg = "#7aa2f7", bold = true }
link = { fg = "#7dcfff", underline = true }
code = { fg = "#9ece6a", bg = "#24283b" }
`},
}

// exists reports whether path exists.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// writeFiles adds the default files to dir, keeping any that already exist.
// A .example file is skipped once its enabled .toml exists.
func writeFiles(dir string, files []struct{ name, body string }) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if exists(path) || exists(strings.TrimSuffix(path, ".example")) {
			continue
		}
		if err := writePrivate(path, []byte(f.body)); err != nil {
			return err
		}
	}
	return nil
}
