package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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
		if !strings.Contains(help, b.desc) {
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
