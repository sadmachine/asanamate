package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// listLines renders the list as plain lines without the trailing padding of
// highlighted rows.
func listLines(m *Model, width, height int) []string {
	lines := strings.Split(ansi.Strip(m.listView(width, height)), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}

func TestSelectedRowFillsWidth(t *testing.T) {
	m, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle}})
	m.Update(tasksMsg{tasks: []asana.Task{{GID: "1", Name: "A"}, {GID: "2", Name: "B"}}})
	got := strings.Split(m.listView(10, 2), "\n")
	if want := selectedStyle.Render("□ A       "); got[0] != want {
		t.Fatalf("selected = %q, want %q", got[0], want)
	}
	if ansi.Strip(got[1]) != "□ B" {
		t.Fatalf("unselected rows are not padded: %q", got[1])
	}
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
	if got := listLines(single, 80, 5); len(got) != 2 || got[0] != "□ Fix login"+strings.Repeat(" ", 59)+"Today  Thu" {
		t.Fatalf("single = %q", got)
	}

	multi, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutMulti, Fields: []string{"section", "due"}}})
	multi.Update(tasksMsg{tasks: []asana.Task{fieldTask, other}})
	got := listLines(multi, 80, 4)
	if want := []string{"□ Fix login", "  Today · Thu", "□ Fix footer", ""}; !slices.Equal(got, want) {
		t.Fatalf("multi = %q, want %q", got, want)
	}
	multi.moveTo(1)
	if got := listLines(multi, 80, 3); got[0] != "□ Fix footer" {
		t.Fatalf("multi scroll keeps the cursor's whole row visible: %q", got)
	}
}

func TestListViewSeparator(t *testing.T) {
	a, b, c := asana.Task{GID: "1", Name: "A"}, asana.Task{GID: "2", Name: "B"}, asana.Task{GID: "3", Name: "C"}
	single, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Separator: true}})
	single.Update(tasksMsg{tasks: []asana.Task{a, b, c}})
	if got := listLines(single, 4, 5); !slices.Equal(got, []string{"────", "□ A", "────", "□ B", "────"}) {
		t.Fatalf("single = %q", got)
	}
	multi, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutMulti, Separator: true}})
	multi.Update(tasksMsg{tasks: []asana.Task{a, b, c}})
	multi.moveTo(2)
	if got := listLines(multi, 3, 7); !slices.Equal(got, []string{"───", "□ B", "", "───", "□ C", "", "───"}) {
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
	got := listLines(m, 24, 10)
	want := []string{
		" Doing (2)",
		"□ A",
		strings.Repeat("─", 24),
		"□ C",
		" Next (1)",
		"□ B",
		strings.Repeat("─", 24),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("grouped =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	m.moveTo(2)
	if got := listLines(m, 24, 3); got[0] != " Next (1)" || got[1] != "□ B" {
		t.Fatalf("scrolled view keeps the cursor's group header: %q", got)
	}
	m.moveTo(1)
	if got := listLines(m, 24, 4); got[0] != " Doing (2)" || got[1] != "□ C" {
		t.Fatalf("first row shown mid-group gets its header: %q", got)
	}
	m.deps.Config.List.Header.Spacing = 1
	m.moveTo(0)
	got = listLines(m, 24, 10)
	want = []string{" Doing (2)", "", "□ A", strings.Repeat("─", 24), "□ C", "", " Next (1)", "", "□ B", strings.Repeat("─", 24)}
	if !slices.Equal(got, want) {
		t.Fatalf("spaced =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	m.moveTo(2)
	if got := listLines(m, 24, 4); got[0] != " Next (1)" || got[1] != "" || got[2] != "□ B" {
		t.Fatalf("header topping the view gets no spacing above: %q", got)
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

func TestSelectionMarker(t *testing.T) {
	m, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Selection: config.Selection{Style: config.StyleMarker}}})
	m.Update(tasksMsg{tasks: []asana.Task{{GID: "1", Name: "A"}, {GID: "2", Name: "B"}}})
	got := strings.Split(m.listView(10, 2), "\n")
	if want := m.markerStyle.Render("▌ ") + titleStyle.Render("□ A"); got[0] != want {
		t.Fatalf("selected = %q, want %q", got[0], want)
	}
	if got[1] != "  □ B" {
		t.Fatalf("unselected rows keep the gutter: %q", got[1])
	}
	m.focusReader = true
	if got := strings.Split(m.listView(10, 2), "\n")[0]; !strings.HasPrefix(got, dimStyle.Render("▌ ")) {
		t.Fatalf("reader focus dims the marker: %q", got)
	}
}

func TestHeaderStyles(t *testing.T) {
	tasks := []asana.Task{{GID: "1", Name: "A", AssigneeSection: &asana.Ref{Name: "Doing"}}}
	rule, _ := testModel(t, config.Config{List: config.List{GroupBy: "section", Header: config.Header{Style: config.StyleRule, Color: "5"}}})
	rule.Update(tasksMsg{tasks: tasks})
	if got := listLines(rule, 20, 2)[0]; got != "── Doing (1) ───────" {
		t.Fatalf("rule header = %q", got)
	}
	bar, _ := testModel(t, config.Config{AccentColor: "2", List: config.List{GroupBy: "section", Header: config.Header{Color: "#ff0000"}}})
	bar.Update(tasksMsg{tasks: tasks})
	want := colorStyle("#ff0000").Reverse(true).Render(" Doing (1)" + strings.Repeat(" ", 10))
	if got := strings.Split(bar.listView(20, 2), "\n")[0]; got != want {
		t.Fatalf("bar header = %q, want %q", got, want)
	}
	if bar.markerStyle.GetForeground() != lipgloss.Color("2") || bar.accentStyle.GetForeground() != lipgloss.Color("2") {
		t.Fatal("marker and reader fall back to accent_color")
	}
}

func TestDueLabel(t *testing.T) {
	for _, tc := range []struct {
		due, want string
		style     lipgloss.Style
	}{
		{"2026-09-25", "3d ago", errorStyle},
		{"2026-09-28", "today", warnStyle},
		{"2026-09-29", "tomorrow", lipgloss.Style{}},
		{"2026-10-02", "Fri", lipgloss.Style{}},
		{"2026-10-20", "Oct 20", dimStyle},
		{"2027-01-04", "Jan 4 2027", dimStyle},
		{"soon", "", lipgloss.Style{}},
	} {
		got, style := dueLabel(strp(tc.due), testToday)
		if got != tc.want || style.Render("x") != tc.style.Render("x") {
			t.Errorf("%s: got %q %q, want %q %q", tc.due, got, style.Render("x"), tc.want, tc.style.Render("x"))
		}
	}
	if got, _ := dueLabel(nil, testToday); got != "" {
		t.Errorf("no due date: %q", got)
	}
}

func TestColumnsAlignAcrossRows(t *testing.T) {
	m, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Fields: []string{"section", "due"}}})
	long := fieldTask
	long.GID, long.Name, long.AssigneeSection, long.DueOn = "2", "Ship", &asana.Ref{Name: "In Progress"}, strp("2026-09-27")
	plain := asana.Task{GID: "3", Name: "Tidy"}
	m.Update(tasksMsg{tasks: []asana.Task{fieldTask, long, plain}})
	got := listLines(m, 40, 3)
	want := []string{
		"□ Fix login          Today        Thu",
		"□ Ship               In Progress  1d ago",
		"□ Tidy",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestDoneRowsAreStruckThrough(t *testing.T) {
	m, _ := testModel(t, config.Config{List: config.List{Selection: config.Selection{Style: config.StyleMarker}}})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask}})
	row, _ := m.listRow(1)
	if !strings.Contains(row[0], "\x1b[2;9m") && !strings.Contains(row[0], "\x1b[9;2m") {
		t.Fatalf("done row = %q", row[0])
	}
}
