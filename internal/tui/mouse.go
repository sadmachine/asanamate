package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// doubleClick is the longest gap between two clicks on one cell that make a
// double click.
const doubleClick = 400 * time.Millisecond

// paneID names a pane the mouse can hit.
type paneID int

const (
	paneNav paneID = iota + 1
	paneList
	paneFeed
	paneReader
	paneActivityList
	paneActivityReader
)

// overlay names the floating box drawn over the panes, which takes the
// mouse from them.
type overlay int

const (
	overlayNone overlay = iota
	overlayNotice
	overlayPicker
	overlayExtension
	overlayHelp
	overlayOther // a box the mouse does nothing in, such as a form
)

// hitArea is a rectangle of the screen, in cells.
type hitArea struct{ x, y, w, h int }

func (a hitArea) contains(x, y int) bool {
	return x >= a.x && x < a.x+a.w && y >= a.y && y < a.y+a.h
}

// hitPane is a pane as drawn: its rectangle, and whether a panel frame
// surrounds its content.
type hitPane struct {
	id paneID
	hitArea
	framed bool
}

// content is the pane's content rectangle: inside the side and bottom
// borders and below the top edge line of a panel.
func (p hitPane) content() hitArea {
	if !p.framed {
		return p.hitArea
	}
	return hitArea{p.x + 1, p.y + 1, p.w - 2, p.h - 2}
}

// mouseHits records where the last frame drew what the mouse can hit, so a
// click acts on what the user saw.
type mouseHits struct {
	panes   []hitPane
	overlay overlay
	box     hitArea // the overlay's box
	boxW    int     // the overlay content's width
	boxH    int     // the overlay content's height limit
	toast   hitArea // zero while no toast shows
}

// hitPane records a pane drawn at x, y.
func (m *Model) hitPane(id paneID, x, y, w, h int, framed bool) {
	m.hits.panes = append(m.hits.panes, hitPane{id, hitArea{x, y, w, h}, framed})
}

// hitList records the list panel at x, w columns wide and h rows tall, and
// the Feed panel at its foot.
func (m *Model) hitList(x, w, h int, framed bool) {
	feed := m.feedHeight()
	m.hitPane(paneList, x, 0, w, h-feed, framed)
	if feed > 0 {
		m.hitPane(paneFeed, x, h-feed, w, feed, true)
	}
}

// paneAt returns the pane under x, y.
func (m *Model) paneAt(x, y int) (hitPane, bool) {
	for _, p := range m.hits.panes {
		if p.contains(x, y) {
			return p, true
		}
	}
	return hitPane{}, false
}

// handleMouse runs mouse events: the wheel scrolls what is under the
// pointer, a click focuses and selects, and a double click runs what enter
// runs there. The mouse does nothing a key could not.
func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		return m.mouseWheel(msg)
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			return m.mouseClick(msg.Mouse())
		}
	}
	return nil
}

func (m *Model) mouseWheel(msg tea.MouseWheelMsg) tea.Cmd {
	var step int
	var dir string // the binding a step runs in the views and Feed panels
	switch msg.Button {
	case tea.MouseWheelDown:
		step, dir = 1, "down"
	case tea.MouseWheelUp:
		step, dir = -1, "up"
	default:
		return nil
	}
	switch m.hits.overlay {
	case overlayPicker:
		if m.modal != nil {
			m.modal.move(step)
		}
		return nil
	case overlayExtension:
		if m.extension != nil {
			m.extension.reader, _ = m.extension.reader.Update(msg)
		}
		return nil
	case overlayNone:
	default:
		return nil
	}
	p, ok := m.paneAt(msg.X, msg.Y)
	if !ok || (m.composer != nil && p.id != paneReader) || m.loading || m.filtering {
		return nil
	}
	a := m.activity
	switch p.id {
	case paneReader:
		m.reader, _ = m.reader.Update(msg)
	case paneActivityReader:
		a.reader, _ = a.reader.Update(msg)
	case paneList:
		m.focusList()
		m.moveTo(m.cursor + step)
		return m.selectionChanged()
	case paneNav:
		if !m.focusNav {
			m.focusViews()
		}
		return m.updateNav(dir)
	case paneFeed:
		m.focusFeedPanel()
		return m.updateFeed(dir)
	case paneActivityList:
		m.focusNav, a.details = false, false
		a.cursor = max(0, min(a.cursor+step, len(a.rows)-1))
		m.renderActivityDetail(false)
	}
	return nil
}

func (m *Model) mouseClick(ms tea.Mouse) tea.Cmd {
	double := m.click.x == ms.X && m.click.y == ms.Y && m.now().Sub(m.click.at) <= doubleClick
	m.click = lastClick{ms.X, ms.Y, m.now()}
	if double {
		// A third click starts over.
		m.click.at = time.Time{}
	}
	switch m.hits.overlay {
	case overlayNotice:
		if m.hits.box.contains(ms.X, ms.Y) && len(m.notices) > 0 {
			m.notices = m.notices[1:]
		}
		return nil
	case overlayPicker:
		return m.clickPicker(ms, double)
	case overlayHelp:
		m.help = false
		return nil
	case overlayNone:
	default:
		return nil
	}
	if m.hits.toast.contains(ms.X, ms.Y) {
		m.toast = nil
		return nil
	}
	if ms.Y == m.height-1 {
		return m.clickStatusline(ms.X)
	}
	p, ok := m.paneAt(ms.X, ms.Y)
	if !ok || m.loading || m.filtering {
		return nil
	}
	c := p.content()
	line, inside := ms.Y-c.y, c.contains(ms.X, ms.Y)
	if p.id == paneReader || p.id == paneActivityReader {
		vp := &m.reader
		if p.id == paneActivityReader {
			vp = &m.activity.reader
		}
		if inside {
			if url := viewportLink(*vp, ms.X-c.x, line); url != "" {
				return openURL(url)
			}
		}
	}
	if m.composer != nil {
		return nil
	}
	if double {
		return m.doubleClickPane(p, inside)
	}
	// Find the row before a focus change redraws the pane.
	a, row := m.activity, -1
	if inside {
		var rows []int
		switch {
		case p.id == paneNav:
			_, rows = m.navRows(c.w, c.h)
		case p.id == paneList && len(m.visible) > 0:
			_, rows = m.listRows(c.w, c.h)
		case p.id == paneFeed:
			_, rows = m.feedLayout(c.w, c.h)
		case p.id == paneActivityList && len(a.rows) > 0:
			_, rows = m.activityRows(c.w, c.h)
		}
		if line < len(rows) {
			row = rows[line]
		}
	}
	switch p.id {
	case paneNav:
		if row >= 0 {
			m.navCursor = row
			return m.updateNav("open")
		}
		m.focusViews()
	case paneList:
		m.focusList()
		if row >= 0 {
			m.moveTo(row)
			return m.selectionChanged()
		}
	case paneFeed:
		m.focusFeedPanel()
		if row >= 0 {
			m.feed.cursor, m.feed.selected = row, m.shownFeed(m.feedRows())[row].key()
		}
	case paneReader:
		if _, _, split := m.paneWidths(); split || m.showNav() {
			m.focusReaderPane()
		}
	case paneActivityList:
		m.focusNav, a.details = false, false
		if row >= 0 {
			a.cursor = row
			m.renderActivityDetail(false)
		}
	case paneActivityReader:
		m.focusNav, a.details = false, true
	}
	return nil
}

// doubleClickPane runs enter on what the first click selected. It does not
// select again, since the first click may have scrolled another row under
// the pointer.
func (m *Model) doubleClickPane(p hitPane, inside bool) tea.Cmd {
	if !inside {
		return nil
	}
	c := p.content()
	switch p.id {
	case paneList:
		if len(m.visible) == 0 {
			return nil
		}
		// Not on a header or below the last row.
		if _, rows := m.listRows(c.w, c.h); m.click.y-c.y >= len(rows) || rows[m.click.y-c.y] < 0 {
			return nil
		}
		if b, ok := m.bindingNamed("action"); ok {
			return b.run(m, tea.KeyPressMsg{})
		}
	case paneFeed:
		return m.updateFeed("open")
	case paneActivityList:
		return m.openActivityEntry()
	}
	return nil
}

// clickPicker highlights the clicked row, or chooses it on a double click.
func (m *Model) clickPicker(ms tea.Mouse, double bool) tea.Cmd {
	p, box := m.modal, m.hits.box
	// Inside the border and the one-column padding.
	x, line := ms.X-box.x-2, ms.Y-box.y-1
	if p == nil || x < 0 || x >= p.listWidth(m.hits.boxW) {
		return nil
	}
	i, ok := p.rowAt(p.listWidth(m.hits.boxW), m.hits.boxH, m.modalLook(), line)
	switch {
	case !ok:
	case double && i == p.cursor:
		return m.modalResult(p.chooseCurrent(), nil)
	default:
		p.cursor = i
	}
	return nil
}

// clickStatusline presses the key of the hint at column x.
func (m *Model) clickStatusline(x int) tea.Cmd {
	_, at, hints := m.statuslineLayout(!m.toastVisible())
	if at < 0 || x < at {
		return nil
	}
	for _, h := range hints {
		w := ansi.StringWidth(h.text)
		if x < at+w {
			if h.key == "" {
				return nil
			}
			return m.handleKey(keyPress(h.key))
		}
		at += w + 2 // the gap between hints
	}
	return nil
}

// lastClick is where and when the last click landed, to find double clicks.
type lastClick struct {
	x, y int
	at   time.Time
}

// viewportLink returns the URL of the OSC 8 hyperlink drawn at column x of
// line in vp, or "".
func viewportLink(vp viewport.Model, x, line int) string {
	lines := strings.Split(vp.View(), "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	return linkAt(lines[line], x)
}

// linkAt returns the URL of the OSC 8 hyperlink drawn at column x of line,
// or "".
func linkAt(line string, x int) string {
	var state byte
	url, col := "", 0
	for line != "" {
		seq, width, n, next := ansi.DecodeSequence(line, state, nil)
		switch {
		case strings.HasPrefix(seq, "\x1b]8;"):
			body := strings.TrimSuffix(strings.TrimSuffix(seq[len("\x1b]8;"):], "\x07"), "\x1b\\")
			_, url, _ = strings.Cut(body, ";")
		case width > 0:
			if x < col+width {
				return url
			}
			col += width
		}
		state, line = next, line[n:]
	}
	return ""
}
