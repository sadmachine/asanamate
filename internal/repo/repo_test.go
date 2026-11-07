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

// TestCandidatesFixture runs testdata/repos.sh, the minimal repo source
// docs/extensions.md describes, so a format change fails here.
func TestCandidatesFixture(t *testing.T) {
	script, err := filepath.Abs("testdata/repos.sh")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", "/home/u")
	got, err := Candidates(context.Background(), config.RepoSource{Command: "/bin/sh '" + script + "' /srv/api"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/home/u/code/web", "/srv/api"}; !slices.Equal(got, want) {
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

// List keeps the repos it found and names the children it cannot inspect.
func TestListReportsUnreadableChildren(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}
	root := t.TempDir()
	repo, locked := filepath.Join(root, "a"), filepath.Join(root, "locked")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	os.MkdirAll(locked, 0o000)
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	got, err := List(root)
	if !slices.Equal(got, []string{repo}) {
		t.Errorf("got %v, want %v", got, []string{repo})
	}
	if err == nil || !strings.Contains(err.Error(), "cannot read locked") {
		t.Errorf("err = %v", err)
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

func TestRemoteKeyNormalizesForms(t *testing.T) {
	for _, tc := range []struct{ url, want string }{
		{"https://github.com/Acme/Site.git", "github.com/acme/site"},
		{"https://github.com/acme/site/", "github.com/acme/site"},
		{"git@github.com:acme/site.git", "github.com/acme/site"},
		{"ssh://git@github.com:22/acme/site.git", "github.com/acme/site"},
		{"https://user@gitlab.example.com/group/sub/site", "gitlab.example.com/group/sub/site"},
		{"/srv/git/site.git", ""},
		{"file:///srv/git/site.git", ""},
		{"https://github.com", ""},
		{"", ""},
	} {
		if got := remoteKey(tc.url); got != tc.want {
			t.Errorf("remoteKey(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestHostPathKeepsPathCase(t *testing.T) {
	for _, tc := range []struct{ url, host, path string }{
		{"git@GitHub.com:Acme/Site.git", "github.com", "Acme/Site"},
		{"ssh://git@ghe.example.com:2222/org/web.git", "ghe.example.com", "org/web"},
		{"https://gitlab.example.com/group/sub/site/", "gitlab.example.com", "group/sub/site"},
		{"/srv/git/site.git", "", ""},
	} {
		if host, path := HostPath(tc.url); host != tc.host || path != tc.path {
			t.Errorf("HostPath(%q) = %q, %q, want %q, %q", tc.url, host, path, tc.host, tc.path)
		}
	}
}

// Remotes lists fetch URLs, origin first, then by name.
func TestRemotesOrder(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", dir},
		{"-C", dir, "remote", "add", "zeta", "https://example.com/z.git"},
		{"-C", dir, "remote", "add", "alpha", "git@example.com:a.git"},
		{"-C", dir, "remote", "add", "origin", "https://github.com/acme/web.git"},
		{"-C", dir, "remote", "set-url", "--push", "origin", "git@github.com:acme/push.git"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	got, err := Remotes(context.Background(), dir)
	want := []Remote{{"origin", "https://github.com/acme/web.git"}, {"alpha", "git@example.com:a.git"}, {"zeta", "https://example.com/z.git"}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("Remotes = %v, %v, want %v", got, err, want)
	}
	if _, err := Remotes(context.Background(), filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing repo: want an error")
	}
}

// Matching finds repos by any remote, in either URL form, listing clones
// before worktrees that share their remotes.
func TestMatchingPrefersClones(t *testing.T) {
	dir := t.TempDir()
	clone, wt, other, plain := filepath.Join(dir, "clone"), filepath.Join(dir, "wt"), filepath.Join(dir, "other"), filepath.Join(dir, "plain")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main", clone},
		{"-C", clone, "remote", "add", "upstream", "git@github.com:Acme/Site.git"},
		{"-C", clone, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
		{"-C", clone, "worktree", "add", "-q", "-b", "feat", wt},
		{"init", "-q", other},
		{"-C", other, "remote", "add", "origin", "https://github.com/acme/other.git"},
		{"init", "-q", plain},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	paths := []string{wt, other, plain, clone, filepath.Join(dir, "missing")}
	got := Matching(context.Background(), paths, []string{"https://github.com/acme/site"})
	if want := []string{clone, wt}; !slices.Equal(got, want) {
		t.Fatalf("Matching = %v, want %v", got, want)
	}
	if got := Matching(context.Background(), paths, []string{"/local/path"}); got != nil {
		t.Fatalf("Matching without usable URLs = %v", got)
	}
}

func TestPendingWork(t *testing.T) {
	dir := t.TempDir()
	origin, clone := filepath.Join(dir, "origin.git"), filepath.Join(dir, "clone")
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q", "--bare", "-b", "main", origin)
	git("clone", "-q", origin, clone)
	git("-C", clone, "commit", "-q", "--allow-empty", "-m", "init")
	git("-C", clone, "push", "-q", "-u", "origin", "main")
	if p, err := PendingWork(context.Background(), clone); err != nil || !p.Empty() {
		t.Fatalf("clean = %+v, %v", p, err)
	}

	git("-C", clone, "commit", "-q", "--allow-empty", "-m", "two")
	git("-C", clone, "commit", "-q", "--allow-empty", "-m", "three")
	os.WriteFile(filepath.Join(clone, "new.txt"), []byte("x"), 0o600)
	p, err := PendingWork(context.Background(), clone)
	if err != nil || p.Uncommitted != 1 || p.ChangedAt.IsZero() || p.Unpushed != 2 || p.NoUpstream || p.CommitAt.IsZero() {
		t.Fatalf("dirty = %+v, %v", p, err)
	}

	git("-C", clone, "switch", "-q", "-c", "feat/x")
	if p, err := PendingWork(context.Background(), clone); err != nil || !p.NoUpstream || p.CommitAt.IsZero() {
		t.Fatalf("no upstream = %+v, %v", p, err)
	}
	if _, err := PendingWork(context.Background(), filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing tree: want an error")
	}
}
