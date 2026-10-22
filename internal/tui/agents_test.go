package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

func TestConversationLinkOverridesBranchAndRepo(t *testing.T) {
	m := agentModel(t, config.SymbolsUnicode, true)
	a := onBranch(agents.Waiting, "explicit")
	a.TaskGID, a.Branch, a.Repo = "1", "other", "/other"
	other := onBranch(agents.Working, "different-ticket")
	other.TaskGID = "2"
	other.Path = a.Path // Several conversations can share one working directory.
	m.Update(agentsMsg{list: []agents.Agent{a, other}})
	if got := m.viewAgents(agentTask("feat/x")); len(got) != 1 || got[0].Target != "explicit" {
		t.Fatalf("ticket links = %+v", got)
	}
	rows := m.agentItems(m.feedLinks().byGID)
	if len(rows) != 2 || rows[0].task == nil || rows[0].task.GID != "1" || rows[1].task == nil || rows[1].task.GID != "2" {
		t.Fatalf("feed must preserve exact links: %+v", rows)
	}
	m.details["1"] = ticket.Ticket{Task: agentTask("feat/x")}
	m.openActionMenu()
	m.Update(key("x"))
	if env := m.ExitCommand().Env; !slices.Contains(env, "ASANAMATE_AGENT_TARGET=explicit") {
		t.Fatalf("action did not retain live target: %v", env)
	}
}

func TestAgentColumnOrdersByUrgency(t *testing.T) {
	m := agentModel(t, config.SymbolsUnicode, true)
	other := onBranch(agents.Working, "elsewhere")
	other.Repo = "/other"
	m.Update(agentsMsg{list: []agents.Agent{onBranch(agents.Working, "w1"), other, onBranch(agents.Waiting, "q1")}})
	first := strings.Split(ansi.Strip(m.listView(30, 4)), "\n")[0]
	if first != "□  Fix login"+strings.Repeat(" ", 10)+"🤖 ⚠ ◐ " {
		t.Fatalf("row = %q", first)
	}
	m.width = 120
	if !strings.Contains(ansi.Strip(m.statusline()), "│ ⚠1 ◐1") {
		t.Fatalf("statusline = %q", ansi.Strip(m.statusline()))
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
	if first := strings.Split(ansi.Strip(m.listView(40, 4)), "\n")[0]; !strings.HasSuffix(first, "⚠1 ◐3 ●2 ") {
		t.Fatalf("row = %q", first)
	}
}

func TestAgentColumnUsesSelectionTheme(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, layout := range []string{config.LayoutSingle, config.LayoutMulti} {
			for _, set := range []string{config.SymbolsASCII, config.SymbolsUnicode, config.SymbolsNerd} {
				t.Run(layout+"/"+set+"/reverse="+fmt.Sprint(reverse), func(t *testing.T) {
					m := agentModel(t, set, true)
					m.deps.Config.List.Layout = layout
					highlight := config.Style{Reverse: &reverse}
					if !reverse {
						highlight.FG, highlight.BG = "#dddddd", "#504945"
					}
					cfg := config.Config{Colors: config.Colors{Highlight: highlight}}
					applyPalette(cfg.Palette(true))
					for _, count := range []int{2, 6} {
						list := []agents.Agent{onBranch(agents.Working, ""), onBranch(agents.Idle, "")}
						m.Update(agentsMsg{list: slices.Repeat(list, count/2)})
						row := strings.Split(m.listView(40, 4), "\n")[0]
						if !strings.Contains(row, selectedStyle.Bold(true).Render(m.sym.robot)) {
							t.Fatal("robot does not use the selection theme")
						}
						for _, st := range []agents.State{agents.Working, agents.Idle} {
							style := stateStyles[st].Bold(true).Reverse(true)
							if !reverse {
								style = selectedStyle.Foreground(stateStyles[st].GetForeground()).Bold(true)
							}
							text := m.sym.agent(st, 0)
							if count > 4 {
								text += fmt.Sprint(count / 2)
							}
							if !strings.Contains(row, style.Render(text)) {
								t.Fatalf("%s does not use the selection theme", st)
							}
						}
						if ansi.StringWidth(row) != 40-listGutter {
							t.Fatalf("highlight changed row width: %d", ansi.StringWidth(row))
						}
						m.focusReader = true
						row = strings.Split(m.listView(40, 4), "\n")[0]
						if !strings.Contains(row, lipgloss.NewStyle().Bold(true).Render(m.sym.robot)) {
							t.Fatal("reader focus must leave the badge unhighlighted")
						}
						m.focusReader = false
					}
				})
			}
		}
	}
}

func TestSpinnerAnimatesWorkingAgents(t *testing.T) {
	m := agentModel(t, config.SymbolsUnicode, false)
	_, cmd := m.Update(agentsMsg{list: []agents.Agent{onBranch(agents.Working, "")}})
	if cmd == nil || !m.spinning || !strings.HasSuffix(strings.Split(ansi.Strip(m.listView(30, 4)), "\n")[0], "⣾ ") {
		t.Fatalf("spinning = %v, row = %q", m.spinning, ansi.Strip(m.listView(30, 4)))
	}
	m.Update(spinnerTickMsg{})
	if !strings.HasSuffix(strings.Split(ansi.Strip(m.listView(30, 4)), "\n")[0], "⣽ ") {
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
	if first := strings.Split(ansi.Strip(m.listView(30, 4)), "\n")[0]; !strings.HasPrefix(first, "[ ]  Fix login") || !strings.HasSuffix(first, "(~) (-) ") || m.spinning {
		t.Fatalf("ascii row = %q, spinning = %v", first, m.spinning)
	}
	r := agentModel(t, config.SymbolsUnicode, true)
	r.Update(agentsMsg{list: []agents.Agent{onBranch(agents.Working, "")}})
	if first := strings.Split(ansi.Strip(r.listView(30, 4)), "\n")[0]; !strings.HasSuffix(first, "◐ ") || r.spinning {
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
	if got := strings.Join(listLines(m, 80, 3), "\n"); got != "□  Fix login" {
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
	if m.modal == nil || m.modal.title != "Which agent?" || len(m.modal.items) != 2 {
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

func TestStatuslineCountsSharedAgentsOnce(t *testing.T) {
	m := agentModel(t, config.SymbolsUnicode, true)
	twin := agentTask("feat/x")
	twin.GID = "3"
	m.Update(tasksMsg{tasks: []asana.Task{agentTask("feat/x"), twin}})
	m.Update(agentsMsg{list: []agents.Agent{onBranch(agents.Working, "w1")}})
	m.width = 120
	if s := ansi.Strip(m.statusline()); !strings.Contains(s, "│ ◐1 ") {
		t.Fatalf("statusline = %q", s)
	}
}

func titled(state agents.State, target, title string) agents.Agent {
	a := onBranch(state, target)
	a.Title = title
	return a
}

func TestPickerShowsTitles(t *testing.T) {
	m := jumpModel(t, titled(agents.Waiting, "a1", "Fix license headings"), onBranch(agents.Idle, "a2"))
	m.pickedAction(0)
	if m.modal == nil || m.modal.items[0].Label != "⚠ waiting · Fix license headings" || m.modal.items[0].Hint != "wt · a1" {
		t.Fatalf("items = %+v", m.modal.items)
	}
	if m.modal.items[1].Label != "○ idle" {
		t.Fatalf("untitled label = %q", m.modal.items[1].Label)
	}
}

func TestReaderListsAgentsAndKeepsScroll(t *testing.T) {
	m := agentModel(t, config.SymbolsUnicode, true)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	long := agentTask("feat/x")
	long.HTMLNotes = "<body>" + strings.Repeat("<p>line</p>", 40) + "</body>"
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: long}})
	m.Update(agentsMsg{list: []agents.Agent{titled(agents.Working, "w1", "Refactor the queue")}})
	content := ansi.Strip(m.reader.GetContent())
	if i, j := strings.Index(content, "Agents"), strings.Index(content, "Description"); i < 0 || j < 0 || i > j {
		t.Fatalf("agents section must come before the description:\n%s", content)
	}
	if !strings.Contains(content, "◐ working · Refactor the queue") {
		t.Fatalf("reader missing agents section:\n%s", content)
	}
	m.reader.SetYOffset(5)
	m.Update(agentsMsg{list: []agents.Agent{titled(agents.Idle, "w1", "Refactor the queue")}})
	if !strings.Contains(ansi.Strip(m.reader.GetContent()), "○ idle") || m.reader.YOffset() != 5 {
		t.Fatalf("refresh must update the section and keep scroll (offset %d)", m.reader.YOffset())
	}
}

func TestReaderAgentRowsStayOnOneLine(t *testing.T) {
	for _, width := range []int{160, 70} {
		m := agentModel(t, config.SymbolsUnicode, true)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
		m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: agentTask("feat/x")}})
		a := titled(agents.Working, "w1", strings.Repeat("Improve SSO OAuth compatibility ", 3))
		a.Path = "/code/web/feature-3091-masterclass-sso-46450568f0323c4b"
		m.Update(agentsMsg{list: []agents.Agent{a}})
		lines := strings.Split(ansi.Strip(m.reader.GetContent()), "\n")
		i := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "◐ working · Improve") })
		if i < 0 || !strings.Contains(lines[i], "…") || strings.Contains(lines[i+1], "compatibility") || strings.Contains(lines[i+1], "46450568") {
			t.Fatalf("width %d: agent row must stay on one line:\n%s", width, strings.Join(lines, "\n"))
		}
	}
}

func TestAgentsOnOtherBranchesHideUntilShown(t *testing.T) {
	other := func(target, branch string) agents.Agent {
		a := onBranch(agents.Idle, target)
		a.Branch, a.Path = branch, "/code/web/"+target
		return a
	}
	for _, width := range []int{260, 70} {
		for _, linked := range []bool{false, true} {
			m := agentModel(t, config.SymbolsUnicode, true)
			m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
			m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: agentTask("feat/x")}})
			list := []agents.Agent{other("o1", "feat/y"), other("o2", "feat/z")}
			if linked {
				list = append(list, onBranch(agents.Working, "w1"))
			}
			m.Update(agentsMsg{list: list})
			if content := ansi.Strip(m.reader.GetContent()); strings.Contains(content, "o1 is on") || !strings.Contains(content, "+ Show 2 unlinked agents") {
				t.Fatalf("width %d, linked %v: other-branch agents must start hidden:\n%s", width, linked, content)
			}
			m.focusReader, m.fieldKey = true, otherAgentsKey
			m.renderDetail(true)
			lines := strings.Split(ansi.Strip(m.reader.GetContent()), "\n")
			if line := lines[m.fieldLines[otherAgentsKey]]; !strings.Contains(line, "+ Show 2 unlinked agents") {
				t.Fatalf("width %d, linked %v: Show row is not on line %d: %q", width, linked, m.fieldLines[otherAgentsKey], line)
			}
			press(m, "enter")
			if content := ansi.Strip(m.reader.GetContent()); !strings.Contains(content, "o1 is on feat/y, not feat/x") || strings.Contains(content, "+ Show") || m.fieldKey != "section:agents" {
				t.Fatalf("width %d, linked %v: enter must show the agents and select the section (field %q):\n%s", width, linked, m.fieldKey, content)
			}
		}
	}
}
