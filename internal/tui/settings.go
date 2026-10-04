package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/state"
)

// onOff names a toggle's state for the settings menu.
func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// openSettings shows the display settings, with the cursor on the given row.
// Picking a toggle flips it and reopens the menu on that row, so several can
// change in a row.
func (m *Model) openSettings(cursor int) {
	toggle := func(row int, fn func()) func() tea.Cmd {
		return func() tea.Cmd {
			fn()
			m.saveDisplay()
			m.openSettings(row)
			return nil
		}
	}
	items := []pickItem{
		{Label: "Separators", Hint: onOff(m.separator), Key: "s", Value: toggle(0, func() { m.separator = !m.separator })},
		{Label: "Header spacing", Hint: onOff(m.spacing), Key: "h", Value: toggle(1, func() { m.spacing = !m.spacing })},
		{Label: "Reader view", Hint: m.readerView, Key: "v", Value: toggle(2, m.toggleReaderView)},
		{Label: "Auto-update interval", Hint: shortDuration(m.refreshInterval), Key: "i", Value: func() tea.Cmd {
			m.modal = nil
			m.openRefreshInterval()
			return nil
		}},
	}
	items = append(items, pickItem{Label: "Build / edit actions", Key: "a", Value: func() tea.Cmd { m.modal = nil; m.openActionBuilder(); return nil }})
	p := newPicker(pickValue(func(open func() tea.Cmd) tea.Cmd { return open() }), "Settings", items)
	p.keySelect = true
	p.cursor = cursor
	m.modal = p
}

// saveDisplay saves the display settings for later sessions.
func (m *Model) saveDisplay() {
	separator, spacing := m.separator, m.spacing
	m.deps.State.Display = state.Display{
		Separator:       &separator,
		HeaderSpacing:   &spacing,
		ReaderView:      m.readerView,
		RefreshInterval: shortDuration(m.refreshInterval),
	}
	if err := m.deps.State.Save(); err != nil {
		m.status = "saving settings: " + err.Error()
	}
}
