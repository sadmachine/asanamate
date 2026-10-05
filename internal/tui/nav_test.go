package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/state"
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
	for _, want := range []string{"My Tasks", "Mobile", "Web", "Group by", "● none", "○ section", "○ due", "Projects ────── [p] ─", "Group by ────── [b] ─", "+4 more"} {
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
	if m.sortBy.By != "title" || m.groupBy != "" {
		t.Fatalf("sort = %v, group = %q, want the last row, title sort", m.sortBy, m.groupBy)
	}
}

func TestViewsPanelSavedViewsOrderAndLimit(t *testing.T) {
	m := wideModel(t, 220)
	st := m.deps.State
	for _, name := range []string{"Current", "Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf"} {
		st.SavedViews[name] = state.View{}
	}
	st.RecentSavedViews = []string{"Gone", "Echo", "Delta", "Echo", "Bravo", "Current"}
	m.savedView = "Current"
	var names []string
	for _, it := range m.navItems() {
		if it.savedView != "" {
			names = append(names, it.savedView)
		}
	}
	if !slices.Equal(names, []string{"Current", "Echo", "Delta", "Bravo", "Alpha"}) {
		t.Fatalf("sidebar views = %v", names)
	}
	body := ansi.Strip(m.navView(navW-panelFrame, 40))
	if !strings.Contains(body, "Saved views ─── [V] ─") || !strings.Contains(body, "▌ Current") || !strings.Contains(body, "+3 more") || strings.Contains(body, "Gone") {
		t.Fatalf("saved views sidebar:\n%s", body)
	}
	if strings.Index(body, "Saved views") > strings.Index(body, "Projects") {
		t.Fatalf("saved views must precede projects:\n%s", body)
	}
}

func TestViewsPanelAppliesAndResetsSavedViewWithoutSwitchingProject(t *testing.T) {
	m := wideModel(t, 220)
	st := m.deps.State
	st.SavedViews["Work"] = state.View{Filter: "is:open", GroupBy: "section", Sort: config.Sort{By: "due", Direction: "asc"}}
	keys(m, "0", "j", "enter") // My Tasks, then Work
	if m.focusNav || m.viewProject != nil || m.savedViewLabel() != "Work" || m.currentView() != st.SavedViews["Work"] {
		t.Fatalf("sidebar recall failed: name = %q, view = %+v", m.savedViewLabel(), m.currentView())
	}
	m.filterInput.SetValue("is:done")
	body := ansi.Strip(m.navView(navW-panelFrame, 40))
	if !strings.Contains(body, "▌ Work *") {
		t.Fatalf("sidebar lost modified mark:\n%s", body)
	}
	keys(m, "0", "j", "enter")
	if m.savedViewLabel() != "Work" || m.filterInput.Value() != "is:open" {
		t.Fatalf("active modified view did not reset: %q", m.savedViewLabel())
	}
	m.pickedProject(&asana.Ref{GID: "p1", Name: "Web"})
	m.Update(tasksMsg{project: m.viewProject, tasks: []asana.Task{openTask, doneTask}})
	keys(m, "0", "g", "j", "enter")
	if m.viewProject == nil || m.viewProject.GID != "p1" || m.savedViewLabel() != "Work" {
		t.Fatalf("sidebar recall switched project: %+v", m.viewProject)
	}
	keys(m, "0", "g", "enter")
	if m.viewProject != nil || m.savedViewLabel() != "Work" {
		t.Fatalf("My Tasks did not restore its view: %q", m.savedViewLabel())
	}
}
