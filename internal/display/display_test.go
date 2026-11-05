package display

import "testing"

func TestCleanKeepsLinesAndRemovesEscapes(t *testing.T) {
	in := "a\x1b[31mred\x1b[0m\tb\nc\x07d\u202eevil\u200b"
	if got := Clean(in); got != "ared\tb\ncdevil" {
		t.Fatalf("Clean = %q", got)
	}
}

func TestLineFlattensAndCollapses(t *testing.T) {
	in := "  a\x1b]0;title\x07 \t b\n\nc\u202e  "
	if got := Line(in); got != "a b c" {
		t.Fatalf("Line = %q", got)
	}
	if got := Line("\x1b]52;c;secret\a\x1b[31mTitle\x1b[0m\n\u202ehidden\tpath"); got != "Title hidden path" {
		t.Fatalf("Line = %q", got)
	}
}
