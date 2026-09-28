package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

var goAction = config.Action{Name: "Go", Key: "x", Mode: config.ModeExit, Command: "true"}

func agentTask(branch string) asana.Task {
	return asana.Task{GID: "1", Name: "Fix login",
		Memberships:  []asana.Membership{{Project: asana.Ref{GID: "p1", Name: "Web"}}},
		CustomFields: []asana.CustomField{{GID: "f1", Name: "Branch Name ", DisplayValue: strp(branch)}}}
}

func TestAgentLinkedByBranchAndRepo(t *testing.T) {
	m, st := testModel(t, config.Config{
		BranchField: "Branch Name",
		Agents:      config.Agents{Command: "unused"},
		List:        config.List{Layout: config.LayoutSingle, Fields: []string{"agent"}},
		Actions:     []config.Action{goAction},
	})
	st.LinkRepo("p1", "/code/web")
	tk := agentTask("feat/x")
	m.Update(tasksMsg{tasks: []asana.Task{tk}})
	m.Update(agentsMsg{list: []agents.Agent{
		{Path: "/other/wt", Status: "idle", Branch: "feat/x", Repo: "/other"},
		{Path: "/code/web/.claude/worktrees/feat-x", Status: "running", Target: "s1", Branch: "feat/x", Repo: "/code/web"},
	}})
	if got := ansi.Strip(m.listView(80, 3)); got != "○ Fix login  agent running" {
		t.Fatalf("list = %q", got)
	}
	m.details["1"] = ticket.Ticket{Task: tk}
	m.openActionMenu()
	m.Update(key("x"))
	env := m.ExitCommand().Env
	for _, want := range []string{"ASANAMATE_AGENT_TARGET=s1", "ASANAMATE_AGENT_STATUS=running", "ASANAMATE_BRANCH=feat/x"} {
		if !slices.Contains(env, want) {
			t.Errorf("env missing %q", want)
		}
	}
}

func TestAgentsDisabledShowNothing(t *testing.T) {
	m, _ := testModel(t, config.Config{BranchField: "Branch Name", List: config.List{Layout: config.LayoutSingle, Fields: []string{"agent"}}})
	m.Update(tasksMsg{tasks: []asana.Task{agentTask("feat/x")}})
	if m.Init() == nil {
		t.Fatal("Init must still load tasks")
	}
	if _, cmd := m.Update(agentsMsg{}); cmd != nil {
		t.Fatal("disabled agents must not schedule refreshes")
	}
	if got := ansi.Strip(m.listView(80, 3)); got != "○ Fix login" {
		t.Fatalf("list = %q", got)
	}
}

func TestAgentsRefreshAndReportErrors(t *testing.T) {
	m, _ := testModel(t, config.Config{Agents: config.Agents{Command: "unused"}})
	if _, cmd := m.Update(agentsMsg{}); cmd == nil {
		t.Fatal("want the next refresh scheduled")
	}
	m.Update(agentsMsg{err: errBoom})
	if !strings.Contains(m.status, "agents") || !strings.Contains(m.status, "boom") {
		t.Fatalf("status = %q", m.status)
	}
}

func TestSharedFieldNamesUseActiveProjectField(t *testing.T) {
	m, _ := testModel(t, config.Config{BranchField: "Branch Name", Actions: []config.Action{goAction}})
	tk := agentTask("from-fuse")
	tk.CustomFields = append(tk.CustomFields, asana.CustomField{GID: "f5", Name: "Branch name", DisplayValue: strp("from-eng")})
	m.tasks, m.visible = []asana.Task{tk}, []asana.Task{tk}
	m.details["1"] = ticket.Ticket{Task: tk}
	m.openActionMenu()
	if cmd := m.pickedAction(0); cmd == nil || m.ExitCommand() != nil {
		t.Fatal("want the project's fields fetched before running")
	}
	m.Update(projectFieldsMsg{gid: "p1", fields: map[string]bool{"f5": true}})
	env := m.ExitCommand().Env
	for _, want := range []string{"ASANAMATE_BRANCH=from-eng", "ASANAMATE_FIELD_BRANCH_NAME=from-eng"} {
		if !slices.Contains(env, want) {
			t.Errorf("env missing %q", want)
		}
	}
}

var errBoom = errorString("boom")

type errorString string

func (e errorString) Error() string { return string(e) }
