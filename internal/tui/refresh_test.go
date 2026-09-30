package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func TestRefreshIntervalSessionOverride(t *testing.T) {
	cfg := config.Default()
	cfg.List.RefreshInterval = "1m"
	m, st := testModel(t, cfg)
	m.loading = false
	if m.refreshInterval != time.Minute {
		t.Fatalf("initial interval = %s", m.refreshInterval)
	}
	m.Update(key("R"))
	if m.input == nil || m.input.area.Value() != "1m" {
		t.Fatal("R did not open modal with current interval")
	}
	m.input.area.SetValue("500ms")
	m.Update(key("ctrl+s"))
	if m.input == nil || m.input.err == "" || m.refreshInterval != time.Minute {
		t.Fatal("invalid interval changed session or closed modal")
	}
	m.input.area.SetValue("15s")
	_, cmd := m.Update(key("ctrl+s"))
	if cmd == nil || m.input != nil || m.refreshInterval != 15*time.Second || m.refreshSeq == 0 {
		t.Fatal("valid interval did not close modal and restart timer")
	}
	if !strings.Contains(m.status, "session only") || m.deps.Config.List.RefreshInterval != "1m" {
		t.Fatal("override changed config or failed to identify session scope")
	}
	// Starting another session from the same config and state restores config.
	next := New(Deps{Config: m.deps.Config, State: st})
	if next.refreshInterval != time.Minute {
		t.Fatalf("new session interval = %s", next.refreshInterval)
	}
	m.Update(key("R"))
	m.input.area.SetValue("1s")
	m.Update(key("esc"))
	if m.input != nil || m.refreshInterval != 15*time.Second {
		t.Fatal("cancel changed interval")
	}
	_, cmd = m.Update(refreshTickMsg{seq: 0})
	if cmd != nil || m.loading {
		t.Fatal("old timer started reload after override")
	}
}

func TestAutoRefreshPreservesViewAndRetriesAfterError(t *testing.T) {
	m, _ := testModel(t, config.Default())
	m.viewProject = &asana.Ref{GID: "project"}
	m.filterInput.SetValue("is:open")
	m.Update(tasksMsg{project: m.viewProject, tasks: []asana.Task{openTask, sideTask}})
	m.Update(key("j"))
	_, cmd := m.Update(refreshTickMsg{seq: m.refreshSeq})
	if cmd == nil || !m.loading || len(m.visible) != 2 || m.cursor != 1 {
		t.Fatal("timer did not reload while retaining current list")
	}
	m.Update(tasksMsg{project: m.viewProject, err: errors.New("offline")})
	if m.loading || len(m.visible) != 2 || !strings.Contains(m.status, "offline") {
		t.Fatal("failed reload did not retain list and report error")
	}
	m.Update(refreshTickMsg{seq: m.refreshSeq})
	if !m.loading {
		t.Fatal("next interval did not retry failed reload")
	}
	m.Update(tasksMsg{project: m.viewProject, tasks: []asana.Task{sideTask, doneTask, openTask}})
	selected, ok := m.selected()
	if m.loading || !ok || selected.GID != sideTask.GID || len(m.visible) != 2 ||
		m.filterInput.Value() != "is:open" || m.viewProject.GID != "project" {
		t.Fatal("reload changed selection, filter, or project")
	}
}

func TestAutoRefreshPreservesReader(t *testing.T) {
	for _, view := range []string{config.ViewCards, config.ViewMarkdown} {
		for _, wider := range []bool{false, true} {
			name := view + "/same width"
			if wider {
				name = view + "/wider list"
			}
			t.Run(name, func(t *testing.T) {
				cfg := config.Default()
				cfg.Reader.View = view
				m, _ := testModel(t, cfg)
				m.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
				m.Update(tasksMsg{tasks: []asana.Task{openTask}})
				detail := ticket.Ticket{Task: openTask}
				detail.HTMLNotes = "<body>" + strings.Repeat("Reading this ticket.<br>", 100) + "</body>"
				m.Update(detailMsg{gid: openTask.GID, ticket: detail})
				m.focusReader = true
				m.fieldKey, m.showEmpty = "description", true
				m.reader.SetYOffset(10)
				before, width := m.reader.View(), m.reader.Width()
				m.Update(refreshTickMsg{seq: m.refreshSeq})
				incoming := sideTask
				if wider {
					incoming.Name = strings.Repeat("New ticket ", 8)
				}
				_, cmd := m.Update(tasksMsg{tasks: []asana.Task{incoming, openTask}})
				selected, ok := m.selected()
				if !ok || selected.GID != openTask.GID || len(m.visible) != 2 || m.loading || cmd != nil {
					t.Fatal("refresh did not update the list while keeping the selected ticket")
				}
				if m.reader.YOffset() != 10 || !m.focusReader || m.fieldKey != "description" || !m.showEmpty {
					t.Fatal("refresh changed reader scroll, focus, or field selection")
				}
				if wider {
					if m.reader.Width() >= width {
						t.Fatal("wider list did not exercise reader reflow")
					}
				} else if m.reader.Width() != width || m.reader.View() != before {
					t.Fatal("unchanged reader width changed the ticket view")
				}
			})
		}
	}
}

func TestAutoRefreshUpdatesReaderWhenTicketRemoved(t *testing.T) {
	for _, empty := range []bool{false, true} {
		name := "select remaining ticket"
		if empty {
			name = "empty list"
		}
		t.Run(name, func(t *testing.T) {
			m, _ := testModel(t, config.Default())
			m.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
			m.Update(tasksMsg{tasks: []asana.Task{openTask, sideTask}})
			detail := ticket.Ticket{Task: openTask}
			detail.HTMLNotes = "<body>" + strings.Repeat("Reading this ticket.<br>", 100) + "</body>"
			m.Update(detailMsg{gid: openTask.GID, ticket: detail})
			m.Update(detailMsg{gid: sideTask.GID, ticket: ticket.Ticket{Task: sideTask}})
			m.fieldKey, m.showEmpty = "description", true
			m.reader.SetYOffset(10)
			m.Update(refreshTickMsg{seq: m.refreshSeq})
			tasks := []asana.Task{sideTask}
			if empty {
				tasks = nil
			}
			m.Update(tasksMsg{tasks: tasks})
			if empty {
				if m.shownGID != "" || strings.Contains(m.reader.View(), openTask.Name) {
					t.Fatal("empty list retained the removed ticket")
				}
			} else if m.shownGID != sideTask.GID || m.reader.YOffset() != 0 || m.fieldKey != "" || m.showEmpty {
				t.Fatal("removed ticket did not reset the reader to the remaining ticket")
			}
		})
	}
}

func TestAutoRefreshSkipsBusyUI(t *testing.T) {
	for name, busy := range map[string]func(*Model){
		"loading":      func(m *Model) { m.loading = true },
		"filter":       func(m *Model) { m.filtering = true },
		"help":         func(m *Model) { m.help = true },
		"picker":       func(m *Model) { m.modal = &picker{} },
		"input":        func(m *Model) { m.input = newInputBox("input", "") },
		"form":         func(m *Model) { m.form = &formModal{} },
		"action":       func(m *Model) { m.run = &pendingRun{} },
		"edit":         func(m *Model) { m.edit = &pendingEdit{} },
		"time":         func(m *Model) { m.timeEntry = &pendingTime{} },
		"pending menu": func(m *Model) { m.menuFor = "1" },
	} {
		t.Run(name, func(t *testing.T) {
			m, _ := testModel(t, config.Default())
			m.loading = false
			busy(m)
			wasLoading := m.loading
			_, cmd := m.Update(refreshTickMsg{seq: m.refreshSeq})
			if cmd == nil || m.loading != wasLoading {
				t.Fatal("busy UI started reload or stopped timer")
			}
		})
	}
}

func TestRefreshTimerWaitsForInterval(t *testing.T) {
	cfg := config.Default()
	cfg.List.RefreshInterval = "1s"
	m, _ := testModel(t, cfg)
	started := time.Now()
	cmd := m.scheduleRefresh()
	m.refreshSeq++ // A queued timer keeps its original generation.
	msg, ok := cmd().(refreshTickMsg)
	if !ok || msg.seq != 0 || time.Since(started) < time.Second {
		t.Fatalf("timer fired early or carried wrong generation: %+v", msg)
	}
}

func TestAutoRefreshShowsNoModal(t *testing.T) {
	m, _ := testModel(t, config.Default())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	if s := ansi.Strip(m.statusline()); !strings.Contains(s, "auto 30s") {
		t.Fatalf("statusline lacks the interval: %q", s)
	}
	m.Update(refreshTickMsg{seq: m.refreshSeq})
	body := ansi.Strip(m.body())
	if !m.loading || strings.Contains(body, "Loading tasks") || !strings.Contains(body, "loading") {
		t.Fatalf("timer refresh must mark the list title, not open the modal:\n%s", body)
	}
	if m.listHighlight() != warnStyle.GetForeground() {
		t.Fatal("timer refresh did not highlight the list")
	}
	m.Update(key("r")) // keys wait for the refresh
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	m.Update(key("r"))
	if !strings.Contains(ansi.Strip(m.body()), "Loading tasks") {
		t.Fatal("a manual reload must keep the modal")
	}
}
