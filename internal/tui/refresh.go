package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/config"
)

type refreshTickMsg struct{ seq uint64 }

func (m *Model) scheduleRefresh() tea.Cmd {
	seq := m.refreshSeq
	return tea.Tick(m.refreshInterval, func(time.Time) tea.Msg { return refreshTickMsg{seq: seq} })
}

// autoRefresh keeps one timer chain and avoids changing the list during input
// or overlapping a task request. A skipped refresh retries next interval.
func (m *Model) autoRefresh(msg refreshTickMsg) tea.Cmd {
	if msg.seq != m.refreshSeq {
		return nil
	}
	next := m.scheduleRefresh()
	if m.loading || m.filtering || m.help || m.modal != nil || m.input != nil || m.form != nil ||
		m.run != nil || m.edit != nil || m.timeEntry != nil || m.menuFor != "" {
		return next
	}
	return tea.Batch(next, m.reload())
}

func (m *Model) openRefreshInterval() {
	b := newInputBox("Auto-update interval (session only; e.g. 15s or 1m)", "")
	b.area.SetValue(m.refreshInterval.String())
	b.onSubmit = func(value string) tea.Cmd {
		interval, err := config.ParseRefreshInterval(value)
		if err != nil {
			b.err = err.Error()
			return nil
		}
		m.refreshInterval = interval
		m.refreshSeq++
		m.input = nil
		m.status = fmt.Sprintf("auto-update: %s (session only)", interval)
		return m.scheduleRefresh()
	}
	m.input = b
}
