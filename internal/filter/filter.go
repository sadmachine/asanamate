// Package filter implements the ticket list query language.
//
// A query is space-separated terms that must all match. Bare words match the
// title. section:, project:, assignee:, and tag: match names by substring.
// is:open and is:done match completion. A leading "-" negates a term, and
// double quotes group words.
package filter

import (
	"strings"
	"unicode"

	"github.com/sadmachine/asanamate/internal/asana"
)

var keys = map[string]bool{"section": true, "project": true, "assignee": true, "tag": true, "is": true}

type term struct {
	key, value string
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
		if k, v, ok := strings.Cut(tok, ":"); ok && keys[strings.ToLower(k)] {
			t.key, t.value = strings.ToLower(k), strings.ToLower(v)
		} else {
			t.value = strings.ToLower(tok)
		}
		f.terms = append(f.terms, t)
	}
	return f
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

// Match reports whether t satisfies every term.
func (f Filter) Match(t asana.Task) bool {
	for _, tm := range f.terms {
		if tm.match(t) == tm.negate {
			return false
		}
	}
	return true
}

// Apply returns the tasks that match, in their original order.
func (f Filter) Apply(tasks []asana.Task) []asana.Task {
	var out []asana.Task
	for _, t := range tasks {
		if f.Match(t) {
			out = append(out, t)
		}
	}
	return out
}

func (tm term) match(t asana.Task) bool {
	switch tm.key {
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
			if contains(m.Project.Name, tm.value) {
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
