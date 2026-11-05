package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/asana"
)

// syncChanged marks the My Tasks tickets modified since they were last shown,
// and forgets seen tickets the list no longer has. Project views mark none.
func (m *Model) syncChanged(msg tasksMsg) {
	m.changed = nil
	if msg.project != nil {
		return
	}
	listed := append(slices.Clone(msg.tasks), msg.pins...)
	saved := m.deps.State.SyncSeen(listed)
	m.changed = map[string]bool{}
	for _, t := range listed {
		if m.deps.State.Changed(t) {
			m.changed[t.GID] = true
		}
	}
	if saved {
		m.saveSeen()
	}
}

// markSeen records t, shown in the reader, as seen at its current version.
func (m *Model) markSeen(t asana.Task) {
	delete(m.changed, t.GID)
	if m.deps.State.MarkSeen(t) {
		m.saveSeen()
	}
}

// splitNew separates the tickets the New section lists from the rest, in
// order: with list.new.section on in My Tasks, the changed tickets not being
// read, and the held one being read.
func (m *Model) splitNew(tasks []asana.Task) (fresh, rest []asana.Task) {
	if !m.deps.Config.List.New.Section || m.changed == nil {
		return nil, tasks
	}
	for _, t := range tasks {
		if t.GID == m.newHeld || m.changed[t.GID] && t.GID != m.shownGID {
			fresh = append(fresh, t)
		} else {
			rest = append(rest, t)
		}
	}
	return fresh, rest
}

// nextNew selects the next ticket with a NEW badge after the cursor,
// wrapping around.
func (m *Model) nextNew() tea.Cmd {
	for step := 1; step <= len(m.visible); step++ {
		if i := (m.cursor + step) % len(m.visible); m.newBadge(i) != "" {
			m.reveal(i)
			return m.selectionChanged()
		}
	}
	m.setStatus(statusInfo, "no new tickets")
	return nil
}

// newHeaderStyle is the New section header: the NEW badge's fill color as
// text, or its text color when the badge has no fill.
func newHeaderStyle() lipgloss.Style {
	c := newStyle.GetBackground()
	if _, none := c.(lipgloss.NoColor); none {
		c = newStyle.GetForeground()
	}
	return lipgloss.NewStyle().Foreground(c).Bold(true)
}

func (m *Model) saveSeen() {
	if err := m.deps.State.Save(); err != nil {
		m.setStatus(statusError, "saving state: "+err.Error())
	}
}

// newBadge is visible ticket i's NEW badge, reversed on the selected row, or
// "" unless it changed since last shown. The shown ticket is being read, so a
// refresh that changed it is not news.
func (m *Model) newBadge(i int) string {
	t := m.visible[i]
	if !m.changed[t.GID] || t.GID == m.shownGID {
		return ""
	}
	return newPill(i == m.cursor)
}

// newPill is the NEW badge, reversed on a selected row.
func newPill(selected bool) string {
	style := newStyle
	if selected {
		style = style.Reverse(!style.GetReverse())
	}
	return style.Render(" NEW ")
}
