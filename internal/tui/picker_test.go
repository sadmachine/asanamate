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
	if !strings.Contains(ansi.Strip(p.view(80, 12, lipgloss.NewStyle())), `Use "/tmp/x"`) {
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
	style := lipgloss.NewStyle()
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
	for _, line := range strings.Split(ansi.Strip(modalHelp(p.items[1].Help, "(i)", 40, 2)), "\n") {
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
	view := ansi.Strip(p.view(80, 12, lipgloss.NewStyle()))
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
