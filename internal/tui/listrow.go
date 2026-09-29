package tui

import (
	"strings"

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

// viewAgents returns the agents linked to t in the current view.
func (m *Model) viewAgents(t asana.Task) []agents.Agent {
	return m.ticketAgents(t, m.projectFields[gidOf(m.viewProject)])
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
