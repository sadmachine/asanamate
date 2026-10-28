// Package filter implements the ticket list query language.
//
// A query is space-separated terms that must all match. Bare words match the
// title. section:, project:, assignee:, and tag: match names by substring;
// assignee:@me matches tickets assigned to the user and assignee:none
// unassigned ones.
// project:<name>[<section>] matches a section within that project only, by
// exact name; project:<name>[<a>|<b>] matches any of the listed sections.
// is:open and is:done match completion. due:overdue, due:today, due:week (the
// next 7 days, today included), and due:none match the due date. agent:any,
// agent:none, and agent:<state> match linked agents. branch:any matches a
// branch saved for the ticket or set in its branch field, and branch:none
// neither. A leading "-" negates a term, and double
// quotes group words.
package filter

import (
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/sadmachine/asanamate/internal/asana"
)

var keys = []string{"section", "project", "assignee", "tag", "is", "due", "agent", "branch"}

// Keys returns the filter fields in display order.
func Keys() []string { return slices.Clone(keys) }

type term struct {
	key, value string
	sections   []string // project:<value>[<a>|<b>]; nil matches any section
	negate     bool
}

// Filter is a parsed query.
type Filter struct {
	terms []term
}

// Parse parses a query. Unknown keys are treated as title words.
func Parse(query string) Filter {
	var f Filter
	for _, tok := range tokenize(query) {
		var t term
		if len(tok) > 1 && tok[0] == '-' {
			t.negate, tok = true, tok[1:]
		}
		if k, v, ok := strings.Cut(tok, ":"); ok && slices.Contains(keys, strings.ToLower(k)) {
			t.key, t.value = strings.ToLower(k), strings.ToLower(v)
			if t.key == "project" {
				t.value, t.sections = splitSections(t.value)
			}
		} else {
			t.value = strings.ToLower(tok)
		}
		f.terms = append(f.terms, t)
	}
	return f
}

// splitSections splits "name[a|b]" into name and its "|"-separated sections,
// dropping empty ones. Values without a trailing bracketed list are returned
// whole.
func splitSections(v string) (name string, sections []string) {
	i := strings.LastIndex(v, "[")
	if i < 0 || !strings.HasSuffix(v, "]") {
		return v, nil
	}
	for _, s := range strings.Split(v[i+1:len(v)-1], "|") {
		if s = strings.TrimSpace(s); s != "" {
			sections = append(sections, s)
		}
	}
	return v[:i], sections
}

func tokenize(query string) []string {
	var toks []string
	var cur strings.Builder
	quoted := false
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range query {
		switch {
		case r == '"':
			quoted = !quoted
		case unicode.IsSpace(r) && !quoted:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return toks
}

// Env is what terms need beyond the task. Today places due dates and Me is
// the user's gid for assignee:@me. AgentStates returns the states of the
// agents linked to a task and HasBranch whether it has an explicit branch;
// nil lookups mean none.
type Env struct {
	Today       time.Time
	Me          string
	AgentStates func(asana.Task) []string
	HasBranch   func(asana.Task) bool
}

// Match reports whether t satisfies every term.
func (f Filter) Match(t asana.Task, env Env) bool {
	var states []string
	if env.AgentStates != nil {
		states = env.AgentStates(t)
	}
	for _, tm := range f.terms {
		if tm.match(t, states, env) == tm.negate {
			return false
		}
	}
	return true
}

// Apply returns the tasks that match, in their original order.
func (f Filter) Apply(tasks []asana.Task, env Env) []asana.Task {
	var out []asana.Task
	for _, t := range tasks {
		if f.Match(t, env) {
			out = append(out, t)
		}
	}
	return out
}

// UsesMe reports whether a term needs Env.Me.
func (f Filter) UsesMe() bool {
	return slices.ContainsFunc(f.terms, func(tm term) bool { return tm.key == "assignee" && tm.value == "@me" })
}

func (tm term) match(t asana.Task, agentStates []string, env Env) bool {
	switch tm.key {
	case "agent":
		switch tm.value {
		case "any":
			return len(agentStates) > 0
		case "none":
			return len(agentStates) == 0
		}
		for _, s := range agentStates {
			if s == tm.value {
				return true
			}
		}
		return false
	case "":
		return contains(t.Name, tm.value)
	case "is":
		switch tm.value {
		case "open":
			return !t.Completed
		case "done":
			return t.Completed
		}
		return false
	case "due":
		_, days, ok := asana.DueDays(t.DueOn, env.Today)
		switch tm.value {
		case "none":
			return !ok
		case "overdue":
			return ok && days < 0
		case "today":
			return ok && days == 0
		case "week":
			return ok && days >= 0 && days < 7
		}
		return false
	case "assignee":
		switch tm.value {
		case "@me":
			return t.Assignee != nil && env.Me != "" && t.Assignee.GID == env.Me
		case "none":
			return t.Assignee == nil || t.Assignee.GID == ""
		}
		return t.Assignee != nil && contains(t.Assignee.Name, tm.value)
	case "branch":
		has := env.HasBranch != nil && env.HasBranch(t)
		switch tm.value {
		case "any":
			return has
		case "none":
			return !has
		}
		return false
	case "tag":
		for _, tag := range t.Tags {
			if contains(tag.Name, tm.value) {
				return true
			}
		}
		return false
	case "project":
		for _, m := range t.Memberships {
			if contains(m.Project.Name, tm.value) &&
				(tm.sections == nil || m.Section != nil && slices.Contains(tm.sections, strings.ToLower(m.Section.Name))) {
				return true
			}
		}
		return false
	case "section":
		if t.AssigneeSection != nil && contains(t.AssigneeSection.Name, tm.value) {
			return true
		}
		for _, m := range t.Memberships {
			if m.Section != nil && contains(m.Section.Name, tm.value) {
				return true
			}
		}
		return false
	}
	return false
}

func contains(s, lowerSub string) bool {
	return strings.Contains(strings.ToLower(s), lowerSub)
}
