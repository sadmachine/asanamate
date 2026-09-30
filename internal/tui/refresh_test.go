package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
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
	if m.input == nil || m.input.area.Value() != "1m0s" {
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
