package extensions

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/sadmachine/asanamate/internal/conversations"
	"github.com/sadmachine/asanamate/internal/feed"
	"github.com/sadmachine/asanamate/internal/projectlinks"
	"github.com/sadmachine/asanamate/internal/timetracking"
)

const repository = "https://github.com/sadmachine/asanamate"

// Contract describes core's interface, independently of an implementation's
// release number or its external service's API. Version zero is unversioned.
type Contract struct {
	ID, Name, Transport, Configuration, Operations, Anchor string
	Version                                                int
}

// Contracts is the shared reference for catalog entries and custom commands.
func Contracts() []Contract {
	return []Contract{
		{ID: "actions", Name: "Actions", Transport: "environment variables and private ticket files; exit status", Configuration: "actions/*.toml", Operations: "run as foreground, background, or exit action", Anchor: "actions"},
		{ID: "agent-sources", Name: "Agent sources", Transport: "tab-separated lines on stdout", Configuration: "agents.command, agents.states", Operations: "list running agents; not persisted conversation history", Anchor: "agent-sources"},
		{ID: "repo-sources", Name: "Repo sources", Transport: "one repository path per stdout line", Configuration: "repo_source.command", Operations: "list repositories", Anchor: "repo-sources"},
		{ID: "time-tracking", Name: "Time tracking", Version: timetracking.ProtocolVersion, Transport: "JSON request on stdin; form/summary JSON on stdout; log exit status", Configuration: "time_tracking.id, time_tracking.command", Operations: "required: form, log; optional: summary and its today_seconds daily total", Anchor: "time-tracking"},
		{ID: "project-links", Name: "Project links", Version: projectlinks.ProtocolVersion, Transport: "JSON request on stdin; JSON reply on stdout", Configuration: "project_links.command", Operations: "required: links; provider-specific link types pass through unchanged", Anchor: "project-links"},
		{ID: "feeds", Name: "Feeds", Version: feed.ProtocolVersion, Transport: "JSON request on stdin; JSON reply on stdout", Configuration: "feed.commands", Operations: "required: feed (read-only)", Anchor: "feeds"},
		{ID: "conversation-discovery", Name: "Conversation discovery", Version: conversations.ProtocolVersion, Transport: "JSON request on stdin; records and diagnostics on stdout", Configuration: "conversations.command (requires conversations.open too)", Operations: "required: list; partial results retain diagnostics; native IDs are opaque", Anchor: "discovery"},
		{ID: "conversation-open", Name: "Conversation open", Version: conversations.ProtocolVersion, Transport: "private selected-record snapshot via ASANAMATE_CONVERSATION_JSON; terminal stdin/stdout/stderr", Configuration: "conversations.open (requires conversations.command too)", Operations: "required: open; revalidate selected identity and ticket link before terminal resume", Anchor: "foreground-opening"},
	}
}

// Contracts returns only implemented interfaces, or all choices for the custom
// command guide. It never interrogates executables to infer their capabilities.
func (e Entry) Contracts() []Contract {
	all := Contracts()
	if e.Builder {
		return all
	}
	var result []Contract
	for _, contract := range all {
		for _, capability := range e.Capabilities {
			if capability.ContractID == contract.ID {
				result = append(result, contract)
				break
			}
		}
	}
	return result
}

var releaseVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$`)

func documentationRevision(version string) string {
	if releaseVersion.MatchString(version) && !strings.Contains(strings.ToLower(version), "snapshot") {
		return "v" + strings.TrimPrefix(version, "v")
	}
	return "main"
}

// DocumentURL pins shipped documentation to the core release when known.
// Paths and anchors come only from the shipped catalog, never provider output.
func DocumentURL(version, path, anchor string) string {
	url := repository + "/blob/" + documentationRevision(version) + "/" + path
	if anchor != "" {
		url += "#" + anchor
	}
	return url
}

func (c Contract) Label() string {
	if c.Version == 0 {
		return c.Name + " (unversioned)"
	}
	// Versions are small protocol constants, independent of package SemVer.
	return c.Name + " v" + strconv.Itoa(c.Version)
}
