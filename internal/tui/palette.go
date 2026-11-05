package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// paletteSkips are the [keys.main] bindings the command palette leaves out:
// moving through the list or reader only makes sense as a key.
var paletteSkips = map[string]bool{
	"down": true, "up": true, "scroll_down": true, "scroll_up": true, "top": true, "bottom": true,
	"half_page_down": true, "half_page_up": true, "page_down": true, "page_up": true,
	"next_pane": true, "prev_pane": true, "open": true, "palette": true,
}

// paletteItem is what a command palette row runs: a [keys.main] binding,
// or the action file at index action when binding is nil.
type paletteItem struct {
	binding *binding
	action  int
}

// openPalette lists every [keys.main] command the current layout runs,
// bound to a key or not, then the actions, and runs the one picked as its
// key would. Typing searches names, keys, and groups.
func (m *Model) openPalette() tea.Cmd {
	var items []pickItem
	for i := range m.bindings {
		b := &m.bindings[i]
		if paletteSkips[b.Name] || !m.runnable(*b) {
			continue
		}
		items = append(items, pickItem{
			Label:  b.Desc,
			Hint:   strings.Join(b.keys, " "),
			Search: b.Name + " " + b.Group,
			Value:  paletteItem{binding: b},
		})
	}
	for i, a := range m.deps.Config.Actions {
		items = append(items, pickItem{Label: "Action: " + a.Name, Hint: a.Key, Search: "action " + a.Mode, Value: paletteItem{action: i}})
	}
	m.modal = newPicker(pickValue(m.pickedPalette), "Commands", items)
	m.modal.searching = true
	return m.modal.input.Focus()
}

// pickedPalette runs a command palette row: a bound command as its first
// key would, so it behaves exactly like pressing it.
func (m *Model) pickedPalette(it paletteItem) tea.Cmd {
	m.modal = nil
	switch b := it.binding; {
	case b == nil:
		return m.requestMenu(func() tea.Cmd { return m.runActionAt(it.action) })
	case len(b.keys) > 0:
		return m.handleKey(keyPress(b.keys[0]))
	default:
		return b.run(m, tea.KeyPressMsg{})
	}
}
