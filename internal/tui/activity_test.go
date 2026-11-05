package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/activity"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/keymap"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func activityModel(t *testing.T, width int) *Model {
	t.Helper()
	m := wideModel(t, width)
	m.openActivity()
	entries := []activity.Entry{
		{Task: sideTask, Story: asana.Story{GID: "s1", CreatedBy: &asana.Ref{GID: "me", Name: "Austin"}, ResourceSubtype: "assigned", Text: "Austin assigned to Sam", Assignee: &asana.Ref{Name: "Sam"}}, At: testToday, Mine: true},
		{Task: openTask, Story: asana.Story{GID: "s2", CreatedBy: &asana.Ref{GID: "other", Name: "Sam"}, ResourceSubtype: "due_date_changed", Text: "Sam changed the due date", NewDates: &asana.StoryDates{DueOn: new("2026-10-14")}}, At: testToday.Add(-time.Hour), Reasons: []string{"currently assigned to you"}},
		{Task: doneTask, Story: asana.Story{GID: "s3", ResourceSubtype: "due_today", Text: "due today"}, At: testToday.Add(-2 * time.Hour), Automatic: true, Reasons: []string{"currently assigned to you"}},
	}
	m.Update(activityMsg{seq: m.activity.seq, report: activity.Report{Entries: entries, Scanned: 3, Total: 3, Since: testToday.Add(-7 * 24 * time.Hour), Until: testToday}})
	t.Cleanup(func() { m.closeActivity() })
	return m
}

// activityRunModel opens Activity on three events in a row on openTask, one
// on sideTask, then one more on openTask.
func activityRunModel(t *testing.T) *Model {
	t.Helper()
	m := wideModel(t, 160)
	m.openActivity()
	var entries []activity.Entry
	for i, task := range []asana.Task{openTask, openTask, openTask, sideTask, openTask} {
		text := fmt.Sprintf("event %d", i+1)
		if i == 0 || i == 4 {
			text += " alpha"
		}
		entries = append(entries, activity.Entry{Task: task, Story: asana.Story{GID: fmt.Sprint("s", i+1), Text: text}, At: testToday.Add(-time.Duration(i) * time.Minute), Mine: true})
	}
	m.Update(activityMsg{seq: m.activity.seq, report: activity.Report{Entries: entries, Since: testToday.Add(-7 * 24 * time.Hour), Until: testToday}})
	t.Cleanup(func() { m.closeActivity() })
	return m
}

func TestActivityGroupsEventsInARowOnOneTicket(t *testing.T) {
	m := activityRunModel(t)
	a := m.activity
	if len(a.rows) != 3 || a.rows[0].more != 2 || a.rows[2].more != 0 {
		t.Fatalf("rows = %+v", a.rows)
	}
	list := ansi.Strip(m.activityList(60, 30))
	if strings.Count(list, "+2 more") != 1 || strings.Count(list, openTask.Name) != 2 {
		t.Fatalf("collapsed list:\n%s", list)
	}
	sendKeys(m, "space")
	if len(a.rows) != 5 || !a.rows[1].child || !a.rows[2].last {
		t.Fatalf("expanded rows = %+v", a.rows)
	}
	list = ansi.Strip(m.activityList(40, 30))
	if strings.Contains(list, "more") || strings.Count(list, openTask.Name) != 2 || !strings.Contains(list, "└ event 3") {
		t.Fatalf("expanded list:\n%s", list)
	}
	for _, line := range strings.Split(m.activityList(40, 30), "\n") {
		if ansi.StringWidth(line) > 40 {
			t.Fatalf("row wider than 40: %q", ansi.Strip(line))
		}
	}
	sendKeys(m, "j")
	if e, _ := a.selected(); e.Story.GID != "s2" {
		t.Fatalf("selected %s, want the first earlier event", e.Story.GID)
	}
	// Hiding from an earlier event selects the run's row.
	sendKeys(m, "space")
	if e, _ := a.selected(); len(a.rows) != 3 || a.cursor != 0 || e.Story.GID != "s1" {
		t.Fatalf("collapsed cursor %d on %s", a.cursor, e.Story.GID)
	}
	// The window keeps the cursor's row in view.
	sendKeys(m, "G")
	if list := ansi.Strip(m.activityList(60, 4)); !strings.Contains(list, "event 5") {
		t.Fatalf("cursor row scrolled out:\n%s", list)
	}
}

func TestActivityGroupsAfterSearch(t *testing.T) {
	m := activityRunModel(t)
	m.activity.query.SetValue("alpha")
	m.filterActivity()
	if rows := m.activity.rows; len(rows) != 1 || rows[0].more != 1 {
		t.Fatalf("events in a row after search must group: %+v", rows)
	}
}

func TestActivityFiltersAndAutomatedToggle(t *testing.T) {
	m := activityModel(t, 160)
	if len(m.activity.visible) != 2 {
		t.Fatalf("Both = %d", len(m.activity.visible))
	}
	sendKeys(m, "]")
	if m.activity.mode != "Mine" || len(m.activity.visible) != 1 || !m.activity.visible[0].Mine {
		t.Fatalf("Mine = %+v", m.activity.visible)
	}
	sendKeys(m, "]")
	if m.activity.mode != "Related" || len(m.activity.visible) != 1 || m.activity.visible[0].Mine {
		t.Fatalf("Related = %+v", m.activity.visible)
	}
	sendKeys(m, "!")
	if len(m.activity.visible) != 2 || !m.activity.visible[1].Automatic {
		t.Fatalf("automated = %+v", m.activity.visible)
	}
	sendKeys(m, "]")
	if m.activity.mode != "Both" || len(m.activity.visible) != 3 {
		t.Fatalf("Both including automated = %+v", m.activity.visible)
	}
}

func TestActivityTabCyclesViewsListAndReader(t *testing.T) {
	m := activityModel(t, 160)
	for i, want := range []string{"reader", "views", "list"} {
		sendKeys(m, "tab")
		got := "list"
		if m.focusNav {
			got = "views"
		} else if m.activity.details {
			got = "reader"
		}
		if got != want {
			t.Fatalf("tab %d focus = %s, want %s", i+1, got, want)
		}
	}
	sendKeys(m, "shift+tab")
	if !m.focusNav {
		t.Fatal("shift+tab from list must focus views")
	}
}

func TestActivityDimsTaskOnlyViews(t *testing.T) {
	m := activityModel(t, 160)
	projects := 0
	for _, it := range m.navItems() {
		if it.project != nil {
			projects++
		}
		if (it.project != nil || it.savedView != "") && !it.dim {
			t.Fatalf("%q must be dim in Activity", it.label)
		}
		if it.activity && it.dim {
			t.Fatalf("%q must not be dim in Activity", it.label)
		}
	}
	if projects == 0 {
		t.Fatal("Places panel lists no projects")
	}
}

func TestActivitySearchDoesNotChangeSavedTicketFilter(t *testing.T) {
	m := activityModel(t, 160)
	before := m.filterInput.Value()
	sendKeys(m, "/", "S", "a", "m", "enter")
	if m.activity.filtering || len(m.activity.visible) != 2 {
		t.Fatalf("search = %q, visible = %d", m.activity.query.Value(), len(m.activity.visible))
	}
	sendKeys(m, "]")
	if len(m.activity.visible) != 1 || m.filterInput.Value() != before {
		t.Fatalf("activity search affected ticket filter")
	}
	if m.deps.State.CurrentSavedViews[""] != "" {
		t.Fatal("Activity must not save a ticket view")
	}
}

func TestActivityTicketRoundTripKeepsEventAndAllowsExistingReader(t *testing.T) {
	m := activityModel(t, 160)
	m.details[sideTask.GID] = ticket.Ticket{Task: sideTask}
	sendKeys(m, "enter")
	if m.activityOpen || !m.activityTicket || !m.focusReader {
		t.Fatal("Enter must open existing ticket reader")
	}
	if m.detailRequests[sideTask.GID] == 0 {
		t.Fatal("cached ticket must refresh on opening from Activity")
	}
	selected, ok := m.selected()
	if !ok || selected.GID != sideTask.GID {
		t.Fatalf("selected = %+v", selected)
	}
	sendKeys(m, "esc")
	if !m.activityOpen || m.activityTicket || m.activity.cursor != 0 {
		t.Fatal("Esc must return to same Activity entry")
	}
	sendKeys(m, "j")
	if m.activity.cursor != 1 {
		t.Fatal("Activity navigation did not resume")
	}
}

func TestActivityNarrowEnterShowsEventThenTicket(t *testing.T) {
	m := activityModel(t, 80)
	sendKeys(m, "j", "enter")
	if !m.activityOpen || !m.activity.details || !strings.Contains(ansi.Strip(m.body()), "New due: 2026-10-14") {
		t.Fatal("narrow Enter must show event details")
	}
	sendKeys(m, "esc")
	if !m.activityOpen || m.activity.details {
		t.Fatal("Esc must return to activity list")
	}
	sendKeys(m, "enter", "enter")
	if m.activityOpen || !m.activityTicket || !m.focusReader {
		t.Fatal("second Enter must open ticket")
	}
}

func TestActivityIgnoresStaleLoadsAndKeepsOldResultsOnError(t *testing.T) {
	m := activityModel(t, 160)
	m.scanActivity(true)
	seq := m.activity.seq
	m.Update(activityMsg{seq: seq - 1, report: activity.Report{}})
	if !m.activity.loading || len(m.activity.visible) != 2 {
		t.Fatal("stale response replaced current activity")
	}
	m.Update(activityMsg{seq: seq, err: fmt.Errorf("search unavailable")})
	if m.activity.loading || len(m.activity.visible) != 2 || !strings.Contains(ansi.Strip(m.body()), "search unavailable") {
		t.Fatal("refresh error must preserve results and display error")
	}
	if m.activity.stopped {
		t.Fatal("a transient error must not stop background scans")
	}
}

func TestActivityStreamsPartialSnapshots(t *testing.T) {
	m := wideModel(t, 160)
	m.openActivity()
	m.scanActivity(true)
	seq := m.activity.seq
	entries := []activity.Entry{
		{Task: openTask, Story: asana.Story{GID: "s1", Text: "one"}, At: testToday},
		{Task: doneTask, Story: asana.Story{GID: "s2", Text: "two"}, At: testToday.Add(-time.Hour)},
	}
	m.activity.updates = make(chan activityMsg, 1)
	_, cmd := m.Update(activityMsg{seq: seq, report: activity.Report{Entries: entries[:1], Scanned: 1, Total: 3}, partial: true})
	if cmd == nil || !m.activity.loading || len(m.activity.visible) != 1 || !strings.Contains(ansi.Strip(m.statusline()), "scanning 1/3") {
		t.Fatal("partial snapshot must show entries and keep scanning")
	}
	_, cmd = m.Update(activityMsg{seq: seq, report: activity.Report{Entries: entries, Scanned: 3, Total: 3, Until: testToday, Complete: true}})
	if cmd != nil || m.activity.loading || len(m.activity.visible) != 2 || !strings.Contains(ansi.Strip(m.statusline()), "updated") {
		t.Fatal("final snapshot must stop scanning")
	}
}

func TestActivityPeriodFiltersCacheWithoutScanning(t *testing.T) {
	m := activityModel(t, 160)
	old := activity.Entry{Task: openTask, Story: asana.Story{GID: "old", Text: "old"}, At: testToday.Add(-3 * 24 * time.Hour)}
	m.Update(activityMsg{seq: m.activity.seq, report: activity.Report{Entries: []activity.Entry{old}}})
	seq := m.activity.seq
	if len(m.activity.visible) != 3 {
		t.Fatalf("7 days = %d", len(m.activity.visible))
	}
	sendKeys(m, "D", "k", "enter")
	if m.activity.days != 1 || m.activity.loading || m.activity.seq != seq || len(m.activity.visible) != 2 || m.activityCount("Both") != 2 {
		t.Fatalf("24 hours = %d visible, loading %v", len(m.activity.visible), m.activity.loading)
	}
}

func TestActivityNewCountAndMarks(t *testing.T) {
	m := activityModel(t, 200)
	if label := activityNavLabel(m); label != "" {
		t.Fatalf("open Activity shows %q", label)
	}
	sendKeys(m, "esc")
	if label := activityNavLabel(m); label != "" {
		t.Fatalf("seeded entries count as new: %q", label)
	}
	found := testToday.Add(time.Minute)
	m.now = func() time.Time { return found.Add(time.Minute) }
	fresh := []activity.Entry{
		{Task: openTask, Story: asana.Story{GID: "new", Text: "Sam commented"}, At: found},
		{Task: openTask, Story: asana.Story{GID: "own", Text: "You commented"}, At: found, Mine: true},
	}
	// Background scans update the cache while the view is closed.
	m.Update(activityMsg{seq: m.activity.seq, report: activity.Report{Entries: fresh}, foundAt: found})
	if label := activityNavLabel(m); label != "1" {
		t.Fatalf("new count = %q, want 1", label)
	}
	if strings.Contains(ansi.Strip(m.body()), "[I]") || strings.Contains(ansi.Strip(m.navView(navW, 20)), " I\n") {
		t.Fatal("Activity row must not show its key")
	}
	sendKeys(m, "I")
	if label := activityNavLabel(m); label != "" || m.activity.newActivity() != 0 {
		t.Fatal("opening Activity must clear the count")
	}
	if list := ansi.Strip(m.activityList(60, 30)); strings.Count(list, "NEW") != 1 {
		t.Fatalf("list must mark the new event once:\n%s", list)
	}
	sendKeys(m, "esc")
	sendKeys(m, "I")
	if strings.Contains(ansi.Strip(m.activityList(60, 30)), "NEW") {
		t.Fatal("marks must clear after leaving Activity")
	}
}

// activityNavLabel is the right column of the Places panel's Activity row.
func activityNavLabel(m *Model) string {
	for _, it := range m.navItems() {
		if it.activity {
			return it.right
		}
	}
	return "missing"
}

func TestActivityBackgroundTicks(t *testing.T) {
	m := wideModel(t, 160)
	if m.activity.interval != 2*time.Minute || m.scheduleActivity() == nil {
		t.Fatal("background scans must default to 2m")
	}
	m.activityTick()
	seq := m.activity.seq
	if !m.activity.loading {
		t.Fatal("tick must start a scan")
	}
	m.activityTick()
	if m.activity.seq != seq {
		t.Fatal("tick during a scan must be skipped")
	}
	m.Update(activityMsg{seq: seq, err: fmt.Errorf("Activity requires paid search: %w", &asana.APIError{Status: 402})})
	if !m.activity.stopped || m.activityTick() != nil || m.scheduleActivity() != nil {
		t.Fatal("402 must stop background scans")
	}
	m.activity.stopped, m.activity.interval = false, 0
	if m.scheduleActivity() != nil || m.startActivity() != nil {
		t.Fatal("interval 0 must not scan in the background")
	}
	m.activity.loading = false
	m.openActivity()
	if !m.activity.loading {
		t.Fatal("with background scans off, opening Activity must scan")
	}
}

func TestActivityNavigationPanelAndProjectSwitch(t *testing.T) {
	m := wideModel(t, 200)
	sendKeys(m, "0", "j", "enter")
	if !m.activityOpen {
		t.Fatal("sidebar Activity did not open")
	}
	m.Update(activityMsg{seq: m.activity.seq, report: activity.Report{}})
	sendKeys(m, "0", "enter")
	if !m.activityOpen {
		t.Fatal("selecting active Activity should keep the view open")
	}
	sendKeys(m, "0", "j", "enter") // Activity, then recent Mobile project.
	if m.activityOpen || m.activityTicket || gidOf(m.viewProject) != "p2" {
		t.Fatalf("project switch = %+v", m.viewProject)
	}
}

func TestActivityFitsTerminalCells(t *testing.T) {
	for _, width := range []int{80, 120, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := activityModel(t, width)
			m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
			for _, focus := range []bool{false, true} {
				m.activity.details = focus
				lines := strings.Split(m.View().Content, "\n")
				if len(lines) != 40 {
					t.Fatalf("height = %d, want 40", len(lines))
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > width {
						t.Fatalf("row wider than %d: %q", width, ansi.Strip(line))
					}
				}
			}
		})
	}
}

func TestActivityTextCannotInjectTerminalControls(t *testing.T) {
	m := activityModel(t, 160)
	m.activity.cache.Entries[0].Task.Name = "Bad\x1b]52;c;c2VjcmV0\a ticket"
	m.activity.cache.Entries[0].Story.Text = "Bad\x1b[2J action"
	m.filterActivity()
	if strings.Contains(m.activity.reader.GetContent(), "\x1b]52;") || strings.Contains(m.activityList(50, 30), "\x1b]52;") || strings.Contains(m.activity.reader.GetContent(), "\x1b[2J") {
		t.Fatal("untrusted activity emitted terminal controls")
	}
}

func TestActivityUsesConfiguredKeys(t *testing.T) {
	m := activityModel(t, 200)
	var err error
	keys, err = keymap.Resolve(map[string]map[string][]string{"activity": {"next_mode": {"M"}}, "main": {"down": {"J"}, "copy_link": {"W"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { keys = keymap.Default() })
	sendKeys(m, "]")
	if m.activity.mode != "Both" {
		t.Fatal("old mode key still active")
	}
	sendKeys(m, "J")
	if m.activity.cursor != 1 {
		t.Fatal("configured movement ignored")
	}
	sendKeys(m, "M")
	if m.activity.mode != "Mine" {
		t.Fatal("configured mode ignored")
	}
	if help := ansi.Strip(m.activityHelp()); !strings.Contains(help, "M") || !strings.Contains(help, "W") {
		t.Fatal("help ignores configured keys")
	}
}

func TestActivityPreviewAndSidebarRemainIndependent(t *testing.T) {
	m := activityModel(t, 200)
	sendKeys(m, "z")
	if !m.readerHidden || !m.showNav() || m.activity.automated {
		t.Fatal("preview toggle changed sidebar or automated filter")
	}
	for _, focus := range []bool{false, true} {
		m.activity.details = focus
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) != m.height {
			t.Fatalf("height = %d, want %d", len(lines), m.height)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > m.width {
				t.Fatal("activity exceeds terminal width with hidden preview")
			}
		}
	}
	sendKeys(m, "Z")
	if m.showNav() || !m.readerHidden {
		t.Fatal("sidebar toggle changed preview")
	}
	sendKeys(m, "z")
	if m.readerHidden || m.showNav() {
		t.Fatal("preview toggle restored sidebar")
	}
}
