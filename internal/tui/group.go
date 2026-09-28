package tui

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// dueBuckets are the due date groups, in display order.
var dueBuckets = []string{"Overdue", "Today", "Tomorrow", "Next 7 days", "Later", "No due date"}

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
	none := "No " + by
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

// groupLabel returns the group t belongs to when grouping by the named field.
func groupLabel(t asana.Task, by string, rc rowContext, today time.Time) string {
	if isDue(by) {
		return dueBucket(t.DueOn, today)
	}
	if v := ticket.OneLine(fieldValue(t, by, rc)); v != "" {
		return v
	}
	return "No " + by
}

// dueBucket places a due date (YYYY-MM-DD) relative to today's calendar date.
func dueBucket(dueOn *string, today time.Time) string {
	if dueOn == nil {
		return dueBuckets[5]
	}
	due, err := time.Parse(time.DateOnly, *dueOn)
	if err != nil {
		return dueBuckets[5]
	}
	day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	switch days := int(due.Sub(day).Hours() / 24); {
	case days < 0:
		return dueBuckets[0]
	case days == 0:
		return dueBuckets[1]
	case days == 1:
		return dueBuckets[2]
	case days < 7:
		return dueBuckets[3]
	default:
		return dueBuckets[4]
	}
}

// openGroupPicker offers the groupings: none, the built-in fields, then the
// configured list fields and group_by, and the custom fields on loaded tickets.
func (m *Model) openGroupPicker() {
	names := []string{"", "section", "due", "assignee", "project", "tags", "completed"}
	names = append(names, m.deps.Config.List.Fields...)
	names = append(names, m.deps.Config.List.GroupBy)
	for _, t := range m.tasks {
		for _, f := range t.CustomFields {
			names = append(names, f.Name)
		}
	}
	var items []pickItem
	current := 0
	for _, n := range names {
		n = strings.TrimSpace(n)
		if slices.ContainsFunc(items, func(it pickItem) bool { return strings.EqualFold(it.Value.(string), n) }) {
			continue
		}
		it := pickItem{Label: cmp.Or(ticket.OneLine(n), "None"), Value: n}
		if strings.EqualFold(n, m.groupBy) {
			it.Hint, current = "current", len(items)
		}
		items = append(items, it)
	}
	m.modal = newPicker(pickGroup, "Group by", items)
	m.modal.cursor = current
}

// pickedGroup groups the list by the named field, keeping the selected ticket.
func (m *Model) pickedGroup(by string) tea.Cmd {
	m.modal = nil
	m.groupBy = by
	m.saveView()
	m.applyFilter()
	m.fitList()
	return m.selectionChanged()
}

// groupHeader renders a group's header across width in the header color: a
// reversed " Label (n)" bar, or a "── Label (n) ───" rule.
func (m *Model) groupHeader(label string, n, width int) string {
	count := " (" + strconv.Itoa(n) + ")"
	if m.deps.Config.List.Header.Style == config.StyleRule {
		rule := m.headerStyle.UnsetBold()
		head := rule.Render("── ") + m.headerStyle.Render(label) + rule.Render(count+" ")
		return ansi.Truncate(head+rule.Render(strings.Repeat("─", max(width-ansi.StringWidth(head), 0))), width, "…")
	}
	head := ansi.Truncate(" "+label+count, width, "…")
	return m.headerStyle.Reverse(true).Render(head + strings.Repeat(" ", max(width-ansi.StringWidth(head), 0)))
}
