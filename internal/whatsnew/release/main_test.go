package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMovesHighlightsAndKeepsTheReadme(t *testing.T) {
	root := t.TempDir()
	unreleased := filepath.Join(root, dir, "unreleased")
	if err := os.MkdirAll(unreleased, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(root, dir, "whats-new.md")
	write(file, "# What's new\n\n## v0.1.0\n\n### New\n\n- **Old.**\n")
	write(filepath.Join(unreleased, "README.md"), "# Unreleased highlights\n")
	write(filepath.Join(unreleased, "fresh.md"), "### New\n\n- **Fresh.**\n")
	t.Chdir(root)
	if err := run([]string{"v0.2.0"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	if !strings.Contains(string(got), "## v0.2.0\n\n### New\n\n- **Fresh.**\n\n## v0.1.0") {
		t.Fatalf("whats-new.md =\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(unreleased, "fresh.md")); !os.IsNotExist(err) {
		t.Fatal("the moved highlights file must be deleted")
	}
	if _, err := os.Stat(filepath.Join(unreleased, "README.md")); err != nil {
		t.Fatal("the README must stay")
	}
	if err := run([]string{"v0.2.0"}); err == nil {
		t.Fatal("releasing the same version twice must fail")
	}
}
