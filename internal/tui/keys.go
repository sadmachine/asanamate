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
	// Arrow keys scroll the reader when it has focus.
	move := func(to func(m *Model) int) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			if m.focusReader {
				return m.scrollReader(msg)
			}
			m.moveTo(to(m))
			return m.selectionChanged()
		}
	}
	cardMove := func(dir int, to func(m *Model) int) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			if m.focusReader {
				if m.readerView != config.ViewMarkdown {
					m.stepCardTarget(dir)
					return nil
				}
				return m.scrollReader(msg)
			}
			m.moveTo(to(m))
			return m.selectionChanged()
		}
	}
	// Home and end jump the reader too, when it has focus.
	jump := func(top bool, to func(m *Model) int) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			if m.focusReader {
				m.jumpReader(top)
				return nil
			}
			m.moveTo(to(m))
			return m.selectionChanged()
		}
	}
	// page moves the list by a share of the pane height, or scrolls the
	// reader by it when the reader has focus: div 1 is a page, 2 half one.
	// The up keys go up, the binding's other keys down.
	page := func(div int, up ...string) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			n := max(m.paneHeight()/div, 1)
			if m.focusReader {
				if slices.Contains(up, msg.String()) {
					m.reader.ScrollUp(n)
				} else {
					m.reader.ScrollDown(n)
				}
				return nil
			}
			if slices.Contains(up, msg.String()) {
				n = -n
			}
			m.moveTo(m.cursor + n)
			return m.selectionChanged()
		}
	}
	do := func(fn func(m *Model)) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd { fn(m); return nil }
	}
	menu := func(open func(m *Model) func()) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			return m.requestMenu(func() tea.Cmd { open(m)(); return nil })
		}
	}
	return []binding{
		{keys: []string{"j"}, desc: "down / next card target", group: "Move", run: cardMove(1, func(m *Model) int { return m.cursor + 1 })},
		{keys: []string{"k"}, desc: "up / previous card target", group: "Move", run: cardMove(-1, func(m *Model) int { return m.cursor - 1 })},
		{keys: []string{"down"}, desc: "down / scroll reader", group: "Move", run: move(func(m *Model) int { return m.cursor + 1 })},
		{keys: []string{"up"}, desc: "up / scroll reader", group: "Move", run: move(func(m *Model) int { return m.cursor - 1 })},
		{keys: []string{"g", "home"}, desc: "top", group: "Move", run: jump(true, func(m *Model) int { return 0 })},
		{keys: []string{"G", "end"}, desc: "end", group: "Move", run: jump(false, func(m *Model) int { return len(m.visible) - 1 })},
		{keys: []string{"ctrl+d", "ctrl+u"}, label: "ctrl+d/u", desc: "half page down / up", group: "Move", run: page(2, "ctrl+u")},
		{keys: []string{"ctrl+f", "ctrl+b", "pgdown", "pgup"}, label: "ctrl+f/b", desc: "page down / up", group: "Move", run: page(1, "ctrl+b", "pgup")},
		{keys: []string{"0"}, desc: "views panel", group: "Move", splitOnly: true, run: do((*Model).focusViews)},
		{keys: []string{"1", "esc"}, label: "1/esc", desc: "list", group: "Move", run: do((*Model).focusList)},
		{keys: []string{"2"}, desc: "reader", group: "Move", splitOnly: true, run: do((*Model).focusReaderPane)},
		{keys: []string{"tab", "shift+tab"}, desc: "next / previous pane", group: "Move", splitOnly: true, run: (*Model).tab},
		{keys: []string{"/"}, desc: "filter", hint: "filter", group: "Move", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			m.filtering = true
			m.reader.SetHeight(m.paneHeight())
			return m.filterInput.Focus()
		}},
		{keys: []string{"p"}, desc: "projects", hint: "proj", group: "Move", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			return m.requestProjects(func() tea.Cmd { m.openProjectPicker(); return nil })
		}},
		{keys: []string{"H"}, desc: "ticket history", group: "Move", splitOnly: true, run: do((*Model).openHistory)},
		{keys: []string{"space", "a"}, label: "space/a", desc: "run action", hint: "act", group: "Ticket", run: menu(func(m *Model) func() { return m.openActionMenu })},
		{keys: []string{"P"}, desc: "pin / unpin", group: "Ticket", run: (*Model).togglePin},
		{keys: []string{"e"}, desc: "edit", hint: "edit", group: "Ticket", run: menu(func(m *Model) func() { return m.openEditMenu })},
		{keys: []string{"C"}, desc: "add comment", group: "Ticket", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			return m.requestMenu(func() tea.Cmd { return m.openField(commentKey) })
		}},
		{keys: []string{"."}, desc: "repeat last action", group: "Ticket", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.requestMenu(m.repeatAction) }},
		{keys: []string{"d"}, desc: "set due date", group: "Ticket", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			return m.requestMenu(func() tea.Cmd { return m.openEdit(editDue) })
		}},
		{keys: []string{"m"}, desc: "move to section", group: "Ticket", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			return m.requestMenu(func() tea.Cmd { return m.openEdit(editSection) })
		}},
		{keys: []string{"A"}, desc: "assign", group: "Ticket", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			return m.requestMenu(func() tea.Cmd { return m.openEdit(editAssignee) })
		}},
		{keys: []string{"t"}, desc: "log time", hint: "time", group: "Ticket", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.requestMenu(m.openTime) }},
		{keys: []string{"f"}, desc: "attachments", group: "Ticket", run: menu(func(m *Model) func() { return m.openAttachments })},
		{keys: []string{"o"}, desc: "open in browser", group: "Ticket", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			if t, ok := m.selected(); ok {
				return openURL(t.PermalinkURL)
			}
			return nil
		}},
		{keys: []string{"c"}, desc: "copy link", hint: "copy link", group: "Ticket", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			if t, ok := m.selected(); ok {
				if t.PermalinkURL == "" {
					m.status = "ticket has no link"
					return nil
				}
				m.status = "link sent to clipboard"
				return tea.SetClipboard(t.PermalinkURL)
			}
			return nil
		}},
		{keys: []string{"y"}, desc: "copy targeted comment", hint: "copy comment", group: "Ticket", splitOnly: true, run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			if !m.focusReader || m.readerView == config.ViewMarkdown {
				return nil
			}
			if t, ok := m.selectedDetail(); ok {
				if c := m.selectedComment(t); c != nil {
					m.status = "comment sent to clipboard"
					return tea.SetClipboard(ticket.HTMLToMarkdown(c.HTMLText))
				}
			}
			return nil
		}},
		{keys: []string{"r"}, desc: "reload / clear image cache", group: "Ticket", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			return m.refreshWithImages()
		}},
		{keys: []string{"v"}, desc: "cards / markdown", group: "View", splitOnly: true, run: do(func(m *Model) { m.toggleReaderView(); m.saveDisplay() })},
		{keys: []string{"V"}, desc: "saved views", group: "View", run: do((*Model).openSavedViews)},
		{keys: []string{"ctrl+s"}, desc: "save current view", group: "View", run: do((*Model).openSaveView)},
		{keys: []string{"R"}, desc: "auto-update interval", group: "View", run: do((*Model).openRefreshInterval)},
		{keys: []string{"L"}, desc: "project repos / ticket repo override", group: "View", run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.requestProjects(m.openLinks) }},
		{keys: []string{"]", "["}, label: "]/[", desc: "next / previous saved view", group: "View", run: func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			if msg.String() == "[" {
				return m.cycleSavedView(-1)
			}
			return m.cycleSavedView(1)
		}},
		{keys: []string{"ctrl+a"}, desc: "build / edit actions", group: "View", run: do((*Model).openActionBuilder)},
		{keys: []string{"s"}, desc: "settings", group: "View", run: do(func(m *Model) { m.openSettings(0) })},
		{keys: []string{"b"}, desc: "group by", group: "View", run: do((*Model).openGroupPicker)},
		{keys: []string{"B"}, desc: "sort by", group: "View", run: do((*Model).openSortPicker)},
		{keys: []string{"="}, desc: "fit list", group: "View", run: do((*Model).fitList)},
		{keys: []string{"?"}, desc: "this help", hint: "keys", group: "View", run: do(func(m *Model) { m.help = true })},
		{keys: []string{"q"}, desc: "quit", group: "View", run: func(*Model, tea.KeyPressMsg) tea.Cmd { return tea.Quit }},
	}
}

// bindingFor returns the binding k runs in the current layout.
func (m *Model) bindingFor(k string) (binding, bool) {
	for _, b := range keyBindings() {
		if slices.Contains(b.keys, k) && (!b.splitOnly || !m.deps.NoPreview) && (b.keys[0] != "t" || m.deps.Config.TimeTrackingEnabled()) {
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

// focusReaderPane gives the reader focus, on its first editable row, and
// counts the selected ticket as viewed.
func (m *Model) focusReaderPane() {
	m.focusNav = false
	if !m.focusReader {
		m.focusReader = true
		m.stepField(1)
		m.recordViewed()
	}
}

// tab moves focus to the next pane: the views panel when it shows, the list,
// then the reader; shift+tab goes backwards.
func (m *Model) tab(msg tea.KeyPressMsg) tea.Cmd {
	panes := []func(){m.focusList, m.focusReaderPane}
	if m.showNav() {
		panes = append([]func(){m.focusViews}, panes...)
	}
	i := len(panes) - 2
	switch {
	case m.focusNav:
		i = 0
	case m.focusReader:
		i = len(panes) - 1
	}
	dir := 1
	if msg.String() == "shift+tab" {
		dir = -1
	}
	panes[(i+dir+len(panes))%len(panes)]()
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
