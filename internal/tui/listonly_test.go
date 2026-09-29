package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func TestNoPreviewShowsOnlyTheList(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.deps.NoPreview = true
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	m.Update(key("tab"))
	if listW, _, split := m.paneWidths(); listW != 160 || split || m.focusReader {
		t.Fatalf("listW = %d, split = %v, focusReader = %v", listW, split, m.focusReader)
	}
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask}})
	body := m.body()
	if m.shownGID != "1" || !strings.Contains(body, "Fix login") || strings.Contains(body, "Loading") {
		t.Fatalf("shown = %q, body = %q", m.shownGID, body)
	}
	if strings.Contains(m.helpView(), "next field") || strings.Contains(m.helpView(), "views panel") {
		t.Fatal("key help must not advertise the reader in list-only mode")
	}
}

func TestEnterOpensMenuAfterDetailLoads(t *testing.T) {
	m, _ := testModel(t, config.Config{Actions: []config.Action{{Name: "Go", Key: "x", Mode: config.ModeExit, Command: "true"}}})
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	_, cmd := m.Update(key("enter"))
	if cmd == nil || m.modal != nil || m.menuFor != "1" {
		t.Fatalf("cmd = %v, modal = %v, menuFor = %q", cmd, m.modal, m.menuFor)
	}
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask}})
	if m.modal == nil || !strings.HasPrefix(m.modal.title, "Run on: ") || m.menuFor != "" {
		t.Fatalf("modal = %+v, menuFor = %q", m.modal, m.menuFor)
	}
}

func TestEnterWithCachedDetailOpensMenuImmediately(t *testing.T) {
	m, _ := testModel(t, config.Config{Actions: []config.Action{{Name: "Go", Key: "x", Mode: config.ModeExit, Command: "true"}}})
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask}})
	m.Update(key("enter"))
	if m.modal == nil || !strings.HasPrefix(m.modal.title, "Run on: ") {
		t.Fatalf("modal = %+v", m.modal)
	}
}
