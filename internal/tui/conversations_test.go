package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/conversations"
	"github.com/sadmachine/asanamate/internal/ticket"
)

const conversationsReply = `{"version":1,"conversations":[
{"provider":"codex","session_id":"old","task_gid":"1","title":"Same [asana:1]","updated_at":"2026-10-01T10:00:00Z","activity":"inactive"},
{"provider":"claude","session_id":"new","task_gid":"1","title":"Same [asana:1]","updated_at":"2026-10-15T10:00:00Z","archived":true,"activity":"unknown"},
{"provider":"claude","session_id":"undated","task_gid":"1","title":"\u001b[31mNo time","cwd":"/gone","branch":"fix","activity":"active"}]}`

// replyCommand is a source fixture that prints reply after reading the request.
func replyCommand(reply string) string {
	return "cat >/dev/null; printf '%s' '" + strings.ReplaceAll(reply, "\n", "") + "'"
}

func conversationModel(t *testing.T, reply string, actions ...config.Action) *Model {
	t.Helper()
	m, _ := testModel(t, config.Config{Actions: actions, Conversations: config.Conversations{Command: replyCommand(reply), Open: "true"}})
	tasks := []asana.Task{{GID: "1", Name: "Fix"}, {GID: "2", Name: "Other"}}
	m.tasks, m.visible = tasks, tasks
	m.details["1"] = ticket.Ticket{Task: tasks[0]}
	m.width, m.height, m.loading = 100, 30, false
	return m
}

// openConversations presses w, checks the modal shows while looking, and
// runs the lookup.
func openConversations(t *testing.T, m *Model) {
	t.Helper()
	_, cmd := m.Update(key("w"))
	if m.modal == nil || !strings.Contains(m.modal.notice, "Finding conversations") {
		t.Fatalf("modal while looking = %+v", m.modal)
	}
	drain(m, cmd)
}

func labels(p *picker) []string {
	var out []string
	for _, it := range p.items {
		out = append(out, it.Label)
	}
	return out
}

func TestAgentsModalListsConversations(t *testing.T) {
	m := conversationModel(t, conversationsReply)
	openConversations(t, m)
	if m.modal == nil || !strings.HasPrefix(m.modal.title, "Agents") {
		t.Fatalf("modal = %+v", m.modal)
	}
	if got := strings.Join(labels(m.modal), "|"); got != "Same|Same|No time|Refresh|Cancel" {
		t.Fatalf("rows = %s", got)
	}
	first, _ := m.modal.items[0].Value.(conversations.Record)
	undated, _ := m.modal.items[2].Value.(conversations.Record)
	if first.SessionID != "new" || !strings.Contains(m.modal.items[0].Hint, "archived") || undated.Title != "\x1b[31mNo time" ||
		!strings.Contains(m.modal.items[2].Hint, "unknown time") || !strings.Contains(m.modal.items[2].Help, "/gone") {
		t.Fatalf("items = %+v", m.modal.items)
	}
	if m.lastAction != -1 {
		t.Fatal("Conversations replaced the repeatable action")
	}
	m.width = 24
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Refresh") {
		t.Fatalf("narrow view lost rows:\n%s", view)
	}
}

func TestActionMenuHasNoConversationsEntry(t *testing.T) {
	m := conversationModel(t, conversationsReply, config.Action{Name: "Go", Key: "g", Command: "true"})
	m.openActionMenu()
	if got := labels(m.modal); len(got) != 1 || got[0] != "Go" {
		t.Fatalf("menu = %v", got)
	}
}

func TestAgentsModalOff(t *testing.T) {
	m := conversationModel(t, conversationsReply)
	m.deps.Config.Conversations = config.Conversations{}
	m.Update(key("w"))
	if m.modal != nil || !strings.Contains(m.status, "no agents or conversations configured") {
		t.Fatalf("modal = %+v, status = %q", m.modal, m.status)
	}
}

func TestConversationsLateRepliesDropped(t *testing.T) {
	m := conversationModel(t, conversationsReply)
	_, cmd := m.Update(key("w"))
	m.Update(key("esc"))
	drain(m, cmd)
	if m.modal != nil || len(m.conversationResults) != 0 {
		t.Fatalf("cancelled lookup opened: %+v", m.modal)
	}

	tk := m.details["1"]
	first := m.lookupConversations(tk, nil)
	second := m.lookupConversations(tk, nil)
	pending := m.modal
	drain(m, first)
	if m.modal != pending || !strings.Contains(m.modal.notice, "Finding conversations") {
		t.Fatalf("superseded lookup attached: %+v", m.modal)
	}
	m.cursor = 1 // the user moved to another ticket
	drain(m, second)
	if m.modal != nil || len(m.conversationResults) != 0 {
		t.Fatalf("reply attached to another ticket: %+v", m.modal)
	}
}

func TestConversationsRefreshShowsStaleAndKeepsSelection(t *testing.T) {
	m := conversationModel(t, conversationsReply)
	openConversations(t, m)
	tk := m.details["1"]
	cmd := m.pickedConversation(tk, conversationsRefresh)
	if m.modal == nil || !strings.Contains(m.modal.notice, "Refreshing") || len(m.modal.items) != 5 {
		t.Fatalf("stale picker = %+v", m.modal)
	}
	m.modal.cursor = 2 // highlight "undated" while refreshing
	drain(m, cmd)
	if it := m.modal.current(); it == nil || it.Value.(conversations.Record).SessionID != "undated" || m.modal.notice != "" {
		t.Fatalf("refresh lost the selection: %+v", it)
	}

	m.deps.Config.Conversations.Command = "echo history unreadable >&2; exit 3"
	drain(m, m.pickedConversation(tk, conversationsRefresh))
	if len(m.modal.items) != 5 || !strings.Contains(m.modal.notice, "unavailable") || !strings.Contains(m.modal.notice, "last successful result") {
		t.Fatalf("failure replaced the last result: %+v", m.modal)
	}
}

func TestConversationsStates(t *testing.T) {
	for reply, want := range map[string]string{
		`{"version":1,"conversations":[]}`: "No linked conversations.",
		`{"version":1,"conversations":[],"diagnostics":[{"provider":"codex","code":"permission_denied","severity":"warning","message":"denied"}]}`: "Incomplete lookup",
		`{"version":2,"conversations":[]}`: "Unsupported conversation source version",
		`{"version":1,"conversations":[],"diagnostics":[{"provider":"claude","code":"metadata_conflict","severity":"warning","message":"tag and title disagree"}]}`: "repair its ticket metadata",
	} {
		m := conversationModel(t, reply)
		openConversations(t, m)
		if !strings.Contains(m.modal.notice, want) || (want != "No linked conversations." && strings.Contains(m.modal.notice, "No linked")) {
			t.Fatalf("%s: notice = %q", reply, m.modal.notice)
		}
	}
	m := conversationModel(t, "")
	m.deps.Config.Conversations.Command = "exit 1"
	openConversations(t, m)
	if strings.Contains(m.modal.notice, "No linked") || !strings.Contains(m.modal.notice, "unavailable") {
		t.Fatalf("failed lookup notice = %q", m.modal.notice)
	}
}

func TestConversationOpenWritesSnapshot(t *testing.T) {
	m := conversationModel(t, conversationsReply)
	openConversations(t, m)
	record := m.modal.items[0].Value.(conversations.Record)
	if cmd := m.pickedConversation(m.details["1"], record); cmd == nil || m.modal != nil {
		t.Fatalf("open did not hand off: %+v", m.status)
	}
	snapshots, _ := filepath.Glob(filepath.Join(m.deps.StateDir, "tickets", "1", "run-*", "conversation.json"))
	if len(snapshots) != 1 {
		t.Fatalf("snapshots = %v", snapshots)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(snapshots[0]), "ticket.json")); err != nil {
		t.Fatal("ticket exports missing beside the snapshot")
	}
}

// runningModel has one running agent on the ticket that reports the "new"
// conversation's exact identity, and a focus command that records its target.
func runningModel(t *testing.T) (*Model, string) {
	t.Helper()
	m := conversationModel(t, conversationsReply)
	out := filepath.Join(t.TempDir(), "focused")
	m.deps.Config.Agents = config.Agents{Command: "unused", Focus: `printf %s "$ASANAMATE_AGENT_TARGET" > ` + out}
	m.Update(agentsMsg{list: []agents.Agent{{TaskGID: "1", Provider: "claude", SessionID: "new", Path: "/code/web",
		Status: "idle", State: agents.Idle, Target: "pane1", Title: "Fixing"}}})
	m.deps.Config.Agents.Command = conversationAgentSource(m.agents...)
	return m, out
}

func TestAgentsModalShowsRunningConversationOnce(t *testing.T) {
	m, out := runningModel(t)
	openConversations(t, m)
	if got := strings.Join(labels(m.modal), "|"); !strings.Contains(got, "Fixing") || strings.Count(got, "Same") != 1 {
		t.Fatalf("rows = %s", got)
	}
	a, ok := m.modal.items[0].Value.(agents.Agent)
	if !ok || a.Target != "pane1" || !strings.Contains(m.modal.items[0].Hint, "running") {
		t.Fatalf("first row = %+v", m.modal.items[0])
	}
	if rec, _ := m.modal.items[1].Value.(conversations.Record); rec.SessionID != "old" {
		t.Fatalf("saved row = %+v", m.modal.items[1])
	}
	_, cmd := m.Update(key("enter"))
	drain(m, cmd)
	if got, err := os.ReadFile(out); err != nil || string(got) != "pane1" || m.modal != nil {
		t.Fatalf("focus wrote %q, err = %v, modal = %+v", got, err, m.modal)
	}
}

func TestAgentsModalWithoutFocusExplains(t *testing.T) {
	m, out := runningModel(t)
	m.deps.Config.Agents.Focus = ""
	openConversations(t, m)
	if cmd := m.pickedConversation(m.details["1"], m.modal.items[0].Value); cmd != nil || !strings.Contains(m.status, "agents.focus") {
		t.Fatalf("status = %q", m.status)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("focus ran without agents.focus")
	}
}

func TestAgentsModalRunningConversationRevalidatesTarget(t *testing.T) {
	m, out := runningModel(t)
	openConversations(t, m)
	m.deps.Config.Agents.Command = conversationAgentSource(
		agents.Agent{Provider: "claude", SessionID: "other", Target: "pane1", Status: "idle"})
	press(m, "enter")
	if _, err := os.Stat(out); !os.IsNotExist(err) || !strings.Contains(m.status, "target is no longer available") {
		t.Fatalf("stale running row focused: error = %v, status = %q", err, m.status)
	}
	if paths, _ := filepath.Glob(filepath.Join(m.deps.StateDir, "tickets", "1", "run-*")); len(paths) != 0 {
		t.Fatal("stale running conversation fell back to resumption")
	}
}

func TestAgentsModalWithoutConversations(t *testing.T) {
	m, _ := runningModel(t)
	m.deps.Config.Conversations = config.Conversations{}
	_, cmd := m.Update(key("w"))
	if cmd != nil || m.modal == nil || m.conversations != nil {
		t.Fatalf("modal = %+v, lookup = %v", m.modal, m.conversations)
	}
	if got := strings.Join(labels(m.modal), "|"); !strings.HasSuffix(got, "|Cancel") || strings.Contains(got, "Refresh") || strings.Count(got, "|") != 1 {
		t.Fatalf("rows = %s", got)
	}
}

func TestAgentsSectionEnterOpensModal(t *testing.T) {
	m, _ := runningModel(t)
	m.focusReader, m.fieldKey = true, agentsKey
	_, cmd := m.Update(key("enter"))
	if m.modal == nil || !strings.HasPrefix(m.modal.title, "Agents") {
		t.Fatalf("modal = %+v", m.modal)
	}
	drain(m, cmd)
}

// conversationAgentSource preserves exact identities and avoids Git fallback.
func conversationAgentSource(list ...agents.Agent) string {
	var lines []string
	for _, a := range list {
		lines = append(lines, fmt.Sprintf("/work\t%s\t%s\t%s\tbranch\t/repo\t%s\t%s\t%s",
			a.Status, a.Target, a.Title, a.TaskGID, a.Provider, a.SessionID))
	}
	return "printf '%s' '" + strings.ReplaceAll(strings.Join(lines, "\n"), "'", "'\\''") + "'"
}

func focusConversationModel(t *testing.T, list ...agents.Agent) (*Model, string) {
	t.Helper()
	m := conversationModel(t, conversationsReply)
	path := filepath.Join(t.TempDir(), "focused")
	t.Setenv("FOCUS_RESULT", path)
	m.deps.Config.Agents = config.Agents{Command: conversationAgentSource(list...),
		Focus: `printf '%s\n' "$ASANAMATE_AGENT_TARGET" "$ASANAMATE_CONVERSATION_PROVIDER" "$ASANAMATE_CONVERSATION_ID" > "$FOCUS_RESULT"`}
	return m, path
}

func savedConversation(t *testing.T, m *Model) conversations.Record {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(m.deps.StateDir, "tickets", "1", "run-*", "conversation.json"))
	if len(paths) != 1 {
		t.Fatalf("conversation snapshots = %v", paths)
	}
	data, err := os.ReadFile(paths[0])
	var req conversations.OpenRequest
	if err != nil || json.Unmarshal(data, &req) != nil {
		t.Fatalf("snapshot = %s, error = %v", data, err)
	}
	return req.Conversation
}

func TestConversationFocusUsesFreshExactIdentity(t *testing.T) {
	// Provider and native identity, not title, branch, path, or task matching,
	// select the live target. Idle is still an open conversation.
	target := `pane ' ; $(touch injected)`
	a := agents.Agent{Provider: "claude", SessionID: "undated", Target: target, Status: "idle"}
	m, path := focusConversationModel(t,
		agents.Agent{Provider: "codex", SessionID: a.SessionID, Target: "wrong-provider", Status: "working"},
		agents.Agent{Provider: a.Provider, SessionID: "other", Target: "wrong-id", Status: "working"}, a)
	// A stale periodic agent list must never supply the target.
	m.agents = []agents.Agent{{Provider: a.Provider, SessionID: a.SessionID, Target: "stale"}}
	m.deps.ExitOnAction = true
	openConversations(t, m)
	record := m.modal.items[2].Value.(conversations.Record)
	record.Title = "stale title"
	drain(m, m.pickedConversation(m.details["1"], record))
	data, err := os.ReadFile(path)
	if err != nil || string(data) != target+"\nclaude\nundated\n" {
		t.Fatalf("focused = %q, error = %v, status = %q", data, err, m.status)
	}
	if m.ExitCommand() != nil || m.modal != nil || m.waiting != "" || m.lastAction != -1 {
		t.Fatalf("focus changed TUI lifecycle: exit = %v, status = %q", m.ExitCommand(), m.status)
	}
	if got := savedConversation(t, m); got.Title != "\x1b[31mNo time" || got.Activity != "active" {
		t.Fatalf("focus did not revalidate native metadata: %+v", got)
	}
}

func TestConversationFocusFallsBackWithoutChangingActivity(t *testing.T) {
	for _, a := range []agents.Agent{
		{Provider: "claude", SessionID: "other", Target: "other", Status: "idle"},
		{Provider: "codex", SessionID: "undated", Target: "other", Status: "idle"},
		{Provider: "claude", SessionID: "undated", Status: "idle"},
	} {
		m, path := focusConversationModel(t, a)
		openConversations(t, m)
		drain(m, m.pickedConversation(m.details["1"], m.modal.items[2].Value))
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("focused a nonmatching or unusable target")
		}
		if got := savedConversation(t, m); got.SessionID != "undated" || got.Activity != "active" {
			t.Fatalf("resume lost activity guard: %+v", got)
		}
	}
}

func TestConversationFocusMultipleTargets(t *testing.T) {
	m, path := focusConversationModel(t,
		agents.Agent{Provider: "claude", SessionID: "undated", Target: "one", Status: "idle"},
		agents.Agent{Provider: "claude", SessionID: "undated", Target: "two", Status: "working"})
	openConversations(t, m)
	drain(m, m.pickedConversation(m.details["1"], m.modal.items[2].Value))
	if m.modal == nil || m.modal.title != "Which agent?" || len(m.modal.items) != 2 {
		t.Fatalf("target picker = %+v", m.modal)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("focused before choosing a target")
	}
	press(m, "down", "enter")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "two\nclaude\nundated\n" {
		t.Fatalf("chosen focus = %q, error = %v", data, err)
	}
}

func TestConversationFocusLookupFailureDoesNotOpen(t *testing.T) {
	for _, source := range []string{"history", "agents", "unlinked"} {
		t.Run(source, func(t *testing.T) {
			m, path := focusConversationModel(t, agents.Agent{Provider: "claude", SessionID: "undated", Target: "live", Status: "idle"})
			openConversations(t, m)
			record := m.modal.items[2].Value
			switch source {
			case "history":
				m.deps.Config.Conversations.Command = "exit 1"
			case "agents":
				m.deps.Config.Agents.Command = "exit 1"
			case "unlinked":
				m.deps.Config.Conversations.Command = replyCommand(`{"version":1,"conversations":[]}`)
			}
			drain(m, m.pickedConversation(m.details["1"], record))
			if paths, _ := filepath.Glob(filepath.Join(m.deps.StateDir, "tickets", "1", "run-*")); len(paths) != 0 {
				t.Fatal("lookup failure handed off to an opener")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) || !strings.Contains(m.status, "failed") {
				t.Fatalf("lookup failure: focus error = %v, status = %q", err, m.status)
			}
		})
	}
}

func TestConversationFocusFailureDoesNotResume(t *testing.T) {
	m, _ := focusConversationModel(t, agents.Agent{Provider: "claude", SessionID: "undated", Target: "live", Status: "idle"})
	m.deps.Config.Agents.Focus = "echo target-gone >&2; exit 3"
	openConversations(t, m)
	drain(m, m.pickedConversation(m.details["1"], m.modal.items[2].Value))
	if !strings.Contains(m.status, "Focus agent failed") || m.ExitCommand() != nil {
		t.Fatalf("focus failure = %q, exit = %v", m.status, m.ExitCommand())
	}
	paths, _ := filepath.Glob(filepath.Join(m.deps.StateDir, "tickets", "1", "run-*", "conversation.json"))
	if len(paths) != 1 {
		t.Fatal("focus failure tried another opener")
	}
}

func TestConversationFocusCancelledOrChangedSelection(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		m, path := focusConversationModel(t, agents.Agent{Provider: "claude", SessionID: "undated", Target: "live", Status: "idle"})
		openConversations(t, m)
		cmd := m.pickedConversation(m.details["1"], m.modal.items[2].Value)
		if cancel {
			m.Update(key("esc"))
		} else {
			m.cursor = 1
		}
		drain(m, cmd)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("cancelled or outdated selection focused an agent")
		}
		if paths, _ := filepath.Glob(filepath.Join(m.deps.StateDir, "tickets", "1", "run-*")); len(paths) != 0 {
			t.Fatal("cancelled or outdated selection opened a conversation")
		}
	}
}

func TestConversationFocusChosenTargetIsRevalidated(t *testing.T) {
	m, path := focusConversationModel(t,
		agents.Agent{Provider: "claude", SessionID: "undated", Target: "one", Status: "idle"},
		agents.Agent{Provider: "claude", SessionID: "undated", Target: "two", Status: "working"})
	openConversations(t, m)
	drain(m, m.pickedConversation(m.details["1"], m.modal.items[2].Value))
	// The selected target now belongs to another conversation. Do not focus it
	// or silently resume the old conversation after the user's choice.
	m.deps.Config.Agents.Command = conversationAgentSource(
		agents.Agent{Provider: "claude", SessionID: "other", Target: "two", Status: "idle"})
	press(m, "down", "enter")
	if _, err := os.Stat(path); !os.IsNotExist(err) || !strings.Contains(m.status, "target is no longer available") {
		t.Fatalf("stale target: focus error = %v, status = %q", err, m.status)
	}
	if paths, _ := filepath.Glob(filepath.Join(m.deps.StateDir, "tickets", "1", "run-*")); len(paths) != 0 {
		t.Fatal("stale chosen target opened a conversation")
	}
}

func TestConversationFocusUnknownArchivedSession(t *testing.T) {
	m, path := focusConversationModel(t, agents.Agent{Provider: "claude", SessionID: "new", Target: "live", Status: "waiting"})
	openConversations(t, m)
	drain(m, m.pickedConversation(m.details["1"], m.modal.items[0].Value))
	if data, err := os.ReadFile(path); err != nil || string(data) != "live\nclaude\nnew\n" {
		t.Fatalf("unknown activity did not focus: %q, %v", data, err)
	}
	if got := savedConversation(t, m); got.Activity != "unknown" || !got.Archived {
		t.Fatalf("focus changed history: %+v", got)
	}
}

func TestConversationFocusChooserCanCancel(t *testing.T) {
	m, path := focusConversationModel(t,
		agents.Agent{Provider: "claude", SessionID: "undated", Target: "one", Status: "idle"},
		agents.Agent{Provider: "claude", SessionID: "undated", Target: "two", Status: "working"})
	openConversations(t, m)
	drain(m, m.pickedConversation(m.details["1"], m.modal.items[2].Value))
	press(m, "esc")
	if _, err := os.Stat(path); !os.IsNotExist(err) || m.modal != nil || m.waiting != "" {
		t.Fatalf("cancelled target picker: focus error = %v, modal = %+v", err, m.modal)
	}
	if paths, _ := filepath.Glob(filepath.Join(m.deps.StateDir, "tickets", "1", "run-*")); len(paths) != 0 {
		t.Fatal("cancelled target picker opened a conversation")
	}
}

func TestConversationWithoutFocusDoesNotQueryAgents(t *testing.T) {
	for _, focus := range []string{"", "  "} {
		m, path := focusConversationModel(t)
		m.deps.Config.Agents.Focus = focus
		m.deps.Config.Agents.Command = `printf queried > "$FOCUS_RESULT"; exit 1`
		openConversations(t, m)
		record := m.modal.items[2].Value.(conversations.Record)
		drain(m, m.pickedConversation(m.details["1"], record))
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("queried agent source without opting into focus")
		}
		if got := savedConversation(t, m); got != record {
			t.Fatalf("existing opener path changed: %+v", got)
		}
	}
}
