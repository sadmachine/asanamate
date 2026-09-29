package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// rowContext is what a row needs beyond the task itself.
type rowContext struct {
	projectGID string          // viewed project, "" for My Tasks
	preferred  map[string]bool // the viewed project's custom field gids
}

// rowContext builds the row context for t in the current view.
func (m *Model) rowContext() rowContext {
	view := gidOf(m.viewProject)
	return rowContext{projectGID: view, preferred: m.projectFields[view]}
}

// ticketAgents returns the running agents on the ticket's branch in the repos
// linked to the ticket's projects (any repo if none are linked), most urgent
// first. preferred picks between same-named branch fields.
func (m *Model) ticketAgents(t asana.Task, preferred map[string]bool) []agents.Agent {
	if len(m.agents) == 0 {
		return nil
	}
	return agents.MatchAll(m.agents, action.Branch(t, m.deps.Config.BranchField, preferred), m.deps.State.LinkedRepos(t))
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
// empty branch field, and, when nothing is linked, the agents in t's repos
// that are on another branch.
func (m *Model) agentNotes(t asana.Task) []string {
	if !m.deps.Config.AgentsEnabled() {
		return nil
	}
	preferred := m.projectFields[gidOf(m.viewProject)]
	var notes []string
	if w := action.BranchWarning(t, m.deps.Config.BranchField, preferred); w != "" {
		notes = append(notes, w)
	}
	if len(m.viewAgents(t)) == 0 {
		branch := action.Branch(t, m.deps.Config.BranchField, preferred)
		for _, a := range agents.Unlinked(m.agents, branch, m.deps.State.LinkedRepos(t)) {
			notes = append(notes, agents.Hint(a, branch))
		}
	}
	return notes
}

// agentStates lists the states of t's agents, for filter terms.
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
// it in: due dates are relative and colored by urgency, and other values
// have no style of their own.
func cellValue(t asana.Task, name string, rc rowContext, today time.Time) (string, lipgloss.Style) {
	name = strings.TrimSpace(name)
	switch {
	case isDue(name):
		return dueLabel(t.DueOn, today)
	case strings.EqualFold(name, initialsField) && t.Assignee != nil:
		// One space each side keeps the badge readable.
		return " " + initials(t.Assignee.Name) + " ", authorStyle(t.Assignee.Name).Reverse(true).Bold(true)
	}
	return ticket.OneLine(fieldValue(t, name, rc)), lipgloss.Style{}
}

// dueLabel shows a due date relative to today: "3d ago" in red, "today" in
// yellow, "tomorrow" and the weekday within a week, then a faint date.
func dueLabel(dueOn *string, today time.Time) (string, lipgloss.Style) {
	due, days, ok := dueDays(dueOn, today)
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
