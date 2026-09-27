package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCandidatesExpandsAndDedupes(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	got, err := Candidates(context.Background(), `printf '~/a\n/b\n/b\n\n  /c  \n'`)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/home/u/a", "/b", "/c"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCandidatesReportsFailure(t *testing.T) {
	if _, err := Candidates(context.Background(), "exit 3"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestResolveGitRepo(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	got, err := Resolve(sub)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if gotReal, _ := filepath.EvalSymlinks(got); gotReal != want {
		t.Fatalf("Resolve = %s, want %s", got, want)
	}
}

func TestResolveRejectsNonRepo(t *testing.T) {
	_, err := Resolve(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("err = %v", err)
	}
}
