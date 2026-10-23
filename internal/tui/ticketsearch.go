package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// openTicketSearch searches the current list without changing its saved filter.
func (m *Model) openTicketSearch() tea.Cmd {
	if len(m.visible) == 0 {
		m.setStatus(statusInfo, "no tickets to search")
		return nil
	}
	items := make([]pickItem, 0, len(m.visible))
	for _, t := range m.visible {
		items = append(items, pickItem{Label: ticket.Clean(t.Name), Hint: t.GID, Search: t.GID, Value: t})
	}
	m.modal = newPicker(pickValue(func(t asana.Task) tea.Cmd {
		m.modal = nil
		m.readerHidden = false
		return m.openTicket(t.GID, t.Name)
	}), "Search current list", items)
	m.modal.searching = true
	return m.modal.input.Focus()
}
