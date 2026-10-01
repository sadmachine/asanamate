// Package filter implements the ticket list query language.
//
// A query is space-separated terms that must all match. Bare words match the
// title. section:, project:, assignee:, and tag: match names by substring.
// project:<name>[<section>] matches a section within that project only.
// is:open and is:done match completion. agent:any, agent:none, and
// agent:<state> match linked agents. A leading "-" negates a term, and double
// quotes group words.
package filter

import (
	"slices"
	"strings"
	"unicode"

	"github.com/sadmachine/asanamate/internal/asana"
)

var keys = []string{"section", "project", "assignee", "tag", "is", "agent"}

// Keys returns the filter fields in display order.
func Keys() []string { return slices.Clone(keys) }

type term struct {
	key, value string
	section    string // project:<value>[<section>]; empty matches any section
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
				t.value, t.section = splitSection(t.value)
			}
		} else {
			t.value = strings.ToLower(tok)
		}
		f.terms = append(f.terms, t)
	}
	return f
}

// splitSection splits "name[section]" into name and section. Values without a
// trailing bracketed section are returned whole.
func splitSection(v string) (name, section string) {
	i := strings.LastIndex(v, "[")
	if i < 0 || !strings.HasSuffix(v, "]") {
		return v, ""
	}
	return v[:i], v[i+1 : len(v)-1]
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

// Match reports whether t satisfies every term. agentStates are the states of
// the agents linked to t.
func (f Filter) Match(t asana.Task, agentStates []string) bool {
	for _, tm := range f.terms {
		if tm.match(t, agentStates) == tm.negate {
			return false
		}
	}
	return true
}

// Apply returns the tasks that match, in their original order. agentStates
// may be nil when agents are not tracked.
func (f Filter) Apply(tasks []asana.Task, agentStates func(asana.Task) []string) []asana.Task {
	var out []asana.Task
	for _, t := range tasks {
		var states []string
		if agentStates != nil {
			states = agentStates(t)
		}
		if f.Match(t, states) {
			out = append(out, t)
		}
	}
	return out
}

func (tm term) match(t asana.Task, agentStates []string) bool {
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
	case "assignee":
		return t.Assignee != nil && contains(t.Assignee.Name, tm.value)
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
				(tm.section == "" || m.Section != nil && contains(m.Section.Name, tm.section)) {
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
