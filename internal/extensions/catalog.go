// Package extensions describes optional integrations and their documented local checks.
// Discovery never executes providers or reads credentials.
package extensions

import (
	"fmt"
	"slices"
	"strings"

	"github.com/sadmachine/asanamate/internal/config"
)

const readme = "https://github.com/sadmachine/asanamate#"
const ProtocolGuide = repository + "/blob/main/docs/extensions.md"

// Capability is one independently configured extension point. Commands reads
// current config; recognizes accepts only documented command forms.
type Capability struct {
	Name, Key, Executable, ContractID string
	Arguments                         []string
	TaskID                            bool
	Commands                          func(config.Config) []string
}

func (c Capability) recognizes(command string) bool {
	words := strings.Fields(command)
	if len(words) == 1+len(c.Arguments) && words[0] == c.Executable && slices.Equal(words[1:], c.Arguments) {
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
	Diagnostics                            []string
	SetupLabel, Supports                   string
	Notes                                  []string
	Builder, CheckArray                    bool
}

// Catalog returns the built-in catalog in display order.
func Catalog() []Entry {
	return []Entry{
		{
			ID: "harvest", Name: "Harvest", Benefit: "Log ticket time and show today's Harvest entries",
			Availability: "macOS/Linux (amd64/arm64); Harvest account with project access",
			Guide:        readme + "time-tracking-optional", Adapters: []string{"asanamate-hrvst"}, Dependencies: nil,
			Capabilities: []Capability{
				{Name: "Time tracking", ContractID: "time-tracking", Key: "time_tracking.command", Executable: "asanamate-hrvst", TaskID: true, Commands: func(c config.Config) []string { return []string{c.TimeTracking.Command} }},
				{Name: "Today's feed", ContractID: "feeds", Key: "feed.commands", Executable: "asanamate-hrvst", Commands: func(c config.Config) []string { return c.Feed.Commands }},
			},
			Supports: "Time tracking: form, log, and optional summary with daily totals. Feeds: feed. These are asanamate contracts, independent of Harvest API v2.",
			Steps: []string{
				"Install the bundled adapter using the same asanamate archive or source checkout (instructions below). No Node.js, npm, or hrvst-cli is needed.",
				"Create a personal access token at https://id.getharvest.com/developers. Export HARVEST_ACCESS_TOKEN in the environment starting asanamate; optionally export HARVEST_ACCOUNT_ID. Never put the token in config or command arguments.",
				"Run asanamate-hrvst setup: verify account access, choose time tracking, feeds, or both, and select a default task by name. It previews nonsecret harvest.toml settings beside core config; it never writes core config. Existing legacy ~/.hrvst/config.json logins remain supported without hrvst-cli.",
				"Add either or both capabilities below. Merge existing tables and preserve other feed commands and custom providers. Time tracking uses the saved default; an existing --task-id override still wins. The optional log_time hooks open the time form after you assign a ticket to someone else or complete it. Feed-only setup needs no task.",
			},
			Snippet: "[time_tracking]\nid = \"hrvst\"\ncommand = \"asanamate-hrvst\"\n\n[feed]\ncommands = [\"asanamate-hrvst\"]\n\n# Optional: open the log time form after assigning a ticket to someone else or completing it.\n[[hooks]]\non = \"assign\"\nwhen = \"-assignee:@me -assignee:none\"\nrun = \"log_time\"\n\n[[hooks]]\non = \"complete\"\nrun = \"log_time\"",
			Check:   []string{"asanamate-hrvst", "check"}, Setup: []string{"asanamate-hrvst", "setup"},
			CheckFailure: "Run asanamate-hrvst setup with HARVEST_ACCESS_TOKEN exported. For explicit read-only account and assignment verification: asanamate-hrvst check --online.",
			Verify:       "asanamate extensions check harvest runs asanamate-hrvst check: validates local settings and credential source only, no network. Remote access remains unverified. No default task is valid for feeds. Run asanamate-hrvst check --online explicitly to verify account/user identity and task eligibility without writes.",
			Example:      "Restart asanamate. Press t on a ticket: inspect the Harvest projects, then Esc without logging time. Enable the feed separately to see today's entries. Saving the form creates an entry, so do so only when intended.",
			Remove:       "Remove time_tracking keys to disable tracking; remove only asanamate-hrvst from feed.commands to disable its feed. Restart. Delete the adapter binary to uninstall. Unset HARVEST_ACCESS_TOKEN/HARVEST_ACCOUNT_ID in your launch environment and remove harvest.toml to discard native settings. Legacy ~/.hrvst/config.json remains untouched; remove it only to discard that login. Revoke tokens in Harvest when needed.",
		},
		{
			ID: "ccmux", Name: "ccmux", Benefit: "Show coding agents beside their tickets",
			Availability: "macOS/Linux (amd64/arm64); ccmux terminal sessions",
			Guide:        readme + "agents-optional", Adapters: []string{"asanamate-ccmux"}, Dependencies: []string{"ccmux", "tmux"},
			Capabilities: []Capability{{Name: "Agent status", ContractID: "agent-sources", Key: "agents.command", Executable: "asanamate-ccmux", Commands: func(c config.Config) []string { return []string{c.Agents.Command} }}},
			Steps: []string{
				"Install the bundled adapter from your existing asanamate archive or source checkout (instructions below).",
				"Install ccmux from https://github.com/motherskitchenblr2/ccmux. macOS with Homebrew: brew install epilande/tap/ccmux. macOS/Linux source: install Bun from https://bun.sh, then git clone https://github.com/epilande/ccmux.git; cd ccmux; bun install; bun link. Install tmux from https://github.com/tmux/tmux (macOS: brew install tmux; Debian/Ubuntu: sudo apt install tmux). Use a current ccmux release supporting ccmux show --json; no numeric minimum is specified. Git 2.31+ enables branch detection. Put the Homebrew or Bun bin directory on PATH; command -v ccmux shows the final executable location. Start a tmux session for your agents.",
				"For Codex status, optionally run asanamate-ccmux setup. It offers to add the status hook to your Codex hooks.json; provider prompts own these writes. Claude Code status needs no hook setup.",
				"Add the agent source below to config.toml, preserving any custom provider until you choose to replace it.",
			},
			Snippet: "[agents]\ncommand = \"asanamate-ccmux\"", Check: []string{"ccmux", "show", "--json"}, CheckArray: true, Setup: []string{"asanamate-ccmux", "setup"},
			CheckFailure: "Start a tmux session, then run ccmux show --json to diagnose local session discovery. See the ccmux installation guide below.",
			Verify:       "asanamate extensions check ccmux runs ccmux show --json, a read-only local session query. It checks for a JSON session array; it does not authenticate any agent service. Empty sessions are valid. Connection to agent services remains unverified.",
			Example:      "Start an agent in ccmux in a ticket's linked repository and branch. Restart asanamate; its state appears beside the ticket. asanamate doctor <task-gid> explains branch and repository matching.",
			Remove:       "Remove agents.command and restart to disable. Remove only the asanamate-ccmux hook entries from Codex hooks.json if you installed them (default ~/.codex/hooks.json, or $CODEX_HOME/hooks.json). Delete asanamate-ccmux from your bin directory. Uninstall ccmux following its own guide if no longer used.",
		},
		{
			ID: "hub", Name: "imarc Hub", Benefit: "Suggest project repositories and Harvest projects",
			Availability: "Private: requires imarc access before visiting its repository. macOS/Linux; Go source installation.",
			Guide:        "https://github.com/imarc/asanamate-hub", Dependencies: []string{"asanamate-hub"},
			Capabilities: []Capability{{Name: "Project links", ContractID: "project-links", Key: "project_links.command", Executable: "asanamate-hub", Commands: func(c config.Config) []string { return []string{c.ProjectLinks.Command} }}},
			Steps: []string{
				"Obtain imarc access to https://github.com/imarc/asanamate-hub. It is private; users without access cannot install this extension.",
				"Install Go 1.26+ from https://go.dev/dl/ and authenticate GitHub access as described in the private README. Install from source: git clone git@github.com:imarc/asanamate-hub.git; cd asanamate-hub; go install . (Authenticated HTTPS also works.) Output is $(go env GOBIN), else $(go env GOPATH)/bin; put that directory on PATH. Hub is external, not bundled. Requires asanamate v0.2.0+; no minimum provider version is specified by core.",
				"Create a read-only service account at https://hub.imarc.net/admin/service-accounts. Export HUB_CLIENT_ID and HUB_CLIENT_SECRET in the environment that starts asanamate. Never paste these secrets into core config. asanamate-hub check verifies only their presence, without contacting Hub. Core does not implement Hub authentication.",
				"Add the project links source below to config.toml. Preserve any custom source until you choose to replace it.",
			},
			Snippet:      "[project_links]\ncommand = \"asanamate-hub\"",
			Check:        []string{"asanamate-hub", "check"},
			CheckFailure: "Export HUB_CLIENT_ID and HUB_CLIENT_SECRET from your Hub service account, then run asanamate extensions check hub. Never paste secrets into core config.",
			Verify:       "asanamate extensions check hub runs asanamate-hub check: checks only that HUB_CLIENT_ID and HUB_CLIENT_SECRET are set, without contacting Hub or printing their values. Connection unverified. Follow the first-use example to verify your imarc Hub service account.",
			Example:      "Restart asanamate. On an Asana project linked in Hub, open the repo picker: matching local repositories appear as suggested. The Harvest time form can also preselect the linked Harvest project. These lookups contact your configured Hub service.",
			Remove:       "Remove project_links.command and restart to disable. Delete asanamate-hub from its bin directory to uninstall. Follow the private guide to revoke or remove provider credentials; core stores none.",
		},
		{
			ID: "asana-sessions", Name: "Conversation history", Benefit: "Find saved conversations and link running agents to tickets",
			Availability: "Separately installed asana-sessions package; macOS/Linux. Core supports the conversation contracts described below. Helper/package and optional native plugins have independent compatibility checks.",
			Guide:        repository + "/blob/main/packages/asana-sessions/README.md", Dependencies: []string{"asana-sessions", "node"},
			Capabilities: []Capability{
				{Name: "History lookup", ContractID: "conversation-discovery", Key: "conversations.command", Executable: "asana-sessions", Arguments: []string{"asanamate", "list"}, Commands: func(c config.Config) []string { return []string{c.Conversations.Command} }},
				{Name: "Terminal resume", ContractID: "conversation-open", Key: "conversations.open", Executable: "asana-sessions", Arguments: []string{"asanamate", "open"}, Commands: func(c config.Config) []string { return []string{c.Conversations.Open} }},
				{Name: "Agent ticket links", ContractID: "agent-sources", Key: "agents.command", Executable: "asana-sessions", Arguments: []string{"asanamate", "agents", "--", "asanamate-ccmux"}, Commands: func(c config.Config) []string { return []string{c.Agents.Command} }},
			},
			Notes: []string{
				"Core support: both conversation contracts described above are implemented by this core build. A helper's version is not its protocol version.",
				"Optional automatic linking: unchecked by discovery and independent of history lookup/resume. Missing, disabled, or untrusted Claude/Codex plugins do not disable manually linked history. Run detailed diagnostics to see each plugin's actual state.",
				"A successful local check is not proof that a selected conversation is inactive or safe to resume. Only a deliberate Conversations action opens a session.",
				"Agent ticket links are independent of history lookup/resume. The source still supplies live status and terminal focus; the helper joins exact provider/session identity to a task GID. Missing identity or unavailable metadata retains repo/branch fallback; conflicting links match no ticket. Source requirements belong to the chosen source: the ccmux example needs asanamate-ccmux and ccmux, but history does not.",
				"For automatic fallback when the helper is missing, set only agents.command to this shell text (merge into the existing [agents] table): if command -v asana-sessions >/dev/null 2>&1; then asana-sessions asanamate agents -- asanamate-ccmux; else asanamate-ccmux; fi. Shell wrappers and other agent sources remain custom commands with unverified implementation; the documented plain decorator is recognized exactly.",
			},
			Steps: []string{
				"Install the standalone asana-sessions .tgz with its install.mjs and verify both checksums using the package guide. It is not bundled with Go archives. From a checkout: cd packages/asana-sessions; npm ci; npm pack; node install.mjs ./asana-sessions-<version>.tgz. Replace <version> with the packed version. The installer defaults to ~/.local; put ~/.local/bin on the provider's PATH. Package CI targets Node 22/24 on macOS/Linux; use the guide's verified provider/SDK compatibility requirements. No public npm registry name is reserved.",
				"Put the helper on PATH in the environment starting asanamate. Run asana-sessions doctor for runtime, history, provider version, and optional plugin diagnostics. No Asana token or automatic-linking plugin is required for history.",
				"For history, add both conversation commands below, preserving any custom source/opener. They are separate contracts: lookup receives JSON on stdin; resume receives a private snapshot and terminal input. Restart asanamate after editing config.",
				"Optional agent ticket links: use asana-sessions asanamate agents -- asanamate-ccmux as agents.command, preserving any custom source until you choose to replace it. The helper and source adapter must support the optional identity columns; older sources retain repo/branch fallback. Any compatible source can be decorated; custom sources need no catalog registration. Neither conversation setting nor automatic-linking plugins are required.",
				"Optional: set up automatic linking with asana-sessions setup. It registers selected native Claude/Codex plugins and prints remaining enable/trust/restart steps. It does not configure core. Review trust yourself; helper installation and manually linked history work without these plugins.",
			},
			Snippet: `[conversations]
command = "asana-sessions asanamate list"
open = "asana-sessions asanamate open"

# Optional agent ticket links; independent of history. Merge into existing [agents].
# [agents]
# command = "asana-sessions asanamate agents -- asanamate-ccmux"`,
			Check: []string{"asana-sessions", "doctor"}, Diagnostics: []string{"asana-sessions", "doctor"}, Setup: []string{"asana-sessions", "setup"}, SetupLabel: "Set up automatic linking (optional)…",
			CheckFailure: "Run detailed diagnostics (asana-sessions doctor) for exact runtime, history, compatibility, and optional plugin states. A failed hook check does not establish that history lookup failed. Follow the package guide; do not resume a session to test readiness.",
			Verify:       "asanamate extensions check asana-sessions runs bounded nonmutating helper diagnostics. Run detailed diagnostics for separate plugin statuses. It never links, renames, unarchives, resumes, or starts a model turn. Terminal opening remains unverified; configured custom sources/openers are never executed by this check.",
			Example:      "On a ticket with an existing [asana:<gid>] title marker, open Conversations from its action menu, inspect history and diagnostics, then cancel. Manual title markers work without hooks. A selected history record is not proof of inactivity; terminal resume is a separate explicit action.",
			Remove:       "Remove both conversations.command and conversations.open to disable core integration, then restart. asana-sessions remove removes only its optional native plugin registrations/pending state; existing histories and ticket markers remain. Removing plugins keeps standalone lookup/open available. Before uninstalling the helper, if agents.command uses its decorator, restore the underlying source (for example command = \"asanamate-ccmux\") or retain the documented missing-helper fallback. Preserve unrelated agent commands. Then uninstall with npm uninstall --global asana-sessions after disabling its core commands.",
		},
		{
			ID: "actions", Name: "Actions and custom commands", Benefit: "Build actions or bring your own extension commands",
			Availability: "macOS/Linux; requirements depend on your commands", Guide: ProtocolGuide, Builder: true,
			Steps: []string{
				"No extension executable is required. Install only the tools your own commands need, following their platform and version requirements.",
				"Open Settings > Build / edit actions (or use Open action builder here). Create an action; the builder validates and saves it in actions/ beside config.toml.",
				"For custom commands, read the extension guide: actions, agent sources, repo sources, time tracking, project links and feeds. Conversation history is also documented there. All remain valid without registering in this catalog.",
			},
			Snippet: "# Example actions/example.toml (or create with the action builder)\nname = \"Show ticket\"\nkey = \"X\"\nmode = \"foreground\"\ncommand = 'printf \"%s\\n\" \"$ASANAMATE_URL\"'",
			Verify:  "asanamate extensions check actions validates saved core configuration and action files only. Custom commands are unverified and never executed by extension checks. Follow each extension protocol's documented verification path.",
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
	return Entry{}, fmt.Errorf("unknown extension %q; available IDs: %s (run asanamate extensions)", id, strings.Join(ids, ", "))
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
