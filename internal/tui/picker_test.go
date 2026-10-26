package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	if r, ok := strings.CutPrefix(s, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: []rune(r)[0], Mod: tea.ModCtrl}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

// search opens p's search field and types s.
func search(p *picker, s string) {
	p.update(key("/"))
	typeText(p, s)
}

func typeText(p *picker, s string) {
	for _, r := range s {
		p.update(key(string(r)))
	}
}

func items(labels ...string) []pickItem {
	out := make([]pickItem, len(labels))
	for i, l := range labels {
		out[i] = pickItem{Label: l, Value: l}
	}
	return out
}

func TestPickerFiltersByWords(t *testing.T) {
	p := newPicker(nil, "Projects", items("Web App", "Mobile", "Web Site"))
	search(p, "site web")
	if len(p.matches) != 1 || p.items[p.matches[0]].Label != "Web Site" {
		t.Fatalf("matches = %v", p.matches)
	}
}

func TestPickerEnterSelectsHighlighted(t *testing.T) {
	p := newPicker(nil, "Projects", items("A", "B"))
	p.update(key("down"))
	res, _ := p.update(key("enter"))
	if !res.done || res.item == nil || res.item.Label != "B" {
		t.Fatalf("res = %+v", res)
	}
}

func TestPickerFreeText(t *testing.T) {
	p := newPicker(nil, "Repo", items("/code/web"))
	p.allowFree = true
	search(p, "/tmp/x")
	if !strings.Contains(ansi.Strip(p.view(80, 12, modalLook{})), `Use "/tmp/x"`) {
		t.Fatal("typed values must show a Use row")
	}
	res, _ := p.update(key("enter"))
	if !res.done || res.item != nil || res.free != "/tmp/x" {
		t.Fatalf("enter on the Use row: res = %+v", res)
	}
	p = newPicker(nil, "Repo", items("/code/web"))
	p.allowFree = true
	search(p, "web")
	if res, _ := p.update(key("enter")); res.item == nil || res.item.Label != "/code/web" {
		t.Fatalf("enter must pick the top match: res = %+v", res)
	}
	p = newPicker(nil, "Repo", items("/code/web"))
	p.allowFree = true
	search(p, "web")
	if res, _ := p.update(key("ctrl+s")); !res.done || res.free != "web" {
		t.Fatalf("ctrl+s: res = %+v", res)
	}
}

func TestPickerKeySelect(t *testing.T) {
	p := newPicker(nil, "Actions", []pickItem{{Label: "Claude", Key: "c"}, {Label: "View", Key: "v"}})
	p.keySelect = true
	if res, _ := p.update(key("z")); res.done {
		t.Fatal("unbound key must not select")
	}
	res, _ := p.update(key("v"))
	if !res.done || res.item.Label != "View" {
		t.Fatalf("res = %+v", res)
	}
}

func TestPickerEscCancels(t *testing.T) {
	p := newPicker(nil, "Projects", items("A"))
	if res, _ := p.update(key("esc")); !res.cancelled {
		t.Fatalf("res = %+v", res)
	}
}

func TestPickerMultiTogglesAndSaves(t *testing.T) {
	p := newMultiPicker(nil, "Scope", items("A", "B", "C"), map[int]bool{0: true})
	p.update(key("j"))
	p.update(key("space"))
	p.update(key("k"))
	p.update(key("tab"))
	res, _ := p.update(key("enter"))
	if !res.done || len(res.items) != 1 || res.items[0].Label != "B" {
		t.Fatalf("res = %+v", res)
	}
}

func TestPickerTypesBoundKeysWhileSearching(t *testing.T) {
	p := newMultiPicker(nil, "Scope", items("a/j k?", "B"), map[int]bool{})
	search(p, "/j k?")
	if p.input.Value() != "/j k?" || len(p.matches) != 1 {
		t.Fatalf("value = %q, matches = %d", p.input.Value(), len(p.matches))
	}
	p.update(key("tab"))
	if !p.checked[0] {
		t.Fatal("tab must toggle while searching")
	}
}

func TestKeySelectItemKeysWinOverNavigation(t *testing.T) {
	p := newPicker(nil, "Actions", []pickItem{{Label: "Jump", Key: "j"}, {Label: "Search", Key: "/"}})
	p.keySelect = true
	if res, _ := p.update(key("j")); res.item == nil || res.item.Label != "Jump" {
		t.Fatalf("j: res = %+v", res)
	}
	if res, _ := p.update(key("/")); res.item == nil || res.item.Label != "Search" {
		t.Fatalf("/: res = %+v", res)
	}
}

func TestTypeFirstStartsSearching(t *testing.T) {
	typeFirst = true
	defer func() { typeFirst = false }()
	p := newPicker(nil, "Projects", items("Alpha", "Beta"))
	typeText(p, "be")
	if len(p.matches) != 1 {
		t.Fatalf("matches = %d", len(p.matches))
	}
	p.update(key("esc"))
	if p.searching {
		t.Fatal("esc must leave search for browse mode")
	}
	if res, _ := p.update(key("esc")); !res.cancelled {
		t.Fatal("a second esc must close the picker")
	}
}

func TestPickerHelpUsesFilteredSelectionAndFitsModal(t *testing.T) {
	p := newPicker(nil, "Settings", []pickItem{
		{Label: "Name", Help: "Required menu name."},
		{Label: "Input title", Help: "Leave blank to skip free-text input. Set a title to ask for notes before running."},
	})
	style := modalLook{}
	initial := p.view(40, 10, style)
	p.update(key("down"))
	selected := p.view(40, 10, style)
	if lipgloss.Height(initial) != lipgloss.Height(selected) {
		t.Fatal("help changed modal height")
	}
	search(p, "input")
	view := ansi.Strip(p.view(40, 10, style))
	if !strings.Contains(view, "Leave blank") || strings.Contains(view, "Required menu name.") {
		t.Fatal("help used unfiltered cursor")
	}
	if lipgloss.Height(view) > 10 || !strings.Contains(view, "enter select") {
		t.Fatal("help hid controls")
	}
	for _, line := range strings.Split(ansi.Strip(modalLook{sym: asciiSymbols}.message(statusInfo, p.items[1].Help, 40, 2)), "\n") {
		if ansi.StringWidth(line) > 40 {
			t.Fatalf("line wider than modal: %q", line)
		}
	}
	typeText(p, "missing")
	view = ansi.Strip(p.view(40, 10, style))
	if !strings.Contains(view, "no matches") || strings.Contains(view, "Leave blank") {
		t.Fatal("empty filter retained stale help")
	}
}

func TestBrowsePickerRequiresSlashBeforeFiltering(t *testing.T) {
	p := newPicker(nil, "Settings", items("Name", "Key", "Input title"))
	p.update(key("j"))
	if p.cursor != 1 || p.input.Value() != "" {
		t.Fatal("j filtered instead of navigating")
	}
	p.update(key("k"))
	if p.cursor != 0 {
		t.Fatal("k did not navigate")
	}
	p.update(key("x"))
	if p.input.Value() != "" || len(p.matches) != 3 {
		t.Fatal("browse accepted search text")
	}
	if p.searching || p.input.Focused() {
		t.Fatal("browse started a search")
	}
	p.update(key("/"))
	if !p.searching || !p.input.Focused() {
		t.Fatal("slash did not start search")
	}
	typeText(p, "k")
	if p.input.Value() != "k" || len(p.matches) != 1 || p.items[p.matches[0]].Label != "Key" {
		t.Fatal("k navigated while searching")
	}
	view := ansi.Strip(p.view(80, 12, modalLook{}))
	if !strings.Contains(view, "/ k") || !strings.Contains(view, "clear search") {
		t.Fatal("search mode not visible")
	}
	if res, _ := p.update(key("esc")); res.cancelled || p.searching || p.input.Value() != "" || p.cursor != 1 {
		t.Fatal("esc did not return to selected browse row")
	}
	if res, _ := p.update(key("esc")); !res.cancelled {
		t.Fatal("browse esc did not cancel")
	}
}

func TestTypeFirstSkipsKeySelectPickers(t *testing.T) {
	typeFirst = true
	defer func() { typeFirst = false }()
	p := newPicker(nil, "Actions", []pickItem{{Label: "View", Key: "v"}})
	p.keySelect = true
	if res, _ := p.update(key("esc")); !res.cancelled {
		t.Fatal("one esc must close a key-select picker")
	}
}

func TestSearchKeepsHomeAndEndForTheText(t *testing.T) {
	p := newPicker(nil, "Pick", items("Ab", "Ac", "Ad"))
	search(p, "a")
	p.cursor = 1
	p.update(key("home"))
	if p.cursor != 1 || p.input.Position() != 0 {
		t.Fatalf("home: cursor = %d, text position = %d", p.cursor, p.input.Position())
	}
	p.update(key("end"))
	if p.cursor != 1 || p.input.Position() != 1 {
		t.Fatalf("end: cursor = %d, text position = %d", p.cursor, p.input.Position())
	}
}

func TestKeySelectHomeAndEndMove(t *testing.T) {
	p := newPicker(nil, "Pick", items("A", "B", "C"))
	p.keySelect = true
	p.update(key("end"))
	if p.cursor != 2 {
		t.Fatalf("end: cursor = %d", p.cursor)
	}
	p.update(key("home"))
	if p.cursor != 0 {
		t.Fatalf("home: cursor = %d", p.cursor)
	}
}

func TestStopSearchFromUseRowGoesToTop(t *testing.T) {
	p := newPicker(nil, "Pick", items("Apple", "Apricot", "Banana"))
	p.allowFree = true
	search(p, "ap")
	p.update(key("down"))
	p.update(key("down"))
	if p.cursor != 2 {
		t.Fatalf("not on the Use row: cursor = %d", p.cursor)
	}
	p.update(key("esc"))
	if p.searching || p.cursor != 0 {
		t.Fatalf("searching = %v, cursor = %d", p.searching, p.cursor)
	}
}

func TestReorderablePickerPreservesChecksAndRequiresUnfilteredOrder(t *testing.T) {
	p := newMultiPicker(nil, "Columns", items("A", "B", "C"), map[int]bool{0: true, 2: true})
	p.reorder = true
	p.update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModCtrl}) // already first
	p.update(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModCtrl})
	if p.cursor != 1 || p.items[1].Label != "A" || !p.checked[1] || p.checked[0] {
		t.Fatal("reordering detached the cursor or check from the item")
	}
	search(p, "C")
	p.update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModCtrl})
	if p.err != "Clear search before reordering" || p.items[2].Label != "C" {
		t.Fatal("filtered reorder changed hidden item order")
	}
	res, _ := p.update(key("enter"))
	if !res.done || len(res.items) != 2 || res.items[0].Label != "A" || res.items[1].Label != "C" {
		t.Fatalf("saving while filtered lost checked items or order: %+v", res)
	}
}

func TestColumnPickerControlsFitNarrowModal(t *testing.T) {
	p := newMultiPicker(nil, strings.Repeat("Long project ", 10), items("A", "B", "C", "D", "E", "F", "G"), map[int]bool{})
	p.reorder, p.allowReset = true, true
	p.searching = true
	p.input.Focus()
	view := ansi.Strip(p.view(40, 12, modalLook{}))
	if lipgloss.Height(view) > 12 || !strings.Contains(view, "enter") || !strings.Contains(view, "ctrl+r") || !strings.Contains(view, "ctrl+up/down") {
		t.Fatalf("column controls do not fit: %q", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > 40 {
			t.Fatalf("line wider than modal: %q", line)
		}
	}
}

// A link picker opens on choose and copies on y, or ctrl+y while searching;
// search matches the URL behind a label.
func TestLinkPickerCopies(t *testing.T) {
	p := newPicker(nil, "Links", []pickItem{
		{Label: "Spec", Hint: "https://a.test/spec", Search: "https://a.test/spec", Value: "https://a.test/spec"},
		{Label: "https://b.test", Value: "https://b.test"},
	})
	p.links = true
	if res, _ := p.update(key("y")); !res.done || !res.copy || res.item.Value != "https://a.test/spec" {
		t.Fatalf("y = %+v, want copy of the first link", res)
	}
	search(p, "a.test")
	if len(p.matches) != 1 {
		t.Fatalf("search by URL matched %d items, want 1", len(p.matches))
	}
	if res, _ := p.update(key("ctrl+y")); !res.copy {
		t.Fatalf("ctrl+y while searching = %+v, want copy", res)
	}
	if res, _ := p.update(key("enter")); !res.done || res.copy {
		t.Fatalf("enter = %+v, want open", res)
	}
}

func TestPickerPreviewHasFullHeightDivider(t *testing.T) {
	for _, rows := range []int{2, 9} {
		p := newPicker(nil, "History", items(strings.Repeat("Message ", 12)))
		p.preview = func(_ *pickItem, width, height int) string {
			return strings.Repeat("Detail\n", rows-1) + "Detail"
		}
		for _, width := range []int{40, 56, 100, 140} {
			view := ansi.Strip(p.view(width, 12, modalLook{}))
			column := -1
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("width %d: split view overflows: %q", width, line)
				}
				index := strings.IndexRune(line, '│')
				if width < minPreviewWidth {
					if index >= 0 {
						t.Fatal("single-pane picker must not show a divider")
					}
					continue
				}
				if index < 0 {
					t.Fatalf("width %d: divider missing from row: %q", width, line)
				}
				if column < 0 {
					column = ansi.StringWidth(line[:index])
				}
				if ansi.StringWidth(line[:index]) != column {
					t.Fatal("divider must line up across both panes' full height")
				}
			}
		}
	}
}
