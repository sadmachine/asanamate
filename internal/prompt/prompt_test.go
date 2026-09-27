package prompt

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func reader(s string) *bufio.Reader { return bufio.NewReader(strings.NewReader(s)) }

func TestLineUsesDefault(t *testing.T) {
	var out strings.Builder
	got, err := Line(reader("\n"), &out, "Dir", "~/code")
	if err != nil || got != "~/code" || out.String() != "Dir [~/code]: " {
		t.Fatalf("got %q, err %v, prompt %q", got, err, out.String())
	}
}

func TestLineEOF(t *testing.T) {
	if _, err := Line(reader(""), io.Discard, "Dir", "x"); err != io.EOF {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func TestConfirm(t *testing.T) {
	for in, want := range map[string]bool{"y\n": true, "YES\n": true, "\n": false, "n\n": false, "yes": true} {
		got, err := Confirm(reader(in), io.Discard, "Go?")
		if err != nil || got != want {
			t.Errorf("Confirm(%q) = %v, %v", in, got, err)
		}
	}
}
