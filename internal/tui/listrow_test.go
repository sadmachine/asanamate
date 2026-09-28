package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
	got := rowFields(fieldTask, names, rowContext{})
	want := []string{"Today", "open", "In Review", "feat/x", "due 2026-10-01", "Ann", "Web", "bug, p1"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if got := rowFields(fieldTask, []string{"section"}, rowContext{projectGID: "p"}); !slices.Equal(got, []string{"Doing"}) {
		t.Fatalf("project view section: %q", got)
	}
}

func TestListViewLayouts(t *testing.T) {
	other := asana.Task{GID: "2", Name: "Fix footer"}
	single, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Fields: []string{"section", "due"}}})
	single.Update(tasksMsg{tasks: []asana.Task{fieldTask, other}})
	if got := strings.Split(ansi.Strip(single.listView(80, 5)), "\n"); len(got) != 2 || got[0] != "□ Fix login  Today · due 2026-10-01" {
		t.Fatalf("single = %q", got)
	}

	multi, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutMulti, Fields: []string{"section", "due"}}})
	multi.Update(tasksMsg{tasks: []asana.Task{fieldTask, other}})
	got := strings.Split(ansi.Strip(multi.listView(80, 4)), "\n")
	if want := []string{"□ Fix login", "  Today · due 2026-10-01", "□ Fix footer", ""}; !slices.Equal(got, want) {
		t.Fatalf("multi = %q, want %q", got, want)
	}
	multi.moveTo(1)
	if got := strings.Split(ansi.Strip(multi.listView(80, 3)), "\n"); got[0] != "□ Fix footer" {
		t.Fatalf("multi scroll keeps the cursor's whole row visible: %q", got)
	}
}

func TestListViewSeparator(t *testing.T) {
	a, b, c := asana.Task{GID: "1", Name: "A"}, asana.Task{GID: "2", Name: "B"}, asana.Task{GID: "3", Name: "C"}
	single, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Separator: true}})
	single.Update(tasksMsg{tasks: []asana.Task{a, b, c}})
	if got := strings.Split(ansi.Strip(single.listView(4, 5)), "\n"); !slices.Equal(got, []string{"────", "□ A", "────", "□ B", "────"}) {
		t.Fatalf("single = %q", got)
	}
	multi, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutMulti, Separator: true}})
	multi.Update(tasksMsg{tasks: []asana.Task{a, b, c}})
	multi.moveTo(2)
	if got := strings.Split(ansi.Strip(multi.listView(3, 7)), "\n"); !slices.Equal(got, []string{"───", "□ B", "", "───", "□ C", "", "───"}) {
		t.Fatalf("multi scrolled = %q", got)
	}
}

func TestGroupTasks(t *testing.T) {
	today := time.Date(2026, 9, 28, 23, 30, 0, 0, time.Local)
	due := func(gid, on string) asana.Task {
		task := asana.Task{GID: gid}
		if on != "" {
			task.DueOn = strp(on)
		}
		return task
	}
	tasks := []asana.Task{due("1", "2026-10-20"), due("2", ""), due("3", "2026-09-28"), due("4", "2026-09-27"), due("5", "2026-10-02"), due("6", "2026-09-29")}
	got, groups := groupTasks(tasks, "due", rowContext{}, today)
	var gids []string
	for _, task := range got {
		gids = append(gids, task.GID)
	}
	if want := []string{"4", "3", "6", "5", "1", "2"}; !slices.Equal(gids, want) {
		t.Fatalf("due order = %q, want %q", gids, want)
	}
	if want := []string{"Overdue", "Today", "Tomorrow", "Next 7 days", "Later", "No due date"}; !slices.Equal(groups, want) {
		t.Fatalf("due groups = %q", groups)
	}

	sec := func(gid, name string) asana.Task {
		task := asana.Task{GID: gid}
		if name != "" {
			task.AssigneeSection = &asana.Ref{Name: name}
		}
		return task
	}
	_, groups = groupTasks([]asana.Task{sec("1", "Later"), sec("2", ""), sec("3", "Today"), sec("4", "Later")}, "section", rowContext{}, today)
	if want := []string{"Later", "Later", "Today", "No section"}; !slices.Equal(groups, want) {
		t.Fatalf("section groups = %q, want first-seen order with empty last", groups)
	}
	if got, groups := groupTasks(tasks, "", rowContext{}, today); groups != nil || len(got) != len(tasks) {
		t.Fatal("empty group_by must leave tasks ungrouped")
	}
}

func TestListViewGroups(t *testing.T) {
	sec := func(gid, name, section string) asana.Task {
		return asana.Task{GID: gid, Name: name, AssigneeSection: &asana.Ref{Name: section}}
	}
	tasks := []asana.Task{sec("1", "A", "Doing"), sec("2", "B", "Next"), sec("3", "C", "Doing")}
	m, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Separator: true, GroupBy: "section"}})
	m.Update(tasksMsg{tasks: tasks})
	got := strings.Split(ansi.Strip(m.listView(24, 10)), "\n")
	want := []string{
		"── Doing (2) ───────────",
		"□ A",
		strings.Repeat("─", 24),
		"□ C",
		"── Next (1) ────────────",
		"□ B",
		strings.Repeat("─", 24),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("grouped =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	m.moveTo(2)
	if got := strings.Split(ansi.Strip(m.listView(24, 3)), "\n"); got[0] != "── Next (1) ────────────" || got[1] != "□ B" {
		t.Fatalf("scrolled view keeps the cursor's group header: %q", got)
	}
	m.moveTo(1)
	if got := strings.Split(ansi.Strip(m.listView(24, 4)), "\n"); got[0] != "── Doing (2) ───────────" || got[1] != "□ C" {
		t.Fatalf("first row shown mid-group gets its header: %q", got)
	}
}

func TestGroupPicker(t *testing.T) {
	m, _ := testModel(t, config.Config{List: config.List{Fields: []string{"Section", "Status"}, GroupBy: "section"}})
	m.Update(tasksMsg{tasks: []asana.Task{
		{GID: "1", AssigneeSection: &asana.Ref{Name: "Later"}, CustomFields: []asana.CustomField{{Name: "Priority"}}},
		{GID: "2", AssigneeSection: &asana.Ref{Name: "Today"}},
		{GID: "3", AssigneeSection: &asana.Ref{Name: "Later"}},
	}})
	m.moveTo(2)
	press := func(s string) {
		for _, r := range s {
			m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
	press("b")
	var labels []string
	for _, it := range m.modal.items {
		labels = append(labels, it.Label)
	}
	if want := []string{"None", "section", "due", "assignee", "project", "tags", "completed", "Status", "Priority"}; !slices.Equal(labels, want) {
		t.Fatalf("options = %q, want %q", labels, want)
	}
	if it := m.modal.items[m.modal.matches[m.modal.cursor]]; it.Hint != "current" || it.Value != "section" {
		t.Fatalf("picker opens on %+v, want the current grouping", it)
	}
	press("none")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if sel, _ := m.selected(); m.modal != nil || m.groupBy != "" || m.groups != nil || sel.GID != "2" {
		t.Fatalf("groupBy = %q, groups = %q, selected = %q", m.groupBy, m.groups, sel.GID)
	}
}
