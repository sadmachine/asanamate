package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/asana"
)

// pinnedTasks returns the explicit pins for the current project or My Tasks.
func (m *Model) pinnedTasks() []string {
	return m.deps.State.PinnedTasks[gidOf(m.viewProject)]
}

// togglePin saves the explicit pin before changing the list. Unpinning keeps
// the selected ticket visible until selection moves, even outside the filter.
func (m *Model) togglePin(_ tea.KeyPressMsg) tea.Cmd {
	t, ok := m.selected()
	if !ok {
		return nil
	}
	key := gidOf(m.viewProject)
	previous := m.pinnedTasks()
	pins := slices.Clone(previous)
	unpin := slices.Contains(pins, t.GID)
	if unpin {
		pins = slices.DeleteFunc(pins, func(gid string) bool { return gid == t.GID })
	} else {
		pins = append(pins, t.GID)
	}
	if m.deps.State.PinnedTasks == nil {
		m.deps.State.PinnedTasks = map[string][]string{}
	}
	if len(pins) == 0 {
		delete(m.deps.State.PinnedTasks, key)
	} else {
		m.deps.State.PinnedTasks[key] = pins
	}
	if err := m.deps.State.Save(); err != nil {
		if previous == nil {
			delete(m.deps.State.PinnedTasks, key)
		} else {
			m.deps.State.PinnedTasks[key] = previous
		}
		m.status = "saving pins: " + err.Error()
		return nil
	}
	m.viewing = &t
	if unpin {
		m.pinExtras = slices.DeleteFunc(m.pinExtras, func(v asana.Task) bool { return v.GID == t.GID })
		m.status = "ticket unpinned"
	} else {
		if !slices.ContainsFunc(m.tasks, func(v asana.Task) bool { return v.GID == t.GID }) {
			m.pinExtras = append(m.pinExtras, t)
		}
		m.status = "ticket pinned"
	}
	m.applyFilter()
	m.fitList()
	return m.selectionChanged()
}

// refreshRetained updates both temporary and explicit tickets from detail loads.
func (m *Model) refreshRetained(t asana.Task) bool {
	changed := m.refreshViewing(t)
	if !slices.Contains(m.pinnedTasks(), t.GID) {
		return changed
	}
	for _, tasks := range [][]asana.Task{m.tasks, m.pinExtras} {
		if i := slices.IndexFunc(tasks, func(v asana.Task) bool { return v.GID == t.GID }); i >= 0 {
			tasks[i] = t
			return true
		}
	}
	return changed
}

// retainedSection separates synthetic sections from regular groups even when
// a project's section happens to have the same name.
func (m *Model) retainedSection(i int) (string, int) {
	if i < m.pinnedCount {
		return pinnedLabel, m.pinnedCount
	}
	if m.viewingRow && i == m.pinnedCount {
		return viewingLabel, 1
	}
	return "", 0
}
