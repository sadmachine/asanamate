package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/activity"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// nowAt is 2026-09-26 at hour h, UTC, in Asana's timestamp format.
func nowAt(h int) string {
	return time.Date(2026, 9, 26, h, 0, 0, 0, time.UTC).Format("2006-01-02T15:04:05.000Z")
}

// nowModel opens Now on My Tasks tickets last seen at 10:00: one with a new
// comment mentioning you, one never seen, one changed, one whose newest
// comment is yours, one quiet, and one assigned to someone else.
func nowModel(t *testing.T) *Model {
	t.Helper()
	m, st := testModel(t, config.Config{})
	m.meGID = "me"
	me, zed := &asana.Ref{GID: "me", Name: "Amy"}, &asana.Ref{GID: "zed", Name: "Zed"}
	tasks := []asana.Task{
		{GID: "quiet", Name: "Quiet", Assignee: me, ModifiedAt: nowAt(10)},
		{GID: "asked", Name: "Asked", Assignee: me, ModifiedAt: nowAt(10)},
		{GID: "changed", Name: "Changed", Assignee: me, ModifiedAt: nowAt(12)},
		{GID: "fresh", Name: "Fresh", Assignee: me, ModifiedAt: nowAt(9)},
		{GID: "mention", Name: "Mention", Assignee: me, ModifiedAt: nowAt(12)},
		{GID: "theirs", Name: "Theirs", Assignee: zed, ModifiedAt: nowAt(12)},
	}
	st.SeenTasks = map[string]string{}
	for _, gid := range []string{"quiet", "asked", "changed", "mention", "theirs"} {
		st.SeenTasks[gid] = nowAt(10)
	}
	comment := func(gid string, by *asana.Ref, h int, html string) activity.Entry {
		at, _ := time.Parse(time.RFC3339Nano, nowAt(h))
		return activity.Entry{Task: asana.Task{GID: gid}, At: at, Mine: by == me,
			Story: asana.Story{GID: gid + nowAt(h), ResourceSubtype: "comment_added", CreatedBy: by, CreatedAt: nowAt(h), HTMLText: html}}
	}
	m.activity.cache.Entries = []activity.Entry{
		comment("mention", zed, 11, `<body><a data-asana-type="user" data-asana-gid="me">@Amy</a> look</body>`),
		comment("asked", zed, 8, "<body>Question</body>"),
		comment("asked", me, 9, "<body>Any news?</body>"),
		comment("theirs", zed, 11, "<body>Later</body>"),
	}
	m.openNow()
	// Opening a list drops cached details, as a reload would.
	for _, task := range tasks {
		m.details[task.GID] = ticket.Ticket{Task: task}
	}
	m.Update(tasksMsg{seq: m.taskSeq, tasks: tasks})
	return m
}

// nowRows lists the visible tickets as "Section/gid".
func nowRows(m *Model) []string {
	var rows []string
	for i, task := range m.visible {
		rows = append(rows, m.groups[i]+"/"+task.GID)
	}
	return rows
}

func TestAttentionTerms(t *testing.T) {
	m := nowModel(t)
	env := m.filterEnv()
	byGID := map[string]asana.Task{}
	for _, task := range m.tasks {
		byGID[task.GID] = task
	}
	for gid, want := range map[string][]string{
		"quiet":   nil,
		"asked":   nil,
		"changed": {newChange},
		"fresh":   {newTicket},
		"mention": {newChange, newComment, newMention},
		"theirs":  nil, // not yours: seen state and the cache cover only your tickets
	} {
		if got := env.Lookup["new"](byGID[gid]); !slices.Equal(got, want) {
			t.Errorf("new values of %s = %v, want %v", gid, got, want)
		}
	}
	if got := env.Lookup["waiting"](byGID["asked"]); !slices.Equal(got, []string{waitingReply}) {
		t.Errorf("waiting values of asked = %v", got)
	}
	if got := env.Lookup["waiting"](byGID["mention"]); got != nil {
		t.Errorf("waiting values of mention = %v", got)
	}
}

func TestNowListsTicketsByWhatNeedsYou(t *testing.T) {
	m := nowModel(t)
	want := []string{"Needs reply/mention", "New to you/fresh", "Updated/changed", "Waiting on others/asked"}
	if got := nowRows(m); !slices.Equal(got, want) {
		t.Fatalf("Now rows = %v, want %v", got, want)
	}
	if !slices.Equal(m.foldedGroups, []string{"Waiting on others"}) {
		t.Fatalf("folded = %v, want Waiting on others folded", m.foldedGroups)
	}
	if m.viewName() != "Now" {
		t.Fatalf("view name = %q", m.viewName())
	}
	body := ansi.Strip(m.navView(navW-panelFrame, 20))
	if !strings.Contains(body, "▌ Now") || !strings.Contains(body, "4") {
		t.Fatalf("Places panel does not mark Now with its count:\n%s", body)
	}

	// Leaving Now for My Tasks keeps each list's own view.
	tasks := m.tasks
	sendKeys(m, "W")
	if m.nowOpen || m.groupBy != "" {
		t.Fatalf("W from Now must return to My Tasks, got now = %v, group = %q", m.nowOpen, m.groupBy)
	}
	m.Update(tasksMsg{seq: m.taskSeq, tasks: tasks})
	if len(m.visible) != len(tasks) {
		t.Fatalf("My Tasks lists %d tickets, want all %d", len(m.visible), len(tasks))
	}
	sendKeys(m, "W")
	if !m.nowOpen || m.groupBy != config.NowGrouping {
		t.Fatalf("W must reopen Now grouped by now, got group %q", m.groupBy)
	}
}

func TestNowHoldsTheTicketBeingRead(t *testing.T) {
	m := nowModel(t)
	if task, _ := m.selected(); task.GID != "mention" || m.nowHeld != "mention" {
		t.Fatalf("selected %s, held %q", task.GID, m.nowHeld)
	}
	if at, _ := m.deps.State.SeenAt("mention"); at != nowAt(12) {
		t.Fatalf("reading did not mark mention seen: %q", at)
	}
	// A refresh while reading keeps the ticket in its section.
	m.applyFilter()
	if got := nowRows(m); got[0] != "Needs reply/mention" {
		t.Fatalf("read ticket left its section during the visit: %v", got)
	}
	sendKeys(m, "j")
	want := []string{"New to you/fresh", "Updated/changed", "Waiting on others/asked"}
	if got := nowRows(m); !slices.Equal(got, want) {
		t.Fatalf("after leaving the read ticket, rows = %v, want %v", got, want)
	}
	if task, _ := m.selected(); task.GID != "fresh" {
		t.Fatalf("selection = %s, want fresh", task.GID)
	}
}
