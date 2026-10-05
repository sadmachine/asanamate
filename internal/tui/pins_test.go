package tui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func TestPinTogglePersistsAndKeepsSelection(t *testing.T) {
	for _, reader := range []bool{false, true} {
		m, _ := testModel(t, config.Config{DefaultFilter: "is:open"})
		path := filepath.Join(t.TempDir(), state.FileName)
		st, err := state.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		m.deps.State = st
		m.Update(tasksMsg{tasks: []asana.Task{openTask, sideTask}})
		m.moveTo(1)
		m.focusReader = reader
		m.Update(key("P"))
		if got, _ := m.selected(); got.GID != sideTask.GID || m.cursor != 0 || m.pinnedCount != 1 || len(m.visible) != 2 {
			t.Fatalf("pin: selected = %q, cursor = %d, visible = %+v", got.GID, m.cursor, m.visible)
		}
		loaded, err := state.Load(path)
		if err != nil || !slices.Equal(loaded.PinnedTasks[""], []string{sideTask.GID}) {
			t.Fatalf("persisted pins = %+v, err = %v", loaded, err)
		}
		restarted := New(Deps{Config: m.deps.Config, State: loaded})
		restarted.Update(tasksMsg{tasks: []asana.Task{openTask, sideTask}})
		if restarted.pinnedCount != 1 || restarted.visible[0].GID != sideTask.GID {
			t.Fatalf("restart lost pin: %+v", restarted.visible)
		}
		m.Update(key("P"))
		if got, _ := m.selected(); got.GID != sideTask.GID || m.pinnedCount != 0 || m.viewingRow || len(st.PinnedTasks[""]) != 0 {
			t.Fatalf("unpin: selected = %q, pins = %v, Viewing = %v", got.GID, st.PinnedTasks, m.viewingRow)
		}
	}
}

func TestPinsIgnoreFilterAndViewingRemainsIndependent(t *testing.T) {
	m, st := testModel(t, config.Config{DefaultFilter: "is:open"})
	st.PinnedTasks = map[string][]string{"": {doneTask.GID, sideTask.GID}}
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask, sideTask}})
	m.filterInput.SetValue("name:nothing")
	m.applyFilter()
	if m.pinnedCount != 2 || len(m.visible) != 2 || m.visible[0].GID != doneTask.GID {
		t.Fatalf("filtered pins = %+v", m.visible)
	}
	m.showTicket(openTask)
	if !m.viewingRow || m.pinnedCount != 2 || len(m.visible) != 3 || m.groups[2] != viewingLabel {
		t.Fatalf("Viewing alongside pins = %+v, groups = %v", m.visible, m.groups)
	}
	m.moveTo(0)
	m.selectionChanged()
	if m.viewing != nil || len(m.visible) != 2 || len(st.PinnedTasks[""]) != 2 {
		t.Fatal("selection must clear Viewing without clearing pins")
	}
	m.Update(key("P"))
	if !m.viewingRow || m.visible[1].GID != doneTask.GID || m.pinnedCount != 1 {
		t.Fatalf("excluded unpin must remain in Viewing: %+v", m.visible)
	}
	m.moveTo(0)
	m.selectionChanged()
	if len(m.visible) != 1 || m.viewingRow {
		t.Fatalf("Viewing did not clear: %+v", m.visible)
	}
}

func TestPinsStayScopedAndSurviveSavedViews(t *testing.T) {
	m, st := testModel(t, config.Config{})
	st.PinnedTasks = map[string][]string{"": {openTask.GID}, "p": {sideTask.GID}}
	st.SavedViews["empty"] = state.View{Filter: "name:nothing"}
	m.Update(tasksMsg{tasks: []asana.Task{openTask, sideTask}})
	m.applySavedView("empty")
	if len(m.visible) != 1 || m.visible[0].GID != openTask.GID {
		t.Fatalf("saved view lost My Tasks pin: %+v", m.visible)
	}
	m.viewProject = &asana.Ref{GID: "p"}
	m.applyFilter()
	if len(m.visible) != 1 || m.visible[0].GID != sideTask.GID {
		t.Fatalf("project pins = %+v", m.visible)
	}
}

func TestPinSaveFailureLeavesListAndStateUnchanged(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	path := filepath.Join(t.TempDir(), "blocked", state.FileName)
	st, err := state.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Dir(path), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	m.deps.State = st
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	m.Update(key("P"))
	if len(m.pinnedTasks()) != 0 || m.pinnedCount != 0 || m.viewing != nil || !strings.HasPrefix(m.status, "saving pins:") {
		t.Fatalf("failed save changed state: pins = %v, status = %q", m.pinnedTasks(), m.status)
	}
}

func TestRetainedHeadersDoNotMergeWithSameNamedGroups(t *testing.T) {
	m, st := testModel(t, config.Config{})
	st.PinnedTasks = map[string][]string{"": {doneTask.GID}}
	regular := openTask
	regular.AssigneeSection = &asana.Ref{Name: pinnedLabel}
	m.groupBy = "section"
	m.Update(tasksMsg{tasks: []asana.Task{regular, doneTask}})
	m.showTicket(sideTask)
	if got := m.groupCounts()[pinnedLabel]; got != 1 {
		t.Fatalf("regular Pinned group count = %d", got)
	}
	for _, cursor := range []int{0, 1, 2} {
		m.cursor = cursor
		view := ansi.Strip(m.listView(50, 20))
		if strings.Count(view, "Pinned (1)") != 2 || !strings.Contains(view, "Viewing (1)") {
			t.Fatalf("separate headers missing: %q", view)
		}
	}
	m.Update(detailMsg{gid: doneTask.GID, ticket: ticket.Ticket{Task: asana.Task{GID: doneTask.GID, Name: "Updated", Completed: true}}})
	if m.visible[0].Name != "Updated" {
		t.Fatalf("pin detail did not refresh: %+v", m.visible[0])
	}
}

func TestLoadTasksFetchesMissingPinsAndReportsFailures(t *testing.T) {
	var fetched []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/p/custom_field_settings":
			io.WriteString(w, `{"data":[]}`)
		case "/projects/p/tasks":
			io.WriteString(w, `{"data":[{"gid":"1","name":"Open"}]}`)
		case "/tasks/2":
			fetched = append(fetched, "2")
			io.WriteString(w, `{"data":{"gid":"2","name":"Old completed pin","completed":true}}`)
		case "/tasks/3":
			fetched = append(fetched, "3")
			http.Error(w, `{"errors":[{"message":"Not Found"}]}`, http.StatusNotFound)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	c := asana.New("token")
	c.BaseURL = srv.URL
	pins := []string{"1", "2", "3"}
	cmd := loadTasks(c, "", &asana.Ref{GID: "p"}, pins)
	pins[1] = "changed after scheduling"
	msg := cmd().(tasksMsg)
	if msg.err != nil || len(msg.tasks) != 1 || len(msg.pins) != 1 || !msg.pins[0].Completed || !slices.Equal(fetched, []string{"2", "3"}) || !strings.Contains(msg.warning, "3:") {
		t.Fatalf("load: tasks = %+v, pins = %+v, fetched = %v, warning = %q, err = %v", msg.tasks, msg.pins, fetched, msg.warning, msg.err)
	}
	m, st := testModel(t, config.Config{DefaultFilter: "is:open"})
	st.PinnedTasks = map[string][]string{"p": {"2", "3"}}
	m.viewProject = &asana.Ref{GID: "p"}
	m.Update(msg)
	if len(m.visible) != 2 || m.visible[0].GID != "2" || m.status != msg.warning || len(m.tasks) != 1 || len(m.pinnedTasks()) != 2 {
		t.Fatalf("missing pin load: visible = %+v, status = %q", m.visible, m.status)
	}
	m.Update(key("P"))
	m.moveTo(1)
	m.selectionChanged()
	if len(m.visible) != 1 || m.visible[0].GID != "1" {
		t.Fatalf("unpin must not add an old task to the normal list: %+v", m.visible)
	}
}
