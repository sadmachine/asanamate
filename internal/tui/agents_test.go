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

func agentModel(t *testing.T, set string, reduced bool) *Model {
	t.Helper()
	m, st := testModel(t, config.Config{
		BranchField: "Branch Name",
		Agents:      config.Agents{Command: "unused"},
		Actions:     []config.Action{goAction},
	})
	m.sym = newSymbols(set, nil, reduced)
	st.LinkRepo("p1", "/code/web")
	m.Update(tasksMsg{tasks: []asana.Task{agentTask("feat/x"), {GID: "2", Name: "Other"}}})
	return m
}

func onBranch(state agents.State, target string) agents.Agent {
	return agents.Agent{Path: "/code/web/wt", Status: string(state), State: state, Target: target, Branch: "feat/x", Repo: "/code/web"}
}

func TestAgentColumnOrdersByUrgency(t *testing.T) {
	m := agentModel(t, config.SymbolsUnicode, true)
	other := onBranch(agents.Working, "elsewhere")
	other.Repo = "/other"
	m.Update(agentsMsg{list: []agents.Agent{onBranch(agents.Working, "w1"), other, onBranch(agents.Waiting, "q1")}})
	first := strings.Split(ansi.Strip(m.listView(30, 4)), "\n")[0]
	if first != "□ Fix login"+strings.Repeat(" ", 16)+"⚠ ◐" {
		t.Fatalf("row = %q", first)
	}
	m.width = 120
	if !strings.Contains(ansi.Strip(m.header()), "agents ⚠1 ◐1") {
		t.Fatalf("header = %q", ansi.Strip(m.header()))
	}
	m.details["1"] = ticket.Ticket{Task: agentTask("feat/x")}
	m.openActionMenu()
	m.Update(key("x"))
	if env := m.ExitCommand().Env; !slices.Contains(env, "ASANAMATE_AGENT_TARGET=q1") || !slices.Contains(env, "ASANAMATE_AGENT_STATE=waiting") {
		t.Fatalf("want the most urgent agent in env, got %v", env)
	}
}

func TestAgentColumnGroupsManyAgents(t *testing.T) {
	m := agentModel(t, config.SymbolsUnicode, true)
	var list []agents.Agent
	for _, s := range []agents.State{agents.Working, agents.Completed, agents.Working, agents.Waiting, agents.Working, agents.Completed} {
		list = append(list, onBranch(s, ""))
	}
	m.Update(agentsMsg{list: list})
	if first := strings.Split(ansi.Strip(m.listView(40, 4)), "\n")[0]; !strings.HasSuffix(first, "⚠1 ◐3 ●2") {
		t.Fatalf("row = %q", first)
	}
}

func TestSpinnerAnimatesWorkingAgents(t *testing.T) {
	m := agentModel(t, config.SymbolsUnicode, false)
	_, cmd := m.Update(agentsMsg{list: []agents.Agent{onBranch(agents.Working, "")}})
	if cmd == nil || !m.spinning || !strings.HasSuffix(strings.Split(ansi.Strip(m.listView(30, 4)), "\n")[0], "⠋") {
		t.Fatalf("spinning = %v, row = %q", m.spinning, ansi.Strip(m.listView(30, 4)))
	}
	m.Update(spinnerTickMsg{})
	if !strings.HasSuffix(strings.Split(ansi.Strip(m.listView(30, 4)), "\n")[0], "⠙") {
		t.Fatal("spinner did not advance")
	}
	m.Update(agentsMsg{list: []agents.Agent{onBranch(agents.Idle, "")}})
	if _, cmd := m.Update(spinnerTickMsg{}); cmd != nil || m.spinning {
		t.Fatal("spinner must stop when nothing is working")
	}
}

func TestReducedMotionAndASCII(t *testing.T) {
	m := agentModel(t, config.SymbolsASCII, false)
	m.Update(agentsMsg{list: []agents.Agent{onBranch(agents.Working, ""), onBranch(agents.Idle, "")}})
	if first := strings.Split(ansi.Strip(m.listView(30, 4)), "\n")[0]; !strings.HasPrefix(first, "[ ] Fix login") || !strings.HasSuffix(first, "(~) (-)") || m.spinning {
		t.Fatalf("ascii row = %q, spinning = %v", first, m.spinning)
	}
	r := agentModel(t, config.SymbolsUnicode, true)
	r.Update(agentsMsg{list: []agents.Agent{onBranch(agents.Working, "")}})
	if first := strings.Split(ansi.Strip(r.listView(30, 4)), "\n")[0]; !strings.HasSuffix(first, "◐") || r.spinning {
		t.Fatalf("reduced motion row = %q, spinning = %v", first, r.spinning)
	}
}

func TestSymbolOverrides(t *testing.T) {
	s := newSymbols(config.SymbolsUnicode, map[string]string{"working": "W", "idle": "i"}, false)
	if s.agent(agents.Working, 3) != "W" || s.agent(agents.Idle, 0) != "i" || s.spinner != nil {
		t.Fatalf("overrides not applied: %+v", s)
	}
}

func TestAgentFilterFollowsRefreshes(t *testing.T) {
	m := agentModel(t, config.SymbolsUnicode, true)
	m.filterInput.SetValue("agent:waiting")
	m.applyFilter()
	if len(m.visible) != 0 {
		t.Fatalf("visible = %+v", m.visible)
	}
	m.Update(agentsMsg{list: []agents.Agent{onBranch(agents.Waiting, "")}})
	if len(m.visible) != 1 || m.visible[0].GID != "1" {
		t.Fatalf("after refresh visible = %+v", m.visible)
	}
}

func TestAgentsDisabledShowNothing(t *testing.T) {
	m, _ := testModel(t, config.Config{BranchField: "Branch Name"})
	m.Update(tasksMsg{tasks: []asana.Task{agentTask("feat/x")}})
	if m.Init() == nil {
		t.Fatal("Init must still load tasks")
	}
	if _, cmd := m.Update(agentsMsg{}); cmd != nil {
		t.Fatal("disabled agents must not schedule refreshes")
	}
	if got := ansi.Strip(m.listView(80, 3)); got != "□ Fix login" {
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

var jumpAction = config.Action{Name: "Jump", Key: "j", Mode: config.ModeExit, Agent: true, Command: "true"}

func jumpModel(t *testing.T, list ...agents.Agent) *Model {
	t.Helper()
	m := agentModel(t, config.SymbolsUnicode, true)
	m.deps.Config.Actions = []config.Action{jumpAction}
	m.Update(agentsMsg{list: list})
	m.details["1"] = ticket.Ticket{Task: agentTask("feat/x")}
	m.openActionMenu()
	return m
}

func TestAgentActionNeedsAnAgent(t *testing.T) {
	m := jumpModel(t)
	m.pickedAction(0)
	if m.ExitCommand() != nil || !strings.Contains(m.status, "no running agent") {
		t.Fatalf("status = %q, exit = %v", m.status, m.ExitCommand())
	}
}

func TestAgentActionWithOneAgentRunsDirectly(t *testing.T) {
	m := jumpModel(t, onBranch(agents.Idle, "only"))
	m.pickedAction(0)
	if cmd := m.ExitCommand(); cmd == nil || !slices.Contains(cmd.Env, "ASANAMATE_AGENT_TARGET=only") {
		t.Fatalf("exit = %+v", cmd)
	}
}

func TestAgentActionPicksAmongSeveral(t *testing.T) {
	m := jumpModel(t, onBranch(agents.Idle, "a1"), onBranch(agents.Waiting, "a2"))
	m.pickedAction(0)
	if m.modal == nil || m.modal.kind != pickAgent || len(m.modal.items) != 2 {
		t.Fatalf("modal = %+v", m.modal)
	}
	if !strings.Contains(m.modal.items[0].Label, "waiting") {
		t.Fatalf("most urgent first: %q", m.modal.items[0].Label)
	}
	m.Update(key("down"))
	m.Update(key("enter"))
	if cmd := m.ExitCommand(); cmd == nil || !slices.Contains(cmd.Env, "ASANAMATE_AGENT_TARGET=a1") {
		t.Fatalf("exit = %+v", cmd)
	}
}

func TestHeaderCountsSharedAgentsOnce(t *testing.T) {
	m := agentModel(t, config.SymbolsUnicode, true)
	twin := agentTask("feat/x")
	twin.GID = "3"
	m.Update(tasksMsg{tasks: []asana.Task{agentTask("feat/x"), twin}})
	m.Update(agentsMsg{list: []agents.Agent{onBranch(agents.Working, "w1")}})
	m.width = 120
	if h := ansi.Strip(m.header()); !strings.HasSuffix(h, "agents ◐1") {
		t.Fatalf("header = %q", h)
	}
}
