package agents

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchArgsPreservesMatchingAndCleansOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known-agent")
	script := "#!/bin/sh\nprintf '/repo\\tbusy\\ts1\\tTitle\\tfeat\\t/outer\\033[2J\\n'\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	list, err := FetchArgs(context.Background(), []string{path}, map[string][]string{"working": {"busy"}})
	if err != nil || len(list) != 1 || list[0].State != Working || list[0].Branch != "feat" || list[0].Repo != "/outer" {
		t.Fatalf("list = %+v, err = %v", list, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'SECRET' >&2; exit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := FetchArgs(context.Background(), []string{path}, nil); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("provider diagnostics leaked: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '/repo\\tbusy\\ts1\\tTitle\\tfeat\\t/outer\\t42\\tcodex\\tnative\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	list, err = FetchArgs(context.Background(), []string{path}, nil)
	if err != nil || len(list) != 1 || list[0].TaskGID != "42" || list[0].SessionID != "native" {
		t.Fatalf("explicit identity through bounded source: %+v, %v", list, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '/repo\\tidle\\t\\t\\tfeat\\t/outer\\tSECRET\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := FetchArgs(context.Background(), []string{path}, nil); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("invalid metadata must fail without exposing provider data: %v", err)
	}
}

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

	cmd := "printf '%s\\trunning\\ts1\\tFix  login\\n%s\\tidle\\n%s\\twaiting\\n' '" + wt + "' '" + repo + "' '" + plain + "'; echo 'no tab here'"
	got, err := List(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	want := []Agent{
		{Path: wt, Status: "running", Target: "s1", Branch: "fix-login", Repo: repo, Title: "Fix login"},
		{Path: repo, Status: "idle", Branch: "main", Repo: repo},
		{Path: plain, Status: "waiting"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("agent %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// Branch and repo columns: both set skip git; a branch alone is kept
	// only where git finds none.
	cmd = "printf '%s\\tidle\\t\\t\\tfeat\\t/outer\\n%s\\tidle\\t\\t\\tother\\t\\n/nowhere\\tidle\\t\\t\\tmain\\t\\n' '" + plain + "' '" + wt + "'"
	if got, err = List(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	want = []Agent{
		{Path: plain, Status: "idle", Branch: "feat", Repo: "/outer"},
		{Path: wt, Status: "idle", Branch: "fix-login", Repo: repo},
		{Path: "/nowhere", Status: "idle", Branch: "main"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("agent %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestCommandFixture runs testdata/agents.sh, the minimal agents source
// docs/extensions.md describes, through Fetch, so a format change fails here.
func TestCommandFixture(t *testing.T) {
	script, err := filepath.Abs("testdata/agents.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := realpath(t, t.TempDir())
	states := map[string][]string{"working": {"busy"}, "waiting": {"permission"}}
	got, err := Fetch(context.Background(), "/bin/sh '"+script+"' '"+dir+"'", states)
	if err != nil {
		t.Fatal(err)
	}
	want := []Agent{
		{Path: dir, Status: "busy", Target: "s1", Title: "Fix login", State: Working},
		{Path: dir, Status: "permission", Target: "s2", State: Waiting},
		{Path: dir, Status: "mystery", State: Unknown},
		{Path: dir, Status: "idle", Target: "s3", Branch: "fix-login", Repo: dir, State: Idle},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
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

func TestMatchAll(t *testing.T) {
	list := []Agent{
		{Path: "/a/wt", State: Idle, Branch: "fix", Repo: "/a"},
		{Path: "/b/wt", State: Idle, Branch: "fix", Repo: "/b"},
		{Path: "/a/wt2", State: Waiting, Branch: "fix", Repo: "/a"},
		{Path: "/a/wt3", State: Working, Branch: "other", Repo: "/a"},
	}
	if got := MatchAll(list, "42", "fix", []string{"/a"}); len(got) != 2 || got[0].Path != "/a/wt2" || got[1].Path != "/a/wt" {
		t.Fatalf("repo-scoped, urgency order = %+v", got)
	}
	if got := MatchAll(list, "42", "fix", nil); len(got) != 3 {
		t.Fatalf("unscoped = %+v", got)
	}
	if got := MatchAll(list, "42", "fix", []string{"/c"}); len(got) != 0 {
		t.Fatalf("other repo matched: %+v", got)
	}
	if got := MatchAll(list, "42", "", nil); len(got) != 0 {
		t.Fatalf("empty branch matched: %+v", got)
	}
}

func TestExplicitTicketLinks(t *testing.T) {
	list := []Agent{
		{Target: "explicit", TaskGID: "42", Branch: "other", Repo: "/other", State: Waiting},
		{Target: "fallback", Branch: "fix", Repo: "/a", State: Idle},
		{Target: "elsewhere", TaskGID: "99", Branch: "fix", Repo: "/a"},
		{Target: "conflict", TaskGID: "-", Branch: "fix", Repo: "/a"},
	}
	got := MatchAll(list, "42", "fix", []string{"/a"})
	if len(got) != 2 || got[0].Target != "explicit" || got[1].Target != "fallback" {
		t.Fatalf("explicit links and fallback = %+v", got)
	}
	if got := MatchAll(list, "42", "", nil); len(got) != 1 || got[0].Target != "explicit" {
		t.Fatalf("explicit link without git = %+v", got)
	}
	if got := Unlinked(list, "42", "fix", []string{"/a"}); len(got) != 2 || got[0].Target != "elsewhere" || got[1].Target != "conflict" {
		t.Fatalf("unlinked explicit identities = %+v", got)
	}
	if got := Hint(list[3], "fix"); !strings.Contains(got, "conflicting ticket link") {
		t.Fatalf("conflict hint = %q", got)
	}
}

func TestListTicketLinkColumns(t *testing.T) {
	got, err := List(context.Background(), "printf '/missing\\tidle\\tx\\ttitle\\tmain\\t/repo\\t42\\tcodex\\tnative\\n'")
	if err != nil || len(got) != 1 || got[0].TaskGID != "42" || got[0].Provider != "codex" || got[0].SessionID != "native" {
		t.Fatalf("identity = %+v, %v", got, err)
	}
	if _, err := List(context.Background(), "printf '/missing\\tidle\\t\\t\\tmain\\t/repo\\tinvalid\\n'"); err == nil {
		t.Fatal("invalid explicit link must not fall back to git")
	}
}

func TestClassify(t *testing.T) {
	list := []Agent{{Status: "Running"}, {Status: "idle"}, {Status: "permission"}, {Status: "completed"}, {Status: "weird"}, {Status: ""}}
	Classify(list, map[string][]string{"working": {"running", "busy"}, "waiting": {"Permission"}})
	want := []State{Working, Idle, Waiting, Completed, Unknown, Unknown}
	for i, w := range want {
		if list[i].State != w {
			t.Errorf("%q → %q, want %q", list[i].Status, list[i].State, w)
		}
	}
}

func TestTitleIsOneShortLine(t *testing.T) {
	if got := Title(strings.Repeat("a", 100)); got != strings.Repeat("a", maxTitle-1)+"…" {
		t.Fatalf("got %q", got)
	}
	if got := Title(" a\tb\n\nc \x1b[31m"); got != "a b c [31m" {
		t.Fatalf("got %q", got)
	}
}
