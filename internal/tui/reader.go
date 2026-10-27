package tui

import (
	"slices"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/kitty"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// readerState is the reading pane: the shown ticket, loaded details and
// images, the selected cards view target, and the attachment viewer.
type readerState struct {
	reader         viewport.Model
	readerView     string // config.ViewCards or config.ViewMarkdown
	renderers      map[rendererKey]*markdownRenderer
	details        map[string]ticket.Ticket
	images         map[string]*inlineImage // inline images by attachment gid
	imageEpoch     uint64                  // rejects image results from before a manual refresh
	imageSeq       int                     // inline image id sequence, from a random start
	imageCell      kitty.CellSize          // cell size reported by the outer terminal
	viewImages     []asana.Attachment      // images the attachment viewer steps through
	viewIndex      int                     // viewImages index of the image in the viewer
	shownGID       string
	draftSaved     bool              // the selected ticket has a saved comment draft
	shownWidth     int               // text width shownGID was rendered at
	shownAgents    string            // agents section rendered for shownGID
	fieldKey       string            // selected cards view target; "" for none
	showEmpty      bool              // the cards view lists empty custom fields; resets per ticket
	showOthers     bool              // Agents lists agents on other branches; resets per ticket
	allActivity    bool              // the cards view shows the All tab, every story, not only comments; kept across tickets
	showComments   bool              // the cards view lists every comment or story, not only the newest; resets per ticket
	openComments   map[string]bool   // comments shown in full instead of a preview, by target key; resets per ticket
	clipped        map[string]bool   // comments the last render cut to a preview, by target key
	fieldLines     map[string]int    // reader line of each cards view target, by key
	fieldEnds      map[string]int    // last reader line of multi-line targets, by key
	cardTargets    []string          // cards view targets in reading order
	flashKey       string            // section the jump menu just picked, drawn in the accent color; "" for none
	flashGID       string            // ticket flashKey belongs to
	flashSeq       uint64            // identifies the newest flash, so older timers leave it
	flashLines     [2]int            // first and last reader line of a flashed open section; empty when first > last
	detailSeq      uint64            // identifies the newest detail request per ticket
	detailRequests map[string]uint64 // in-flight detail requests by ticket gid
}

// renderDetail renders the selected ticket in the reader, from the top or at
// the current scroll position.
func (m *Model) renderDetail(keepScroll bool) {
	t, ok := m.selectedDetail()
	if !ok {
		return
	}
	m.markSeen(t.Task)
	m.checkDraft()
	if !m.readerOn() {
		m.shownGID = t.GID
		return
	}
	_, readerW, _ := m.paneWidths()
	section := m.agentsSection(t.Task)
	offset := m.reader.YOffset()
	anchor, anchorLine := "", -1
	selectedIndex := slices.Index(m.cardTargets, m.fieldKey)
	if keepScroll && m.readerView == config.ViewCards {
		// Keep the content at the viewport's top in place even when earlier
		// fields change height. Prefer a selected target that is on screen.
		for _, key := range m.cardTargets {
			line := m.fieldLines[key]
			if line <= offset && line > anchorLine {
				anchor, anchorLine = key, line
			}
		}
		if line, ok := m.fieldLines[m.fieldKey]; ok && line < offset+m.reader.Height() && max(line, m.fieldEnds[m.fieldKey]) >= offset {
			anchor, anchorLine = m.fieldKey, line
		}
	}
	width := max(readerW-2, 20)
	var content string
	if m.readerView == config.ViewMarkdown {
		extra := section
		if value := m.timeSummaryValue(t.GID); value != "" {
			extra += "\n" + m.sym.icon(iconClock) + "**Time tracked:** " + value + "\n\n"
		}
		if path, own := m.effectiveRepo(t.Task); path != "" {
			repo := "`" + path + "`"
			if own {
				repo += " (this ticket only)"
			}
			extra = "\n## Local repo\n\n" + repo + "\n" + extra
		}
		t.HTMLNotes = mentionHTML(t.HTMLNotes)
		t.Comments = slices.Clone(t.Comments)
		for i := range t.Comments {
			t.Comments[i].HTMLText = mentionHTML(t.Comments[i].HTMLText)
		}
		content = m.glamour(t.MarkdownWith(extra), width, false)
	} else {
		content = m.flashGutter(lipgloss.NewStyle().PaddingLeft(1).Render(m.renderCards(t, width)))
		if keepScroll && selectedIndex >= 0 && !slices.Contains(m.cardTargets, m.fieldKey) {
			m.fieldKey = m.cardTargets[min(selectedIndex, len(m.cardTargets)-1)]
			content = m.flashGutter(lipgloss.NewStyle().PaddingLeft(1).Render(m.renderCards(t, width)))
		}
	}
	lines := m.reader.TotalLineCount()
	if content != m.reader.GetContent() {
		m.reader.SetContent(content)
	}
	if keepScroll {
		if line, ok := m.fieldLines[anchor]; anchor != "" && ok {
			offset += line - anchorLine
		}
		// Markdown has no targets to anchor on; a new width keeps the same
		// share of the ticket above the viewport.
		if m.readerView == config.ViewMarkdown && width != m.shownWidth && lines > 0 {
			offset = offset * m.reader.TotalLineCount() / lines
		}
		m.reader.SetYOffset(offset)
	} else {
		m.reader.GotoTop()
	}
	m.shownGID, m.shownAgents, m.shownWidth = t.GID, section, width
}

// stepField moves the cards view's tabbed-to row by dir, wrapping, and
// scrolls it into view. It reports false when there is no row to move to.
func (m *Model) stepField(dir int) bool {
	t, ok := m.selectedDetail()
	if !ok || m.readerView == config.ViewMarkdown {
		return false
	}
	keys := m.fieldTargets(t)
	i := slices.Index(keys, m.fieldKey)
	switch {
	case i < 0 && dir < 0:
		i = len(keys) - 1
	case i < 0:
		i = 0
	default:
		i = (i + dir + len(keys)) % len(keys)
	}
	m.fieldKey = keys[i]
	m.showTarget()
	return true
}

// stepCardTarget moves through fields, section headings, and comments.
func (m *Model) stepCardTarget(dir int) bool {
	if _, ok := m.selectedDetail(); !ok || m.readerView == config.ViewMarkdown || len(m.cardTargets) == 0 {
		return false
	}
	i := slices.Index(m.cardTargets, m.fieldKey)
	switch {
	case i < 0 && dir < 0:
		i = len(m.cardTargets) - 1
	case i < 0:
		i = 0
	default:
		i = (i + dir + len(m.cardTargets)) % len(m.cardTargets)
	}
	m.fieldKey = m.cardTargets[i]
	m.showTarget()
	return true
}

// jumpReader scrolls the reader to its top or bottom, selecting the cards
// view's first or last target.
func (m *Model) jumpReader(top bool) {
	if _, ok := m.selectedDetail(); ok && m.readerView != config.ViewMarkdown && len(m.cardTargets) > 0 {
		m.fieldKey = m.cardTargets[len(m.cardTargets)-1]
		if top {
			m.fieldKey = m.cardTargets[0]
		}
		m.renderDetail(true)
	}
	if top {
		m.reader.GotoTop()
	} else {
		m.reader.GotoBottom()
	}
}

func (m *Model) editableTarget() bool {
	t, ok := m.selectedDetail()
	return ok && m.readerView != config.ViewMarkdown && slices.Contains(m.fieldTargets(t), m.fieldKey)
}

// showTarget redraws the highlight and brings the whole target into view, or
// its first line when it is taller than the reader. A target that fits on
// screen with the ticket's top scrolls to the top, so the head stays reachable.
func (m *Model) showTarget() {
	m.renderDetail(true)
	line, top, h := m.fieldLines[m.fieldKey], m.reader.YOffset(), m.reader.Height()
	last := max(m.fieldEnds[m.fieldKey], line)
	switch {
	case line < top && last < h:
		m.reader.GotoTop()
	case line < top:
		m.reader.SetYOffset(line)
	case last >= top+h:
		m.reader.SetYOffset(min(line, last-h+1))
	}
}

// rendererKey identifies a cached Markdown renderer. Bare renderers drop the
// document margins, for Markdown set inside cards and sections.
type rendererKey struct {
	width int
	bare  bool
}

func (m *Model) glamour(md string, width int, bare bool) string {
	key := rendererKey{width, bare}
	r := m.renderers[key]
	if r == nil {
		r = newMarkdownRenderer(m.palette, width, bare, m.sym.codeWrap)
		m.renderers[key] = r
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return out
}
