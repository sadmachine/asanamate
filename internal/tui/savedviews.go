package tui

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// savedViewItems lists presets by name, with their filter, grouping, and sort visible.
func (m *Model) savedViewItems() []pickItem {
	names := make([]string, 0, len(m.deps.State.SavedViews))
	for name := range m.deps.State.SavedViews {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(strings.Compare(strings.ToLower(a), strings.ToLower(b)), strings.Compare(a, b))
	})
	items := make([]pickItem, 0, len(names))
	for _, name := range names {
		v := m.deps.State.SavedViews[name]
		hint := cmp.Or(v.Filter, "all tickets") + " · group: " + cmp.Or(v.GroupBy, "none") + " · sort: " + sortLabel(v.Sort)
		items = append(items, pickItem{Label: ticket.OneLine(name), Hint: ticket.OneLine(hint), Value: name})
	}
	return items
}

// openSavedViews offers recall and management in every terminal layout.
func (m *Model) openSavedViews() {
	items := m.savedViewItems()
	items = append(items, pickItem{Label: "Save current view…", Value: m.openSaveView})
	if len(m.deps.State.SavedViews) > 0 {
		items = append(items, pickItem{Label: "Delete saved view…", Value: m.openDeleteView})
	}
	m.modal = newPicker(func(res pickResult) tea.Cmd {
		m.modal = nil
		if open, ok := res.item.Value.(func()); ok {
			open()
			return nil
		}
		return m.applySavedView(res.item.Value.(string))
	}, "Saved views · apply to current project", items)
}

// applySavedView applies the saved view's filter, grouping, and sort.
func (m *Model) applySavedView(name string) tea.Cmd {
	v := m.deps.State.SavedViews[name]
	m.savedView = name
	m.filterInput.SetValue(v.Filter)
	m.viewing = nil
	m.groupBy, m.sortBy = v.GroupBy, v.Sort
	return m.listViewChanged()
}

// cycleSavedView applies the saved view dir steps from the last one applied,
// wrapping around.
func (m *Model) cycleSavedView(dir int) tea.Cmd {
	items := m.savedViewItems()
	if len(items) == 0 {
		m.status = "no saved views"
		return nil
	}
	i := slices.IndexFunc(items, func(it pickItem) bool { return it.Value == m.savedView })
	if i < 0 && dir < 0 {
		i = 0
	}
	name := items[(i+dir+len(items))%len(items)].Value.(string)
	cmd := m.applySavedView(name)
	m.status = "view: " + ticket.OneLine(name)
	return cmd
}

// openSaveView captures a snapshot; subsequent filter edits do not change it.
func (m *Model) openSaveView() {
	v := state.View{Filter: m.filterInput.Value(), GroupBy: m.groupBy, Sort: m.sortBy}
	b := newInputBox("Save current view", "view name")
	b.onSubmit = func(name string) tea.Cmd {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsFunc(name, unicode.IsControl) {
			b.err = "enter a nonempty, single-line name without control characters"
			return nil
		}
		m.input = nil
		save := func() tea.Cmd { return m.storeSavedView(name, &v) }
		if _, exists := m.deps.State.SavedViews[name]; exists {
			m.confirmSavedView("Replace saved view "+ticket.OneLine(name)+"?", save)
			return nil
		}
		return save()
	}
	m.input = b
}

func (m *Model) openDeleteView() {
	m.modal = newPicker(pickValue(func(name string) tea.Cmd {
		m.confirmSavedView("Delete saved view "+ticket.OneLine(name)+"?", func() tea.Cmd {
			return m.storeSavedView(name, nil)
		})
		return nil
	}), "Delete saved view", m.savedViewItems())
}

func (m *Model) confirmSavedView(title string, confirm func() tea.Cmd) {
	m.modal = newPicker(func(res pickResult) tea.Cmd {
		m.modal = nil
		if res.item.Value.(bool) {
			return confirm()
		}
		return nil
	}, title, []pickItem{{Label: "Cancel", Value: false}, {Label: "Confirm", Value: true}})
}

// storeSavedView rolls back memory on a failed write so later saves cannot
// silently persist an operation reported as failed. A nil view deletes it.
func (m *Model) storeSavedView(name string, v *state.View) tea.Cmd {
	s := m.deps.State
	old, existed := s.SavedViews[name]
	if v == nil {
		delete(s.SavedViews, name)
	} else {
		s.SavedViews[name] = *v
	}
	if err := s.Save(); err != nil {
		if existed {
			s.SavedViews[name] = old
		} else {
			delete(s.SavedViews, name)
		}
		m.status = "saving state: " + err.Error()
		return nil
	}
	m.status = "saved view: " + ticket.OneLine(name)
	if v == nil {
		m.status = "deleted view: " + ticket.OneLine(name)
	}
	return nil
}
