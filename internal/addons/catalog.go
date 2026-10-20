// Package addons describes optional integrations and their documented local checks.
// Discovery never executes providers or reads credentials.
package addons

import (
	"fmt"
	"strings"

	"github.com/sadmachine/asanamate/internal/config"
)

const readme = "https://github.com/sadmachine/asanamate#"
const Extensions = "https://github.com/sadmachine/asanamate/blob/main/docs/extensions.md"

// Capability is one independently configured extension point. Commands reads
// current config; recognizes accepts only documented command forms.
type Capability struct {
	Name, Key, Executable string
	TaskID                bool
	Commands              func(config.Config) []string
}

func (c Capability) recognizes(command string) bool {
	words := strings.Fields(command)
	if len(words) == 1 && words[0] == c.Executable && !c.TaskID {
		return true
	}
	if !c.TaskID || len(words) != 3 || words[0] != c.Executable || words[1] != "--task-id" || words[2] == "" {
		return false
	}
	for _, r := range words[2] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Entry is shared content for CLI and TUI. All commands here are literal,
// shipped data, never commands returned by another executable.
type Entry struct {
	ID, Name, Benefit, Availability, Guide string
	Adapters, Dependencies                 []string
	Capabilities                           []Capability
	Steps                                  []string
	Snippet, Verify, Example, Remove       string
	CheckFailure                           string
	Check, Setup                           []string
	Builder, CheckArray                    bool
}

// Catalog returns the built-in catalog in display order.
func Catalog() []Entry {
	return []Entry{
		{
			ID: "harvest", Name: "Harvest", Benefit: "Log ticket time and show today's Harvest entries",
			Availability: "macOS/Linux (amd64/arm64); Harvest account with project access",
			Guide:        readme + "time-tracking-optional", Adapters: []string{"asanamate-hrvst"}, Dependencies: []string{"hrvst", "node", "npm"},
			Capabilities: []Capability{
				{Name: "Time tracking", Key: "time_tracking.command", Executable: "asanamate-hrvst", TaskID: true, Commands: func(c config.Config) []string { return []string{c.TimeTracking.Command} }},
				{Name: "Today's feed", Key: "feed.commands", Executable: "asanamate-hrvst", Commands: func(c config.Config) []string { return c.Feed.Commands }},
			},
			Steps: []string{
				"Install the bundled adapter using the same asanamate archive or source checkout (instructions below).",
				"Install Node.js 20+ from https://nodejs.org/en/download, then hrvst 3.x from https://github.com/kgajera/hrvst-cli:\nnpm install -g hrvst-cli@3\nThe npm global bin directory must be on PATH (npm prefix -g; bin/ on macOS/Linux).",
				"Run hrvst login to authorize your Harvest account. Provider setup below runs this command. Credentials stay in ~/.hrvst/config.json; core never reads them.",
				"Find your numeric task.id under task_assignments:\nhrvst users project-assignments me --output=json\nAdd either or both capabilities below to config.toml. Replace YOUR_TASK_ID; keep your existing provider ID if already configured. Merge into existing tables; do not create duplicate tables or replace custom commands.",
			},
			Snippet: "[time_tracking]\nid = \"hrvst\"\ncommand = \"asanamate-hrvst --task-id YOUR_TASK_ID\"\n\n[feed]\ncommands = [\"asanamate-hrvst\"]",
			Check:   []string{"asanamate-hrvst", "check"}, Setup: []string{"hrvst", "login"},
			CheckFailure: "Run hrvst login, then asanamate addons check harvest. For live account diagnostics, run hrvst users project-assignments me --output=json.",
			Verify:       "asanamate addons check harvest runs asanamate-hrvst check: installed hrvst and readable local login only, no Harvest API call. Connection remains unverified. Explicit live verification contacts your authorized Harvest account: hrvst users project-assignments me --output=json.",
			Example:      "Restart asanamate. Press t on a ticket: a form listing your Harvest projects proves live read access; Esc cancels without logging time. Enable the feed separately to see today's entries. Saving the time form creates an entry, so do so only when intended.",
			Remove:       "Remove the time_tracking keys to disable tracking; remove only asanamate-hrvst from feed.commands to disable its feed. Restart. Delete asanamate-hrvst from your bin directory to uninstall the adapter. npm uninstall -g hrvst-cli removes the dependency. Remove ~/.hrvst/config.json only if you want to discard its login; revoke authorization in Harvest if needed.",
		},
		{
			ID: "ccmux", Name: "ccmux", Benefit: "Show coding agents beside their tickets",
			Availability: "macOS/Linux (amd64/arm64); ccmux terminal sessions",
			Guide:        readme + "agents-optional", Adapters: []string{"asanamate-ccmux"}, Dependencies: []string{"ccmux", "tmux"},
			Capabilities: []Capability{{Name: "Agent status", Key: "agents.command", Executable: "asanamate-ccmux", Commands: func(c config.Config) []string { return []string{c.Agents.Command} }}},
			Steps: []string{
				"Install the bundled adapter from your existing asanamate archive or source checkout (instructions below).",
				"Install ccmux from https://github.com/motherskitchenblr2/ccmux. macOS with Homebrew: brew install epilande/tap/ccmux. macOS/Linux source: install Bun from https://bun.sh, then git clone https://github.com/epilande/ccmux.git; cd ccmux; bun install; bun link. Install tmux from https://github.com/tmux/tmux (macOS: brew install tmux; Debian/Ubuntu: sudo apt install tmux). Use a current ccmux release supporting ccmux show --json; no numeric minimum is specified. Git 2.31+ enables branch detection. Put the Homebrew or Bun bin directory on PATH; command -v ccmux shows the final executable location. Start a tmux session for your agents.",
				"For Codex status, optionally run asanamate-ccmux setup. It offers to add the status hook to your Codex hooks.json; provider prompts own these writes. Claude Code status needs no hook setup.",
				"Add the agent source below to config.toml, preserving any custom provider until you choose to replace it.",
			},
			Snippet: "[agents]\ncommand = \"asanamate-ccmux\"", Check: []string{"ccmux", "show", "--json"}, CheckArray: true, Setup: []string{"asanamate-ccmux", "setup"},
			CheckFailure: "Start a tmux session, then run ccmux show --json to diagnose local session discovery. See the ccmux installation guide below.",
			Verify:       "asanamate addons check ccmux runs ccmux show --json, a read-only local session query. It checks for a JSON session array; it does not authenticate any agent service. Empty sessions are valid. Connection to agent services remains unverified.",
			Example:      "Start an agent in ccmux in a ticket's linked repository and branch. Restart asanamate; its state appears beside the ticket. asanamate doctor <task-gid> explains branch and repository matching.",
			Remove:       "Remove agents.command and restart to disable. Remove only the asanamate-ccmux hook entries from Codex hooks.json if you installed them (default ~/.codex/hooks.json, or $CODEX_HOME/hooks.json). Delete asanamate-ccmux from your bin directory. Uninstall ccmux following its own guide if no longer used.",
		},
		{
			ID: "hub", Name: "imarc Hub", Benefit: "Suggest project repositories and Harvest projects",
			Availability: "Private: requires imarc access before visiting its repository. macOS/Linux; Go source installation.",
			Guide:        "https://github.com/imarc/asanamate-hub", Dependencies: []string{"asanamate-hub"},
			Capabilities: []Capability{{Name: "Project links", Key: "project_links.command", Executable: "asanamate-hub", Commands: func(c config.Config) []string { return []string{c.ProjectLinks.Command} }}},
			Steps: []string{
				"Obtain imarc access to https://github.com/imarc/asanamate-hub. It is private; users without access cannot install this addon.",
				"Install Go 1.26+ from https://go.dev/dl/ and authenticate GitHub access as described in the private README. Install from source: git clone git@github.com:imarc/asanamate-hub.git; cd asanamate-hub; go install . (Authenticated HTTPS also works.) Output is $(go env GOBIN), else $(go env GOPATH)/bin; put that directory on PATH. Hub is external, not bundled. Requires asanamate v0.2.0+; no minimum provider version is specified by core.",
				"Create a read-only service account at https://hub.imarc.net/admin/service-accounts. Export HUB_CLIENT_ID and HUB_CLIENT_SECRET in the environment that starts asanamate. Never paste these secrets into core config. asanamate-hub check verifies only their presence, without contacting Hub. Core does not implement Hub authentication.",
				"Add the project links source below to config.toml. Preserve any custom source until you choose to replace it.",
			},
			Snippet:      "[project_links]\ncommand = \"asanamate-hub\"",
			Check:        []string{"asanamate-hub", "check"},
			CheckFailure: "Export HUB_CLIENT_ID and HUB_CLIENT_SECRET from your Hub service account, then run asanamate addons check hub. Never paste secrets into core config.",
			Verify:       "asanamate addons check hub runs asanamate-hub check: checks only that HUB_CLIENT_ID and HUB_CLIENT_SECRET are set, without contacting Hub or printing their values. Connection unverified. Follow the first-use example to verify your imarc Hub service account.",
			Example:      "Restart asanamate. On an Asana project linked in Hub, open the repo picker: matching local repositories appear as suggested. The Harvest time form can also preselect the linked Harvest project. These lookups contact your configured Hub service.",
			Remove:       "Remove project_links.command and restart to disable. Delete asanamate-hub from its bin directory to uninstall. Follow the private guide to revoke or remove provider credentials; core stores none.",
		},
		{
			ID: "actions", Name: "Actions and custom commands", Benefit: "Build actions or bring your own extension commands",
			Availability: "macOS/Linux; requirements depend on your commands", Guide: Extensions, Builder: true,
			Steps: []string{
				"No addon executable is required. Install only the tools your own commands need, following their platform and version requirements.",
				"Open Settings > Build / edit actions (or use Open action builder here). Create an action; the builder validates and saves it in actions/ beside config.toml.",
				"For custom commands, read the extension guide: actions, agent sources, repo sources, time tracking, project links and feeds. Conversation history is also documented there. All remain valid without registering in this catalog.",
			},
			Snippet: "# Example actions/example.toml (or create with the action builder)\nname = \"Show ticket\"\nkey = \"X\"\nmode = \"foreground\"\ncommand = 'printf \"%s\\n\" \"$ASANAMATE_URL\"'",
			Verify:  "asanamate addons check actions validates saved core configuration and action files only. Custom commands are unverified and never executed by addon checks. Follow each extension protocol's documented verification path.",
			Example: "Use the action builder to save the example, then select Show ticket from a ticket's action menu. It prints that ticket's URL without changing Asana.",
			Remove:  "Remove or rename an action's .toml file to .toml.example. Remove a custom command from its owning config key to disable that extension. Restart after hand edits. Uninstall external tools separately only if no other commands use them.",
		},
	}
}

// Find reports an actionable error including every available ID.
func Find(id string) (Entry, error) {
	var ids []string
	for _, e := range Catalog() {
		if e.ID == id {
			return e, nil
		}
		ids = append(ids, e.ID)
	}
	return Entry{}, fmt.Errorf("unknown addon %q; available IDs: %s (run asanamate addons)", id, strings.Join(ids, ", "))
}

// KnownCommand returns only an exact documented argument vector for an
// extension point. A false result identifies an unverified custom command.
func KnownCommand(key, command string) ([]string, bool) {
	for _, e := range Catalog() {
		for _, c := range e.Capabilities {
			if c.Key == key && c.recognizes(command) {
				return strings.Fields(command), true
			}
		}
	}
	return nil, false
}
