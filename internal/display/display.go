// Package display makes untrusted text, from Asana, extensions, or local
// commands, safe to print in a terminal.
package display

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Clean removes terminal escape sequences, control characters other than
// newline and tab, and Unicode format characters such as bidirectional
// overrides, so text cannot restyle or reorder the terminal.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unsafe(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

// Line is Clean for a single line: every control and format character,
// newlines and tabs included, becomes a space, and runs of spaces collapse.
func Line(s string) string {
	s = strings.Map(func(r rune) rune {
		if unsafe(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
	return strings.Join(strings.Fields(s), " ")
}

func unsafe(r rune) bool {
	return unicode.IsControl(r) || unicode.In(r, unicode.Cf)
}
