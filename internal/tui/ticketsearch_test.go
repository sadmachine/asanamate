package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/keymap"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func TestTicketSearchSelectsWithoutChangingFilter(t *testing.T) {
	for _, tt := range []struct {
		name, query string
		width       int
		hidden      bool
		move        bool
	}{
		{name: "split, title words", query: "FOOTER fix", width: 120},
		{name: "narrow, navigation", query: "fix", width: 80, move: true},
		{name: "hidden reader, GID", query: "3", width: 120, hidden: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.List.DefaultView.Filter = "is:open"
			m, st := testModel(t, cfg)
			m.Update(tea.WindowSizeMsg{Width: tt.width, Height: 30})
			m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask, sideTask}})
			m.Update(detailMsg{gid: sideTask.GID, ticket: ticket.Ticket{Task: sideTask}})
			m.readerHidden = tt.hidden
			m.saveView()
			wantView := m.currentView()

			m.Update(key("S"))
			if m.modal == nil || !m.modal.searching || !m.modal.input.Focused() {
				t.Fatal("S must open a picker ready for typing, even with picker.type_first = false")
			}
			for _, r := range tt.query {
				m.Update(key(string(r)))
			}
			if tt.move {
				m.Update(key("down"))
			}
			if got := m.modal.current(); got == nil || got.Value.(asana.Task).GID != sideTask.GID {
				t.Fatalf("highlighted = %+v, want Fix footer", got)
			}
			if sel, _ := m.selected(); sel.GID != openTask.GID || m.focusReader || m.currentView() != wantView {
				t.Fatal("typing or moving in search changed the underlying selection or filter")
			}

			m.Update(key("enter"))
			if sel, _ := m.selected(); sel.GID != sideTask.GID || m.modal != nil || !m.focusReader || !m.readerOn() {
				t.Fatalf("selected = %q, modal = %v, reader focused = %v", sel.GID, m.modal, m.focusReader)
			}
			if !strings.Contains(m.reader.View(), sideTask.Name) {
				t.Fatal("reader does not show the chosen ticket")
			}
			if m.currentView() != wantView || len(m.visible) != 2 || m.visible[0].GID != openTask.GID || m.visible[1].GID != sideTask.GID {
				t.Fatal("selection changed the original filter or its results")
			}
			loaded, err := state.Load(st.Path())
			if err != nil {
				t.Fatal(err)
			}
			if got, ok := loaded.View(""); !ok || got != wantView {
				t.Fatalf("persisted view = %+v, exists = %v, want %+v", got, ok, wantView)
			}
			m.Update(key("esc"))
			if m.focusReader || len(m.visible) != 2 || m.filterInput.Value() != "is:open" {
				t.Fatal("returning to the list must restore the original filtered list")
			}
		})
	}
}

func TestTicketSearchCancelPreservesSelectionAndReader(t *testing.T) {
	m, st := testModel(t, config.Config{List: config.List{DefaultView: config.DefaultView{Filter: "is:open"}}})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask, sideTask}})
	m.Update(key("j"))
	m.readerHidden = true
	m.saveView()
	wantView := m.currentView()
	m.Update(key("S"))
	for _, r := range "login" {
		m.Update(key(string(r)))
	}
	m.Update(key("esc"))
	if m.modal == nil || m.modal.searching || m.modal.input.Value() != "" || len(m.modal.matches) != 2 {
		t.Fatal("first escape must clear the temporary search")
	}
	m.Update(key("esc"))
	if sel, _ := m.selected(); m.modal != nil || sel.GID != sideTask.GID || m.focusReader || m.readerOn() || m.currentView() != wantView {
		t.Fatal("cancel must preserve selection, reader visibility, and original filter")
	}
	if got, ok := st.View(""); !ok || got != wantView {
		t.Fatal("cancel changed the saved view")
	}
	m.Update(key("S"))
	if m.modal == nil || !m.modal.searching || m.modal.input.Value() != "" || len(m.modal.matches) != 2 {
		t.Fatal("reopening search must start fresh within the original filtered list")
	}
}

func TestTicketSearchCannotSelectFilteredOutTicket(t *testing.T) {
	m, _ := testModel(t, config.Config{List: config.List{DefaultView: config.DefaultView{Filter: "is:open"}}})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask, sideTask}})
	m.Update(key("S"))
	if m.modal == nil {
		t.Fatal("S did not open search")
	}
	for _, r := range "docs" {
		m.Update(key(string(r)))
	}
	if len(m.modal.matches) != 0 {
		t.Fatal("search included a ticket outside the filtered list")
	}
	m.Update(key("enter"))
	if sel, _ := m.selected(); m.modal == nil || sel.GID != openTask.GID || m.focusReader || m.filterInput.Value() != "is:open" {
		t.Fatal("enter with no matches must keep search open and leave the list unchanged")
	}
}

func TestTicketSearchIncludesVisiblePins(t *testing.T) {
	m, st := testModel(t, config.Config{List: config.List{DefaultView: config.DefaultView{Filter: "is:open"}}})
	st.PinnedTasks = map[string][]string{"": {doneTask.GID}}
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask}})
	m.Update(key("S"))
	if m.modal == nil {
		t.Fatal("S did not open search")
	}
	for _, r := range "docs" {
		m.Update(key(string(r)))
	}
	m.Update(key("enter"))
	if sel, _ := m.selected(); m.modal != nil || sel.GID != doneTask.GID || !m.focusReader || m.filterInput.Value() != "is:open" || m.pinnedCount != 1 {
		t.Fatal("visible pinned tickets must be searchable without changing the filter or pins")
	}
}

func TestTicketSearchEmptyList(t *testing.T) {
	m, _ := testModel(t, config.Config{List: config.List{DefaultView: config.DefaultView{Filter: "is:open"}}})
	m.Update(tasksMsg{tasks: []asana.Task{doneTask}})
	m.Update(key("S"))
	if m.modal != nil || m.status != "no tickets to search" || m.filterInput.Value() != "is:open" {
		t.Fatal("searching an empty filtered list must leave the filter unchanged and explain why no picker opens")
	}
}

func TestTicketSearchUsesReboundKey(t *testing.T) {
	cfg := config.Default()
	var err error
	cfg.Keymap, err = keymap.Resolve(map[string]map[string][]string{"main": {"search": {";"}}})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := testModel(t, cfg)
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	m.Update(key("S"))
	if m.modal != nil {
		t.Fatal("S still opens search after rebinding")
	}
	m.Update(key(";"))
	if m.modal == nil || !m.modal.searching {
		t.Fatal("rebound key must open search ready for typing")
	}
}
