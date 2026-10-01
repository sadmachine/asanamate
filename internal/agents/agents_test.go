package agents

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if got := MatchAll(list, "fix", []string{"/a"}); len(got) != 2 || got[0].Path != "/a/wt2" || got[1].Path != "/a/wt" {
		t.Fatalf("repo-scoped, urgency order = %+v", got)
	}
	if got := MatchAll(list, "fix", nil); len(got) != 3 {
		t.Fatalf("unscoped = %+v", got)
	}
	if got := MatchAll(list, "fix", []string{"/c"}); len(got) != 0 {
		t.Fatalf("other repo matched: %+v", got)
	}
	if got := MatchAll(list, "", nil); len(got) != 0 {
		t.Fatalf("empty branch matched: %+v", got)
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

// Trimmed from real `ccmux show --json` output (ccmux 1.4.1).
const ccmuxJSON = `[
 {"id":"d48f","cwd":"/r/.claude/worktrees/wicpa","status":"idle","attentionState":"unread",
  "gitBranch":"update/3068","mainRepoRoot":"/r","isWorktree":true,"tmuxTarget":"amagent:1.0",
  "summary":"Fix license headings","prompts":["first prompt"],"paneTitle":"claude"},
 {"id":"e19a","cwd":"/nowhere","status":"working","attentionState":null,"gitBranch":"main","mainRepoRoot":null,
  "summary":null,"prompts":["  Fix the queue\ntimeout  "],"paneTitle":"claude"},
 {"id":"f2c0","cwd":"/r","status":"idle","attentionState":"seen","gitBranch":"main","mainRepoRoot":"/r",
  "prompts":[],"paneTitle":"✳ Claude Code"},
 {"id":"codex_pane1","agentType":"codex","trackingMode":"pane","cwd":"/r","status":"idle","gitBranch":"main",
  "mainRepoRoot":"/r","prompts":[],"paneTitle":"[ ! ] Action Required | Fix editor | r"},
 {"id":"codex_pane2","agentType":"codex","trackingMode":"pane","cwd":"/r","status":"idle","gitBranch":"main",
  "mainRepoRoot":"/r","prompts":[],"paneTitle":"⠼ Fix editor | r"},
 {"id":"codex_pane3","agentType":"codex","trackingMode":"pane","cwd":"/r","status":"idle","gitBranch":"main",
  "mainRepoRoot":"/r","prompts":[],"paneTitle":"Fix editor | r"},
 {"id":"codex_pane4","agentType":"codex","trackingMode":"pane","cwd":"/r","status":"waiting","gitBranch":"main",
  "mainRepoRoot":"/r","prompts":[],"paneTitle":"⠼ Fix editor | r"},
 {"id":"codex_pane5","agentType":"codex","trackingMode":"pane","nativeSessionId":"n5","cwd":"/r","status":"idle",
  "statusChangedAt":"2026-01-01T00:00:00.000Z","gitBranch":"main","mainRepoRoot":"/r","prompts":[],"paneTitle":"⠼ Fix editor | r"},
 {"id":"n6","nativeSessionId":"n6","cwd":"/r","status":"idle","statusChangedAt":"2026-01-01T00:00:00.000Z",
  "gitBranch":"main","mainRepoRoot":"/r","prompts":[],"paneTitle":"claude"}
]`

func TestParseCCMux(t *testing.T) {
	changed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reported := map[string]live{
		"n5": {State: Idle, At: changed.Add(time.Minute)},     // newer than ccmux: wins over the pane title
		"n6": {State: Working, At: changed.Add(-time.Minute)}, // older than ccmux: ignored
	}
	got, err := parseCCMux(context.Background(), []byte(ccmuxJSON), reported)
	if err != nil {
		t.Fatal(err)
	}
	want := []Agent{
		{Path: "/r/.claude/worktrees/wicpa", Status: "completed", Target: "d48f", Branch: "update/3068", Repo: "/r", Title: "Fix license headings"},
		{Path: "/nowhere", Status: "working", Target: "e19a", Branch: "main", Title: "Fix the queue timeout"},
		{Path: "/r", Status: "idle", Target: "f2c0", Branch: "main", Repo: "/r", Title: "✳ Claude Code"},
		{Path: "/r", Status: "waiting", Target: "codex_pane1", Branch: "main", Repo: "/r", Title: "Fix editor | r"},
		{Path: "/r", Status: "working", Target: "codex_pane2", Branch: "main", Repo: "/r", Title: "Fix editor | r"},
		{Path: "/r", Status: "idle", Target: "codex_pane3", Branch: "main", Repo: "/r", Title: "Fix editor | r"},
		{Path: "/r", Status: "waiting", Target: "codex_pane4", Branch: "main", Repo: "/r", Title: "Fix editor | r"},
		{Path: "/r", Status: "idle", Target: "codex_pane5", Branch: "main", Repo: "/r", Title: "Fix editor | r"},
		{Path: "/r", Status: "idle", Target: "n6", Branch: "main", Repo: "/r", Title: "claude"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("session %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if _, err := parseCCMux(context.Background(), []byte("not json"), nil); err == nil {
		t.Fatal("want a parse error")
	}
}

func TestTitleIsOneShortLine(t *testing.T) {
	if got := title(strings.Repeat("a", 100)); got != strings.Repeat("a", maxTitle-1)+"…" {
		t.Fatalf("got %q", got)
	}
	if got := title(" a\tb\n\nc \x1b[31m"); got != "a b c [31m" {
		t.Fatalf("got %q", got)
	}
}

func TestHookRecordsSessionState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	hook := func(event, id string) {
		t.Helper()
		if err := WriteHook(strings.NewReader(`{"hook_event_name":"`+event+`","session_id":"`+id+`"}`), dir); err != nil {
			t.Fatal(err)
		}
	}
	state := func(id string) State {
		got := map[string]live{}
		readHookFiles(dir, got)
		return got[id].State
	}
	hook("UserPromptSubmit", "s1")
	if got := state("s1"); got != Working {
		t.Fatalf("after prompt = %q", got)
	}
	hook("PermissionRequest", "s1")
	if got := state("s1"); got != Waiting {
		t.Fatalf("after permission request = %q", got)
	}
	hook("PostCompact", "s1") // untracked: no change
	hook("Stop", "s1")
	if got := state("s1"); got != Idle {
		t.Fatalf("after stop = %q", got)
	}
	hook("SessionEnd", "s1")
	hook("SessionEnd", "s1") // already gone: no error
	if got := state("s1"); got != "" {
		t.Fatalf("after session end = %q", got)
	}
	hook("UserPromptSubmit", "../escape")
	if entries, _ := os.ReadDir(filepath.Dir(dir)); len(entries) != 1 {
		t.Fatalf("unsafe session id wrote outside dir: %v", entries)
	}
}

func TestReadClaudeSessions(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("1.json", `{"sessionId":"a","status":"busy","statusUpdatedAt":1790000000000}`)
	write("2.json", `{"sessionId":"b","status":"something-new"}`)
	write("3.json", `not json`)
	got := map[string]live{}
	readClaudeSessions(dir, got)
	if len(got) != 1 || got["a"].State != Working || !got["a"].At.Equal(time.UnixMilli(1790000000000)) {
		t.Fatalf("got %+v", got)
	}
}
