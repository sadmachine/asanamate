package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/feed"
	"github.com/sadmachine/asanamate/internal/form"
	"github.com/sadmachine/asanamate/internal/keymap"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// cellOf draws the frame and returns the cell where text first shows.
func cellOf(t *testing.T, m *Model, text string) (x, y int) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return ansi.StringWidth(line[:i]), y
		}
	}
	t.Fatalf("%q is not drawn", text)
	return 0, 0
}

// clickOn clicks the cell where text first shows.
func clickOn(t *testing.T, m *Model, text string) tea.Cmd {
	t.Helper()
	x, y := cellOf(t, m, text)
	_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	return cmd
}

// wheelOn turns the wheel over the cell where text first shows.
func wheelOn(t *testing.T, m *Model, text string, button tea.MouseButton) {
	t.Helper()
	x, y := cellOf(t, m, text)
	m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: button})
}

func TestKeyPressMatchesEveryKeyName(t *testing.T) {
	var all []string
	for _, scope := range keymap.Default() {
		for _, ks := range scope {
			all = append(all, ks...)
		}
	}
	all = append(all, "ctrl+shift+q", "alt+enter", "f12", "space", "Q")
	for _, k := range all {
		if got := keyPress(k).String(); got != k {
			t.Errorf("keyPress(%q).String() = %q", k, got)
		}
	}
}

func TestUnlabelReadsOneKey(t *testing.T) {
	for label, want := range map[string]string{"enter": "enter", "↓": "down", "?": "?", "ctrl+/": "ctrl+/", "j/k": "", "↓/↑": ""} {
		if got, ok := keymap.Unlabel(label); ok != (want != "") || (ok && got != want) {
			t.Errorf("Unlabel(%q) = %q, %v", label, got, ok)
		}
	}
}

func TestMouseModeFollowsConfig(t *testing.T) {
	m, _ := testModel(t, config.Config{Mouse: config.Mouse{Enabled: true}})
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("mouse.enabled = true leaves the mouse off")
	}
	m, _ = testModel(t, config.Config{})
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("mouse.enabled = false turns the mouse on")
	}
}

func TestClickSelectsListRowAndFocusesPanes(t *testing.T) {
	m := splitModel(t)
	clickOn(t, m, "Write docs")
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	clickOn(t, m, "[2] Ticket")
	if !m.focusReader {
		t.Fatal("clicking the reader leaves it unfocused")
	}
	clickOn(t, m, "Fix login")
	if m.focusReader || m.cursor != 0 {
		t.Fatalf("focusReader = %v, cursor = %d", m.focusReader, m.cursor)
	}
}

func TestWheelMovesListAndScrollsReader(t *testing.T) {
	m := splitModel(t)
	wheelOn(t, m, "Fix login", tea.MouseWheelDown)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	wheelOn(t, m, "Fix login", tea.MouseWheelUp)
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
	m.reader.SetContent(strings.Repeat("line\n", 100))
	wheelOn(t, m, "[2] Ticket", tea.MouseWheelDown)
	if m.reader.YOffset() == 0 || m.focusReader {
		t.Fatalf("offset = %d, focusReader = %v", m.reader.YOffset(), m.focusReader)
	}
}

func TestDoubleClickOpensActionMenu(t *testing.T) {
	m := splitModel(t)
	m.deps.Config.Actions = []config.Action{goAction}
	m.Update(key("j"))
	clickOn(t, m, "Fix login")
	if m.modal != nil {
		t.Fatal("a single click opened a menu")
	}
	clickOn(t, m, "Fix login")
	if m.modal == nil || m.cursor != 0 {
		t.Fatalf("modal = %v, cursor = %d", m.modal, m.cursor)
	}
}

func TestClickPickerRowsAndChoose(t *testing.T) {
	m := splitModel(t)
	var picked string
	m.modal = newPicker(pickValue(func(s string) tea.Cmd { picked = s; return nil }), "Pick", items("Alpha", "Beta", "Gamma"))
	clickOn(t, m, "Gamma")
	if m.modal.cursor != 2 || picked != "" {
		t.Fatalf("cursor = %d, picked = %q", m.modal.cursor, picked)
	}
	clickOn(t, m, "Gamma")
	if picked != "Gamma" {
		t.Fatalf("picked = %q", picked)
	}
}

func TestWheelMovesPickerCursor(t *testing.T) {
	m := splitModel(t)
	m.modal = newPicker(nil, "Pick", items("Alpha", "Beta"))
	wheelOn(t, m, "Alpha", tea.MouseWheelDown)
	if m.modal.cursor != 1 {
		t.Fatalf("cursor = %d", m.modal.cursor)
	}
	if m.cursor != 0 {
		t.Fatal("the wheel over a picker moved the list")
	}
}

func TestClickViewsPanelOpensItem(t *testing.T) {
	m := wideModel(t, 200)
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	clickOn(t, m, "section")
	if m.groupBy != "section" || m.focusNav {
		t.Fatalf("groupBy = %q, focusNav = %v", m.groupBy, m.focusNav)
	}
	clickOn(t, m, "[0] Views")
	if !m.focusNav {
		t.Fatal("clicking the views panel's title leaves it unfocused")
	}
}

func TestClickStatuslineHintRunsItsKey(t *testing.T) {
	m := splitModel(t)
	help := keyLabel("main", "help")
	_, y := cellOf(t, m, " NORMAL ")
	line := ansi.Strip(m.statusline())
	x := strings.LastIndex(line, help+" keys")
	if x < 0 {
		t.Fatalf("statusline = %q", line)
	}
	m.Update(tea.MouseClickMsg{X: ansi.StringWidth(line[:x]), Y: y, Button: tea.MouseLeft})
	if !m.help {
		t.Fatal("clicking the keys hint left help closed")
	}
	m.View()
	m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if m.help {
		t.Fatal("a click left help open")
	}
}

func TestClickDismissesNoticeAndToast(t *testing.T) {
	m := splitModel(t)
	m.setStatus(statusInfo, "saved it")
	clickOn(t, m, "saved it")
	if m.toast != nil {
		t.Fatal("clicking the toast kept it")
	}
	m.notices = []statusMessage{{level: statusError, text: "it broke"}}
	m.View()
	m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if len(m.notices) != 1 {
		t.Fatal("a click outside the notice dismissed it")
	}
	clickOn(t, m, "it broke")
	if len(m.notices) != 0 {
		t.Fatal("clicking the notice kept it")
	}
}

func TestClickFeedSelectsItem(t *testing.T) {
	m := feedModel(t)
	m.Update(feedMsg{reply: feed.Reply{Source: "ci", Items: []feed.Item{{Text: "first entry"}, {Text: "second entry"}}}})
	clickOn(t, m, "second entry")
	if !m.focusFeed || m.feedCursor(m.shownFeed(m.feedRows())) != 1 {
		t.Fatalf("focusFeed = %v, cursor = %d", m.focusFeed, m.feed.cursor)
	}
}

func TestLinkAtFindsHyperlink(t *testing.T) {
	line := "see " + ansi.SetHyperlink("https://example.com/docs") + "the docs" + ansi.ResetHyperlink() + " now"
	for x, want := range map[int]string{0: "", 4: "https://example.com/docs", 11: "https://example.com/docs", 12: "", 40: ""} {
		if got := linkAt(line, x); got != want {
			t.Errorf("linkAt(%d) = %q, want %q", x, got, want)
		}
	}
}

func TestReaderDrawsDescriptionLinksAsHyperlinks(t *testing.T) {
	m := splitModel(t)
	task := openTask
	task.HTMLNotes = `<body>See <a href="https://example.com/docs">the docs</a></body>`
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: task}})
	x, y := cellOf(t, m, "the docs")
	var reader hitPane
	for _, p := range m.hits.panes {
		if p.id == paneReader {
			reader = p
		}
	}
	c := reader.content()
	if got := viewportLink(m.reader, x-c.x, y-c.y); got != "https://example.com/docs" {
		t.Fatalf("link = %q", got)
	}
	clickOn(t, m, "the docs")
	if _, cmd := m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}); cmd == nil {
		t.Fatal("clicking a link opens nothing")
	}
}

// dragFrom presses at from, moves to to, and releases there.
func dragFrom(m *Model, from, to cell) tea.Cmd {
	m.View()
	m.Update(tea.MouseClickMsg{X: from.x, Y: from.y, Button: tea.MouseLeft})
	m.View()
	m.Update(tea.MouseMotionMsg{X: to.x, Y: to.y, Button: tea.MouseLeft})
	m.View()
	_, cmd := m.Update(tea.MouseReleaseMsg{X: to.x, Y: to.y, Button: tea.MouseLeft})
	return cmd
}

func TestDragSelectsTextInsideOnePane(t *testing.T) {
	m := splitModel(t)
	m.focusReaderPane() // so the press does not redraw the ticket
	m.reader.SetContent("alpha beta\ngamma delta\nepsilon")
	x, y := cellOf(t, m, "beta")
	gx, gy := cellOf(t, m, "gamma")
	if cmd := dragFrom(m, cell{x, y}, cell{gx + 4, gy}); cmd == nil {
		t.Fatal("a drag copied nothing")
	}
	if got := m.selection.text(m.hits.frame); got != "beta\ngamma" {
		t.Fatalf("selection = %q", got)
	}
	view := m.View().Content
	if ansi.Strip(view) != ansi.Strip(strings.Join(m.hits.frame, "\n")) || !strings.Contains(view, "\x1b[7m") {
		t.Fatal("the selection is not highlighted in place")
	}
	// Dragging past the pane stops at its edge, leaving the list out.
	lx, ly := cellOf(t, m, "Fix login")
	dragFrom(m, cell{x, y}, cell{lx, ly + 3})
	if got := m.selection.text(m.hits.frame); strings.Contains(got, "Fix login") || !strings.HasPrefix(got, "beta\n") {
		t.Fatalf("selection = %q", got)
	}
	m.Update(key("j"))
	if m.selection != nil {
		t.Fatal("a key kept the selection")
	}
}

func TestDragOverLinkCopiesInsteadOfOpening(t *testing.T) {
	m := splitModel(t)
	m.focusReaderPane()
	m.reader.SetContent("see " + ansi.SetHyperlink("https://example.com") + "the docs" + ansi.ResetHyperlink())
	x, y := cellOf(t, m, "the docs")
	dragFrom(m, cell{x, y}, cell{x + 3, y})
	if m.selection == nil || m.selection.text(m.hits.frame) != "the" {
		t.Fatalf("selection = %q", m.selection.text(m.hits.frame))
	}
}

func TestDragSelectsInEditor(t *testing.T) {
	m := splitModel(t)
	m.input = newInputBox("Title", "")
	m.input.area.SetValue("hello world")
	x, y := cellOf(t, m, "world")
	if cmd := dragFrom(m, cell{x, y}, cell{x + 5, y}); cmd == nil {
		t.Fatal("an editor drag copied nothing")
	}
	if got := m.input.area.SelectedText(); got != "world" {
		t.Fatalf("selected = %q", got)
	}
	m.input.area.ClearSelection()
	clickOn(t, m, "hello")
	if m.input.area.Line() != 0 || m.input.area.Column() != 0 {
		t.Fatalf("cursor = %d:%d", m.input.area.Line(), m.input.area.Column())
	}
}

func TestClickFormFieldsAndTheirPicker(t *testing.T) {
	m := splitModel(t)
	spec := form.Spec{Info: []string{"Pick a project"}, Fields: []form.Field{
		{ID: "notes", Label: "Notes", Type: form.String},
		{ID: "project", Label: "Project", Type: form.Select, Options: []form.Option{{ID: "a", Name: "Apple"}, {ID: "b", Name: "Banana"}}},
	}}
	m.form = newFormModal("Log", spec, nil, func(map[string]string) tea.Cmd { return nil })
	clickOn(t, m, "Project:")
	if m.form.cursor != 1 {
		t.Fatalf("cursor = %d", m.form.cursor)
	}
	clickOn(t, m, "Project:")
	if m.form.picker == nil {
		t.Fatal("a double click left the field closed")
	}
	clickOn(t, m, "Banana")
	clickOn(t, m, "Banana")
	if m.form.picker != nil || m.form.values["project"] != "b" {
		t.Fatalf("picker = %v, values = %v", m.form.picker, m.form.values)
	}
	wheelOn(t, m, "Notes:", tea.MouseWheelUp)
	if m.form.cursor != 0 {
		t.Fatalf("cursor = %d", m.form.cursor)
	}
}

func TestComposerPaneTakesClicksAndDrags(t *testing.T) {
	for _, width := range []int{140, 80} { // beside the list, full-screen reader
		m, _ := editModel(t)
		m.width, m.height = width, 40
		m.layout()
		press(m, "c")
		m.input.area.SetValue("first line\nsecond words")
		press(m, "esc")
		if !m.composerPane() || !m.composer.reading {
			t.Fatalf("width %d: composer pane not shown beside the reader", width)
		}
		x, y := cellOf(t, m, "second")
		if cmd := dragFrom(m, cell{x, y}, cell{x + 6, y}); cmd == nil {
			t.Fatalf("width %d: a composer drag copied nothing", width)
		}
		if got := m.composer.box.area.SelectedText(); got != "second" || m.composer.reading {
			t.Fatalf("width %d: selected = %q, reading = %v", width, got, m.composer.reading)
		}
		m.composer.box.area.ClearSelection()
		fx, fy := cellOf(t, m, "first")
		m.Update(tea.MouseClickMsg{X: fx + 2, Y: fy, Button: tea.MouseLeft})
		if a := m.composer.box.area; a.Line() != 0 || a.Column() != 2 {
			t.Fatalf("width %d: cursor = %d:%d", width, a.Line(), a.Column())
		}
		m.reader.SetContent(strings.Repeat("line\n", 100))
		wheelOn(t, m, "first", tea.MouseWheelDown)
		if m.reader.YOffset() != 0 {
			t.Fatalf("width %d: the wheel over the composer scrolled the reader", width)
		}
	}
}
