// Package filter implements the ticket list query language.
//
// A query is space-separated terms that must all match. Bare words match the
// title. section:, project:, assignee:, and tag: match names by substring;
// assignee:@me matches tickets assigned to the user and assignee:none
// unassigned ones.
// project:<name>[<section>] matches a section within that project only, by
// exact name; project:<name>[<a>|<b>] matches any of the listed sections.
// is:open and is:done match completion. due:overdue, due:today, due:week (the
// next 7 days, today included), and due:none match the due date.
// blocked:any matches a ticket waiting on an open dependency, and
// blocked:none one that is not.
//
// The other keys read values the caller supplies through Env.Lookup: agent
// (the linked agents' states), branch (a branch saved for the ticket or set
// in its branch field), and git (dirty or unpushed work on that branch).
// <key>:any matches a ticket with a value, <key>:none one without, and
// <key>:<value> one with that value.
//
// A leading "-" negates a term, and double quotes group words.
package filter

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/sadmachine/asanamate/internal/asana"
)

var keys = []string{"section", "project", "assignee", "tag", "is", "due", "blocked", "agent", "branch", "git"}

// Keys whose values come from Env.Lookup.
const (
	KeyAgent  = "agent"
	KeyBranch = "branch"
	KeyGit    = "git"
)

// lookupKeys are the keys matched against Env.Lookup values.
var lookupKeys = []string{KeyAgent, KeyBranch, KeyGit}

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
// the user's gid for assignee:@me. Lookup returns a task's values for a
// lookup key, such as KeyAgent; a missing lookup means no values.
type Env struct {
	Today  time.Time
	Me     string
	Lookup map[string]func(asana.Task) []string
}

// values returns t's values for key, lowercased.
func (env Env) values(key string, t asana.Task) []string {
	lookup := env.Lookup[key]
	if lookup == nil {
		return nil
	}
	var values []string
	for _, v := range lookup(t) {
		values = append(values, strings.ToLower(v))
	}
	return values
}

// Validate reports the first term whose key is not a filter key, such as a
// misspelled "agnet:waiting", which Parse would take as title words.
func Validate(query string) error {
	for _, tok := range tokenize(query) {
		tok = strings.TrimPrefix(tok, "-")
		if k, _, ok := strings.Cut(tok, ":"); ok && !slices.Contains(keys, strings.ToLower(k)) {
			return fmt.Errorf("unknown filter key %q in %q", k, tok)
		}
	}
	return nil
}

// Match reports whether t satisfies every term.
func (f Filter) Match(t asana.Task, env Env) bool {
	for _, tm := range f.terms {
		if tm.match(t, env) == tm.negate {
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

func (tm term) match(t asana.Task, env Env) bool {
	if slices.Contains(lookupKeys, tm.key) {
		values := env.values(tm.key, t)
		switch tm.value {
		case "any":
			return len(values) > 0
		case "none":
			return len(values) == 0
		}
		return slices.Contains(values, tm.value)
	}
	switch tm.key {
	case "blocked":
		blocked := slices.ContainsFunc(t.Dependencies, func(d asana.Ref) bool { return !d.Completed })
		switch tm.value {
		case "any":
			return blocked
		case "none":
			return !blocked
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
