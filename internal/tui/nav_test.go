package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
)

func wideModel(t *testing.T, width int) *Model {
	t.Helper()
	m, st := testModel(t, config.Config{List: config.List{Fields: []string{"section", "due"}}})
	st.RecentProjects = []string{"p2", "p1"}
	m.projects = []asana.Project{{GID: "p1", Name: "Web"}, {GID: "p2", Name: "Mobile"}}
	m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask}})
	return m
}

// keys sends keys without running the commands they return.
func keys(m *Model, ks ...string) {
	for _, k := range ks {
		m.Update(key(k))
	}
}

func TestViewsPanelOnlyOnWideScreens(t *testing.T) {
	if top := strings.Split(ansi.Strip(wideModel(t, 200).body()), "\n")[0]; !strings.HasPrefix(top, "╭─ [0] Views") {
		t.Fatalf("wide top = %q", top)
	}
	m := wideModel(t, 140)
	if top := strings.Split(ansi.Strip(m.body()), "\n")[0]; strings.Contains(top, "Views") {
		t.Fatalf("narrow top = %q", top)
	}
	m.Update(key("0"))
	if m.focusNav {
		t.Fatal("0 must not focus a hidden panel")
	}
}

func TestViewsPanelListsRecentProjectsAndGroupings(t *testing.T) {
	m := wideModel(t, 200)
	body := ansi.Strip(m.navView(navW-panelFrame, 20))
	for _, want := range []string{"My Tasks", "Mobile", "Web", "Group by", "● none", "○ section", "○ due"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Index(body, "Mobile") > strings.Index(body, "Web") {
		t.Fatalf("recent projects out of order:\n%s", body)
	}
}

func TestViewsPanelPicksGroupingAndProject(t *testing.T) {
	m := wideModel(t, 200)
	m.Update(key("0"))
	if !m.focusNav || m.navCursor != 0 || !strings.HasPrefix(ansi.Strip(m.statusline()), " VIEWS ") {
		t.Fatalf("focusNav = %v, navCursor = %d", m.focusNav, m.navCursor)
	}
	// My Tasks, Mobile, Web, then none, section, due.
	keys(m, "j", "j", "j", "j", "enter")
	if m.focusNav || m.groupBy != "section" {
		t.Fatalf("focusNav = %v, groupBy = %q", m.focusNav, m.groupBy)
	}
	keys(m, "0", "j", "enter")
	if gidOf(m.viewProject) != "p2" {
		t.Fatalf("viewProject = %+v", m.viewProject)
	}
	m.Update(key("0"))
	if m.navCursor != 1 {
		t.Fatalf("focus starts on the viewed project, got %d", m.navCursor)
	}
	m.Update(key("esc"))
	if m.focusNav || m.focusReader {
		t.Fatal("esc returns to the list")
	}
}

func TestViewsPanelCursorSurvivesShrinkingRows(t *testing.T) {
	m := wideModel(t, 200)
	m.groupBy = "Priority"
	keys(m, "0", "G")
	m.groupBy = "" // Priority's row is gone
	keys(m, "enter")
	if m.groupBy != "due" {
		t.Fatalf("groupBy = %q, want the last row, due", m.groupBy)
	}
}
