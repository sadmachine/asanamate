package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/feed"
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
	if cmd := clickOn(t, m, "the docs"); cmd == nil {
		t.Fatal("clicking a link opens nothing")
	}
}
