package dateinput

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveKeepsExactDatesWithoutDate(t *testing.T) {
	t.Setenv("PATH", "")
	got, err := Resolve(context.Background(), " 2026-10-09\n")
	if err != nil || got != "2026-10-09" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
	for _, text := range []string{"", "  ", "tomorrow"} {
		if _, err := Resolve(context.Background(), text); err == nil {
			t.Errorf("Resolve(%q) succeeded without date", text)
		}
	}
}

func TestResolveRejectsInvalidDates(t *testing.T) {
	for _, text := range []string{"2026-02-30", "nonsense", "next blursday"} {
		if got, err := Resolve(context.Background(), text); err == nil {
			t.Errorf("Resolve(%q) = %q, want error", text, got)
		}
	}
}

// A stub gdate shows what Resolve passes and uses, on every platform.
func TestResolvePassesRelativeDatesToGNUDate(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\n[ \"$1\" = -d ] && [ \"$2\" = \"next fri\" ] && [ \"$3\" = +%F ] && echo 2026-10-09\n"
	if err := os.WriteFile(filepath.Join(dir, "gdate"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got, err := Resolve(context.Background(), "next fri"); err != nil || got != "2026-10-09" {
		t.Fatalf("Resolve(next fri) = %q, %v", got, err)
	}
}

// With GNU date installed, a relative date anchored to a fixed day resolves
// without depending on today.
func TestResolveRelativeDatesWithGNUDate(t *testing.T) {
	if _, err := Resolve(context.Background(), "today"); err != nil {
		t.Skipf("GNU date (date on Linux, gdate on macOS) is not installed: %v", err)
	}
	if got, err := Resolve(context.Background(), "2026-01-31 +1 day"); err != nil || got != "2026-02-01" {
		t.Fatalf("Resolve(2026-01-31 +1 day) = %q, %v", got, err)
	}
}
