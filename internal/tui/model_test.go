package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func testModel(t *testing.T, cfg config.Config) (*Model, *state.State) {
	t.Helper()
	st, err := state.Load(filepath.Join(t.TempDir(), state.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme == "" {
		cfg.Theme = "dark"
	}
	// Most tests assert on bare rows; TestSelectionMarker covers the default.
	if cfg.List.Selection.Style == "" {
		cfg.List.Selection.Style = config.StyleBar
	}
	return New(Deps{Config: cfg, State: st, StateDir: t.TempDir()}), st
}

var (
	openTask = asana.Task{GID: "1", Name: "Fix login"}
	doneTask = asana.Task{GID: "2", Name: "Write docs", Completed: true}
	sideTask = asana.Task{GID: "3", Name: "Fix footer"}
)

func TestTasksFilteredByDefault(t *testing.T) {
	m, _ := testModel(t, config.Config{DefaultFilter: "is:open"})
	_, cmd := m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask}})
	if len(m.visible) != 1 || m.visible[0].GID != "1" || m.loading {
		t.Fatalf("visible = %+v, loading = %v", m.visible, m.loading)
	}
	if cmd == nil {
		t.Fatal("want a detail fetch to be scheduled")
	}
}

func TestStaleTasksIgnored(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.viewProject = &asana.Ref{GID: "p2"}
	m.Update(tasksMsg{project: &asana.Ref{GID: "p1"}, tasks: []asana.Task{openTask}})
	if m.tasks != nil || !m.loading {
		t.Fatalf("stale response applied: tasks = %+v", m.tasks)
	}
}

func TestDetailForOtherTicketCachedNotShown(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, sideTask}})
	m.Update(detailMsg{gid: "3", ticket: ticket.Ticket{Task: sideTask}})
	if _, ok := m.details["3"]; !ok || m.shownGID != "" {
		t.Fatalf("details = %v, shown = %q", m.details, m.shownGID)
	}
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask}})
	if m.shownGID != "1" {
		t.Fatalf("shown = %q, want 1", m.shownGID)
	}
}

func TestReloadReselectsTicket(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask, sideTask}})
	m.Update(key("j"))
	m.Update(key("j"))
	m.Update(key("r"))
	m.Update(tasksMsg{tasks: []asana.Task{openTask, sideTask, doneTask}})
	if t2, _ := m.selected(); t2.GID != "3" {
		t.Fatalf("selected = %q, want 3 after reorder", t2.GID)
	}
	m.Update(key("r"))
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask}})
	if t2, _ := m.selected(); t2.GID != "1" {
		t.Fatalf("selected = %q, want first when ticket is gone", t2.GID)
	}
}

func TestFilterTyping(t *testing.T) {
	m, _ := testModel(t, config.Config{DefaultFilter: "is:open"})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask, sideTask}})
	m.Update(key("/"))
	for _, r := range " footer" {
		m.Update(key(string(r)))
	}
	m.Update(key("enter"))
	if m.filtering || len(m.visible) != 1 || m.visible[0].GID != "3" {
		t.Fatalf("filtering = %v, visible = %+v", m.filtering, m.visible)
	}
}

func TestProjectPickerOrder(t *testing.T) {
	m, st := testModel(t, config.Config{})
	st.RecentProjects = []string{"c", "gone"}
	m.projects = []asana.Project{{GID: "b", Name: "beta"}, {GID: "a", Name: "Alpha"}, {GID: "c", Name: "Gamma"}}
	m.openProjectPicker()
	var labels []string
	for _, it := range m.modal.items {
		labels = append(labels, it.Label)
	}
	if want := []string{"My Tasks", "Gamma", "Alpha", "beta"}; !slices.Equal(labels, want) {
		t.Fatalf("labels = %v, want %v", labels, want)
	}
}

func TestPickingProjectRecordsRecent(t *testing.T) {
	m, st := testModel(t, config.Config{})
	m.projects = []asana.Project{{GID: "b", Name: "Beta"}}
	m.openProjectPicker()
	m.Update(key("down"))
	m.Update(key("enter"))
	if m.modal != nil || m.viewProject == nil || m.viewProject.GID != "b" || !m.loading {
		t.Fatalf("modal = %v, view = %v, loading = %v", m.modal, m.viewProject, m.loading)
	}
	again, _ := state.Load(st.Path())
	if len(again.RecentProjects) == 0 || again.RecentProjects[0] != "b" {
		t.Fatalf("recent = %v", again.RecentProjects)
	}
}

func TestReaderViewSwitch(t *testing.T) {
	m, _ := testModel(t, config.Config{Reader: config.Reader{View: config.ViewCards}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask,
		Comments: []asana.Story{{CreatedBy: &asana.Ref{Name: "Sam"}, CreatedAt: "2026-09-20T10:00:00Z", HTMLText: "<body>Looks good</body>"}}}})
	cards := ansi.Strip(m.reader.GetContent())
	for _, want := range []string{"Details", "Comments 1", "Sam · 2026-09-20", "Looks good"} {
		if !strings.Contains(cards, want) {
			t.Errorf("cards view missing %q:\n%s", want, cards)
		}
	}
	m.Update(key("v"))
	if md := ansi.Strip(m.reader.GetContent()); m.readerView != config.ViewMarkdown || strings.Contains(md, "Details") || !strings.Contains(md, "Sam · 2026-09-20") {
		t.Fatalf("view = %q, content:\n%s", m.readerView, md)
	}
	m.Update(key("v"))
	if m.readerView != config.ViewCards {
		t.Fatalf("view = %q, want cards", m.readerView)
	}
}
