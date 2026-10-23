package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// ticketOnlyModel opens ticket "1" alone, as asanamate read does.
func ticketOnlyModel(t *testing.T) *Model {
	t.Helper()
	_, st := testModel(t, config.Config{})
	m := New(Deps{Config: config.Default(), State: st, StateDir: t.TempDir(), Ticket: "1"})
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask}})
	return m
}

func TestTicketOnlyShowsOnlyTheReader(t *testing.T) {
	m := ticketOnlyModel(t)
	if m.loading || !m.focusReader || m.shownGID != "1" {
		t.Fatalf("loading = %v, focusReader = %v, shown = %q", m.loading, m.focusReader, m.shownGID)
	}
	if _, readerW, split := m.paneWidths(); split || readerW != 200 {
		t.Fatalf("readerW = %d, split = %v", readerW, split)
	}
	body := m.body()
	if !strings.Contains(body, "Fix login") || strings.Contains(body, "[2] Ticket") || strings.Contains(body, "Views") {
		t.Fatalf("body = %q", body)
	}
	if bar := ansi.Strip(m.statusline()); strings.Contains(bar, "My Tasks") || strings.Contains(bar, "pane") || strings.Contains(bar, "list") {
		t.Fatalf("statusline = %q", bar)
	}
}

func TestTicketOnlyIgnoresListKeys(t *testing.T) {
	m := ticketOnlyModel(t)
	for _, k := range []string{"esc", "1", "0", "3", "tab", "shift+tab", "/", "S", "p", "I", "H", "z", "Z", "F", "V", "b", "n"} {
		m.Update(key(k))
		if !m.focusReader || m.filtering || m.activityOpen || m.modal != nil || m.input != nil || m.showNav() || !m.readerOn() {
			t.Fatalf("%s: focusReader = %v, filtering = %v, activity = %v, modal = %v", k, m.focusReader, m.filtering, m.activityOpen, m.modal != nil)
		}
	}
	if help := m.helpView(); strings.Contains(help, "filter") || strings.Contains(help, "views panel") {
		t.Fatalf("key help advertises list keys: %q", help)
	}
}

func TestTicketOnlyReloadFetchesOnlyTheTicket(t *testing.T) {
	m := ticketOnlyModel(t)
	m.Update(key("r"))
	if m.loading || m.detailRequests["1"] == 0 {
		t.Fatalf("loading = %v, detail requests = %v", m.loading, m.detailRequests)
	}
}
