package agents

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func realpath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestListResolvesBranchAndRepo(t *testing.T) {
	repo := realpath(t, t.TempDir())
	git(t, "init", "-q", "-b", "main", repo)
	git(t, "-C", repo, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(repo, ".claude", "worktrees", "fix-login")
	git(t, "-C", repo, "worktree", "add", "-q", "-b", "fix-login", wt)
	plain := realpath(t, t.TempDir())

	cmd := "printf '%s\\trunning\\ts1\\n%s\\tidle\\n%s\\twaiting\\n' '" + wt + "' '" + repo + "' '" + plain + "'; echo 'no tab here'"
	got, err := List(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	want := []Agent{
		{Path: wt, Status: "running", Target: "s1", Branch: "fix-login", Repo: repo},
		{Path: repo, Status: "idle", Branch: "main", Repo: repo},
		{Path: plain, Status: "waiting"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("agent %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestListReportsCommandFailure(t *testing.T) {
	if _, err := List(context.Background(), "exit 2"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestMatch(t *testing.T) {
	agents := []Agent{
		{Path: "/a/wt", Status: "running", Branch: "fix", Repo: "/a"},
		{Path: "/b/wt", Status: "idle", Branch: "fix", Repo: "/b"},
	}
	if a := Match(agents, "fix", []string{"/b"}); a == nil || a.Repo != "/b" {
		t.Fatalf("repo-scoped match = %+v", a)
	}
	if a := Match(agents, "fix", nil); a == nil || a.Repo != "/a" {
		t.Fatalf("unscoped match = %+v", a)
	}
	if a := Match(agents, "fix", []string{"/c"}); a != nil {
		t.Fatalf("other repo matched: %+v", a)
	}
	if a := Match(agents, "", nil); a != nil {
		t.Fatalf("empty branch matched: %+v", a)
	}
}
