package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sadmachine/asanamate/internal/filter"
)

// Grouping is a named way to group the list by rules. Each ticket joins the
// first section with a match query it satisfies; tickets no section matches
// go to OtherSection, last.
type Grouping struct {
	Name     string            `toml:"name"`
	Sections []GroupingSection `toml:"sections"`
}

// GroupingSection is one group of a Grouping. Match lists list filter
// queries; a ticket matching any of them belongs here.
type GroupingSection struct {
	Label string   `toml:"label"`
	Match []string `toml:"match"`
}

// OtherSection labels the tickets no section of a grouping matches.
const OtherSection = "Other"

// builtinGroupings are always available unless a configured grouping takes
// the same name.
var builtinGroupings = []Grouping{{Name: "state", Sections: []GroupingSection{
	{Label: "Needs me", Match: []string{"agent:waiting"}},
	{Label: "Active", Match: []string{"agent:working", "git:dirty", "git:unpushed"}},
	{Label: "Blocked", Match: []string{"blocked:any"}},
	{Label: "Ready", Match: []string{"is:open"}},
}}}

// AllGroupings returns the configured groupings, then the built-in ones whose
// names they do not take.
func (c Config) AllGroupings() []Grouping {
	all := slices.Clone(c.Groupings)
	for _, b := range builtinGroupings {
		if !slices.ContainsFunc(c.Groupings, func(g Grouping) bool { return strings.EqualFold(g.Name, b.Name) }) {
			all = append(all, b)
		}
	}
	return all
}

// Grouping returns the grouping named name, ignoring case.
func (c Config) Grouping(name string) (Grouping, bool) {
	all := c.AllGroupings()
	i := slices.IndexFunc(all, func(g Grouping) bool { return strings.EqualFold(g.Name, name) })
	if i < 0 {
		return Grouping{}, false
	}
	return all[i], true
}

// validate checks a grouping; errors start with the key they name.
func (g Grouping) validate(seen []string) error {
	name := strings.TrimSpace(g.Name)
	switch {
	case name == "":
		return errors.New("name: must not be empty")
	case name != g.Name:
		return fmt.Errorf("name: %q must not have surrounding whitespace", g.Name)
	case slices.ContainsFunc(seen, func(s string) bool { return strings.EqualFold(s, name) }):
		return fmt.Errorf("name: %q is already used by another grouping", name)
	case len(g.Sections) == 0:
		return errors.New("sections: must list at least one section")
	}
	var labels []string
	for i, s := range g.Sections {
		label := strings.TrimSpace(s.Label)
		switch {
		case label == "":
			return fmt.Errorf("sections[%d].label: must not be empty", i+1)
		case strings.EqualFold(label, OtherSection):
			return fmt.Errorf("sections[%d].label: %q is reserved for tickets no section matches", i+1, label)
		case slices.ContainsFunc(labels, func(l string) bool { return strings.EqualFold(l, label) }):
			return fmt.Errorf("sections[%d].label: %q is already used in this grouping", i+1, label)
		case len(s.Match) == 0:
			return fmt.Errorf("sections[%d].match: must list at least one filter query", i+1)
		}
		labels = append(labels, label)
		for j, q := range s.Match {
			if strings.TrimSpace(q) == "" {
				return fmt.Errorf("sections[%d].match[%d]: must not be empty", i+1, j+1)
			}
			if err := filter.Validate(q); err != nil {
				return fmt.Errorf("sections[%d].match[%d]: %w", i+1, j+1, err)
			}
		}
	}
	return nil
}
