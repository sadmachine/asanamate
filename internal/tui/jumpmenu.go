package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
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

// detailsKey names the details card in the jump menu and the flash.
const detailsKey = "details"

// flashDuration is how long a jumped-to section stays drawn in the accent
// color. It flashes once, so the eye finds the section without a blink.
const flashDuration = 750 * time.Millisecond

// flashDoneMsg ends the flash with seq, unless a newer one replaced it.
type flashDoneMsg struct{ seq uint64 }

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
	if len(m.fieldTargets(t)) > 1 {
		items = append(items, pickItem{Label: "Details", Key: "t", Value: detailsKey})
	}
	for _, key := range m.cardTargets {
		if s, ok := jumpSections[key]; ok {
			items = append(items, pickItem{Label: s.label, Key: s.key, Value: key})
		}
	}
	p := newPicker(pickValue(func(section string) tea.Cmd {
		m.modal = nil
		m.focusReaderPane()
		return m.jumpTo(t, section)
	}), "Jump to", items)
	p.keySelect = true
	m.modal = p
}

// jumpTo selects section of ticket t, or the first details row, and scrolls
// it to the reader's top, or shows the ticket's top when it fits on screen
// from there. The section flashes for flashDuration.
func (m *Model) jumpTo(t ticket.Ticket, section string) tea.Cmd {
	key := section
	if section == detailsKey {
		key = m.fieldTargets(t)[0]
	}
	m.fieldKey = key
	m.flashKey, m.flashGID = section, t.GID
	m.flashSeq++
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
	seq := m.flashSeq
	return tea.Tick(flashDuration, func(time.Time) tea.Msg { return flashDoneMsg{seq: seq} })
}

// endFlash draws the flashed section normally again, unless a newer flash
// replaced it.
func (m *Model) endFlash(msg flashDoneMsg) {
	if msg.seq == m.flashSeq && m.flashKey != "" {
		m.flashKey = ""
		m.renderDetail(true)
	}
}

// flashGutter draws the accent bar in the reader's left margin beside the
// flashed open section's lines.
func (m *Model) flashGutter(content string) string {
	from, to := m.flashLines[0], m.flashLines[1]
	if from > to {
		return content
	}
	lines := strings.Split(content, "\n")
	bar := m.accentStyle.Render(m.sym.border.Left)
	for i := from; i <= to && i < len(lines); i++ {
		if rest, ok := strings.CutPrefix(lines[i], " "); ok {
			lines[i] = bar + rest
		}
	}
	return strings.Join(lines, "\n")
}
