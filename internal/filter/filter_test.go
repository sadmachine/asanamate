package filter

import (
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
)

var (
	open = asana.Task{
		GID: "a", Name: "Fix login bug",
		Assignee:        &asana.Ref{Name: "Ann Lee"},
		Tags:            []asana.Ref{{Name: "bug"}},
		AssigneeSection: &asana.Ref{Name: "Today"},
		Memberships:     []asana.Membership{{Project: asana.Ref{Name: "Web"}, Section: &asana.Ref{Name: "In Progress"}}},
	}
	done = asana.Task{
		GID: "b", Name: "Write docs", Completed: true,
		Memberships: []asana.Membership{{Project: asana.Ref{Name: "Docs"}, Section: &asana.Ref{Name: "Done"}}},
	}
)

func TestMatch(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{"", "ab"},
		{"LOGIN", "a"},
		{"is:open", "a"},
		{"is:done", "b"},
		{"-is:done", "a"},
		{`section:"in progress"`, "a"},
		{"section:today", "a"},
		{"-section:done", "a"},
		{"project:docs", "b"},
		{"assignee:ann", "a"},
		{"tag:bug", "a"},
		{"is:open fix", "a"},
		{"is:open docs", ""},
		{"is:bogus", ""},
		{"unknown:thing", ""},
	}
	for _, c := range cases {
		f := Parse(c.query)
		got := ""
		for _, task := range []asana.Task{open, done} {
			if f.Match(task, nil) {
				got += task.GID
			}
		}
		if got != c.want {
			t.Errorf("Parse(%q) matched %q, want %q", c.query, got, c.want)
		}
	}
}

func TestApplyKeepsOrder(t *testing.T) {
	got := Parse("").Apply([]asana.Task{done, open}, nil)
	if len(got) != 2 || got[0].GID != "b" || got[1].GID != "a" {
		t.Fatalf("got %+v", got)
	}
	if got := Parse("is:done").Apply([]asana.Task{open}, nil); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestAgentTerms(t *testing.T) {
	states := func(t asana.Task) []string {
		if t.GID == "a" {
			return []string{"waiting", "working"}
		}
		return nil
	}
	for q, want := range map[string]string{"agent:any": "a", "agent:none": "b", "agent:waiting": "a", "agent:idle": "", "-agent:any": "b"} {
		got := ""
		for _, task := range Parse(q).Apply([]asana.Task{open, done}, states) {
			got += task.GID
		}
		if got != want {
			t.Errorf("%q matched %q, want %q", q, got, want)
		}
	}
}
