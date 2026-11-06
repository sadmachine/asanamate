package tui

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/updatecheck"
	"github.com/sadmachine/asanamate/internal/whatsnew"
)

// whatsNewModal shows release highlights in a scrollable reader.
type whatsNewModal struct {
	sections []whatsnew.Section
	reader   viewport.Model
}

// startupWhatsNew opens What's new on the first launch of a newer release, or
// records the release silently when there is nothing to show or the user
// turned it off. A first run records the release without showing anything.
func (m *Model) startupWhatsNew() {
	v, st := m.deps.Version, m.deps.State
	if !whatsnew.Newer(v, st.LastSeenVersion) {
		return
	}
	var sections []whatsnew.Section
	if !st.Fresh() {
		sections = whatsnew.Since(m.deps.WhatsNew, v, st.LastSeenVersion)
	}
	if m.deps.Config.WhatsNew.OnStartup && len(sections) > 0 {
		m.openWhatsNew(sections)
		return
	}
	m.recordWhatsNewSeen()
}

// openWhatsNew shows sections, newest first.
func (m *Model) openWhatsNew(sections []whatsnew.Section) {
	m.modal = nil
	m.whatsNew = &whatsNewModal{sections: sections, reader: viewport.New()}
}

// closeWhatsNew closes the modal and marks the running release as seen.
func (m *Model) closeWhatsNew() {
	m.whatsNew = nil
	if whatsnew.Newer(m.deps.Version, m.deps.State.LastSeenVersion) {
		m.recordWhatsNewSeen()
	}
}

// recordWhatsNewSeen saves the running release as the last one offered.
func (m *Model) recordWhatsNewSeen() {
	old := m.deps.State.LastSeenVersion
	m.deps.State.LastSeenVersion = m.deps.Version
	if err := m.deps.State.Save(); err != nil {
		m.deps.State.LastSeenVersion = old
		m.setStatus(statusError, "Saving state: "+err.Error())
	}
}

// setWhatsNewOnStartup changes whats_new.on_startup for this session and
// saves it.
func (m *Model) setWhatsNewOnStartup(on bool) {
	m.deps.Config.WhatsNew.OnStartup = on
	m.saveSetting("whats_new.on_startup", on)
}

func (m *Model) updateWhatsNew(msg tea.KeyPressMsg) tea.Cmd {
	w := m.whatsNew
	if msg.String() == "d" {
		m.setWhatsNewOnStartup(false)
		m.closeWhatsNew()
		return nil
	}
	switch bound("picker", msg, false) {
	case "cancel", "choose":
		m.closeWhatsNew()
	case "down":
		w.reader.ScrollDown(1)
	case "up":
		w.reader.ScrollUp(1)
	case "top":
		w.reader.GotoTop()
	case "bottom":
		w.reader.GotoBottom()
	default:
		w.reader, _ = w.reader.Update(msg)
	}
	return nil
}

func (m *Model) whatsNewView(width, height int) string {
	w := m.whatsNew
	footer := strings.Split(wrapHints(m.accentStyle, width,
		[2]string{keyLabel("picker", "cancel"), "got it"},
		[2]string{"d", "don't show on startup"},
		[2]string{keyLabel("picker", "down", "up"), "scroll"},
	), "\n")
	if _, ok := updatecheck.ParseStable(m.deps.Version); ok {
		// GoReleaser injects the version without its tag's v.
		url := "https://github.com/sadmachine/asanamate/releases/tag/v" + strings.TrimPrefix(m.deps.Version, "v")
		footer = append(footer, dimStyle.Render(ansi.Truncate("Full notes: "+url, width, "…")))
	}
	// Keep at least one body line: drop the notes link, then the blank
	// lines around the body, when the pane is short.
	gap := "\n"
	if height < 3+len(footer) {
		footer = footer[:1]
	}
	if height < 5+len(footer) {
		gap = ""
	}
	header := m.accentStyle.Bold(true).Render(ansi.Truncate("What's new in asanamate", width, "…"))
	body := m.whatsNewBody(w.sections, width)
	room := height - 2 - len(footer) - 2*strings.Count(gap, "\n")
	w.reader.SetWidth(width)
	w.reader.SetHeight(max(min(strings.Count(body, "\n")+1, room), 1))
	w.reader.SetContent(body)
	return header + "\n" + gap + w.reader.View() + "\n" + gap + strings.Join(footer, "\n")
}

// whatsNewBody lays out each version under a rule, then its New and Changed
// labels, each over its entries, reflowed to width and spaced apart.
func (m *Model) whatsNewBody(sections []whatsnew.Section, width int) string {
	if len(sections) == 0 {
		return "No highlights for this version."
	}
	label := lipStyle(m.palette.Colors.Markdown.Heading).Bold(true)
	var blocks []string
	for _, s := range sections {
		blocks = append(blocks, m.styledRule(m.accentStyle.Bold(true).Render(s.Version), width))
		heading, entries := "", []string{}
		var entry strings.Builder
		flushEntry := func() {
			if md := strings.TrimSpace(entry.String()); md != "" {
				entries = append(entries, trimBlankLines(m.glamour(md, width, true)))
			}
			entry.Reset()
		}
		flushGroup := func() {
			flushEntry()
			if len(entries) > 0 {
				out := strings.Join(entries, "\n\n")
				if heading != "" {
					out = label.Render(heading) + "\n" + out
				}
				blocks = append(blocks, out)
			}
			heading, entries = "", nil
		}
		for _, line := range strings.Split(unwrapMarkdown(s.Body), "\n") {
			if h, ok := strings.CutPrefix(strings.TrimSpace(line), "### "); ok {
				flushGroup()
				heading = h
				continue
			}
			// An unindented line starts an entry; indented ones continue it.
			if line != "" && line[0] != ' ' {
				flushEntry()
			}
			entry.WriteString(line + "\n")
		}
		flushGroup()
	}
	return strings.Join(blocks, "\n\n")
}

// trimBlankLines drops the leading and trailing lines that show nothing,
// such as the padded blank rows the Markdown renderer adds around lists.
func trimBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	blank := func(line string) bool { return strings.TrimSpace(ansi.Strip(line)) == "" }
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// unwrapMarkdown joins hard-wrapped paragraph and list item lines, so the
// renderer, which keeps every source line break, can reflow them to width.
// Lines that start a block and fenced code stay as written.
func unwrapMarkdown(md string) string {
	var out []string
	fenced, joinable := false, false
	for line := range strings.Lines(md) {
		line = strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			fenced = !fenced
		}
		if !fenced && joinable && trimmed != "" && !startsBlock(trimmed) {
			out[len(out)-1] += " " + trimmed
			continue
		}
		out = append(out, line)
		joinable = !fenced && trimmed != "" && !strings.HasPrefix(trimmed, "```") && !strings.HasPrefix(trimmed, "#")
	}
	return strings.Join(out, "\n")
}

// startsBlock reports whether a trimmed Markdown line opens a new block.
func startsBlock(line string) bool {
	for _, p := range []string{"- ", "* ", "+ ", "#", ">", "```", "|"} {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	digits := len(line) - len(strings.TrimLeft(line, "0123456789"))
	return digits > 0 && (strings.HasPrefix(line[digits:], ". ") || strings.HasPrefix(line[digits:], ") "))
}
