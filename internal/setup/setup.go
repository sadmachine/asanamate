// Package setup creates the asanamate config file interactively.
package setup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	// CodexHome and Executable let setup offer the Codex status hook; leave
	// either unset to skip it.
	CodexHome  string
	Executable string
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
	warnMissingPager(o.Out)
	return OfferCodexHook(o)
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

// warnMissingPager warns when the pager action's pager is not on PATH.
func warnMissingPager(out io.Writer) {
	pager := Pager()
	if _, err := exec.LookPath(pager); err == nil {
		return
	}
	lipgloss.Fprintln(out, "\n"+warning.Render("Warning: pager "+strconv.Quote(pager)+" not found")+"\n"+
		"  The \"View ticket in pager\" action (v) needs it. Install less, or set\n"+
		"  $PAGER to a pager you have.")
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
# "pgdown"). An empty list unbinds. ctrl+c always quits. Action keys stay in
# each actions/*.toml file.
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
  - Actions: one file each in %s. Rename the .example file to enable it.
  - Themes: "auto" follows the terminal background. Copy the example in
    themes/ to make your own.
  - Agent status and time tracking are off. See the Agents and Time tracking
    sections of the README to turn them on.
  - Repo links and recent projects are stored in %s.

Run asanamate to start.
`

const configTemplate = `# asanamate configuration.
# Reference: https://github.com/sadmachine/asanamate#configuration

# Asana workspace: %s
workspace = %q

# Filter applied at startup. Terms: words, section:, project:, assignee:, tag:,
# project:<name>[<section>], is:open, is:done. Prefix a term with "-" to negate it; quote multi-word values.
default_filter = "is:open"

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

[theme]
# "auto", "dark", "light", or the name of a file in themes/ (without .toml).
# Copy themes/example.toml.example to start your own.
name = "auto"
# Themes "auto" uses on dark and light terminal backgrounds.
dark = "dark"
light = "light"

[colors]
# Overrides on top of the active theme. Each value is a color (an ANSI number
# 0-255, which follows your terminal's scheme, or "#rrggbb") or a style table:
#   { fg = "4", bg = "#1a1b26", bold = true, italic = false,
#     underline = false, faint = false, reverse = false }
# A bare color sets fg only; fields left out keep the theme's value.
# Values below are the dark theme's.
# Reader headings, the focused panel border, NORMAL and VIEWS pills.
# accent = { fg = "4", bold = true }
# Unfocused panel and card borders.
# border = "8"
# Secondary text such as distant due dates.
# muted = { faint = true }
# Bar selection and the views panel cursor.
# highlight = { reverse = true }
# Status messages and due-date urgency (today: warn, overdue: error).
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
# Comment authors cycle through these.
# authors = ["4", "5", "6", "2", "3", "1"]

[colors.mode]
# Statusline mode pills. A pill without bg is drawn reversed.
# Unset normal uses accent; unset filter uses warn.
# normal = { fg = "4", bold = true }
# filter = "3"
# read = "6"
# edit = "5"

[colors.markdown]
# Reading pane Markdown, on top of the theme's glamour style. Unset by default.
# text = ""
# heading = ""
# h1 = { fg = "228", bg = "63", bold = true }
# link = ""
# code = ""
# code_block = ""
# quote = ""
# rule = ""

[list]
# Automatically reload the list at this interval (at least 1s).
# R changes it for the current session only; use durations such as "15s" or "1m".
refresh_interval = "30s"
# "single": one line per ticket. "multi": title on line one, fields on line two.
layout = "single"
# Values shown with the title (the title is always shown), in this order.
# Built-ins: section, due, assignee, initials (the assignee's, as a badge),
# project, tags, completed. Any other name is matched to a custom field, for
# example "Status" or "Branch Name".
fields = ["section", "due"]
# Frame each ticket with lines above and below (neighbours share one); toggle
# it in settings (s).
separator = false
# Group tickets under a header per value of one field, such as "section" or
# "due"; press b to pick another. Empty: ungrouped.
group_by = ""

[list.sort]
# Sort within each group, or the whole ungrouped list; B picks another.
# Any list field or "title". Empty: keep Asana order.
by = ""
# "asc": earliest dates / A-Z; "desc": latest dates / Z-A. Missing values last.
direction = "asc"

[list.header]
# Group headers: "rule" (── Label (n) ───) or "bar" (reversed bar).
style = "rule"
# Blank line above and below each group header; toggle it in settings (s).
spacing = false

[list.selection]
# Selected ticket: "marker" (bold title with a left marker) or "bar" (reversed row).
style = "marker"

[reader]
# Starting view for the reading pane; press v to switch. "cards": sections
# and boxed comments. "markdown": the rendered ticket Markdown.
view = "cards"
# Wrap description and comment text in the cards view at this many columns;
# 0 wraps at the pane width.
max_text_width = 0

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

# Optional: show running coding agents next to their tickets. Off unless a
# preset or command is set. A ticket matches agents on its branch (see
# branch_field) in one of its linked repos. See the README for agent actions.
# [agents]
# preset = "ccmux"
#
# Or, instead of preset, any tool: a command printing one line per agent,
#   path<TAB>status[<TAB>target[<TAB>title]]
# command = '''my-agents --tsv'''
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

# Optional completed time tracking. The command reads a JSON request from
# stdin and writes a form spec as JSON. See README for the protocol.
[time_tracking]
# id = "hrvst"
# command = "asanamate time-provider hrvst --task-id YOUR_TASK_ID"


[repo_source]
# Directory whose git repositories (its direct children) fill the repo picker.
# Empty: type a repo path when an action asks for one.
root = %s
# Or a command printing one repo path per line; it replaces root when set.
# Examples: "sesh list -z", "zoxide query -l".
# command = ""
`

// actionFiles are written to the actions directory by setup. The .example
// file stays inactive until renamed to .toml.
var actionFiles = []struct{ name, body string }{
	{"pager.toml", `# One action per file. Every *.toml file here is an action, in file name order.
# Commands run with /bin/sh -c. Ticket data arrives in ASANAMATE_* environment
# variables and in the files $ASANAMATE_TICKET_JSON and $ASANAMATE_TICKET_MD.
# Never paste ticket text into the command; always use the variables.
# Optional [form] fields provide select or hours controls. Values reach the
# command as $ASANAMATE_PARAM_<ID>. See README for an example.
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
tmux new-window -c "$ASANAMATE_REPO" -n "$ASANAMATE_SLUG" -e "ASANAMATE_TICKET_MD=$ASANAMATE_TICKET_MD" -e "ASANAMATE_GID=$ASANAMATE_GID" 'claude "$(cat "$ASANAMATE_TICKET_MD")"' '''
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

// writeFiles adds the default files to dir, keeping any that already exist.
func writeFiles(dir string, files []struct{ name, body string }) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := writePrivate(path, []byte(f.body)); err != nil {
			return err
		}
	}
	return nil
}
