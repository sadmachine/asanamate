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
	if cfg.Keymap, err = keymap.Resolve(map[string]map[string][]string{"main": {"quit": {"Q"}}}); err != nil {
		t.Fatal(err)
	}
	m, _ := testModel(t, cfg)
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	if _, cmd := m.Update(key("q")); cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("q still quits")
		}
	}
	_, cmd := m.Update(key("Q"))
	if cmd == nil {
		t.Fatal("Q did nothing")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("Q did not quit")
	}
}

func TestPairedBindingShowsAloneWhenItsPartnerIsUnbound(t *testing.T) {
	cfg := config.Config{}
	var err error
	if cfg.Keymap, err = keymap.Resolve(map[string]map[string][]string{"main": {"half_page_down": {}}}); err != nil {
		t.Fatal(err)
	}
	m, _ := testModel(t, cfg)
	if help := ansi.Strip(m.helpView()); !strings.Contains(help, "ctrl+u") || !strings.Contains(help, "half page up") {
		t.Fatalf("help lacks half_page_up:\n%s", help)
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
				m.readerHidden = target == "list-only"
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
			cfg := config.Default()
			cfg.Keymap, _ = keymap.Resolve(map[string]map[string][]string{"main": {"copy_comment": {"ctrl+y"}}})
			m, _ := testModel(t, cfg)
			m.Update(tasksMsg{tasks: []asana.Task{openTask}})
			if !tt.noDetail {
				m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask, Comments: []asana.Story{
					{GID: "c1", HTMLText: body},
					{HTMLText: "<body>Second comment</body>"},
				}}})
			}
			m.focusReader, m.fieldKey, m.readerHidden = !tt.list, tt.target, tt.noPreview
			if tt.markdown {
				m.readerView = config.ViewMarkdown
			}
			_, cmd := m.Update(key("ctrl+y"))
			if tt.want == "" {
				if cmd != nil || m.status != "" {
					t.Fatalf("non-comment target copied: command present = %v, status = %q", cmd != nil, m.status)
				}
				return
			}
			if cmd == nil {
				t.Fatal("comment target did not request clipboard write")
			}
			if got, want := commandMessages(cmd), tea.SetClipboard(tt.want)(); !slices.Contains(got, want) {
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
	want := []string{"assignee", "project:p1", addProjectKey, "my_tasks", "field:f1", emptyFieldsKey, "section:description", tabsKey, commentKey, "comment:c1", "comment:c2"}
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
	if m.modal == nil || m.modal.items[0].Label != "React" || m.input != nil {
		t.Fatal("enter on a comment must open the comment menu, not an editor")
	}
	m.modal = nil
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

func TestJumpMenuFocusesReaderSections(t *testing.T) {
	m, _ := editModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	tk := m.details["1"]
	tk.HTMLNotes = "<p>Description text</p>"
	tk.Comments = []asana.Story{
		{GID: "c1", CreatedBy: &asana.Ref{Name: "Amy"}, HTMLText: "<p>First</p>"},
		{GID: "c2", CreatedBy: &asana.Ref{Name: "Zed"}, HTMLText: "<p>Second</p>"},
	}
	m.details["1"] = tk
	m.renderDetail(false)
	press(m, "'")
	if m.modal == nil {
		t.Fatal("' must open the jump menu")
	}
	var got []string
	for _, item := range m.modal.items {
		got = append(got, item.Key+" "+item.Label)
	}
	if want := []string{"t Details", "d Description", "c Comments"}; !slices.Equal(got, want) {
		t.Fatalf("jump items = %q, want %q", got, want)
	}
	// Update, not press: press would wait out the flash timer.
	if _, cmd := m.Update(key("c")); cmd == nil {
		t.Fatal("c must start the flash timer")
	}
	if m.modal != nil || !m.focusReader || m.fieldKey != commentKey {
		t.Fatalf("c: modal %v, reader focus %v, target %q", m.modal != nil, m.focusReader, m.fieldKey)
	}
	heading := m.fieldLines[commentKey] - 1
	if m.reader.YOffset() != min(heading, m.reader.TotalLineCount()-m.reader.Height()) {
		t.Fatalf("comments offset = %d, want the heading on line %d at the top", m.reader.YOffset(), heading)
	}
	bar := m.sym.border.Left
	margin := func() string { return ansi.Strip(strings.Split(m.reader.GetContent(), "\n")[heading])[:len(bar)] }
	if margin() != bar {
		t.Fatalf("flashed comments margin = %q, want %q", margin(), bar)
	}
	m.Update(flashDoneMsg{seq: m.flashSeq - 1})
	if margin() != bar {
		t.Fatal("an older flash timer must not end the newest flash")
	}
	m.Update(flashDoneMsg{seq: m.flashSeq})
	if margin() == bar || m.fieldKey != commentKey {
		t.Fatalf("ended flash margin = %q, target %q", margin(), m.fieldKey)
	}
	m.Update(key("'"))
	m.Update(key("t"))
	if m.fieldKey != "assignee" || m.reader.YOffset() != 0 || m.flashKey != detailsKey {
		t.Fatalf("t target = %q at offset %d, flash %q", m.fieldKey, m.reader.YOffset(), m.flashKey)
	}
	if m.flashLines[0] <= m.flashLines[1] {
		t.Fatal("the boxed details card flashes its border, not the margin")
	}
	m.readerView = config.ViewMarkdown
	press(m, "'")
	if m.modal != nil {
		t.Fatal("the markdown view has no sections to jump to")
	}
}

func TestCardsShowNewestCommentsAsPreviews(t *testing.T) {
	m, _ := editModel(t)
	m.deps.Config.Reader.Comments = config.Default().Reader.Comments
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	tk := m.details["1"]
	long := "<p>" + strings.Repeat("word ", 40) + "</p>"
	for _, gid := range []string{"c1", "c2", "c3", "c4", "c5"} {
		tk.Comments = append(tk.Comments, asana.Story{GID: gid, CreatedBy: &asana.Ref{Name: "Amy"}, HTMLText: long})
	}
	tk.Comments[3].HTMLText = "<p>Short</p>"
	m.details["1"] = tk
	press(m, "2")

	lines := strings.Split(ansi.Strip(m.renderCards(tk, 60)), "\n")
	if line := lines[m.fieldLines[allCommentsKey]]; !strings.Contains(line, "Show all 5") {
		t.Fatalf("show all line = %q", line)
	}
	want := []string{commentKey, allCommentsKey, "comment:c3", "comment:c4", "comment:c5"}
	if got := m.cardTargets[len(m.cardTargets)-len(want):]; !slices.Equal(got, want) {
		t.Fatalf("comment targets = %q, want %q", got, want)
	}
	if !m.clipped["comment:c3"] || m.fieldEnds["comment:c3"]-m.fieldLines["comment:c3"] != 2 {
		t.Fatalf("c3 preview spans lines %d-%d, clipped %v", m.fieldLines["comment:c3"], m.fieldEnds["comment:c3"], m.clipped["comment:c3"])
	}
	if line := lines[m.fieldEnds["comment:c3"]]; !strings.HasSuffix(strings.TrimSpace(line), "see more …") {
		t.Fatalf("preview end = %q", line)
	}
	if m.clipped["comment:c4"] || m.clipped["comment:c5"] {
		t.Fatal("a short comment and the newest one must show in full")
	}
	if end := lines[m.fieldEnds["comment:c5"]]; strings.Contains(end, "see more") {
		t.Fatalf("newest comment end = %q", end)
	}

	hint := func(want string) bool {
		_, _, hints := m.mode()
		return slices.ContainsFunc(hints, func(h [2]string) bool { return h[1] == want })
	}
	m.fieldKey = "comment:c3"
	if !hint("see more") || hint("comment menu") {
		t.Fatal("a preview's enter hint must say see more")
	}
	press(m, "enter")
	if !hint("comment menu") {
		t.Fatal("a full comment's enter hint must say comment menu")
	}
	if m.modal != nil || !m.openComments["comment:c3"] || m.clipped["comment:c3"] {
		t.Fatalf("enter on a preview must show it in full: modal %v, open %v", m.modal != nil, m.openComments)
	}
	press(m, "enter")
	if m.modal == nil || m.modal.items[0].Label != "React" {
		t.Fatal("enter on a full comment must open its menu")
	}
	m.modal = nil

	m.fieldKey = allCommentsKey
	if !hint("show all") {
		t.Fatal("the Show all row's enter hint must say show all")
	}
	press(m, "enter")
	if !m.showComments || m.fieldKey != "comment:c1" || !slices.Contains(m.cardTargets, "comment:c2") || slices.Contains(m.cardTargets, allCommentsKey) {
		t.Fatalf("show all: shown %v, target %q, targets %q", m.showComments, m.fieldKey, m.cardTargets)
	}

	other := asana.Task{GID: "2", Name: "Other"}
	m.details["2"] = ticket.Ticket{Task: other}
	m.Update(tasksMsg{tasks: []asana.Task{m.tasks[0], other}})
	press(m, "esc", "j")
	if m.shownGID != "2" {
		t.Fatalf("shown = %q, want the next ticket", m.shownGID)
	}
	if m.showComments || m.openComments != nil {
		t.Fatalf("another ticket kept comment state: all %v, open %v", m.showComments, m.openComments)
	}
}

func TestCardsAllTabInterleavesActivity(t *testing.T) {
	m, _ := editModel(t)
	m.deps.Config.Reader.Comments = config.Default().Reader.Comments
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	tk := m.details["1"]
	tk.Comments = []asana.Story{
		{GID: "c1", CreatedAt: "2026-09-20T10:00:00Z", CreatedBy: &asana.Ref{Name: "Amy"}, HTMLText: "<p>First</p>"},
		{GID: "c2", CreatedAt: "2026-09-22T10:00:00Z", CreatedBy: &asana.Ref{Name: "Zed"}, HTMLText: "<p>Second</p>"},
	}
	tk.Events = []asana.Story{
		{GID: "e1", CreatedAt: "2026-09-19T10:00:00Z", CreatedBy: &asana.Ref{Name: "Sam"}, Text: "created this task"},
		{GID: "e2", CreatedAt: "2026-09-21T10:00:00Z", CreatedBy: &asana.Ref{Name: "Sam"}, Text: "assigned to Amy"},
		{GID: "e3", CreatedAt: "2026-09-21T11:00:00Z", CreatedBy: &asana.Ref{Name: "Sam"}, Text: "changed the due date"},
	}
	m.details["1"] = tk
	press(m, "2")
	text := ansi.Strip(m.renderCards(tk, 60))
	if !strings.Contains(text, "Comments 2 │ All 5") || strings.Contains(text, "assigned to Amy") {
		t.Fatalf("Comments tab:\n%s", text)
	}

	m.fieldKey = tabsKey
	_, _, hints := m.mode()
	if !slices.Contains(hints, [2]string{keyLabel("main", "open"), "switch tab"}) {
		t.Fatalf("tabs hints = %q", hints)
	}
	press(m, "enter")
	if !m.allActivity || m.fieldKey != tabsKey {
		t.Fatalf("enter on the tabs: all %v, target %q", m.allActivity, m.fieldKey)
	}
	lines := strings.Split(ansi.Strip(m.renderCards(tk, 60)), "\n")
	if line := lines[m.fieldLines[allCommentsKey]]; !strings.Contains(line, "Show all 5") {
		t.Fatalf("show all line = %q", line)
	}
	want := []string{tabsKey, commentKey, allCommentsKey, "comment:c2"}
	if got := m.cardTargets[len(m.cardTargets)-len(want):]; !slices.Equal(got, want) {
		t.Fatalf("All targets = %q, want %q", got, want)
	}
	// The newest three: two events on adjacent lines, then the newest comment.
	at := m.fieldLines[allCommentsKey] + 2
	if !strings.Contains(lines[at], "Sam · ") || !strings.Contains(lines[at], "assigned to Amy") ||
		!strings.Contains(lines[at+1], "changed the due date") || m.fieldLines["comment:c2"] != at+3 || !strings.Contains(lines[at+3], "Zed") {
		t.Fatalf("All tab lines from %d:\n%s", at, strings.Join(lines[at:], "\n"))
	}

	other := asana.Task{GID: "2", Name: "Other"}
	m.details["2"] = ticket.Ticket{Task: other}
	m.Update(tasksMsg{tasks: []asana.Task{m.tasks[0], other}})
	press(m, "esc", "j")
	if m.shownGID != "2" || !m.allActivity {
		t.Fatalf("shown %q, all %v: the All tab must stay chosen across tickets", m.shownGID, m.allActivity)
	}
}

func TestCardsCommentLimitsFollowConfig(t *testing.T) {
	m, _ := editModel(t)
	tk := m.details["1"]
	long := "<p>" + strings.Repeat("word ", 40) + "</p>"
	for _, gid := range []string{"c1", "c2", "c3"} {
		tk.Comments = append(tk.Comments, asana.Story{GID: gid, CreatedBy: &asana.Ref{Name: "Amy"}, HTMLText: long})
	}

	m.deps.Config.Reader.Comments = config.Comments{Latest: 2, PreviewLines: 1}
	m.renderCards(tk, 60)
	if slices.Contains(m.cardTargets, "comment:c1") || !slices.Contains(m.cardTargets, allCommentsKey) {
		t.Fatalf("latest 2 targets = %q", m.cardTargets)
	}
	if !m.clipped["comment:c2"] || m.fieldEnds["comment:c2"] != m.fieldLines["comment:c2"]+1 {
		t.Fatalf("preview_lines 1: c2 spans lines %d-%d", m.fieldLines["comment:c2"], m.fieldEnds["comment:c2"])
	}

	m.deps.Config.Reader.Comments = config.Comments{}
	m.renderCards(tk, 60)
	if !slices.Contains(m.cardTargets, "comment:c1") || slices.Contains(m.cardTargets, allCommentsKey) || len(m.clipped) != 3 || m.clipped["comment:c1"] {
		t.Fatalf("0s must list every comment in full: targets %q, clipped %v", m.cardTargets, m.clipped)
	}
}

func TestPreviewText(t *testing.T) {
	short := []string{"one   ", "", "two   "}
	if got, cut := previewText(short, 2, 20); cut || !slices.Equal(got, short) {
		t.Fatalf("short = %q, cut %v; want it unchanged", got, cut)
	}
	got, cut := previewText([]string{"one   ", "", "two   ", "three"}, 2, 20)
	if !cut || len(got) != 2 || ansi.Strip(got[1]) != "two see more …" {
		t.Fatalf("cut = %q, %v", got, cut)
	}
	got, _ = previewText([]string{"a", strings.Repeat("x", 30), "b"}, 2, 20)
	if line := ansi.Strip(got[1]); ansi.StringWidth(line) != 20 || !strings.HasSuffix(line, "x… see more …") {
		t.Fatalf("long line = %q", line)
	}
}

func TestCardsHeadLeavesAsanaFieldsToDetails(t *testing.T) {
	m, _ := editModel(t)
	due := "2026-10-13"
	tk := m.details["1"]
	tk.DueOn = &due
	m.deps.State.TaskBranches["1"] = "feature/x"
	out := ansi.Strip(m.renderCards(tk, 100))
	head := out[:strings.Index(out, "Details")]
	if strings.Contains(head, "Web") || strings.Contains(head, "Oct 13") || !strings.Contains(head, "feature/x") {
		t.Fatalf("head = %q, want the branch without project or due", head)
	}
	if body := out[strings.Index(out, "Details"):]; !strings.Contains(body, "Web") || !strings.Contains(body, "Oct 13") {
		t.Fatal("details must still show the project and due date")
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

func TestReboundMoveKeysScrollTheReader(t *testing.T) {
	for _, tc := range []struct{ binding, key string }{{"scroll_down", "ctrl+e"}, {"down", "w"}} {
		t.Run(tc.binding, func(t *testing.T) {
			m, _ := editModel(t)
			km, err := keymap.Resolve(map[string]map[string][]string{"main": {tc.binding: {tc.key}}})
			if err != nil {
				t.Fatal(err)
			}
			keys = km
			m.bindings = keyBindings()
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
			m.readerView = config.ViewMarkdown
			m.focusReader = true
			m.reader.SetContent(strings.Repeat("line\n", 100))
			m.Update(key(tc.key))
			if m.reader.YOffset() == 0 {
				t.Fatalf("%s bound to %s did not scroll the reader", tc.binding, tc.key)
			}
		})
	}
}
