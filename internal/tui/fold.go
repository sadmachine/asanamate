package tui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/ticket"
)

// A folded group shows only its header. The header stands in for the
// group's first visible ticket: the cursor rests on that index, selected
// reports no ticket there, and the group's other tickets are skipped.

// foldable reports whether visible ticket i sits in a group that can fold:
// the list is grouped and i is outside Pinned, Viewing, and New.
func (m *Model) foldable(i int) bool {
	if m.groupBy == "" || i < 0 || i >= len(m.groups) {
		return false
	}
	label, _ := m.retainedSection(i)
	return label == ""
}

// groupFolded reports whether visible ticket i sits in a folded group.
func (m *Model) groupFolded(i int) bool {
	return m.foldable(i) && slices.Contains(m.foldedGroups, m.groups[i])
}

// hidden reports whether visible ticket i sits in a folded group after its
// first ticket, whose place the group's header takes.
func (m *Model) hidden(i int) bool {
	return m.groupFolded(i) && i > 0 && m.sectionKey(i) == m.sectionKey(i-1)
}

// onFoldedHeader reports whether the cursor rests on a folded group's header.
func (m *Model) onFoldedHeader() bool {
	return m.groupFolded(m.cursor)
}

// sectionStart returns the first visible index of the section holding i.
func (m *Model) sectionStart(i int) int {
	for i > 0 && m.sectionKey(i-1) == m.sectionKey(i) {
		i--
	}
	return i
}

// step moves the cursor n stops, down when n is positive. Stops are the
// shown tickets and the folded groups' headers.
func (m *Model) step(n int) {
	i := m.cursor
	for ; n > 0; n-- {
		j := i + 1
		for j < len(m.visible) && m.hidden(j) {
			j++
		}
		if j >= len(m.visible) {
			break
		}
		i = j
	}
	for ; n < 0 && i > 0; n++ {
		i--
		if m.hidden(i) {
			i = m.sectionStart(i)
		}
	}
	m.cursor = i
}

// reveal selects visible ticket i, opening its group when folded.
func (m *Model) reveal(i int) {
	if m.groupFolded(i) {
		m.setFolded(m.groups[i], false)
	}
	m.moveTo(i)
}

// setFolded folds or opens the group label and remembers it in the view.
func (m *Model) setFolded(label string, fold bool) {
	m.foldedGroups = slices.DeleteFunc(slices.Clone(m.foldedGroups), func(g string) bool { return g == label })
	if fold {
		m.foldedGroups = append(m.foldedGroups, label)
		slices.Sort(m.foldedGroups)
	}
	m.saveView()
}

// toggleFold folds the cursor's group, leaving the cursor on its header, or
// opens it from the header onto its first ticket.
func (m *Model) toggleFold() tea.Cmd {
	if !m.foldable(m.cursor) {
		return nil
	}
	m.setFolded(m.groups[m.cursor], !m.groupFolded(m.cursor))
	m.moveTo(m.sectionStart(m.cursor))
	return m.selectionChanged()
}

// foldOthers folds every group but the cursor's, or opens every group when
// the others are already folded.
func (m *Model) foldOthers() tea.Cmd {
	if m.groupBy == "" {
		return nil
	}
	current := ""
	if m.foldable(m.cursor) {
		current = m.groups[m.cursor]
	}
	var others []string
	for i, g := range m.groups {
		if m.foldable(i) && g != current && !slices.Contains(others, g) {
			others = append(others, g)
		}
	}
	if !slices.ContainsFunc(others, func(g string) bool { return !slices.Contains(m.foldedGroups, g) }) {
		m.foldedGroups = nil
	} else {
		kept := slices.DeleteFunc(slices.Clone(m.foldedGroups), func(g string) bool { return g == current || slices.Contains(others, g) })
		m.foldedGroups = append(kept, others...)
		slices.Sort(m.foldedGroups)
	}
	m.saveView()
	m.moveTo(m.cursor)
	return m.selectionChanged()
}

// foldNote is the reader's note while the cursor rests on a folded group's
// header, or "" elsewhere.
func (m *Model) foldNote() string {
	if !m.onFoldedHeader() {
		return ""
	}
	label := m.groups[m.cursor]
	n, noun := m.groupCounts()[label], "tickets"
	if n == 1 {
		noun = "ticket"
	}
	note := fmt.Sprintf("%s · %d %s folded", ticket.OneLine(label), n, noun)
	if k := keyLabel("main", "toggle_fold"); k != "" {
		note += " · " + k + " to open"
	}
	return dimStyle.Render(note)
}
