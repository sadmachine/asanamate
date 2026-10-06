package tui

import (
	"cmp"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/keymap"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// binding is one [keys.main] binding: its catalog entry, its resolved keys,
// and its handler. splitOnly bindings need the reading pane.
type binding struct {
	keymap.Binding
	keys      []string
	splitOnly bool
	run       func(m *Model, msg tea.KeyPressMsg) tea.Cmd
}

// handler runs a [keys.main] binding.
type handler struct {
	splitOnly bool
	run       func(m *Model, msg tea.KeyPressMsg) tea.Cmd
}

// helpGroups are the key help's columns, in order.
var helpGroups = []string{"Move", "Ticket", "View"}

// keyBindings lists [keys.main] in key help order with their keys. Modals,
// the filter, and the views panel handle their own scopes first.
func keyBindings() []binding {
	h := handlers()
	var out []binding
	for _, s := range keymap.Catalog() {
		if s.Name != "main" {
			continue
		}
		for _, b := range s.Bindings {
			out = append(out, binding{Binding: b, keys: keys.Keys("main", b.Name), splitOnly: h[b.Name].splitOnly, run: h[b.Name].run})
		}
	}
	return out
}

// handlers runs each [keys.main] binding by name. A function, not a
// variable, because the handlers reach code that reads them.
func handlers() map[string]handler {
	// scroll moves the reader a line, whichever key the binding uses.
	scroll := func(m *Model, dir int) {
		if dir < 0 {
			m.reader.ScrollUp(1)
		} else {
			m.reader.ScrollDown(1)
		}
	}
	// Arrow keys scroll the reader when it has focus.
	move := func(dir int, to func(m *Model) int) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			if m.focusReader {
				scroll(m, dir)
				return nil
			}
			m.moveTo(to(m))
			return m.selectionChanged()
		}
	}
	cardMove := func(dir int, to func(m *Model) int) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			if m.focusReader {
				if m.readerView != config.ViewMarkdown {
					m.stepCardTarget(dir)
					return nil
				}
				scroll(m, dir)
				return nil
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
	do := func(fn func(m *Model)) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd { fn(m); return nil }
	}
	menu := func(open func(m *Model) func()) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			return m.requestMenu(func() tea.Cmd { open(m)(); return nil })
		}
	}
	// page moves the list by a share of the pane height, or scrolls the
	// reader by it when the reader has focus: div 1 is a page, 2 half one.
	page := func(div int, up bool) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			n := max(m.paneHeight()/div, 1)
			if m.focusReader {
				if up {
					m.reader.ScrollUp(n)
				} else {
					m.reader.ScrollDown(n)
				}
				return nil
			}
			if up {
				n = -n
			}
			m.moveTo(m.cursor + n)
			return m.selectionChanged()
		}
	}
	edit := func(op editOp) func(*Model, tea.KeyPressMsg) tea.Cmd {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			return m.requestMenu(func() tea.Cmd { return m.openEdit(op) })
		}
	}
	return map[string]handler{
		"down":           {run: cardMove(1, func(m *Model) int { return m.cursor + 1 })},
		"up":             {run: cardMove(-1, func(m *Model) int { return m.cursor - 1 })},
		"scroll_down":    {run: move(1, func(m *Model) int { return m.cursor + 1 })},
		"scroll_up":      {run: move(-1, func(m *Model) int { return m.cursor - 1 })},
		"top":            {run: jump(true, func(m *Model) int { return 0 })},
		"bottom":         {run: jump(false, func(m *Model) int { return len(m.visible) - 1 })},
		"half_page_down": {run: page(2, false)},
		"half_page_up":   {run: page(2, true)},
		"page_down":      {run: page(1, false)},
		"page_up":        {run: page(1, true)},
		"focus_views":    {splitOnly: true, run: do((*Model).focusViews)},
		"focus_list":     {run: do((*Model).focusList)},
		"focus_reader":   {splitOnly: true, run: do((*Model).focusReaderPane)},
		"next_pane":      {splitOnly: true, run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.cyclePane(false) }},
		"prev_pane":      {splitOnly: true, run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.cyclePane(true) }},
		"open":           {splitOnly: true, run: (*Model).open},
		"filter": {run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			m.filtering, m.filterBefore = true, m.filterInput.Value()
			m.reader.SetHeight(m.paneHeight())
			return m.filterInput.Focus()
		}},
		"projects": {run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			return m.requestProjects(func() tea.Cmd { m.openProjectPicker(); return nil })
		}},
		"history":       {splitOnly: true, run: do((*Model).openHistory)},
		"action":        {run: menu(func(m *Model) func() { return m.openActionMenu })},
		"repeat_action": {run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.requestMenu(m.repeatAction) }},
		"pin":           {run: (*Model).togglePin},
		"edit":          {run: menu(func(m *Model) func() { return m.openEditMenu })},
		"comment": {run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			return m.requestMenu(func() tea.Cmd { return m.openField(commentKey) })
		}},
		"due":          {run: edit(editDue)},
		"move_section": {run: edit(editSection)},
		"assign":       {run: edit(editAssignee)},
		"log_time":     {run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.requestMenu(m.openTime) }},
		"attachments":  {run: menu(func(m *Model) func() { return m.openAttachments })},
		"open_browser": {run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			if t, ok := m.selected(); ok {
				return openURL(t.PermalinkURL)
			}
			return nil
		}},
		"copy_link": {run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
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
		"copy_comment": {splitOnly: true, run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
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
		"reload":           {run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.refreshWithImages() }},
		"reader_view":      {splitOnly: true, run: do(func(m *Model) { m.toggleReaderView(); m.saveDisplay() })},
		"saved_views":      {run: do((*Model).openSavedViews)},
		"save_view":        {run: do((*Model).openSaveView)},
		"next_saved_view":  {run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.cycleSavedView(1) }},
		"prev_saved_view":  {run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.cycleSavedView(-1) }},
		"refresh_interval": {run: do((*Model).openRefreshInterval)},
		"repos":            {run: func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.requestProjects(m.openLinks) }},
		"action_builder":   {run: do((*Model).openActionBuilder)},
		"settings":         {run: do(func(m *Model) { m.openSettings(0) })},
		"group_by":         {run: do((*Model).openGroupPicker)},
		"sort_by":          {run: do((*Model).openSortPicker)},
		"fit_list":         {run: do((*Model).fitList)},
		"help":             {run: do(func(m *Model) { m.help = true })},
		"quit":             {run: func(*Model, tea.KeyPressMsg) tea.Cmd { return tea.Quit }},
	}
}

// open focuses the reader from the list. In the reader, it edits the
// targeted row or shows the empty fields.
func (m *Model) open(tea.KeyPressMsg) tea.Cmd {
	if !m.focusReader {
		m.focusReaderPane()
		return nil
	}
	switch {
	case m.fieldKey == emptyFieldsKey:
		m.showEmptyFields()
	case m.fieldKey != "" && m.editableTarget():
		return m.openField(m.fieldKey)
	}
	return nil
}

// available reports whether b runs in the current layout.
func (m *Model) available(b binding) bool {
	return len(b.keys) > 0 && (!b.splitOnly || !m.deps.NoPreview) &&
		(b.Name != "log_time" || m.deps.Config.TimeTrackingEnabled())
}

// bindingFor returns the binding k runs in the current layout.
func (m *Model) bindingFor(k string) (binding, bool) {
	name := keys.Name("main", k)
	for _, b := range keyBindings() {
		if b.Name == name && m.available(b) {
			return b, true
		}
	}
	return binding{}, false
}

// keyHints are the statusline hints for the named bindings, in that order.
func (m *Model) keyHints(names ...string) [][2]string {
	all := keyBindings()
	var hints [][2]string
	for _, name := range names {
		i := slices.IndexFunc(all, func(b binding) bool { return b.Name == name })
		if i >= 0 && m.available(all[i]) {
			hints = append(hints, [2]string{keymap.Label(all[i].keys[0]), cmp.Or(all[i].Hint, all[i].Desc)})
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

// cyclePane moves focus to the next pane: the views panel when it shows, the
// list, then the reader; back goes the other way.
func (m *Model) cyclePane(back bool) tea.Cmd {
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
	if back {
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
