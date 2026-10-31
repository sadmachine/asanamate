package tui

import (
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	paneComposer // the comment composer under the reader
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
	overlayForm
	overlayInput // a text editor, such as a title or a comment
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
	// frame is the frame's lines as drawn, before the selection's highlight.
	frame []string
}

// boxContent is the overlay box's content: inside its border and its
// one-column padding.
func (h mouseHits) boxContent() hitArea {
	return hitArea{h.box.x + 2, h.box.y + 1, h.box.w - 4, h.box.h - 2}
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

// hitReader records the reader at x, w columns wide and h rows tall, and
// the composer pane at its foot.
func (m *Model) hitReader(x, w, h int, framed bool) {
	composer := 0
	if m.composerPane() {
		composer = m.composerH
	}
	m.hitPane(paneReader, x, 0, w, h-composer, framed)
	if composer > 0 {
		m.hitPane(paneComposer, x, h-composer, w, composer, true)
	}
}

// paneByID returns the pane id as drawn.
func (m *Model) paneByID(id paneID) (hitPane, bool) {
	for _, p := range m.hits.panes {
		if p.id == id {
			return p, true
		}
	}
	return hitPane{}, false
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
// pointer, a click focuses and selects, a double click runs what enter runs
// there, and a drag selects and copies text. Besides copying, the mouse does
// nothing a key could not.
func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		m.selection = nil
		return m.mouseWheel(msg)
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			m.selection = nil
			// The drag starts on what the press saw, before the click
			// changes it.
			d := m.startDrag(msg.Mouse())
			cmd := m.mouseClick(msg.Mouse())
			m.drag = d
			return cmd
		}
	case tea.MouseMotionMsg:
		m.mouseDrag(msg.Mouse())
	case tea.MouseReleaseMsg:
		return m.mouseRelease()
	}
	return nil
}

// cell is a screen position.
type cell struct{ x, y int }

// drag follows the left button from its press to its release, selecting
// text in an editor, or else in the pane or box it was pressed in.
type drag struct {
	area   hitArea // the pane's or box's content, which holds the selection
	anchor cell
	moved  bool
	link   string // the link under the press, opened by a release that did not move

	editor   *textarea.Model
	editorAt cell // the editor's top-left cell
}

// textSelection is the frame's text from anchor to head, read as lines
// running the width of area.
type textSelection struct {
	area         hitArea
	anchor, head cell
}

// span is the columns [from, to) of row y that a selection covers.
type span struct{ y, from, to int }

func (s textSelection) spans() []span {
	a, b := s.anchor, s.head
	if b.y < a.y || (b.y == a.y && b.x < a.x) {
		a, b = b, a
	}
	var spans []span
	for y := a.y; y <= b.y; y++ {
		from, to := s.area.x, s.area.x+s.area.w
		if y == a.y {
			from = a.x
		}
		if y == b.y {
			to = b.x + 1
		}
		spans = append(spans, span{y, from, to})
	}
	return spans
}

// text is the selected text of frame, without trailing blanks on each line.
func (s textSelection) text(frame []string) string {
	var lines []string
	for _, sp := range s.spans() {
		if sp.y < len(frame) {
			lines = append(lines, strings.TrimRight(ansi.Strip(ansi.Cut(frame[sp.y], sp.from, sp.to)), " "))
		}
	}
	return strings.Join(lines, "\n")
}

// highlight draws frame with the selection in reverse video.
func (s textSelection) highlight(frame []string) string {
	lines := slices.Clone(frame)
	for _, sp := range s.spans() {
		if sp.y >= len(lines) {
			continue
		}
		line := lines[sp.y]
		selected := ansi.Strip(ansi.Cut(line, sp.from, sp.to))
		lines[sp.y] = ansi.Cut(line, 0, sp.from) + ansi.ResetStyle + selectionStyle.Render(selected) + ansi.Cut(line, sp.to, ansi.StringWidth(line))
	}
	return strings.Join(lines, "\n")
}

var selectionStyle = lipgloss.NewStyle().Reverse(true)

// startDrag starts a drag at a press: a selection in the editor under it,
// or else in the pane or box under it. It returns nil elsewhere, such as on
// the statusline.
func (m *Model) startDrag(ms tea.Mouse) *drag {
	if ed, at, ok := m.editorAt(ms.X, ms.Y); ok {
		if c := m.composer; c != nil && c.reading && ed == &c.box.area {
			m.focusComposer()
		}
		ed.BeginSelection(ms.X-at.x, ms.Y-at.y)
		return &drag{editor: ed, editorAt: at}
	}
	var area hitArea
	d := &drag{anchor: cell{ms.X, ms.Y}}
	switch m.hits.overlay {
	case overlayNone:
		p, ok := m.paneAt(ms.X, ms.Y)
		if !ok {
			return nil
		}
		area = p.content()
		switch p.id {
		case paneReader:
			d.link = viewportLink(m.reader, ms.X-area.x, ms.Y-area.y)
		case paneActivityReader:
			d.link = viewportLink(m.activity.reader, ms.X-area.x, ms.Y-area.y)
		}
	case overlayNotice, overlayHelp:
		return nil // the press closes them
	default:
		area = m.hits.boxContent()
	}
	if !area.contains(ms.X, ms.Y) {
		return nil
	}
	d.area = area
	return d
}

// editorAt returns the text editor under x, y, with its top-left cell.
func (m *Model) editorAt(x, y int) (*textarea.Model, cell, bool) {
	var box *inputBox
	var area hitArea
	switch {
	case m.hits.overlay == overlayInput:
		box = m.input
		if m.builder != nil {
			box = m.builder.input
		}
		// Below the title.
		c := m.hits.boxContent()
		area = hitArea{c.x, c.y + 1, c.w, c.h - 1}
	case m.hits.overlay == overlayNone && m.composer != nil:
		p, ok := m.paneByID(paneComposer)
		if !ok {
			return nil, cell{}, false
		}
		// Inside the panel's one-column padding.
		box, area = m.composer.box, p.content()
		area.x, area.w = area.x+1, area.w-2
	}
	if box == nil {
		return nil, cell{}, false
	}
	area.h = min(area.h, box.area.Height())
	return &box.area, cell{area.x, area.y}, area.contains(x, y)
}

func (m *Model) mouseDrag(ms tea.Mouse) {
	d := m.drag
	switch {
	case d == nil:
	case d.editor != nil:
		d.editor.ExtendSelection(ms.X-d.editorAt.x, ms.Y-d.editorAt.y)
	default:
		a := d.area
		head := cell{max(a.x, min(ms.X, a.x+a.w-1)), max(a.y, min(ms.Y, a.y+a.h-1))}
		if head != d.anchor || d.moved {
			d.moved = true
			m.selection = &textSelection{a, d.anchor, head}
		}
	}
}

// mouseRelease ends a drag: it copies the selected text, or opens the link
// a click without a drag landed on.
func (m *Model) mouseRelease() tea.Cmd {
	d := m.drag
	m.drag = nil
	switch {
	case d == nil:
	case d.editor != nil:
		d.editor.EndSelection()
		return yankValue{"Selection", d.editor.SelectedText()}.copy(m)
	case d.moved && m.selection != nil:
		return yankValue{"Selection", m.selection.text(m.hits.frame)}.copy(m)
	case d.link != "":
		return openURL(d.link)
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
		if p := m.shownPicker(); p != nil {
			p.move(step)
		}
		return nil
	case overlayForm:
		if f := m.form; f != nil && !f.editing && !f.busy {
			f.cursor = max(0, min(f.cursor+step, len(f.spec.Fields)-1))
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
	case overlayForm:
		return m.clickForm(ms, double)
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

// shownPicker is the picker in the overlay box, as body chooses it.
func (m *Model) shownPicker() *picker {
	switch {
	case m.builder != nil:
		return m.builder.menu
	case m.composer != nil && m.composer.confirm != nil:
		return m.composer.confirm
	case m.form != nil:
		return m.form.picker
	}
	return m.modal
}

// pressKey runs the first key of a binding, as pressing it would.
func (m *Model) pressKey(scope, name string) tea.Cmd {
	if ks := keys.Keys(scope, name); len(ks) > 0 {
		return m.handleKey(keyPress(ks[0]))
	}
	return nil
}

// clickPicker highlights the clicked row, or chooses it on a double click.
func (m *Model) clickPicker(ms tea.Mouse, double bool) tea.Cmd {
	p, c := m.shownPicker(), m.hits.boxContent()
	x := ms.X - c.x
	if p == nil || x < 0 || x >= p.listWidth(m.hits.boxW) {
		return nil
	}
	i, ok := p.rowAt(p.listWidth(m.hits.boxW), m.hits.boxH, m.modalLook(), ms.Y-c.y)
	switch {
	case !ok:
	case double && i == p.cursor:
		return m.pressKey("picker", "choose")
	default:
		p.cursor = i
	}
	return nil
}

// clickForm moves to the clicked field, or edits it on a double click.
func (m *Model) clickForm(ms tea.Mouse, double bool) tea.Cmd {
	f, c := m.form, m.hits.boxContent()
	if f == nil || f.editing || f.busy || !c.contains(ms.X, ms.Y) {
		return nil
	}
	i, ok := f.fieldAt(ms.Y - c.y)
	switch {
	case !ok:
	case double && i == f.cursor:
		return m.pressKey("form", "edit")
	default:
		f.cursor = i
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
