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
	markdown string
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
	md := whatsnew.Markdown(sections)
	if md == "" {
		md = "No highlights for this version."
	}
	m.modal = nil
	m.whatsNew = &whatsNewModal{markdown: md, reader: viewport.New()}
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
	hints := keyLabel("picker", "cancel") + " got it  d don't show on startup  " + keyLabel("picker", "down", "up") + " scroll"
	footer := []string{hints}
	if _, ok := updatecheck.ParseStable(m.deps.Version); ok {
		// GoReleaser injects the version without its tag's v.
		footer = append(footer, "Full notes: https://github.com/sadmachine/asanamate/releases/tag/v"+strings.TrimPrefix(m.deps.Version, "v"))
	}
	// Keep at least one body line: drop the notes link, then the blank
	// line under the title, when the pane is short.
	gap := "\n"
	if height < 3+len(footer) {
		footer = footer[:1]
	}
	if height < 4+len(footer) {
		gap = ""
	}
	for i, line := range footer {
		footer[i] = ansi.Truncate(line, width, "…")
	}
	header := m.accentStyle.Render(ansi.Truncate("What's new in asanamate", width, "…"))
	body := w.markdown
	if out, err := newMarkdownRenderer(m.palette, width, false, m.sym.codeWrap).Render(w.markdown); err == nil {
		body = strings.Trim(out, "\n")
	}
	w.reader.SetWidth(width)
	w.reader.SetHeight(max(height-1-len(footer)-strings.Count(gap, "\n"), 1))
	w.reader.SetContent(body)
	return header + "\n" + gap + w.reader.View() + "\n" + strings.Join(footer, "\n")
}
