package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/config"
)

// jumpSections are the cards view targets the jump menu lists, by target
// key, with the fixed key that picks each.
var jumpSections = map[string]struct{ key, label string }{
	"section:agents":      {"a", "Agents"},
	"section:description": {"d", "Description"},
	"section:subtasks":    {"s", "Subtasks"},
	"section:blocked-by":  {"b", "Blocked by"},
	"section:blocking":    {"B", "Blocking"},
	"section:attachments": {"f", "Attachments"},
	commentKey:            {"c", "Comments"},
}

// openJumpMenu lists the selected ticket's cards view sections in reading
// order, each under a fixed key. Picking one focuses the reader on it.
func (m *Model) openJumpMenu() {
	t, ok := m.selectedDetail()
	if !ok {
		return
	}
	if m.readerView == config.ViewMarkdown {
		m.setStatus(statusWarning, "jump to section needs the cards view")
		return
	}
	var items []pickItem
	if fields := m.fieldTargets(t); len(fields) > 1 {
		items = append(items, pickItem{Label: "Details", Key: "t", Value: fields[0]})
	}
	for _, key := range m.cardTargets {
		if s, ok := jumpSections[key]; ok {
			items = append(items, pickItem{Label: s.label, Key: s.key, Value: key})
		}
	}
	p := newPicker(pickValue(func(key string) tea.Cmd {
		m.modal = nil
		m.focusReaderPane()
		m.jumpTo(key)
		return nil
	}), "Jump to", items)
	p.keySelect = true
	m.modal = p
}

// jumpTo selects the cards view target key and scrolls it to the reader's
// top, or shows the ticket's top when the target fits on screen from there.
func (m *Model) jumpTo(key string) {
	m.fieldKey = key
	m.renderDetail(true)
	line, last := m.fieldLines[key], m.fieldEnds[key]
	if key == commentKey {
		// The Comments heading sits above the Add comment row, and the
		// section runs to the ticket's end.
		line, last = line-1, m.reader.TotalLineCount()-1
	}
	if max(last, line) < m.reader.Height() {
		m.reader.GotoTop()
	} else {
		m.reader.SetYOffset(line)
	}
}
