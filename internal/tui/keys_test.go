package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/keymap"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func TestEveryMainBindingHasAHandler(t *testing.T) {
	h := handlers()
	var names []string
	for _, b := range keyBindings() {
		names = append(names, b.Name)
		if b.run == nil {
			t.Errorf("%s has no handler", b.Name)
		}
	}
	for name := range h {
		if !slices.Contains(names, name) {
			t.Errorf("handler %s is not in the keymap catalog", name)
		}
	}
}

func TestHelpAndHintsComeFromTheTable(t *testing.T) {
	m := splitModel(t)
	help := ansi.Strip(m.helpView())
	for _, b := range keyBindings() {
		if b.Pair == "" && m.available(b) && !strings.Contains(help, b.Desc) {
			t.Errorf("help lacks %q", b.Desc)
		}
	}
	if !strings.Contains(help, "ctrl+d/ctrl+u") {
		t.Error("paired bindings must share a help row")
	}
	if got := m.keyHints("action", "help", "nope"); len(got) != 2 || got[0] != [2]string{"space", "act"} || got[1] != [2]string{"?", "keys"} {
		t.Fatalf("hints = %q", got)
	}
}

func TestReboundKeyRunsItsBinding(t *testing.T) {
	cfg := config.Config{}
	var err error
	if cfg.Keymap, err = keymap.Resolve(map[string]map[string][]string{"main": {"quit": {"x"}}}); err != nil {
		t.Fatal(err)
	}
	m, _ := testModel(t, cfg)
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	if _, cmd := m.Update(key("q")); cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("q still quits")
		}
	}
	_, cmd := m.Update(key("x"))
	if cmd == nil {
		t.Fatal("x did nothing")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("x did not quit")
	}
}

func TestEnterOpensTheReader(t *testing.T) {
	m := splitModel(t)
	m.Update(key("enter"))
	if !m.focusReader {
		t.Fatal("enter in the list must focus the reader")
	}
}

func TestActionMenuKeys(t *testing.T) {
	for _, target := range []string{"list", "list-only", "markdown", "assignee", "comment:9"} {
		for _, k := range []string{" ", "a"} {
			t.Run(target+"/"+k, func(t *testing.T) {
				m, _ := testModel(t, config.Config{Actions: []config.Action{{Name: "Go", Key: "x", Mode: config.ModeExit, Command: "true"}}})
				m.Update(tasksMsg{tasks: []asana.Task{openTask}})
				m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask}})
				m.deps.NoPreview = target == "list-only"
				m.focusReader = target != "list" && target != "list-only"
				if target == "markdown" {
					m.readerView = config.ViewMarkdown
				} else if m.focusReader {
					m.fieldKey = target
				}
				if target != "assignee" {
					m.Update(key("enter"))
					if m.modal != nil || m.menuFor != "" || m.status != "" {
						t.Fatal("Enter must not request actions")
					}
				}
				m.Update(key(k))
				if m.modal == nil || !strings.HasPrefix(m.modal.title, "Run on: ") {
					t.Fatalf("modal = %+v", m.modal)
				}
			})
		}
	}
}

func TestCopyCommentOnlyOnTarget(t *testing.T) {
	const body = `<body>Hello <a data-asana-type="user" href="https://app.asana.com/0/123/list">@Austin Fishbaugh</a><br><strong>Ready</strong> <a href="https://example.com">docs</a></body>`
	for _, tt := range []struct {
		name, target                        string
		list, markdown, noPreview, noDetail bool
		want                                string
	}{
		{name: "comment", target: "comment:c1", want: "Hello @Austin Fishbaugh  \n**Ready** [docs](https://example.com)"},
		{name: "comment without GID", target: "comment:index:1", want: "Second comment"},
		{name: "list", target: "comment:c1", list: true},
		{name: "markdown", target: "comment:c1", markdown: true},
		{name: "list only", target: "comment:c1", noPreview: true},
		{name: "missing details", target: "comment:c1", noDetail: true},
		{name: "field", target: "assignee"},
		{name: "section", target: "section:description"},
		{name: "add comment", target: commentKey},
		{name: "stale comment", target: "comment:missing"},
		{name: "no target"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := testModel(t, config.Default())
			m.Update(tasksMsg{tasks: []asana.Task{openTask}})
			if !tt.noDetail {
				m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask, Comments: []asana.Story{
					{GID: "c1", HTMLText: body},
					{HTMLText: "<body>Second comment</body>"},
				}}})
			}
			m.focusReader, m.fieldKey, m.deps.NoPreview = !tt.list, tt.target, tt.noPreview
			if tt.markdown {
				m.readerView = config.ViewMarkdown
			}
			_, _, hints := m.mode()
			if got := slices.Contains(hints, [2]string{"Y", "copy comment"}); got != (tt.want != "" && !tt.noPreview) {
				t.Fatalf("copy hint = %v, hints = %v", got, hints)
			}
			_, cmd := m.Update(key("Y"))
			if tt.want == "" {
				if cmd != nil || m.status != "" {
					t.Fatalf("non-comment target copied: command present = %v, status = %q", cmd != nil, m.status)
				}
				return
			}
			if cmd == nil {
				t.Fatal("comment target did not request clipboard write")
			}
			if got, want := cmd(), tea.SetClipboard(tt.want)(); got != want {
				t.Fatalf("clipboard message = %#v, want %#v", got, want)
			}
			if m.status != "comment sent to clipboard" {
				t.Fatalf("status = %q", m.status)
			}
		})
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

func TestPageKeysMoveTheList(t *testing.T) {
	m := splitModel(t)
	ctrl := func(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }
	for _, tc := range []struct {
		key  rune
		want int
	}{{'d', 1}, {'u', 0}, {'f', 1}, {'b', 0}} {
		m.Update(ctrl(tc.key))
		if m.cursor != tc.want {
			t.Fatalf("ctrl+%c: cursor = %d, want %d", tc.key, m.cursor, tc.want)
		}
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
	want := []string{"assignee", "project:p1", addProjectKey, "my_tasks", "field:f1", emptyFieldsKey, "section:description", commentKey, "comment:c1", "comment:c2"}
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
