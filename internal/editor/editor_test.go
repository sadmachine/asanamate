package editor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNamePrefersVisualThenEditor(t *testing.T) {
	t.Setenv("VISUAL", " ")
	t.Setenv("EDITOR", " nano ")
	if got := Name("vi"); got != "nano" {
		t.Fatalf("Name = %q", got)
	}
	t.Setenv("VISUAL", "code --wait")
	if got := Name("vi"); got != "code --wait" {
		t.Fatalf("Name = %q", got)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if got := Name("vim"); got != "vim" {
		t.Fatalf("Name = %q", got)
	}
}

func TestCommandPassesThePathAsAnArgument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, `it's "$(touch pwned)".md`)
	cmd := Command("printf x >", path)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "x" {
		t.Fatalf("file = %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Fatal("the path ran as shell code")
	}
}
