package tui

import (
	"cmp"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// sortTasks stably orders each contiguous group, preserving group order and
// putting missing values last in either direction. Nil groups sorts the whole list.
func sortTasks(tasks []asana.Task, groups []string, sort config.Sort, rc rowContext) {
	by := strings.TrimSpace(sort.By)
	if by == "" {
		return
	}
	compare := func(a, b asana.Task) int {
		av, bv := sortValue(a, by, rc), sortValue(b, by, rc)
		if av == "" && bv != "" {
			return 1
		}
		if bv == "" && av != "" {
			return -1
		}
		order := strings.Compare(av, bv)
		if sort.Direction == "desc" {
			return -order
		}
		return order
	}
	for start := 0; start < len(tasks); {
		end := start + 1
		for end < len(tasks) && (groups == nil || groups[end] == groups[start]) {
			end++
		}
		slices.SortStableFunc(tasks[start:end], compare)
		start = end
	}
}

func sortValue(t asana.Task, by string, rc rowContext) string {
	if strings.EqualFold(by, "title") {
		return strings.ToLower(ticket.OneLine(t.Name))
	}
	if isDue(by) {
		if t.DueOn == nil {
			return ""
		}
		if _, err := time.Parse(time.DateOnly, *t.DueOn); err != nil {
			return ""
		}
		return *t.DueOn
	}
	return strings.ToLower(strings.TrimSpace(fieldValue(t, by, rc)))
}

func sortLabel(sort config.Sort) string {
	if sort.By == "" {
		return "none"
	}
	return ticket.OneLine(sort.By) + " " + cmp.Or(sort.Direction, "asc")
}

func (m *Model) sortings() []string {
	return uniqueFold(append(append([]string{"", "title"}, m.groupings()...), m.deps.Config.List.Sort.By, m.sortBy.By))
}

// openSortPicker offers the same fields as grouping, plus title, in both directions.
func (m *Model) openSortPicker() {
	items := []pickItem{{Label: "None", Value: config.Sort{}}}
	current := 0
	for _, by := range m.sortings() {
		if by == "" {
			continue
		}
		for _, direction := range []string{"asc", "desc"} {
			sort := config.Sort{By: by, Direction: direction}
			item := pickItem{Label: sortLabel(sort), Value: sort}
			if strings.EqualFold(by, m.sortBy.By) && direction == cmp.Or(m.sortBy.Direction, "asc") {
				item.Hint, current = "current", len(items)
			}
			items = append(items, item)
		}
	}
	if m.sortBy.By == "" {
		items[0].Hint = "current"
	}
	m.modal = newPicker(pickValue(m.pickedSort), "Sort by", items)
	m.modal.cursor = current
}

// pickedSort changes ordering while keeping the selected ticket.
func (m *Model) pickedSort(sort config.Sort) tea.Cmd {
	m.sortBy = sort
	return m.listViewChanged()
}
