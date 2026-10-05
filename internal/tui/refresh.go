package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/config"
)

type refreshTickMsg struct{ seq uint64 }

// refresh updates the current view without covering it with a loading modal.
func (m *Model) refresh() tea.Cmd {
	cmd := m.reload()
	m.background = true
	return cmd
}

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
	if m.loading || len(m.detailRequests) > 0 || m.filtering || m.help || m.modal != nil || m.input != nil || m.form != nil || m.builder != nil ||
		m.run != nil || m.edit != nil || m.timeEntry != nil || m.menuFor != "" {
		return next
	}
	cmd := m.refresh()
	return tea.Batch(next, cmd)
}

func (m *Model) openRefreshInterval() {
	b := newInputBox("Auto-update interval (e.g. 15s or 1m)", "")
	b.area.SetValue(shortDuration(m.refreshInterval))
	b.onSubmit = func(value string) tea.Cmd {
		interval, err := config.ParseRefreshInterval(value)
		if err != nil {
			b.err = err.Error()
			return nil
		}
		m.refreshInterval = interval
		m.refreshSeq++
		m.input = nil
		m.status = "auto-update: " + shortDuration(interval)
		m.saveDisplay()
		return m.scheduleRefresh()
	}
	m.input = b
}

// refreshLabel is the statusline's auto-update interval, led by the refresh
// icon, or by "auto" when the symbol set has no icons.
func (m *Model) refreshLabel() string {
	lead := m.sym.icon(iconRefresh)
	if lead == "" {
		lead = "auto "
	}
	return lead + shortDuration(m.refreshInterval)
}

// shortDuration drops a duration's zero trailing units: 1m, not 1m0s.
func shortDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}
