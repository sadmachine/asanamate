package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/config"
)

func TestCandidatesExpandsAndDedupes(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	got, err := Candidates(context.Background(), config.RepoSource{Root: "/ignored", Command: `printf '~/a\n/b\n/b\n\n  /c  \n'`})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/home/u/a", "/b", "/c"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCandidatesReportsFailure(t *testing.T) {
	if _, err := Candidates(context.Background(), config.RepoSource{Command: "exit 3"}); err == nil {
		t.Fatal("expected an error")
	}
}

// List finds clones and worktrees directly inside root, keeping unusual
// names as data, and does not scan deeper.
func TestListFindsDirectChildRepos(t *testing.T) {
	root := t.TempDir()
	clone, worktree := filepath.Join(root, `it's $(x) ☃`), filepath.Join(root, "wt")
	os.MkdirAll(filepath.Join(clone, ".git"), 0o755)
	os.MkdirAll(worktree, 0o755)
	os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: /x\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "group", "nested", ".git"), 0o755)
	os.MkdirAll(filepath.Join(root, "plain"), 0o755)
	os.WriteFile(filepath.Join(root, "file"), nil, 0o644)
	for _, got := range [][]string{must(List(root)), must(Candidates(context.Background(), config.RepoSource{Root: root, Command: " "}))} {
		if want := []string{clone, worktree}; !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	}
	if got := must(List("")); got != nil {
		t.Errorf("List(\"\") = %v, want none", got)
	}
	if _, err := List(filepath.Join(root, "missing")); err == nil || !strings.Contains(err.Error(), "repo_source.root") {
		t.Errorf("missing root err = %v", err)
	}
}

func TestListExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, "code", "a", ".git"), 0o755)
	if got, want := must(List("~/code")), []string{filepath.Join(home, "code", "a")}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
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

func TestResolveReportsMissingGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Resolve(t.TempDir()); err == nil || !strings.Contains(err.Error(), "git is not installed") {
		t.Fatalf("err = %v", err)
	}
}

func TestWorktreeFindsBranch(t *testing.T) {
	dir := t.TempDir()
	main, wt := filepath.Join(dir, "main"), filepath.Join(dir, "wt")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main", main},
		{"-C", main, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
		{"-C", main, "worktree", "add", "-q", "-b", "feat/x", wt},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	real := func(p string) string { r, _ := filepath.EvalSymlinks(p); return r }
	if got := Worktree(main, "feat/x"); real(got) != real(wt) {
		t.Errorf("Worktree(feat/x) = %q, want %q", got, wt)
	}
	if got := Worktree(main, "main"); real(got) != real(main) {
		t.Errorf("Worktree(main) = %q, want %q", got, main)
	}
	if got := Worktree(main, "feat"); got != "" {
		t.Errorf("Worktree(feat) = %q, want none", got)
	}
}

func TestCollapseHome(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	for in, want := range map[string]string{"/home/u": "~", "/home/u/code/web": "~/code/web", "/home/user2/x": "/home/user2/x", "/b": "/b"} {
		if got := CollapseHome(in); got != want {
			t.Errorf("CollapseHome(%q) = %q, want %q", in, got, want)
		}
	}
}
