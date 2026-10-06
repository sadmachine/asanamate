package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/keymap"
)

// keys is the resolved key map and typeFirst opens list pickers in search
// mode. New sets both from the config; a process runs one Model.
var (
	keys      = keymap.Default()
	typeFirst bool
)

// bound returns the binding msg runs in scope, or "". While a text field has
// focus (typing), only Typing bindings run, and printable keys are typed.
func bound(scope string, msg tea.KeyPressMsg, typing bool) string {
	k := msg.String()
	name := keys.Name(scope, k)
	if typing && (keymap.Printable(k) || !keymap.Typing(scope, name)) {
		return ""
	}
	return name
}

// keyLabel shows the first key of each named binding, as hints do.
func keyLabel(scope string, names ...string) string {
	var first []string
	for _, n := range names {
		if ks := keys.Keys(scope, n); len(ks) > 0 {
			first = append(first, ks[0])
		}
	}
	return keymap.Label(first...)
}

// typedLabel is keyLabel for while a text field has focus: the first key of
// each binding that types nothing.
func typedLabel(scope string, names ...string) string {
	var first []string
	for _, n := range names {
		for _, k := range keys.Keys(scope, n) {
			if !keymap.Printable(k) {
				first = append(first, k)
				break
			}
		}
	}
	return keymap.Label(first...)
}

// hintLine renders modal key hints as "key desc · key desc", leaving out
// hints whose binding has no keys.
func hintLine(accent lipgloss.Style, hints ...[2]string) string {
	var parts []string
	for _, h := range hints {
		if h[0] != "" {
			parts = append(parts, accent.Render(h[0])+dimStyle.Render(" "+h[1]))
		}
	}
	return strings.Join(parts, dimStyle.Render(" · "))
}
