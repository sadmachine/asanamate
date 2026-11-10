package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// listColumns keeps the global fields as defaults. A present, empty project
// override intentionally shows only the title (and the independent agent badge).
func (m *Model) listColumns() []state.Column {
	if columns, ok := m.deps.State.Columns[gidOf(m.viewProject)]; ok {
		return columns
	}
	columns := make([]state.Column, 0, len(m.deps.Config.List.Fields))
	for _, name := range m.deps.Config.List.Fields {
		columns = append(columns, state.Column{Field: strings.TrimSpace(name)})
	}
	return columns
}

// columnValue resolves selected custom fields by ID, without falling back to
// a same-named field from another project. Names remain supported for config.
func columnValue(t asana.Task, column state.Column, rc rowContext, today time.Time) (string, lipgloss.Style) {
	if column.GID == "" {
		return cellValue(t, column.Field, rc, today)
	}
	for _, f := range t.CustomFields {
		if f.GID == column.GID {
			return ticket.OneLine(f.Value()), customCellStyle(f, rc.branchField)
		}
	}
	return "", lipgloss.Style{}
}

func fieldGIDs(fields []asana.Ref) map[string]bool {
	if fields == nil {
		return nil
	}
	gids := make(map[string]bool, len(fields))
	for _, f := range fields {
		gids[f.GID] = true
	}
	return gids
}

func (m *Model) cacheProjectFields(gid string, catalog []asana.Ref) {
	m.projectCatalogs[gid] = catalog
	m.projectFields[gid], m.linked = fieldGIDs(catalog), nil
}

// openColumns loads authoritative project metadata when a task reload could
// not get it. My Tasks offers the custom fields present on its loaded tasks.
func (m *Model) openColumns() tea.Cmd {
	if gid := gidOf(m.viewProject); gid != "" {
		if _, known := m.projectCatalogs[gid]; !known {
			if m.deps.Client == nil {
				m.setStatus(statusWarning, "project fields are unavailable; reload and try again")
				return nil
			}
			m.modal, m.columnsFor = nil, gid
			cmd := loadProjectFields(m.deps.Client, gid)
			return m.wait("Loading project fields…", func() tea.Msg {
				msg := cmd().(projectFieldsMsg)
				msg.columns = true
				return msg
			})
		}
	}
	m.openColumnPicker()
	return nil
}

func columnKey(column state.Column) string {
	if column.GID != "" {
		return "gid:" + column.GID
	}
	return "name:" + strings.ToLower(strings.TrimSpace(column.Field))
}

// availableColumns excludes fields belonging only to other projects on
// multi-homed tasks. IDs distinguish custom fields with identical names.
func (m *Model) availableColumns() []state.Column {
	var columns []state.Column
	for _, name := range append(slices.Clone(builtinFields), initialsField) {
		columns = append(columns, state.Column{Field: name})
	}
	fields := m.projectCatalogs[gidOf(m.viewProject)]
	if m.viewProject == nil {
		for _, tasks := range [][]asana.Task{m.tasks, m.pinExtras} {
			for _, t := range tasks {
				for _, f := range t.CustomFields {
					fields = append(fields, asana.Ref{GID: f.GID, Name: f.Name})
				}
			}
		}
	}
	seen := map[string]bool{}
	var custom []state.Column
	for _, f := range fields {
		if f.GID == "" {
			continue
		}
		column := state.Column{Field: f.Name, GID: f.GID}
		if key := columnKey(column); !seen[key] {
			seen[key] = true
			custom = append(custom, column)
		}
	}
	slices.SortFunc(custom, func(a, b state.Column) int {
		if order := strings.Compare(strings.ToLower(a.Field), strings.ToLower(b.Field)); order != 0 {
			return order
		}
		return strings.Compare(a.GID, b.GID)
	})
	return append(columns, custom...)
}

func (m *Model) openColumnPicker() {
	available := m.availableColumns()
	var items []pickItem
	checked, seen := map[int]bool{}, map[string]bool{}
	add := func(column state.Column, selected, known bool) {
		key := columnKey(column)
		if seen[key] {
			return
		}
		seen[key] = true
		hint := ""
		if !known {
			hint = "unavailable"
		} else if column.GID != "" {
			hint = "custom field"
			for _, other := range available {
				if columnKey(other) != key && asana.SameFieldName(other.Field, column.Field) {
					hint += " · " + ticket.OneLine(column.GID)
					break
				}
			}
		}
		checked[len(items)] = selected
		items = append(items, pickItem{Label: ticket.OneLine(column.Field), Hint: hint, Value: column})
	}
	for _, column := range m.listColumns() {
		// Resolve old name-based defaults to a native field first, then to a
		// project custom field. Saved custom IDs never match by name.
		i := slices.IndexFunc(available, func(c state.Column) bool {
			if column.GID != "" {
				return column.GID == c.GID
			}
			return asana.SameFieldName(column.Field, c.Field)
		})
		if i >= 0 {
			column = available[i]
		}
		add(column, true, i >= 0)
	}
	for _, column := range available {
		add(column, false, true)
	}
	title := "Columns · My Tasks"
	if m.viewProject != nil {
		title = "Columns · " + ticket.OneLine(m.viewProject.Name)
	}
	p := newMultiPicker(func(res pickResult) tea.Cmd {
		columns := make([]state.Column, 0, len(res.items))
		for _, item := range res.items {
			columns = append(columns, item.Value.(state.Column))
		}
		return m.storeColumns(columns, res.reset)
	}, title, items, checked)
	p.reorder, p.allowReset = true, true
	m.modal = p
}

// storeColumns rolls back a failed write. Reset removes the override rather
// than copying today's defaults, so later config edits still take effect.
func (m *Model) storeColumns(columns []state.Column, reset bool) tea.Cmd {
	gid := gidOf(m.viewProject)
	old, exists := m.deps.State.Columns[gid]
	if reset {
		delete(m.deps.State.Columns, gid)
	} else {
		m.deps.State.Columns[gid] = slices.Clone(columns)
	}
	if err := m.deps.State.Save(); err != nil {
		if exists {
			m.deps.State.Columns[gid] = old
		} else {
			delete(m.deps.State.Columns, gid)
		}
		m.modal.err = fmt.Sprintf("Saving columns: %v", err)
		return nil
	}
	m.modal = nil
	m.measureColumns()
	m.fitList()
	message := "columns saved for this project"
	if reset {
		message = "columns reset to list.fields"
	}
	m.setStatus(statusInfo, message)
	return nil
}
