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
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
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
	if want := selectedStyle.Render("□  A     "); got[0] != want {
		t.Fatalf("selected = %q, want %q", got[0], want)
	}
	if ansi.Strip(got[1]) != "□  B" {
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
	if got := listLines(single, 80, 5); len(got) != 2 || got[0] != "□  Fix login"+strings.Repeat(" ", 57)+"Today  Thu" {
		t.Fatalf("single = %q", got)
	}

	multi, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutMulti, Fields: []string{"section", "due"}}})
	multi.Update(tasksMsg{tasks: []asana.Task{fieldTask, other}})
	got := listLines(multi, 80, 4)
	if want := []string{"□  Fix login", "   Today · Thu", "□  Fix footer", ""}; !slices.Equal(got, want) {
		t.Fatalf("multi = %q, want %q", got, want)
	}
	multi.moveTo(1)
	if got := listLines(multi, 80, 3); got[0] != "□  Fix footer" {
		t.Fatalf("multi scroll keeps the cursor's whole row visible: %q", got)
	}
}

func TestListViewSeparator(t *testing.T) {
	a, b, c := asana.Task{GID: "1", Name: "A"}, asana.Task{GID: "2", Name: "B"}, asana.Task{GID: "3", Name: "C"}
	single, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Separator: true}})
	single.Update(tasksMsg{tasks: []asana.Task{a, b, c}})
	if got := listLines(single, 5, 5); !slices.Equal(got, []string{"─────", "□  A", "─────", "□  B", "─────"}) {
		t.Fatalf("single = %q", got)
	}
	single.Update(key("s"))
	single.Update(key("s"))
	single.Update(key("esc"))
	if got := listLines(single, 5, 3); !slices.Equal(got, []string{"□  A", "□  B", "□  C"}) {
		t.Fatalf("single toggled off = %q", got)
	}
	multi, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutMulti, Separator: true}})
	multi.Update(tasksMsg{tasks: []asana.Task{a, b, c}})
	multi.moveTo(2)
	if got := listLines(multi, 5, 7); !slices.Equal(got, []string{"─────", "□  B", "", "─────", "□  C", "", "─────"}) {
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
	m, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Separator: true, DefaultView: config.DefaultView{GroupBy: "section"}}})
	m.Update(tasksMsg{tasks: tasks})
	got := listLines(m, 24, 10)
	want := []string{
		" Doing (2)",
		"□  A",
		strings.Repeat("─", 24),
		"□  C",
		" Next (1)",
		"□  B",
		strings.Repeat("─", 24),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("grouped =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	m.moveTo(2)
	if got := listLines(m, 24, 3); got[0] != " Next (1)" || got[1] != "□  B" {
		t.Fatalf("scrolled view keeps the cursor's group header: %q", got)
	}
	m.moveTo(1)
	if got := listLines(m, 24, 4); got[0] != " Doing (2)" || got[1] != "□  C" {
		t.Fatalf("first row shown mid-group gets its header: %q", got)
	}
	m.Update(key("s"))
	m.Update(key("h"))
	m.Update(key("esc"))
	m.moveTo(0)
	got = listLines(m, 24, 10)
	want = []string{" Doing (2)", "", "□  A", strings.Repeat("─", 24), "□  C", "", " Next (1)", "", "□  B", strings.Repeat("─", 24)}
	if !slices.Equal(got, want) {
		t.Fatalf("spaced =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	m.moveTo(2)
	if got := listLines(m, 24, 4); got[0] != " Next (1)" || got[1] != "" || got[2] != "□  B" {
		t.Fatalf("header topping the view gets no spacing above: %q", got)
	}
}

func TestFoldGroups(t *testing.T) {
	sec := func(gid, name, section string) asana.Task {
		return asana.Task{GID: gid, Name: name, AssigneeSection: &asana.Ref{Name: section}}
	}
	tasks := []asana.Task{sec("1", "A", "Doing"), sec("2", "B", "Next"), sec("3", "C", "Doing"), sec("4", "D", "Later")}
	m, st := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, DefaultView: config.DefaultView{GroupBy: "section"}}})
	m.Update(tasksMsg{tasks: tasks})
	view := func(want ...string) {
		t.Helper()
		if got := listLines(m, 24, 10); !slices.Equal(got, want) {
			t.Fatalf("list =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}

	m.Update(key("-"))
	view(" ▸ Doing (2)", " Next (1)", "□  B", " Later (1)", "□  D")
	if _, ok := m.selected(); ok || m.cursor != 0 {
		t.Fatalf("folding leaves the cursor on the header: cursor = %d, selected = %v", m.cursor, ok)
	}
	if got := ansi.Strip(m.reader.GetContent()); got != "Doing · 2 tickets folded · - to open" {
		t.Fatalf("reader note = %q", got)
	}
	m.Update(key("j"))
	if sel, _ := m.selected(); sel.Name != "B" {
		t.Fatalf("down skips the folded tickets, selected %q", sel.Name)
	}
	m.Update(key("k"))
	if !m.onFoldedHeader() {
		t.Fatalf("up stops on the folded header, cursor = %d", m.cursor)
	}

	m.Update(key("+"))
	view(" Doing (2)", "□  A", "□  C", " ▸ Next (1)", " ▸ Later (1)")
	if v, _ := st.View(""); !slices.Equal(v.Folded, []string{"Later", "Next"}) {
		t.Fatalf("saved folds = %q", v.Folded)
	}
	m.Update(key("+"))
	view(" Doing (2)", "□  A", "□  C", " Next (1)", "□  B", " Later (1)", "□  D")

	m.moveTo(2)
	m.Update(key("-"))
	m.moveTo(0)
	m.selectTicket(tasks[1])
	if sel, _ := m.selected(); sel.Name != "B" || len(m.foldedGroups) != 0 {
		t.Fatalf("selecting a folded ticket opens its group: selected %q, folded %q", sel.Name, m.foldedGroups)
	}

	m.Update(key("-"))
	m.pickedGroup("due")
	if len(m.foldedGroups) != 0 {
		t.Fatalf("a new grouping keeps folds %q", m.foldedGroups)
	}
}

func TestSharedColumnValuesMoveToHeaders(t *testing.T) {
	sec := func(name string) *asana.Ref { return &asana.Ref{Name: name} }
	tasks := []asana.Task{
		{GID: "1", Name: "A", AssigneeSection: sec("Inbox"), DueOn: strp("2026-09-28")},
		{GID: "2", Name: "B", AssigneeSection: sec("Inbox"), DueOn: strp("2026-10-01")},
		{GID: "3", Name: "C", AssigneeSection: sec("Later"), DueOn: strp("2026-10-20")},
	}
	fields := []string{"section", "due"}
	t.Run("grouped by the column", func(t *testing.T) {
		m, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Fields: fields, DefaultView: config.DefaultView{GroupBy: "section"}, Header: config.Header{Style: config.StyleRule}}})
		m.Update(tasksMsg{tasks: tasks})
		got := strings.Join(listLines(m, 40, 6), "\n")
		if strings.Count(got, "Inbox") != 1 || !strings.Contains(got, "── Inbox (2) ──") || !strings.Contains(got, "today") {
			t.Fatalf("grouped column must leave the rows, header unchanged:\n%s", got)
		}
	})
	t.Run("one value per group", func(t *testing.T) {
		m, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Fields: fields, DefaultView: config.DefaultView{GroupBy: "due"}, Header: config.Header{Style: config.StyleRule}}})
		m.Update(tasksMsg{tasks: append(slices.Clone(tasks[:2]), asana.Task{GID: "4", Name: "D", AssigneeSection: sec("Inbox"), DueOn: strp("2026-09-30")})})
		got := strings.Join(listLines(m, 40, 6), "\n")
		row := slices.IndexFunc(strings.Split(got, "\n"), func(l string) bool { return strings.Contains(l, "□  D") })
		if !strings.Contains(got, "Next 7 days (2) · Inbox") || row < 0 || strings.Contains(strings.Split(got, "\n")[row], "Inbox") {
			t.Fatalf("shared section must move to the group header:\n%s", got)
		}
	})
	t.Run("mixed values stay", func(t *testing.T) {
		m, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Fields: fields}})
		m.Update(tasksMsg{tasks: tasks})
		if got := strings.Join(listLines(m, 40, 4), "\n"); strings.Count(got, "Inbox") != 2 || !strings.Contains(got, "Later") {
			t.Fatalf("mixed column must stay:\n%s", got)
		}
	})
	t.Run("ungrouped list", func(t *testing.T) {
		m, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Fields: fields}})
		m.Update(tasksMsg{tasks: tasks[:2]})
		if _, count := m.listTitle(); !strings.Contains(count, "Inbox") || strings.Contains(strings.Join(listLines(m, 40, 3), "\n"), "Inbox") {
			t.Fatalf("shared value must move to the list title, count = %q", count)
		}
	})
	t.Run("one ticket per group", func(t *testing.T) {
		m, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Fields: fields, DefaultView: config.DefaultView{GroupBy: "due"}}})
		m.Update(tasksMsg{tasks: []asana.Task{tasks[0], tasks[2]}})
		if got := strings.Join(listLines(m, 40, 4), "\n"); strings.Count(got, "Inbox") != 1 || !strings.Contains(got, "Later") {
			t.Fatalf("single-ticket groups keep their columns:\n%s", got)
		}
	})
}

func TestGroupPicker(t *testing.T) {
	m, _ := testModel(t, config.Config{List: config.List{Fields: []string{"Section", "Status"}, DefaultView: config.DefaultView{GroupBy: "section"}}})
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
	if want := []string{"None", "section", "due", "assignee", "project", "tags", "completed", "state", "Status", "Priority"}; !slices.Equal(labels, want) {
		t.Fatalf("options = %q, want %q", labels, want)
	}
	if it := m.modal.items[m.modal.matches[m.modal.cursor]]; it.Hint != "current" || it.Value != "section" {
		t.Fatalf("picker opens on %+v, want the current grouping", it)
	}
	press("/none")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if sel, _ := m.selected(); m.modal != nil || m.groupBy != "" || m.groups != nil || sel.GID != "2" {
		t.Fatalf("groupBy = %q, groups = %q, selected = %q", m.groupBy, m.groups, sel.GID)
	}
}

func TestSelectionMarker(t *testing.T) {
	m, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, Selection: config.Selection{Style: config.StyleMarker}}})
	m.Update(tasksMsg{tasks: []asana.Task{{GID: "1", Name: "A"}, {GID: "2", Name: "B"}}})
	got := strings.Split(m.listView(10, 2), "\n")
	if want := m.markerStyle.Render("▌ ") + titleStyle.Render("□  A"); got[0] != want {
		t.Fatalf("selected = %q, want %q", got[0], want)
	}
	if ansi.Strip(got[1]) != "  □  B" {
		t.Fatalf("unselected rows keep the gutter: %q", got[1])
	}
	m.focusReader = true
	if got := strings.Split(m.listView(10, 2), "\n")[0]; !strings.HasPrefix(got, dimStyle.Render("▌ ")) {
		t.Fatalf("reader focus dims the marker: %q", got)
	}
}

func TestHeaderStyles(t *testing.T) {
	tasks := []asana.Task{{GID: "1", Name: "A", AssigneeSection: &asana.Ref{Name: "Doing"}}}
	rule, _ := testModel(t, config.Config{List: config.List{DefaultView: config.DefaultView{GroupBy: "section"}, Header: config.Header{Style: config.StyleRule}}, Colors: config.Colors{Header: config.Style{FG: "5"}}})
	rule.Update(tasksMsg{tasks: tasks})
	if got := listLines(rule, 20, 2)[0]; got != "── Doing (1) ───────" {
		t.Fatalf("rule header = %q", got)
	}
	bar, _ := testModel(t, config.Config{Colors: config.Colors{Accent: config.Style{FG: "2"}, Header: config.Style{FG: "#ff0000"}}, List: config.List{DefaultView: config.DefaultView{GroupBy: "section"}}})
	bar.Update(tasksMsg{tasks: tasks})
	if bar.headerStyle.GetForeground() != lipgloss.Color("#ff0000") {
		t.Fatalf("header color = %v", bar.headerStyle.GetForeground())
	}
	want := bar.headerStyle.Reverse(true).Render(" Doing (1)" + strings.Repeat(" ", 10))
	if got := strings.Split(bar.listView(20, 2), "\n")[0]; got != want {
		t.Fatalf("bar header = %q, want %q", got, want)
	}
	if bar.markerStyle.GetForeground() != lipgloss.Color("2") || bar.accentStyle.GetForeground() != lipgloss.Color("2") {
		t.Fatal("marker and reader fall back to colors.accent")
	}
}

func TestViewingHeaderColor(t *testing.T) {
	for _, style := range []string{config.StyleRule, config.StyleBar} {
		for _, color := range []string{"5", "#aabbcc", ""} {
			t.Run(style+"/"+color, func(t *testing.T) {
				bold := true
				cfg := config.Default()
				cfg.List.DefaultView.GroupBy = "section"
				cfg.List.Header.Style = style
				cfg.Colors.Header = config.Style{FG: "2"}
				cfg.Colors.Viewing = config.Style{FG: color}
				m, st := testModel(t, cfg)
				st.PinnedTasks = map[string][]string{"": {doneTask.GID}}
				regular := openTask
				regular.AssigneeSection = &asana.Ref{Name: viewingLabel}
				m.Update(tasksMsg{tasks: []asana.Task{regular, doneTask}})
				m.showTicket(sideTask)
				wantColor := color
				if wantColor == "" {
					wantColor = "5" // the dark theme's viewing color
				}
				if m.viewingStyle.GetForeground() != lipgloss.Color(wantColor) {
					t.Fatalf("Viewing color = %v, want %q", m.viewingStyle.GetForeground(), wantColor)
				}
				m.focusList() // headers are muted while the list lacks focus
				lines := strings.Split(m.listView(50, 20), "\n")
				for _, header := range []struct{ label, color string }{
					{pinnedLabel, "208"}, {viewingLabel, wantColor}, {viewingLabel, "2"},
				} {
					if len(lines) < 2 {
						t.Fatalf("missing header %q", header.label)
					}
					want := m.groupHeader(lipStyle(config.Style{FG: header.color, Bold: &bold}), header.label, 1, 50, "")
					if lines[0] != want {
						t.Fatalf("%s header = %q, want %q", header.label, lines[0], want)
					}
					lines = lines[2:]
				}
			})
		}
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
		"□  Fix login        Today        Thu",
		"□  Ship             In Progress  1d ago",
		"□  Tidy",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestInitials(t *testing.T) {
	for name, want := range map[string]string{"Austin Fishbaugh": "AF", "jane q doe": "JD", "Ann": "AN", "X": "X", "  ": "", "Émile Zola": "ÉZ"} {
		if got := initials(name); got != want {
			t.Errorf("initials(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestInitialsColumnIsABadge(t *testing.T) {
	m, _ := testModel(t, config.Config{List: config.List{Fields: []string{"initials"}}})
	bob := asana.Task{GID: "2", Name: "Other", Assignee: &asana.Ref{Name: "Bob Ray"}}
	m.Update(tasksMsg{tasks: []asana.Task{fieldTask, bob, {GID: "3", Name: "Nobody's"}}})
	m.moveTo(2)
	got := listLines(m, 30, 3)
	if !strings.HasSuffix(got[0], " AN") || !strings.HasSuffix(got[1], " BR") || strings.TrimSpace(got[2]) != "□  Nobody's" {
		t.Fatalf("rows = %q", got)
	}
	if _, tail := m.listRow(1, m.cols); !strings.Contains(tail, authorStyle("Bob Ray").Reverse(true).Bold(true).Render(" BR ")) {
		t.Fatalf("tail = %q", tail)
	}
}

func TestChangedMyTasksRowsMarked(t *testing.T) {
	m, st := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle}})
	a := asana.Task{GID: "1", Name: "A", ModifiedAt: "t1"}
	b := asana.Task{GID: "2", Name: "B", ModifiedAt: "t1"}
	c := asana.Task{GID: "3", Name: "C", ModifiedAt: "t1"}
	// The first load is the baseline, so nothing is marked.
	m.Update(tasksMsg{tasks: []asana.Task{a, b}})
	if got := listLines(m, 20, 2); !slices.Equal(got, []string{"□  A", "□  B"}) {
		t.Fatalf("baseline rows = %q", got)
	}
	b.ModifiedAt = "t2"
	m.Update(tasksMsg{tasks: []asana.Task{a, b, c}})
	if got := listLines(m, 20, 3); !slices.Equal(got, []string{"□  A", "□  B  NEW", "□  C  NEW"}) {
		t.Fatalf("changed rows = %q", got)
	}
	if !strings.Contains(m.listView(20, 3), newStyle.Render(" NEW ")) {
		t.Fatal("unselected badge is not in the new style")
	}

	// Showing B in the reader records it as seen.
	m.cursor = 1
	m.Update(detailMsg{gid: "2", ticket: ticket.Ticket{Task: b}})
	if got := listLines(m, 20, 3); !slices.Equal(got, []string{"□  A", "□  B", "□  C  NEW"}) || st.SeenTasks["2"] != "t2" {
		t.Fatalf("viewed rows = %q, seen = %v", got, st.SeenTasks)
	}

	// A refresh that changes the shown ticket does not mark it.
	b.ModifiedAt = "t3"
	m.Update(tasksMsg{tasks: []asana.Task{a, b, c}})
	if got := listLines(m, 20, 3); !slices.Equal(got, []string{"□  A", "□  B", "□  C  NEW"}) {
		t.Fatalf("shown row marked: %q", got)
	}

	// The selected row reverses its badge.
	m.cursor = 2
	if !strings.Contains(m.listView(20, 3), newStyle.Reverse(true).Render(" NEW ")) {
		t.Fatal("selected badge is not reversed")
	}

	// Project views never mark rows.
	m.viewProject = &asana.Ref{GID: "p"}
	m.Update(tasksMsg{project: m.viewProject, tasks: []asana.Task{c}})
	if got := listLines(m, 20, 1); !slices.Equal(got, []string{"□  C"}) {
		t.Fatalf("project view rows = %q", got)
	}
}

func TestNewSectionHoldsChangedTickets(t *testing.T) {
	gids := func(m *Model) string {
		var out []string
		for _, t := range m.visible {
			out = append(out, t.GID)
		}
		return strings.Join(out, ",")
	}
	for _, on := range []bool{false, true} {
		a := asana.Task{GID: "1", Name: "A", ModifiedAt: "t1"}
		b := asana.Task{GID: "2", Name: "B", ModifiedAt: "t1"}
		c := asana.Task{GID: "3", Name: "C", ModifiedAt: "t1"}
		m, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, New: config.NewTickets{Section: on}}})
		m.Update(tasksMsg{tasks: []asana.Task{a, b, c}})
		c.ModifiedAt = "t2"
		m.Update(tasksMsg{tasks: []asana.Task{a, b, c}})
		if !on {
			if gids(m) != "1,2,3" {
				t.Fatalf("section off: order = %s", gids(m))
			}
			continue
		}
		if label, n := m.retainedSection(0); gids(m) != "3,1,2" || label != newLabel || n != 1 {
			t.Fatalf("order = %s, section = %q (%d); want C under New", gids(m), label, n)
		}
		if got := listLines(m, 20, 6); !slices.ContainsFunc(got, func(l string) bool { return strings.Contains(l, "New (1)") }) {
			t.Fatalf("New header missing: %q", got)
		}

		// Reading C keeps it under New, through a refresh, until the cursor leaves.
		m.cursor = 0
		m.selectionChanged()
		m.Update(detailMsg{gid: "3", ticket: ticket.Ticket{Task: c}})
		m.Update(tasksMsg{tasks: []asana.Task{a, b, c}})
		if label, _ := m.retainedSection(0); gids(m) != "3,1,2" || label != newLabel {
			t.Fatalf("read ticket left New early: order = %s", gids(m))
		}
		m.Update(key("j"))
		if sel, _ := m.selected(); gids(m) != "1,2,3" || sel.GID != "1" {
			t.Fatalf("after moving off: order = %s, selected = %s", gids(m), sel.GID)
		}
	}
}

func TestNextNewSelectsChangedTickets(t *testing.T) {
	m, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle}})
	tasks := []asana.Task{{GID: "1", ModifiedAt: "t1"}, {GID: "2", ModifiedAt: "t1"}, {GID: "3", ModifiedAt: "t1"}, {GID: "4", ModifiedAt: "t1"}}
	m.Update(tasksMsg{tasks: tasks})
	tasks[1].ModifiedAt, tasks[3].ModifiedAt = "t2", "t2"
	m.Update(tasksMsg{tasks: tasks})
	for _, want := range []int{1, 3, 1} {
		m.Update(key("i"))
		if m.cursor != want {
			t.Fatalf("cursor = %d, want %d", m.cursor, want)
		}
	}
	m.viewProject = &asana.Ref{GID: "p"}
	m.Update(tasksMsg{project: m.viewProject, tasks: tasks})
	m.Update(key("i"))
	if m.status != "no new tickets" {
		t.Fatalf("status = %q", m.status)
	}
}

func TestDueGroupsShowUrgency(t *testing.T) {
	tasks := []asana.Task{
		{GID: "1", Name: "Late", DueOn: strp("2026-09-27")},
		{GID: "2", Name: "Now", DueOn: strp("2026-09-28")},
		{GID: "3", Name: "Far", DueOn: strp("2026-10-20")},
		{GID: "4", Name: "Undated"},
	}
	m, _ := testModel(t, config.Config{List: config.List{Layout: config.LayoutSingle, DefaultView: config.DefaultView{GroupBy: "due"}}})
	m.Update(tasksMsg{tasks: tasks})
	if got := m.groupStyle(dueOverdue).GetForeground(); got != errorStyle.GetForeground() {
		t.Fatalf("Overdue header = %v, want the error color", got)
	}
	if got := m.groupStyle(dueToday).GetForeground(); got != warnStyle.GetForeground() {
		t.Fatalf("Today header = %v, want the warn color", got)
	}
	if got := m.groupStyle(dueLater).GetForeground(); got != m.headerStyle.GetForeground() {
		t.Fatalf("Later header = %v, want the header color", got)
	}
	for i, want := range []bool{false, false, true, true} {
		if m.distant(i) != want {
			t.Fatalf("distant(%d) = %v, want %v", i, !want, want)
		}
	}
	if view := m.listView(30, 12); !strings.Contains(view, dimStyle.Render("Far")) || strings.Contains(view, dimStyle.Render("Now")) {
		t.Fatal("only Later and No due date titles fade")
	}

	m.groupBy = "section"
	m.Update(tasksMsg{tasks: tasks})
	if m.groupStyle(dueOverdue).GetForeground() != m.headerStyle.GetForeground() || slices.ContainsFunc([]int{0, 1, 2, 3}, m.distant) {
		t.Fatal("urgency styling applies only when grouped by due date")
	}
}

func TestFieldCellsMutedUnlessEnumColored(t *testing.T) {
	testModel(t, config.Config{})
	task := asana.Task{CustomFields: []asana.CustomField{
		{GID: "f1", Name: "Priority", DisplayValue: strp("High"), EnumValue: &asana.EnumOption{Name: "High", Color: "red"}},
		{GID: "f2", Name: "Stage", DisplayValue: strp("QA"), EnumValue: &asana.EnumOption{Name: "QA", Color: "none"}},
	}}
	if v, style := cellValue(task, "Priority", rowContext{}, testToday); v != "High" || style.GetForeground() != errorStyle.GetForeground() {
		t.Fatalf("red enum = %q %v, want the error color", v, style.GetForeground())
	}
	if _, style := columnValue(task, state.Column{GID: "f1"}, rowContext{}, testToday); style.GetForeground() != errorStyle.GetForeground() {
		t.Fatal("a column by ID keeps the enum color")
	}
	for _, name := range []string{"Stage", "section"} {
		if _, style := cellValue(task, name, rowContext{}, testToday); style.Render("x") != dimStyle.Render("x") {
			t.Fatalf("%s cell is not muted", name)
		}
	}
	if got, ok := fieldEnumStyle(ticket.Ticket{Task: task}, ticket.FieldKey("f1")); !ok || got.GetForeground() != errorStyle.GetForeground() {
		t.Fatal("details enum value lacks its color")
	}
	if _, ok := fieldEnumStyle(ticket.Ticket{Task: task}, ticket.FieldKey("f2")); ok {
		t.Fatal(`an enum colored "none" has no color`)
	}
}
