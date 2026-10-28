package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/filter"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// rowContext is what a row needs beyond the task itself.
type rowContext struct {
	branchField string          // configured branch custom field name
	projectGID  string          // viewed project, "" for My Tasks
	preferred   map[string]bool // the viewed project's custom field gids
}

// rowContext builds the row context for t in the current view.
func (m *Model) rowContext() rowContext {
	view := gidOf(m.viewProject)
	return rowContext{projectGID: view, preferred: m.projectFields[view], branchField: m.deps.Config.BranchField}
}

// ticketAgents returns explicit task links with repo/branch fallback, urgency
// first. preferred picks between same-named branch fields.
func (m *Model) ticketAgents(t asana.Task, preferred map[string]bool) []agents.Agent {
	if len(m.agents) == 0 {
		return nil
	}
	return agents.MatchAll(m.agents, t.GID, action.Branch(t, m.deps.State.TaskBranches[t.GID], m.deps.Config.BranchField, preferred), m.deps.State.LinkedRepos(t))
}

// viewAgents returns the agents linked to t in the current view. Results are
// cached per ticket until the agents, tasks, view, or repo links change,
// since every frame asks for them.
func (m *Model) viewAgents(t asana.Task) []agents.Agent {
	if len(m.agents) == 0 {
		return nil
	}
	if list, ok := m.linked[t.GID]; ok {
		return list
	}
	list := m.ticketAgents(t, m.projectFields[gidOf(m.viewProject)])
	if m.linked == nil {
		m.linked = map[string][]agents.Agent{}
	}
	m.linked[t.GID] = list
	return list
}

// agentNotes explains, in the current view, why t may be missing agents: an
// empty branch field, and unlinked agents in t's repos.
// Unlinked agents stay hidden until shown, and hidden counts them.
func (m *Model) agentNotes(t asana.Task) (notes []string, hidden int) {
	if !m.deps.Config.AgentsEnabled() {
		return nil, 0
	}
	preferred := m.projectFields[gidOf(m.viewProject)]
	saved := m.deps.State.TaskBranches[t.GID]
	if w := action.BranchWarning(t, saved, m.deps.Config.BranchField, preferred); w != "" {
		notes = append(notes, w)
	}
	branch := action.Branch(t, saved, m.deps.Config.BranchField, preferred)
	others := agents.Unlinked(m.agents, t.GID, branch, m.deps.State.LinkedRepos(t))
	if !m.showOthers {
		return notes, len(others)
	}
	for _, a := range others {
		notes = append(notes, agents.Hint(a, branch))
	}
	return notes, 0
}

// otherAgents names n nearby agents not linked to this ticket.
func otherAgents(n int) string {
	if n == 1 {
		return "1 unlinked agent"
	}
	return fmt.Sprintf("%d unlinked agents", n)
}

// agentStates lists the states of t's agents, for filter terms.
// filterEnv is what filter terms need beyond each ticket.
func (m *Model) filterEnv() filter.Env {
	return filter.Env{Today: m.now(), Me: m.meGID, AgentStates: m.agentStates, HasBranch: m.hasBranch}
}

// hasBranch reports whether t has a branch saved for it or set in its branch
// field, rather than the ID field or title slug fallback.
func (m *Model) hasBranch(t asana.Task) bool {
	preferred := m.projectFields[gidOf(m.viewProject)]
	return action.ExplicitBranch(t, m.deps.State.TaskBranches[t.GID], m.deps.Config.BranchField, preferred) != ""
}

func (m *Model) agentStates(t asana.Task) []string {
	var out []string
	for _, a := range m.viewAgents(t) {
		out = append(out, string(a.State))
	}
	return out
}

// builtinFields are the list field names fieldValue resolves itself, in the
// group picker's order; any other name, except initialsField, is a custom
// field.
var builtinFields = []string{"section", "due", "assignee", "project", "tags", "completed"}

// initialsField shows the assignee's initials as a colored badge. It is left
// out of builtinFields since grouping by it would repeat assignee.
const initialsField = "initials"

// rowFields returns the non-empty display values of the configured list
// fields, in order.
func rowFields(t asana.Task, names []string, rc rowContext) []string {
	var out []string
	for _, name := range names {
		if v := ticket.OneLine(fieldValue(t, strings.TrimSpace(name), rc)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// cellValue is a list field's value as a row shows it, with the style to show
// it in: due dates are relative and colored by urgency, and other values are
// muted, as secondary to the title, unless an enum's Asana color sets them.
func cellValue(t asana.Task, name string, rc rowContext, today time.Time) (string, lipgloss.Style) {
	name = strings.TrimSpace(name)
	switch {
	case isDue(name):
		return dueLabel(t.DueOn, today)
	case strings.EqualFold(name, initialsField) && t.Assignee != nil:
		// One space each side keeps the badge readable.
		return " " + initials(t.Assignee.Name) + " ", authorStyle(t.Assignee.Name).Reverse(true).Bold(true)
	}
	v := ticket.OneLine(fieldValue(t, name, rc))
	if !slices.Contains(builtinFields, strings.ToLower(name)) {
		if f, ok := t.Field(name, rc.preferred); ok {
			return v, customCellStyle(f, rc.branchField)
		}
	}
	return v, dimStyle
}

// customCellStyle gives the configured branch field its theme role.
func customCellStyle(f asana.CustomField, branchField string) lipgloss.Style {
	if branchField != "" && asana.SameFieldName(f.Name, branchField) {
		return branchStyle
	}
	return fieldCellStyle(f)
}

// fieldCellStyle is a custom field's list cell style: its enum color, else muted.
func fieldCellStyle(f asana.CustomField) lipgloss.Style {
	if style, ok := enumStyle(f); ok {
		return style
	}
	return dimStyle
}

// dueLabel shows a due date relative to today: "3d ago" in red, "today" in
// yellow, "tomorrow" and the weekday within a week, then a faint date.
func dueLabel(dueOn *string, today time.Time) (string, lipgloss.Style) {
	due, days, ok := asana.DueDays(dueOn, today)
	switch {
	case !ok:
		return "", lipgloss.Style{}
	case days < 0:
		return fmt.Sprintf("%dd ago", -days), errorStyle
	case days == 0:
		return "today", warnStyle
	case days == 1:
		return "tomorrow", lipgloss.Style{}
	case days < 7:
		return due.Format("Mon"), lipgloss.Style{}
	case due.Year() == today.Year():
		return due.Format("Jan 2"), dimStyle
	default:
		return due.Format("Jan 2 2006"), dimStyle
	}
}

// fieldValue resolves a built-in field name, falling back to the custom field
// with that name (see asana.Task.Field for same-named fields).
func fieldValue(t asana.Task, name string, rc rowContext) string {
	switch strings.ToLower(name) {
	case "section":
		return t.SectionFor(rc.projectGID)
	case "completed":
		return t.Status()
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
	case initialsField:
		if t.Assignee != nil {
			return initials(t.Assignee.Name)
		}
		return ""
	case "project":
		var names []string
		for _, m := range t.Memberships {
			names = append(names, m.Project.Name)
		}
		return strings.Join(names, ", ")
	case "tags":
		return strings.Join(asana.Names(t.Tags), ", ")
	}
	if f, ok := t.Field(name, rc.preferred); ok {
		return f.Value()
	}
	return ""
}

// initials returns the first letters of a name's first and last words, or
// the first two letters of a one-word name, in upper case.
func initials(name string) string {
	words := strings.Fields(ticket.OneLine(name))
	switch len(words) {
	case 0:
		return ""
	case 1:
		r := []rune(words[0])
		return strings.ToUpper(string(r[:min(2, len(r))]))
	}
	first, last := []rune(words[0]), []rune(words[len(words)-1])
	return strings.ToUpper(string(first[0]) + string(last[0]))
}
