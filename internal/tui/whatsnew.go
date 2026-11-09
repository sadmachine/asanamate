package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/updatecheck"
	"github.com/sadmachine/asanamate/internal/whatsnew"
)

// whatsNewModal lists release highlights by title beside the highlighted
// entry in full.
type whatsNewModal struct {
	list *picker
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

// openWhatsNew lists the titles of the entries in sections under their
// versions, newest first, beside the highlighted entry in full.
func (m *Model) openWhatsNew(sections []whatsnew.Section) {
	m.modal = nil
	var items []pickItem
	prev := ""
	for _, e := range whatsNewEntries(sections) {
		if e.version != prev {
			items = append(items, pickItem{Label: e.version, Header: true})
			prev = e.version
		}
		items = append(items, pickItem{Label: e.title, Search: e.version + " " + e.body, Value: e})
	}
	list := newPicker(func(pickResult) tea.Cmd { m.closeWhatsNew(); return nil }, "What's new in asanamate", items)
	list.listMaxWidth = 48
	list.closeHint = "got it"
	list.extraHints = [][2]string{{"d", "don't show on startup"}}
	list.preview = func(it *pickItem, width, height int) string {
		if it == nil {
			return ""
		}
		e := it.Value.(whatsNewEntry)
		out := m.accentStyle.Bold(true).Render(ansi.Wrap(e.title, width, "")) + "\n" +
			dimStyle.Render(e.version+" · "+e.heading) + "\n\n" +
			trimBlankLines(m.glamour(e.body, width, true))
		lines := strings.Split(out, "\n")
		return strings.Join(lines[:min(len(lines), height)], "\n")
	}
	m.whatsNew = &whatsNewModal{list: list}
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
	list := m.whatsNew.list
	if msg.String() == "d" && !list.searching {
		m.setWhatsNewOnStartup(false)
		m.closeWhatsNew()
		return nil
	}
	res, cmd := list.update(msg)
	if res.cancelled || res.done {
		m.closeWhatsNew()
	}
	return cmd
}

func (m *Model) whatsNewView(width, height int) string {
	var footer string
	if _, ok := updatecheck.ParseStable(m.deps.Version); ok && height > 8 {
		// GoReleaser injects the version without its tag's v.
		url := "https://github.com/sadmachine/asanamate/releases/tag/v" + strings.TrimPrefix(m.deps.Version, "v")
		footer = "\n" + dimStyle.Render(ansi.Truncate("Full notes: "+url, width, "…"))
	}
	return m.whatsNew.list.view(width, height-strings.Count(footer, "\n"), m.modalLook()) + footer
}

// whatsNewEntry is one highlight: its version, its New or Changed heading,
// its bold title, and the Markdown after the title.
type whatsNewEntry struct {
	version, heading, title, body string
}

// whatsNewEntries splits each section into its entries, in order. An
// unindented line starts an entry; indented ones continue it.
func whatsNewEntries(sections []whatsnew.Section) []whatsNewEntry {
	var entries []whatsNewEntry
	for _, s := range sections {
		heading := ""
		var entry strings.Builder
		flush := func() {
			if md := strings.TrimSpace(entry.String()); md != "" {
				title, body := splitWhatsNewEntry(md)
				entries = append(entries, whatsNewEntry{version: s.Version, heading: heading, title: title, body: body})
			}
			entry.Reset()
		}
		for _, line := range strings.Split(unwrapMarkdown(s.Body), "\n") {
			if h, ok := strings.CutPrefix(strings.TrimSpace(line), "### "); ok {
				flush()
				heading = h
				continue
			}
			if line != "" && line[0] != ' ' {
				flush()
			}
			entry.WriteString(strings.TrimPrefix(line, "  ") + "\n")
		}
		flush()
	}
	return entries
}

// splitWhatsNewEntry returns a list item's leading bold title as plain text,
// without its code marks or closing period, and the Markdown after it. An
// item with no bold title is all body, titled by its first line.
func splitWhatsNewEntry(md string) (title, body string) {
	for _, bullet := range []string{"- ", "* ", "+ "} {
		md = strings.TrimPrefix(md, bullet)
	}
	title, body = md, md
	if rest, ok := strings.CutPrefix(md, "**"); ok {
		if t, after, ok := strings.Cut(rest, "**"); ok {
			title, body = strings.TrimSuffix(strings.TrimSpace(t), "."), strings.TrimSpace(after)
		}
	}
	title, _, _ = strings.Cut(title, "\n")
	return strings.ReplaceAll(title, "`", ""), body
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
