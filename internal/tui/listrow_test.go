package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
)

func strp(s string) *string { return &s }

var fieldTask = asana.Task{
	GID: "1", Name: "Fix login", DueOn: strp("2026-10-01"),
	Assignee:        &asana.Ref{Name: "Ann"},
	AssigneeSection: &asana.Ref{Name: "Today"},
	Tags:            []asana.Ref{{Name: "bug"}, {Name: "p1"}},
	Memberships:     []asana.Membership{{Project: asana.Ref{GID: "p", Name: "Web"}, Section: &asana.Ref{Name: "Doing"}}},
	CustomFields: []asana.CustomField{
		{Name: "Status", DisplayValue: strp("In\tReview")},
		{Name: "Branch Name ", DisplayValue: strp("")},
		{Name: "Branch name", DisplayValue: strp("feat/x\x1b")},
		{Name: "Empty"},
	},
}

func TestRowFields(t *testing.T) {
	names := []string{"section", "completed", "STATUS", "branch name", "due", "assignee", "project", "tags", "empty", "missing"}
	got := rowFields(fieldTask, names, "")
	want := []string{"Today", "open", "In Review", "feat/x", "due 2026-10-01", "Ann", "Web", "bug, p1"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if got := rowFields(fieldTask, []string{"section"}, "p"); !slices.Equal(got, []string{"Doing"}) {
		t.Fatalf("project view section: %q", got)
	}
}

func TestListViewLayouts(t *testing.T) {
	other := asana.Task{GID: "2", Name: "Fix footer"}
	single, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Fields: []string{"section", "due"}}})
	single.Update(tasksMsg{tasks: []asana.Task{fieldTask, other}})
	if got := strings.Split(ansi.Strip(single.listView(80, 5)), "\n"); len(got) != 2 || got[0] != "○ Fix login  Today · due 2026-10-01" {
		t.Fatalf("single = %q", got)
	}

	multi, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutMulti, Fields: []string{"section", "due"}}})
	multi.Update(tasksMsg{tasks: []asana.Task{fieldTask, other}})
	got := strings.Split(ansi.Strip(multi.listView(80, 4)), "\n")
	if want := []string{"○ Fix login", "  Today · due 2026-10-01", "○ Fix footer", ""}; !slices.Equal(got, want) {
		t.Fatalf("multi = %q, want %q", got, want)
	}
	multi.moveTo(1)
	if got := strings.Split(ansi.Strip(multi.listView(80, 3)), "\n"); got[0] != "○ Fix footer" {
		t.Fatalf("multi scroll keeps the cursor's whole row visible: %q", got)
	}
}

func TestListViewSeparator(t *testing.T) {
	a, b, c := asana.Task{GID: "1", Name: "A"}, asana.Task{GID: "2", Name: "B"}, asana.Task{GID: "3", Name: "C"}
	single, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Separator: true}})
	single.Update(tasksMsg{tasks: []asana.Task{a, b, c}})
	if got := strings.Split(ansi.Strip(single.listView(4, 4)), "\n"); !slices.Equal(got, []string{"○ A", "────", "○ B"}) {
		t.Fatalf("single = %q", got)
	}
	multi, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutMulti, Separator: true}})
	multi.Update(tasksMsg{tasks: []asana.Task{a, b, c}})
	multi.moveTo(2)
	if got := strings.Split(ansi.Strip(multi.listView(3, 5)), "\n"); !slices.Equal(got, []string{"○ B", "", "───", "○ C", ""}) {
		t.Fatalf("multi scrolled = %q", got)
	}
}
