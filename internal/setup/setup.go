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

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/prompt"
	"github.com/sadmachine/asanamate/internal/repo"
	"github.com/sadmachine/asanamate/internal/ticket"
)

const defaultRepoRoot = "~/code"

// Options wires setup to its input, output, and file locations.
type Options struct {
	In         *bufio.Reader
	Out        io.Writer
	Client     *asana.Client
	ConfigPath string
	StatePath  string
}

// Run asks for the workspace and repo directory, then writes the config file.
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
	fmt.Fprintf(o.Out, summary, o.ConfigPath, root, o.StatePath)
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

func chooseRepoRoot(o Options) (string, error) {
	for {
		answer, err := prompt.Line(o.In, o.Out, "Directory that contains your git repositories", defaultRepoRoot)
		if err != nil {
			return "", err
		}
		dir, err := filepath.Abs(repo.ExpandHome(answer))
		if err != nil {
			return "", err
		}
		if strings.Contains(dir, "'") {
			fmt.Fprintln(o.Out, "Paths containing ' are not supported.")
			continue
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			fmt.Fprintf(o.Out, "%s is not a directory.\n", dir)
			continue
		}
		return dir, nil
	}
}

// Render returns the commented config file for a workspace and repo directory.
func Render(workspace asana.Ref, repoRoot string) string {
	name := strings.Join(strings.Fields(ticket.Clean(workspace.Name)), " ")
	return fmt.Sprintf(configTemplate, name, workspace.GID, repoRoot, repoRoot)
}

const summary = `
Wrote %s.

Defaults:
  - View: My Tasks, filtered by "is:open". Press p to switch projects, / to filter.
  - Repo picker: git repositories directly inside %s.
    Change [repo_source] command to use sesh, zoxide, or anything else.
  - Writes to Asana ask for confirmation (confirm_writes = true).
  - Repo links and recent projects are stored in %s.

Run asanamate to start.
`

const configTemplate = `# asanamate configuration.
# Reference: https://github.com/sadmachine/asanamate#configuration

# Asana workspace: %s
workspace = %q

# Markdown style for the reading pane: "dark" or "light".
theme = "dark"

# Accent for reader headings, group headers, and the selection marker: an ANSI
# color number (0-255) or "#rrggbb".
accent_color = "4"

# Images: "auto" detects kitty-protocol terminals, "kitty" forces on, "off" disables.
images = "auto"

# Filter applied at startup. Terms: words, section:, project:, assignee:, tag:,
# is:open, is:done. Prefix a term with "-" to negate it; quote multi-word values.
default_filter = "is:open"

# Ask before "asanamate comment/move/field" writes to Asana.
# Override per action with confirm_writes, or per call with --yes.
confirm_writes = true

# Custom field holding a ticket's git branch, exposed to actions as
# $ASANAMATE_BRANCH. Empty uses the title slug. Example: "Branch Name".
branch_field = ""

# Symbols for ticket markers and agent states: "unicode", "nerd" (needs a Nerd
# Font), or "ascii". Unset: unicode on UTF-8 locales, else ascii.
# symbols = "unicode"
# Static agent symbols instead of the spinner. Unset: follow the OS setting.
# reduced_motion = true

[list]
# "single": one line per ticket. "multi": title on line one, fields on line two.
layout = "single"
# Values shown with the title (the title is always shown). Built-ins: section,
# due, assignee, project, tags, completed. Any other name is matched to a
# custom field, for example "Status" or "Branch Name".
fields = ["section"]
# Frame each ticket with lines above and below (neighbours share one).
separator = false
# Group tickets under a header per value of one field, such as "section" or
# "due"; press b to pick another. Empty: ungrouped.
group_by = ""

[list.header]
# Group headers: "bar" (reversed bar) or "rule" (── Label (n) ───).
style = "bar"
# Header color; unset uses accent_color.
# color = "4"

[list.selection]
# Selected ticket: "marker" (bold title with a left marker) or "bar" (reversed row).
style = "marker"
# Marker color; unset uses accent_color.
# color = "4"

[reader]
# Starting view for the reading pane; press v to switch. "cards": sections
# and boxed comments. "markdown": the rendered ticket Markdown.
view = "cards"
# Wrap description and comment text in the cards view at this many columns;
# 0 wraps at the pane width.
max_text_width = 0

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


[repo_source]
# Prints one git repository path per line for the repo picker.
# The default lists repositories directly inside %s.
# Alternatives: "sesh list -z", "zoxide query -l".
command = '''find '%s' -mindepth 2 -maxdepth 2 -name .git -exec dirname {} \;'''

# Actions run with /bin/sh -c. Ticket data arrives in ASANAMATE_* environment
# variables and in the files $ASANAMATE_TICKET_JSON and $ASANAMATE_TICKET_MD.
# Never paste ticket text into the command; always use the variables.
# mode: "foreground" (suspend the TUI), "background" (detached, logged), or
# "exit" (quit asanamate, then run). repo = true resolves the ticket's repo first.

[[actions]]
name = "View ticket in pager"
key = "v"
mode = "foreground"
command = '${PAGER:-less} "$ASANAMATE_TICKET_MD"'

# [[actions]]
# name = "Start Claude in a new tmux window"
# key = "c"
# mode = "background"
# repo = true
# # Switch to the ticket's branch first so the agent links to the ticket.
# # tmux new-window does not inherit this environment; pass variables with -e.
# command = '''git switch "$ASANAMATE_BRANCH" 2>/dev/null || git switch -c "$ASANAMATE_BRANCH" &&
# tmux new-window -c "$ASANAMATE_REPO" -n "$ASANAMATE_SLUG" -e "ASANAMATE_TICKET_MD=$ASANAMATE_TICKET_MD" -e "ASANAMATE_GID=$ASANAMATE_GID" 'claude "$(cat "$ASANAMATE_TICKET_MD")"' '''
`
