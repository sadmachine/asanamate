package tui

import (
	"strings"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// rowFields returns the non-empty display values of the configured list
// fields, in order.
func rowFields(t asana.Task, names []string, projectGID string) []string {
	var out []string
	for _, name := range names {
		if v := ticket.OneLine(fieldValue(t, strings.TrimSpace(name), projectGID)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// fieldValue resolves a built-in field name, falling back to the first custom
// field with that name (case-insensitive) that has a value.
func fieldValue(t asana.Task, name, projectGID string) string {
	switch strings.ToLower(name) {
	case "section":
		return t.SectionFor(projectGID)
	case "completed":
		if t.Completed {
			return "done"
		}
		return "open"
	case "due":
		if t.DueOn != nil && *t.DueOn != "" {
			return "due " + *t.DueOn
		}
		return ""
	case "assignee":
		if t.Assignee != nil {
			return t.Assignee.Name
		}
		return ""
	case "project":
		var names []string
		for _, m := range t.Memberships {
			names = append(names, m.Project.Name)
		}
		return strings.Join(names, ", ")
	case "tags":
		var names []string
		for _, tag := range t.Tags {
			names = append(names, tag.Name)
		}
		return strings.Join(names, ", ")
	}
	for _, f := range t.CustomFields {
		if strings.EqualFold(strings.TrimSpace(f.Name), name) && f.DisplayValue != nil && *f.DisplayValue != "" {
			return *f.DisplayValue
		}
	}
	return ""
}
