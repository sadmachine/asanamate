package tui

import (
	"cmp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// binding is one key of the list and reader: the keys that run it, the label
// the key help shows for them (the first key when empty), what it does in
// the help and, shortened, in the statusline hints, and the help column it
// sits in. splitOnly bindings need the reading pane.
type binding struct {
	keys              []string
	label             string
	desc, hint, group string
	splitOnly         bool
	run               func(m *Model, msg tea.KeyPressMsg) tea.Cmd
}

// helpGroups are the key help's columns, in order.
var helpGroups = []string{"Move", "Ticket", "View"}

// keyBindings lists every key of the list and reader, in key help order.
// Modals, the filter, the views panel, and the cards view's selected row
// handle their own keys first. A function, not a variable, because the
// bindings reach code that reads them.
func keyBindings() []binding {
	// Moves scroll the reader when it has focus.
	move := func(to func(m *Model) int) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			if m.focusReader {
				return m.scrollReader(msg)
			}
			m.moveTo(to(m))
			return m.selectionChanged()
		}
	}
	do := func(fn func(m *Model)) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd { fn(m); return nil }
	}
	menu := func(open func(m *Model) func()) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.requestMenu(open(m)) }
	}
	return []binding{
		{keys: []string{"j", "down"}, desc: "down", group: "Move", run: move(func(m *Model) int { return m.cursor + 1 })},
		{keys: []string{"k", "up"}, desc: "up", group: "Move", run: move(func(m *Model) int { return m.cursor - 1 })},
		{keys: []string{"g", "home"}, desc: "top", group: "Move", run: move(func(m *Model) int { return 0 })},
		{keys: []string{"G", "end"}, desc: "end", group: "Move", run: move(func(m *Model) int { return len(m.visible) - 1 })},
		{keys: []string{"0"}, desc: "views panel", group: "Move", splitOnly: true, run: do((*Model).focusViews)},
		{keys: []string{"1", "esc"}, label: "1/esc", desc: "list", group: "Move", run: do((*Model).focusList)},
		{keys: []string{"2"}, desc: "reader", group: "Move", splitOnly: true, run: do((*Model).focusReaderPane)},
		{keys: []string{"tab", "shift+tab"}, desc: "reader / next field", group: "Move", splitOnly: true, run: (*Model).tab},
		{keys: []string{"/"}, desc: "filter", hint: "filter", group: "Move", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			m.filtering = true
			return m.filterInput.Focus()
		}},
		{keys: []string{"p"}, desc: "projects", hint: "proj", group: "Move", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.requestProjects(func() tea.Cmd { m.openProjectPicker(); return nil }) }},
		{keys: []string{"enter", "a"}, desc: "run action", hint: "act", group: "Ticket", run: menu(func(m *Model) func() { return m.openActionMenu })},
		{keys: []string{"e"}, desc: "edit", hint: "edit", group: "Ticket", run: menu(func(m *Model) func() { return m.openEditMenu })},
		{keys: []string{"f"}, desc: "attachments", group: "Ticket", run: menu(func(m *Model) func() { return m.openAttachments })},
		{keys: []string{"o"}, desc: "open in browser", group: "Ticket", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			if t, ok := m.selected(); ok {
				return openURL(t.PermalinkURL)
			}
			return nil
		}},
		{keys: []string{"r"}, desc: "reload", group: "Ticket", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			m.details = map[string]ticket.Ticket{}
			m.shownGID = ""
			return m.reload()
		}},
		{keys: []string{"v"}, desc: "cards / markdown", group: "View", splitOnly: true, run: do((*Model).toggleReaderView)},
		{keys: []string{"L"}, desc: "repo links", group: "View", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.requestProjects(m.openLinks) }},
		{keys: []string{"s"}, desc: "separators", group: "View", run: do(func(m *Model) { m.separator = !m.separator })},
		{keys: []string{"S"}, desc: "header spacing", group: "View", run: do(func(m *Model) { m.spacing = !m.spacing })},
		{keys: []string{"b"}, desc: "group by", group: "View", run: do((*Model).openGroupPicker)},
		{keys: []string{"="}, desc: "fit list", group: "View", run: do((*Model).fitList)},
		{keys: []string{"?"}, desc: "this help", hint: "keys", group: "View", run: do(func(m *Model) { m.help = true })},
		{keys: []string{"q"}, desc: "quit", group: "View", run: func(*Model, tea.KeyPressMsg) tea.Cmd { return tea.Quit }},
	}
}

// bindingFor returns the binding k runs in the current layout.
func (m *Model) bindingFor(k string) (binding, bool) {
	for _, b := range keyBindings() {
		if slices.Contains(b.keys, k) && (!b.splitOnly || !m.deps.NoPreview) {
			return b, true
		}
	}
	return binding{}, false
}

// keyHints are the statusline hints for keys, from their bindings.
func (m *Model) keyHints(keys ...string) [][2]string {
	var hints [][2]string
	for _, k := range keys {
		if b, ok := m.bindingFor(k); ok {
			hints = append(hints, [2]string{b.keys[0], cmp.Or(b.hint, b.desc)})
		}
	}
	return hints
}

// scrollReader passes a key to the reading pane's viewport.
func (m *Model) scrollReader(msg tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	m.reader, cmd = m.reader.Update(msg)
	return cmd
}

// focusList gives the list focus.
func (m *Model) focusList() {
	m.focusNav = false
	if m.focusReader {
		m.focusReader = false
		m.clearField()
	}
}

// focusViews gives the views panel focus, when it shows.
func (m *Model) focusViews() {
	if m.showNav() {
		m.focusList()
		m.focusNavPanel()
	}
}

// focusReaderPane gives the reader focus, on its first editable row.
func (m *Model) focusReaderPane() {
	m.focusNav = false
	if !m.focusReader {
		m.focusReader = true
		m.stepField(1)
	}
}

// tab moves from the views panel to the list, from the list into the reader,
// and through the reader's editable rows back to the list; shift+tab goes
// through the rows backwards.
func (m *Model) tab(msg tea.KeyPressMsg) tea.Cmd {
	dir := 1
	if msg.String() == "shift+tab" {
		dir = -1
	}
	switch {
	case m.focusNav:
		m.focusNav = false
	case !m.focusReader:
		m.focusReader = true
		m.stepField(dir)
	case !m.stepField(dir):
		m.focusReader = false
	}
	return nil
}

// toggleReaderView switches the reader between the cards and markdown views.
func (m *Model) toggleReaderView() {
	if m.readerView == config.ViewCards {
		m.readerView = config.ViewMarkdown
	} else {
		m.readerView = config.ViewCards
	}
	m.fieldKey = ""
	m.renderDetail(false)
}

// requestProjects runs open, loading the projects first when they have not
// loaded yet.
func (m *Model) requestProjects(open func() tea.Cmd) tea.Cmd {
	if m.projects != nil {
		return open()
	}
	m.afterProjects = open
	m.status = "loading projects…"
	if m.loadingProjects {
		return nil
	}
	m.loadingProjects = true
	return loadProjects(m.deps.Client, m.deps.Config.Workspace)
}

// helpLabel is how the key help shows b's keys.
func (b binding) helpLabel() string {
	if b.label != "" {
		return b.label
	}
	return strings.TrimSpace(b.keys[0])
}
