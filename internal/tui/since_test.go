package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
)

// sinceModel is editModel's ticket, last seen at 10:00, with an old comment,
// a newer comment and change from Zed, and a newer comment of your own.
func sinceModel(t *testing.T) *Model {
	t.Helper()
	m, _ := editModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	m.sinceShow, m.meGID = true, "u1"
	m.deps.State.SeenTasks = map[string]string{"1": "2026-09-26T10:00:00.000Z"}
	tk := m.details["1"]
	tk.ModifiedAt = "2026-09-26T12:00:00.000Z"
	zed, me := &asana.Ref{GID: "u2", Name: "Zed"}, &asana.Ref{GID: "u1", Name: "Amy"}
	tk.Comments = []asana.Story{
		{GID: "c1", CreatedBy: zed, CreatedAt: "2026-09-26T09:00:00.000Z", HTMLText: "<body>First</body>"},
		{GID: "c2", CreatedBy: zed, CreatedAt: "2026-09-26T11:00:00.000Z", HTMLText: "<body>Second</body>"},
		{GID: "c3", CreatedBy: me, CreatedAt: "2026-09-26T12:00:00.000Z", HTMLText: "<body>Mine</body>"},
	}
	tk.Events = []asana.Story{{GID: "e1", CreatedBy: zed, CreatedAt: "2026-09-26T11:30:00.000Z", Text: "changed the due date"}}
	m.details["1"] = tk
	m.tasks[0].ModifiedAt, m.visible[0].ModifiedAt = tk.ModifiedAt, tk.ModifiedAt
	return m
}

func readerText(m *Model) string { return ansi.Strip(m.reader.GetContent()) }

func TestSinceCardListsNewEntriesOnce(t *testing.T) {
	m := sinceModel(t)
	m.renderDetail(false)
	text := readerText(m)
	if !strings.Contains(text, "Since you looked 2") {
		t.Fatalf("no Since card with the new comment and change:\n%s", text)
	}
	for word, want := range map[string]int{"First": 1, "Second": 1, "Mine": 1, "changed the due date": 1} {
		if got := strings.Count(text, word); got != want {
			t.Errorf("%q shows %d times, want %d", word, got, want)
		}
	}
	since, tabs := strings.Index(text, "Since you looked"), strings.Index(text, "Comments 3")
	if !(since < strings.Index(text, "Second") && strings.Index(text, "Second") < tabs && tabs < strings.Index(text, "First")) {
		t.Fatal("the new comment must sit in the card, above the older activity")
	}
	if i, j := slices.Index(m.cardTargets, "comment:c2"), slices.Index(m.cardTargets, tabsKey); i < 0 || i > j {
		t.Fatalf("targets %q must list the new comment before the tabs", m.cardTargets)
	}
	if line := strings.Split(text, "\n")[m.fieldLines["comment:c2"]]; !strings.Contains(line, "Zed") {
		t.Fatalf("comment:c2 line = %q", line)
	}

	// MarkSeen still runs on open, and the cutoff holds for the visit.
	if got := m.deps.State.SeenTasks["1"]; got != "2026-09-26T12:00:00.000Z" {
		t.Fatalf("seen = %q, want the opened version", got)
	}
	m.renderDetail(true)
	if !strings.Contains(readerText(m), "Since you looked 2") {
		t.Fatal("a refresh during the visit must keep the card")
	}

	// The comment acts the same inside the card as in the activity.
	m.focusReader, m.fieldKey = true, "comment:c2"
	press(m, "enter")
	if m.modal == nil || m.modal.items[0].Label != "React" {
		t.Fatal("enter on a comment in the card must open the comment menu")
	}
}

func TestSinceCardClearsOnTheNextVisit(t *testing.T) {
	m := sinceModel(t)
	m.renderDetail(false)
	m.keepSinceCutoff("2") // another ticket's visit
	m.renderDetail(false)
	if text := readerText(m); strings.Contains(text, "Since you looked") || strings.Count(text, "Second") != 1 {
		t.Fatalf("a later visit with nothing new must show no card:\n%s", text)
	}
}

func TestSinceCardHidden(t *testing.T) {
	for name, setup := range map[string]func(m *Model){
		"first view":  func(m *Model) { delete(m.deps.State.SeenTasks, "1") },
		"setting off": func(m *Model) { m.sinceShow = false },
	} {
		t.Run(name, func(t *testing.T) {
			m := sinceModel(t)
			setup(m)
			m.renderDetail(false)
			if text := readerText(m); strings.Contains(text, "Since you looked") || strings.Count(text, "Second") != 1 {
				t.Fatalf("want no card and every comment in the activity:\n%s", text)
			}
		})
	}
}

func TestSinceSectionInMarkdownView(t *testing.T) {
	m := sinceModel(t)
	m.readerView = config.ViewMarkdown
	m.renderDetail(false)
	text := readerText(m)
	since, activity := strings.Index(text, "Since you looked"), strings.Index(text, "Activity")
	if since < 0 || activity < since || strings.Count(text, "Second") != 1 || strings.Index(text, "Second") > activity {
		t.Fatalf("markdown must list the new comment once, in its own section above the activity:\n%s", text)
	}
}
