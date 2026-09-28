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
	agent      *agents.Agent   // running agent linked to the task
}

// rowContext builds the row context for t in the current view.
func (m *Model) rowContext(t asana.Task) rowContext {
	view := gidOf(m.viewProject)
	preferred := m.projectFields[view]
	return rowContext{projectGID: view, preferred: preferred, agent: m.ticketAgent(t, preferred)}
}

// ticketAgent returns the running agent on the ticket's branch in one of the
// repos linked to the ticket's projects (any repo if none are linked).
func (m *Model) ticketAgent(t asana.Task, preferred map[string]bool) *agents.Agent {
	if len(m.agents) == 0 {
		return nil
	}
	var repos []string
	for _, mb := range t.Memberships {
		if path, ok := m.deps.State.Repos[mb.Project.GID]; ok {
			repos = append(repos, path)
		}
	}
	return agents.Match(m.agents, action.Branch(t, m.deps.Config.BranchField, preferred), repos)
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
	case "agent":
		if rc.agent != nil {
			return "agent " + rc.agent.Status
		}
		return ""
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
	if f, ok := t.Field(name, rc.preferred); ok {
		return f.Value()
	}
	return ""
}
