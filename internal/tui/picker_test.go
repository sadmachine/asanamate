package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
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
	p := newPicker(pickProject, "Projects", items("Web App", "Mobile", "Web Site"))
	typeText(p, "site web")
	if len(p.matches) != 1 || p.items[p.matches[0]].Label != "Web Site" {
		t.Fatalf("matches = %v", p.matches)
	}
}

func TestPickerEnterSelectsHighlighted(t *testing.T) {
	p := newPicker(pickProject, "Projects", items("A", "B"))
	p.update(key("down"))
	res, _ := p.update(key("enter"))
	if !res.done || res.item == nil || res.item.Label != "B" {
		t.Fatalf("res = %+v", res)
	}
}

func TestPickerFreeText(t *testing.T) {
	p := newPicker(pickRepo, "Repo", items("/code/web"))
	p.allowFree = true
	typeText(p, "/tmp/x")
	res, _ := p.update(key("enter"))
	if !res.done || res.item != nil || res.free != "/tmp/x" {
		t.Fatalf("enter with no matches: res = %+v", res)
	}
	p = newPicker(pickRepo, "Repo", items("/code/web"))
	p.allowFree = true
	typeText(p, "web")
	res, _ = p.update(key("tab"))
	if !res.done || res.free != "web" {
		t.Fatalf("tab: res = %+v", res)
	}
}

func TestPickerKeySelect(t *testing.T) {
	p := newPicker(pickAction, "Actions", []pickItem{{Label: "Claude", Key: "c"}, {Label: "View", Key: "v"}})
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
	p := newPicker(pickProject, "Projects", items("A"))
	if res, _ := p.update(key("esc")); !res.cancelled {
		t.Fatalf("res = %+v", res)
	}
}

func TestPickerMultiTogglesAndSaves(t *testing.T) {
	p := newMultiPicker(pickMultiEnum, "Scope", items("A", "B", "C"), map[int]bool{0: true})
	p.update(key("down"))
	if res, _ := p.update(key("enter")); res.done {
		t.Fatal("enter must toggle, not save")
	}
	p.update(key("up"))
	p.update(key("enter"))
	res, _ := p.update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if !res.done || len(res.items) != 1 || res.items[0].Label != "B" {
		t.Fatalf("res = %+v", res)
	}
}
