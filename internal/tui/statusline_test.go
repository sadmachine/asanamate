package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func splitModel(t *testing.T) *Model {
	t.Helper()
	m, _ := testModel(t, config.Config{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask}})
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask}})
	return m
}

func TestStatuslineShowsModeAndHints(t *testing.T) {
	m := splitModel(t)
	for _, tc := range []struct {
		key, want string
	}{
		{"", " NORMAL  My Tasks │ 2/2"},
		{"/", " FILTER "},
		{"esc", " NORMAL "},
		{"tab", " EDIT "},
		{"esc", " NORMAL "},
	} {
		if tc.key != "" {
			m.Update(key(tc.key))
		}
		if s := ansi.Strip(m.statusline()); !strings.HasPrefix(s, tc.want) {
			t.Fatalf("after %q: statusline = %q", tc.key, s)
		}
	}
	if s := ansi.Strip(m.statusline()); !strings.HasSuffix(s, "? keys") || ansi.StringWidth(s) != 120 {
		t.Fatalf("statusline = %q", s)
	}
}

func TestStatusMessageReplacesHints(t *testing.T) {
	m := splitModel(t)
	m.status = "Comment: done"
	s := ansi.Strip(m.statusline())
	if !strings.HasSuffix(s, "Comment: done") || strings.Contains(s, "? keys") {
		t.Fatalf("statusline = %q", s)
	}
}

func TestHelpOpensAndAnyKeyCloses(t *testing.T) {
	m := splitModel(t)
	m.Update(key("?"))
	if !m.help || !strings.Contains(ansi.Strip(m.body()), "jump to panel") {
		t.Fatalf("help = %v, body = %q", m.help, ansi.Strip(m.body()))
	}
	m.Update(key("j"))
	if m.help || m.cursor != 0 {
		t.Fatalf("help = %v, cursor = %d: the closing key must not also move", m.help, m.cursor)
	}
}

func TestPanelTitlesShowViewAndCount(t *testing.T) {
	m := splitModel(t)
	top := strings.Split(ansi.Strip(m.body()), "\n")[0]
	if !strings.Contains(top, "[1] Tickets · My Tasks") || !strings.Contains(top, " 2/2 ") || !strings.Contains(top, "[2] Ticket") {
		t.Fatalf("top = %q", top)
	}
}

func TestEmptyListExplainsAndHints(t *testing.T) {
	m := splitModel(t)
	m.filterInput.SetValue("tag:nope")
	m.applyFilter()
	body := ansi.Strip(m.listView(60, 10))
	for _, want := range []string{"All clear.", "Nothing matches tag:nope", "/ filter", "p projects"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}

func TestFirstLoadShowsPlaceholderRows(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	body := ansi.Strip(m.body())
	lines := strings.Split(body, "\n")
	if !strings.Contains(lines[0], " loading ") || !strings.Contains(lines[1], "▆ ▆▆▆") || strings.Contains(body, "Loading tasks") {
		t.Fatalf("first load:\n%s", body)
	}
	if s := ansi.Strip(m.statusline()); !strings.HasSuffix(s, "q quit") || strings.Contains(s, "enter") {
		t.Fatalf("first load statusline = %q", s)
	}
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	m.reload()
	if body := ansi.Strip(m.body()); !strings.Contains(body, "Loading tasks") || !strings.Contains(body, "Fix login") {
		t.Fatalf("a reload keeps the list under the loading modal:\n%s", body)
	}
}
