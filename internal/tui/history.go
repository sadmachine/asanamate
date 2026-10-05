package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// recordViewed puts the selected ticket first in the ticket history.
func (m *Model) recordViewed() {
	t, ok := m.selected()
	if !ok {
		return
	}
	rt := state.RecentTicket{GID: t.GID, Name: t.Name}
	if len(t.Memberships) > 0 {
		rt.Project = t.Memberships[0].Project.Name
	}
	m.deps.State.TouchTicket(rt)
	if err := m.deps.State.Save(); err != nil {
		m.status = "saving state: " + err.Error()
	}
}

// openHistory lists the ticket history, most recent first.
func (m *Model) openHistory() {
	if len(m.deps.State.RecentTickets) == 0 {
		m.status = "no ticket history yet"
		return
	}
	items := make([]pickItem, 0, len(m.deps.State.RecentTickets))
	for _, rt := range m.deps.State.RecentTickets {
		items = append(items, pickItem{Label: ticket.Clean(rt.Name), Hint: ticket.Clean(rt.Project), Value: rt})
	}
	m.modal = newPicker(pickValue(m.pickedHistory), "Ticket history", items)
}

// pickedHistory shows the chosen ticket, loading it first when the view has
// never had it.
func (m *Model) pickedHistory(rt state.RecentTicket) tea.Cmd {
	m.modal = nil
	byGID := func(t asana.Task) bool { return t.GID == rt.GID }
	if i := slices.IndexFunc(m.visible, byGID); i >= 0 {
		return m.showTicket(m.visible[i])
	}
	if i := slices.IndexFunc(m.tasks, byGID); i >= 0 {
		return m.showTicket(m.tasks[i])
	}
	if d, ok := m.details[rt.GID]; ok {
		return m.showTicket(d.Task)
	}
	m.openGID = rt.GID
	m.status = "loading " + ticket.Clean(rt.Name) + "…"
	return loadDetail(m.deps.Client, rt.GID)
}

// showTicket selects t, keeping it in the list when the view doesn't show
// it, and focuses the reader on it.
func (m *Model) showTicket(t asana.Task) tea.Cmd {
	byGID := func(v asana.Task) bool { return v.GID == t.GID }
	if !slices.ContainsFunc(m.visible, byGID) {
		m.viewing = &t
		m.applyFilter()
	}
	m.moveTo(slices.IndexFunc(m.visible, byGID))
	cmd := m.selectionChanged()
	if !m.deps.NoPreview {
		m.focusList()
		m.focusReaderPane()
	}
	return cmd
}
