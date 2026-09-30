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

func TestKeyBindingsAreUniqueAndDocumented(t *testing.T) {
	seen := map[string]string{}
	for _, b := range keyBindings() {
		if b.desc == "" || !slices.Contains(helpGroups, b.group) || b.run == nil {
			t.Errorf("binding %q: desc %q, group %q", b.keys, b.desc, b.group)
		}
		for _, k := range b.keys {
			if prev, ok := seen[k]; ok {
				t.Errorf("%q runs both %q and %q", k, prev, b.desc)
			}
			seen[k] = b.desc
		}
	}
}

func TestHelpAndHintsComeFromTheTable(t *testing.T) {
	m := splitModel(t)
	help := ansi.Strip(m.helpView())
	for _, b := range keyBindings() {
		if _, ok := m.bindingFor(b.keys[0]); ok && !strings.Contains(help, b.desc) {
			t.Errorf("help lacks %q", b.desc)
		}
	}
	if got := m.keyHints("enter", "?", "nope"); len(got) != 2 || got[0] != [2]string{"enter", "act"} || got[1] != [2]string{"?", "keys"} {
		t.Fatalf("hints = %q", got)
	}
}

func TestReaderScrollsWithMoveKeys(t *testing.T) {
	m := splitModel(t)
	m.Update(key("2"))
	m.Update(key("esc")) // back to the list, then into the reader without a field
	m.focusReader = true
	m.Update(key("j"))
	if m.cursor != 0 {
		t.Fatalf("j in the reader moved the list to %d", m.cursor)
	}
}

func TestCardsNavigateFieldsSectionsAndComments(t *testing.T) {
	m, _ := editModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	tk := m.details["1"]
	tk.HTMLNotes = "<p>Description text</p>"
	tk.Comments = []asana.Story{
		{GID: "c1", CreatedBy: &asana.Ref{Name: "Amy"}, HTMLText: "<p>First</p>"},
		{GID: "c2", CreatedBy: &asana.Ref{Name: "Zed"}, HTMLText: "<p>Second</p>"},
	}
	m.details["1"] = tk
	press(m, "2")
	want := []string{"assignee", "project:p1", "my_tasks", "field:f1", "section:description", commentKey, "comment:c1", "comment:c2"}
	for i, key := range want {
		if m.fieldKey != key {
			t.Fatalf("target %d = %q, want %q", i, m.fieldKey, key)
		}
		if i < len(want)-1 {
			press(m, "j")
		}
	}
	lines := strings.Split(ansi.Strip(m.renderCards(tk, 60)), "\n")
	if line := lines[m.fieldLines["comment:c2"]]; !strings.Contains(line, "Zed") {
		t.Fatalf("comment line = %q", line)
	}
	if line := lines[m.fieldEnds["comment:c2"]]; !strings.Contains(line, "Second") {
		t.Fatalf("comment end line = %q", line)
	}
	for _, line := range []int{m.fieldLines["comment:c2"], m.fieldEnds["comment:c2"]} {
		if line < m.reader.YOffset() || line >= m.reader.YOffset()+m.reader.Height() {
			t.Fatalf("comment line %d outside reader at %d, height %d", line, m.reader.YOffset(), m.reader.Height())
		}
	}
	if name, _, _ := m.mode(); name != "READ" {
		t.Fatalf("comment mode = %q", name)
	}
	press(m, "enter")
	if m.modal != nil || m.input != nil {
		t.Fatal("comment selection opened an editor")
	}
	press(m, "j")
	if m.fieldKey != "assignee" || m.reader.YOffset() != 0 {
		t.Fatalf("wrapped target = %q at offset %d, want the ticket's top", m.fieldKey, m.reader.YOffset())
	}
	press(m, "k")
	if m.fieldKey != "comment:c2" {
		t.Fatalf("reverse target = %q", m.fieldKey)
	}
	offset := m.reader.YOffset()
	press(m, "up")
	if m.fieldKey != "comment:c2" || m.reader.YOffset() >= offset {
		t.Fatalf("up changed target %q or did not scroll from %d to %d", m.fieldKey, offset, m.reader.YOffset())
	}
	press(m, "tab")
	if m.fieldKey != "assignee" {
		t.Fatalf("tab from comment = %q", m.fieldKey)
	}
	press(m, "G")
	if m.fieldKey != "comment:c2" || !m.reader.AtBottom() {
		t.Fatalf("G target = %q, at bottom %v", m.fieldKey, m.reader.AtBottom())
	}
	press(m, "g")
	if m.fieldKey != "assignee" || m.reader.YOffset() != 0 {
		t.Fatalf("g target = %q at offset %d", m.fieldKey, m.reader.YOffset())
	}
}

func TestMarkdownKeepsJScroll(t *testing.T) {
	m, _ := editModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m.readerView = config.ViewMarkdown
	m.focusReader = true
	m.reader.SetContent(strings.Repeat("line\n", 100))
	press(m, "j")
	if m.reader.YOffset() == 0 || m.fieldKey != "" {
		t.Fatalf("markdown scroll = %d, target = %q", m.reader.YOffset(), m.fieldKey)
	}
	if press(m, "G"); !m.reader.AtBottom() {
		t.Fatalf("markdown G scroll = %d", m.reader.YOffset())
	}
	if press(m, "g"); m.reader.YOffset() != 0 {
		t.Fatalf("markdown g scroll = %d", m.reader.YOffset())
	}
}
