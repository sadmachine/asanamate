package tui

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// Due date groups.
const (
	dueOverdue  = "Overdue"
	dueToday    = "Today"
	dueTomorrow = "Tomorrow"
	dueWeek     = "Next 7 days"
	dueLater    = "Later"
	dueNone     = "No due date"
)

// Section labels for explicit pins, a viewed ticket outside the filter,
// changed My Tasks tickets (list.new.section), and tickets of an ungrouped list.
const (
	pinnedLabel    = "Pinned"
	viewingLabel   = "Viewing"
	newLabel       = "New"
	ungroupedLabel = "Tickets"
)

// dueBuckets are the due date groups, in display order.
var dueBuckets = []string{dueOverdue, dueToday, dueTomorrow, dueWeek, dueLater, dueNone}

// groupTasks orders tasks into groups by the named list field and returns each
// task's group label. Tasks keep their order within a group. Groups appear in
// first-seen order with the group of empty values last, except due dates,
// which use dueBuckets. An empty name leaves tasks ungrouped.
func groupTasks(tasks []asana.Task, by string, rc rowContext, today time.Time) ([]asana.Task, []string) {
	if by == "" {
		return tasks, nil
	}
	rank := map[string]int{}
	if isDue(by) {
		for i, b := range dueBuckets {
			rank[b] = i
		}
	}
	none := noneLabel(by)
	labels := make([]string, len(tasks))
	for i, t := range tasks {
		labels[i] = groupLabel(t, by, rc, today)
		if _, ok := rank[labels[i]]; !ok && labels[i] != none {
			rank[labels[i]] = len(rank)
		}
	}
	if !isDue(by) {
		rank[none] = len(rank)
	}
	order := make([]int, len(tasks))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return rank[labels[a]] - rank[labels[b]] })
	grouped := make([]asana.Task, len(tasks))
	groups := make([]string, len(tasks))
	for i, j := range order {
		grouped[i], groups[i] = tasks[j], labels[j]
	}
	return grouped, groups
}

func isDue(by string) bool { return strings.EqualFold(by, "due") }

// noneLabel is the group of tasks with no value for the named field.
func noneLabel(by string) string { return "No " + by }

// groupLabel returns the group t belongs to when grouping by the named field.
func groupLabel(t asana.Task, by string, rc rowContext, today time.Time) string {
	if isDue(by) {
		return dueBucket(t.DueOn, today)
	}
	if v := ticket.OneLine(fieldValue(t, by, rc)); v != "" {
		return v
	}
	return noneLabel(by)
}

// dueBucket places a due date (YYYY-MM-DD) relative to today's calendar date.
func dueBucket(dueOn *string, today time.Time) string {
	_, days, ok := asana.DueDays(dueOn, today)
	if !ok {
		return dueNone
	}
	switch {
	case days < 0:
		return dueOverdue
	case days == 0:
		return dueToday
	case days == 1:
		return dueTomorrow
	case days < 7:
		return dueWeek
	default:
		return dueLater
	}
}

// groupings lists the fields the list can group by: none, the built-in
// fields, then the configured list fields and group_by, and the custom fields
// on loaded tickets.
func (m *Model) groupings() []string {
	names := append([]string{""}, builtinFields...)
	names = append(names, m.deps.Config.List.Fields...)
	names = append(names, m.deps.Config.List.DefaultView.GroupBy)
	for _, t := range m.tasks {
		for _, f := range t.CustomFields {
			names = append(names, f.Name)
		}
	}
	return uniqueFold(names)
}

// uniqueFold trims names and drops repeats, ignoring case.
func uniqueFold(names []string) []string {
	var out []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		if !slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, n) }) {
			out = append(out, n)
		}
	}
	return out
}

// openGroupPicker offers the groupings.
func (m *Model) openGroupPicker() {
	var items []pickItem
	current := 0
	for _, n := range m.groupings() {
		it := pickItem{Label: cmp.Or(ticket.OneLine(n), "None"), Value: n}
		if strings.EqualFold(n, m.groupBy) {
			it.Hint, current = "current", len(items)
		}
		items = append(items, it)
	}
	m.modal = newPicker(pickValue(m.pickedGroup), "Group by", items)
	m.modal.cursor = current
}

// pickedGroup groups the list by the named field, keeping the selected
// ticket. A new grouping starts with every group open.
func (m *Model) pickedGroup(by string) tea.Cmd {
	if !strings.EqualFold(by, m.groupBy) {
		m.foldedGroups = nil
	}
	m.groupBy = by
	return m.listViewChanged()
}

// listViewChanged persists and rebuilds the view while keeping the selected ticket.
func (m *Model) listViewChanged() tea.Cmd {
	m.modal = nil
	m.saveView()
	m.applyFilter()
	m.fitList()
	return m.selectionChanged()
}

// groupStyle is a group header's style. Grouped by due date, the Overdue and
// Today headers take the urgency colors of their due dates.
func (m *Model) groupStyle(label string) lipgloss.Style {
	if isDue(m.groupBy) {
		switch label {
		case dueOverdue:
			return m.headerStyle.Foreground(errorStyle.GetForeground())
		case dueToday:
			return m.headerStyle.Foreground(warnStyle.GetForeground())
		}
	}
	return m.headerStyle
}

// distant reports whether visible ticket i is far enough off to fade: grouped
// by due date, in the Later or No due date group and not retained.
func (m *Model) distant(i int) bool {
	if label, _ := m.retainedSection(i); label != "" || !isDue(m.groupBy) {
		return false
	}
	// A project switch restores the destination's grouping while the source
	// project's rows remain visible during loading. Those stale rows may not
	// have group labels for the restored grouping yet.
	if i < 0 || i >= len(m.groups) {
		return false
	}
	return m.groups[i] == dueLater || m.groups[i] == dueNone
}

// groupHeaderWidth is the width that shows a group's header in full, rule or
// bar, with its note.
func groupHeaderWidth(label string, n int, note string) int {
	if note != "" {
		note = " · " + note
	}
	return ansi.StringWidth("── " + label + " (" + strconv.Itoa(n) + ")" + note + " ─")
}

// groupHeader renders a group's header across width in style: a reversed
// " Label (n)" bar, or a "── Label (n) ───" rule. A note, such as the values
// its tickets share, follows the count.
func (m *Model) groupHeader(style lipgloss.Style, label string, n, width int, note string) string {
	count := " (" + strconv.Itoa(n) + ")"
	if note != "" {
		count += " · " + note
	}
	if m.deps.Config.List.Header.Style == config.StyleRule {
		rule := style.UnsetBold()
		head := rule.Render("── ") + style.Render(label) + rule.Render(count+" ")
		return ansi.Truncate(head+rule.Render(strings.Repeat("─", max(width-ansi.StringWidth(head), 0))), width, "…")
	}
	head := ansi.Truncate(" "+label+count, width, "…")
	return style.Reverse(true).Render(head + strings.Repeat(" ", max(width-ansi.StringWidth(head), 0)))
}
