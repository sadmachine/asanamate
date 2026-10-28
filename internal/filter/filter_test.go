package filter

import (
	"testing"
	"time"

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
		{"project:web[progress]", ""},
		{`project:web["in progress"]`, "a"},
		{`project:web["In Progress"]`, "a"},
		{`project:web[done|"in progress"]`, "a"},
		{"project:web[done|backlog]", ""},
		{"project:[done|backlog]", "b"},
		{`-project:web[backlog|"in progress"]`, "b"},
		{"project:web[done]", ""},
		{"project:docs[done]", "b"},
		{"-project:web[progress]", "ab"},
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
			if f.Match(task, Env{}) {
				got += task.GID
			}
		}
		if got != c.want {
			t.Errorf("Parse(%q) matched %q, want %q", c.query, got, c.want)
		}
	}
}

func TestApplyKeepsOrder(t *testing.T) {
	got := Parse("").Apply([]asana.Task{done, open}, Env{})
	if len(got) != 2 || got[0].GID != "b" || got[1].GID != "a" {
		t.Fatalf("got %+v", got)
	}
	if got := Parse("is:done").Apply([]asana.Task{open}, Env{}); len(got) != 0 {
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
		for _, task := range Parse(q).Apply([]asana.Task{open, done}, Env{AgentStates: states}) {
			got += task.GID
		}
		if got != want {
			t.Errorf("%q matched %q, want %q", q, got, want)
		}
	}
}

func TestDueTerms(t *testing.T) {
	today := time.Date(2026, 9, 28, 12, 0, 0, 0, time.Local)
	due := func(gid, on string) asana.Task { return asana.Task{GID: gid, DueOn: &on} }
	tasks := []asana.Task{due("o", "2026-09-27"), due("t", "2026-09-28"), due("w", "2026-10-04"), due("l", "2026-10-05"), {GID: "n"}}
	for q, want := range map[string]string{"due:overdue": "o", "due:today": "t", "due:week": "tw", "due:none": "n", "-due:none": "otwl", "due:bogus": ""} {
		got := ""
		for _, task := range Parse(q).Apply(tasks, Env{Today: today}) {
			got += task.GID
		}
		if got != want {
			t.Errorf("%q matched %q, want %q", q, got, want)
		}
	}
}

func TestAssigneeAndBranchTerms(t *testing.T) {
	mine := asana.Task{GID: "m", Assignee: &asana.Ref{GID: "u1", Name: "Amy Nonez"}}
	theirs := asana.Task{GID: "t", Assignee: &asana.Ref{GID: "u2", Name: "Ann Lee"}}
	nobody := asana.Task{GID: "n"}
	tasks := []asana.Task{mine, theirs, nobody}
	env := Env{Me: "u1", HasBranch: func(t asana.Task) bool { return t.GID == "t" }}
	for q, want := range map[string]string{
		"assignee:@me":                 "m",
		"-assignee:@me":                "tn",
		"assignee:none":                "n",
		"-assignee:none":               "mt",
		"-assignee:@me -assignee:none": "t",
		"assignee:ann":                 "t",
		"assignee:nonez":               "m",
		"branch:any":                   "t",
		"branch:none":                  "mn",
		"-branch:none":                 "t",
		"branch:bogus":                 "",
	} {
		got := ""
		for _, task := range Parse(q).Apply(tasks, env) {
			got += task.GID
		}
		if got != want {
			t.Errorf("%q matched %q, want %q", q, got, want)
		}
	}
	if got := Parse("assignee:@me").Apply(tasks, Env{}); len(got) != 0 {
		t.Errorf("assignee:@me without a user matched %+v", got)
	}
	if got := Parse("branch:none").Apply(tasks, Env{}); len(got) != 3 {
		t.Errorf("branch:none without a lookup matched %+v", got)
	}
	if !Parse("is:open assignee:@me").UsesMe() || Parse("assignee:me").UsesMe() {
		t.Error("UsesMe")
	}
}
