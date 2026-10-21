package tui

import (
	"cmp"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/config"
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
	toggle := func(row int, key string, change func() any) func() tea.Cmd {
		return func() tea.Cmd {
			m.saveSetting(key, change())
			m.openSettings(row)
			return nil
		}
	}
	items := []pickItem{
		{Label: "Separators", Hint: onOff(m.separator), Key: "s", Value: toggle(0, "list.separator", func() any { m.separator = !m.separator; return m.separator })},
		{Label: "Header spacing", Hint: onOff(m.spacing), Key: "h", Value: toggle(1, "list.header.spacing", func() any { m.spacing = !m.spacing; return m.spacing })},
		{Label: "Reader view", Hint: m.readerView, Key: "v", Value: toggle(2, "reader.view", func() any { m.toggleReaderView(); return m.readerView })},
		{Label: "Selection style", Hint: m.selectionStyle, Key: "l", Value: toggle(3, "list.selection.style", func() any {
			if m.selectionStyle == config.StyleBar {
				m.selectionStyle = config.StyleMarker
			} else {
				m.selectionStyle = config.StyleBar
			}
			m.fitList()
			return m.selectionStyle
		})},
		{Label: "Dim unfocused panes", Hint: onOff(m.focusDim), Key: "d", Value: toggle(4, "layout.focus.dim", func() any { m.focusDim = !m.focusDim; return m.focusDim })},
		{Label: "Theme", Hint: cmp.Or(m.deps.Config.Theme.Name, config.ThemeAuto), Key: "t", Value: func() tea.Cmd {
			m.modal = nil
			return m.openThemes()
		}},
		{Label: "List columns for this project", Key: "c", Value: m.openColumns},
	}
	p := newPicker(pickValue(func(open func() tea.Cmd) tea.Cmd { return open() }), "Visual settings", items)
	p.keySelect = true
	p.cursor = cursor
	m.modal = p
}

// openTools shows program settings and tools independently of display settings.
func (m *Model) openTools() {
	items := []pickItem{
		{Label: "Build / edit actions", Key: "a", Value: func() tea.Cmd { m.modal = nil; m.openActionBuilder(); return nil }},
		{Label: "Extensions", Key: "o", Value: func() tea.Cmd { m.openExtensions(); return nil }},
		{Label: "Auto-update interval", Hint: shortDuration(m.refreshInterval), Key: "i", Value: func() tea.Cmd {
			m.modal = nil
			m.openRefreshInterval()
			return nil
		}},
		{Label: "Status history", Key: "n", Value: func() tea.Cmd { m.openStatusHistory(); return nil }},
	}
	p := newPicker(pickValue(func(open func() tea.Cmd) tea.Cmd { return open() }), "Settings & tools", items)
	p.keySelect = true
	m.modal = p
}

// saveSetting persists one setting, retaining session changes on failure.
func (m *Model) saveSetting(key string, value any) {
	path, err := m.configPath()
	if err == nil {
		err = config.SaveSetting(path, key, value)
	}
	if err != nil {
		m.setStatus(statusError, "saving settings: "+err.Error())
	}
}
