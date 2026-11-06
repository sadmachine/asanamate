package config

import (
	"strings"
	"testing"
)

func TestBuiltinGroupingsAreValid(t *testing.T) {
	for _, g := range builtinGroupings {
		if err := g.validate(nil); err != nil {
			t.Errorf("built-in grouping %s: %v", g.Name, err)
		}
	}
}

func TestGroupingsOverrideBuiltinsByName(t *testing.T) {
	var c Config
	if g, ok := c.Grouping("STATE"); !ok || g.Sections[0].Label != "Needs me" {
		t.Fatalf("built-in state grouping = %+v, %v", g, ok)
	}
	c.Groupings = []Grouping{
		{Name: "State", Sections: []GroupingSection{{Label: "Mine", Match: []string{"assignee:@me"}}}},
		{Name: "review", Sections: []GroupingSection{{Label: "Waiting", Match: []string{"agent:waiting"}}}},
	}
	all := c.AllGroupings()
	if len(all) != 3 || all[2].Name != NowGrouping || all[0].Sections[0].Label != "Mine" || all[1].Name != "review" {
		t.Fatalf("a configured grouping must replace the built-in one of its name: %+v", all)
	}
}

func TestGroupingsValidation(t *testing.T) {
	section := func(label string, match ...string) GroupingSection {
		return GroupingSection{Label: label, Match: match}
	}
	ok := Grouping{Name: "review", Sections: []GroupingSection{section("Waiting", "agent:waiting", "git:dirty -is:done")}}
	for _, tc := range []struct {
		groupings []Grouping
		want      string // "" for valid
	}{
		{[]Grouping{ok}, ""},
		{[]Grouping{ok, {Name: "Review", Sections: ok.Sections}}, `groupings[2].name: "Review" is already used`},
		{[]Grouping{{Name: " x", Sections: ok.Sections}}, "groupings[1].name:"},
		{[]Grouping{{Name: "x"}}, "groupings[1].sections: must list"},
		{[]Grouping{{Name: "x", Sections: []GroupingSection{section("", "is:open")}}}, "groupings[1].sections[1].label: must not be empty"},
		{[]Grouping{{Name: "x", Sections: []GroupingSection{section("other", "is:open")}}}, "groupings[1].sections[1].label: \"other\" is reserved"},
		{[]Grouping{{Name: "x", Sections: []GroupingSection{section("A", "is:open"), section("a", "is:done")}}}, "groupings[1].sections[2].label:"},
		{[]Grouping{{Name: "x", Sections: []GroupingSection{section("A")}}}, "groupings[1].sections[1].match: must list"},
		{[]Grouping{{Name: "x", Sections: []GroupingSection{section("A", " ")}}}, "groupings[1].sections[1].match[1]: must not be empty"},
		{[]Grouping{{Name: "x", Sections: []GroupingSection{section("A", "is:open", "agnet:waiting")}}}, `groupings[1].sections[1].match[2]: unknown filter key "agnet"`},
	} {
		c := Default()
		c.Workspace = "1"
		c.Groupings = tc.groupings
		err := c.validate()
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("groupings %+v: error = %v, want %q", tc.groupings, err, tc.want)
		}
	}
}

func TestGroupingFoldedSections(t *testing.T) {
	var c Config
	now, ok := c.Grouping(NowGrouping)
	if !ok {
		t.Fatal("no built-in now grouping")
	}
	if got := now.FoldedSections(); len(got) != 1 || got[0] != "Waiting on others" {
		t.Fatalf("now folds %v", got)
	}
}
