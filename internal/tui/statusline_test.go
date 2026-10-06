package tui

import (
	"regexp"
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
	if !m.help || !regexp.MustCompile(`esc/1 +list`).MatchString(ansi.Strip(m.body())) {
		t.Fatalf("help = %v, body = %q", m.help, ansi.Strip(m.body()))
	}
	m.Update(key("j"))
	if m.help || m.cursor != 0 {
		t.Fatalf("help = %v, cursor = %d: the closing key must not also move", m.help, m.cursor)
	}
}

func TestFilterGuideLayout(t *testing.T) {
	m := splitModel(t)
	m.Update(key("/"))
	for _, size := range []tea.WindowSizeMsg{
		{Width: 120, Height: 20},
		{Width: 45, Height: 20},
		{Width: 45, Height: 7},
	} {
		m.Update(size)
		hints := m.filterHints()
		plain := ansi.Strip(strings.Join(hints, " "))
		for _, want := range []string{"section:", "project:", "assignee:", "tag:", "is:", "agent:", "? guide"} {
			if !strings.Contains(plain, want) {
				t.Errorf("%dx%d guide lacks %q: %q", size.Width, size.Height, want, plain)
			}
		}
		for _, line := range hints {
			if width := ansi.StringWidth(line); width > size.Width {
				t.Errorf("%dx%d guide line width = %d", size.Width, size.Height, width)
			}
		}
		if !strings.Contains(ansi.Strip(m.body()), "? guide") {
			t.Errorf("%dx%d body lacks guide", size.Width, size.Height)
		}
	}
	m.Update(key("esc"))
	if m.filtering || len(m.filterHints()) != 0 || strings.Contains(ansi.Strip(m.body()), "FILTER BY") {
		t.Fatal("filter guide remains after editing")
	}
}

func TestFilterHelpReturnsToEditing(t *testing.T) {
	m := splitModel(t)
	m.Update(tea.WindowSizeMsg{Width: 45, Height: 20})
	m.filterInput.SetValue("is:open")
	m.applyFilter()
	m.Update(key("/"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(key("?"))
	if !m.help || !m.filtering || m.filterInput.Value() != "is:open" || !strings.Contains(ansi.Strip(m.body()), "agent:any") {
		t.Fatal("filter help did not open over the current query")
	}
	for _, line := range strings.Split(m.filterHelpView(), "\n") {
		if width := ansi.StringWidth(line); width > m.width-6 {
			t.Errorf("filter help line too wide: %d cells: %q", width, ansi.Strip(line))
		}
	}
	if height := strings.Count(m.filterHelpView(), "\n") + 3; height > m.bodyHeight() {
		t.Errorf("filter help height = %d, body height = %d", height, m.bodyHeight())
	}
	m.Update(key("j"))
	if m.help || !m.filtering || m.filterInput.Value() != "is:open" || m.cursor != 0 {
		t.Fatal("closing filter help changed the query or selection")
	}
	m.Update(key("x"))
	if got := m.filterInput.Value(); got != "is:opexn" {
		t.Fatalf("filter cursor moved while help was open: %q", got)
	}
	m.Update(key("enter"))
	if m.filtering || m.filterInput.Value() != "is:opexn" {
		t.Fatal("filter editing did not finish with the query intact")
	}
	m.Update(key("?"))
	if !m.help || !strings.Contains(ansi.Strip(m.body()), "Keys") {
		t.Fatal("key help did not open outside filter editing")
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
