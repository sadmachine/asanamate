package main

import (
	"bytes"
	"testing"

	"github.com/sadmachine/asanamate/internal/whatsnew"
)

func TestRunWhatsNew(t *testing.T) {
	sections := []whatsnew.Section{{Version: "v0.5.0", Body: "five"}, {Version: "v0.4.0", Body: "four"}}
	var b bytes.Buffer
	if err := runWhatsNew(nil, &b, sections, "v0.4.0"); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != "## v0.4.0\n\nfour\n" {
		t.Fatalf("got %q", got)
	}
	b.Reset()
	if err := runWhatsNew(nil, &b, sections, "v0.3.0"); err != nil || b.String() != "No highlights for v0.3.0.\n" {
		t.Fatalf("got %q, err = %v", b.String(), err)
	}
	if err := runWhatsNew([]string{"x"}, &b, sections, "v0.4.0"); err == nil {
		t.Fatal("extra argument accepted")
	}
}
