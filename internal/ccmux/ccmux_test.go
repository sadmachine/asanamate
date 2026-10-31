package ccmux

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sadmachine/asanamate/internal/agents"
)

// Trimmed from real `ccmux show --json` output (ccmux 1.4.1).
const ccmuxJSON = `[
 {"id":"d48f","agentType":"claude","nativeSessionId":"01a14609-3a63-7c02-bd2b-c313bedc3c37","cwd":"/r/.claude/worktrees/wicpa","status":"idle","attentionState":"unread",
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
 {"id":"codex_pane6","agentType":"codex","trackingMode":"pane","tmuxPane":"%6","cwd":"/r","status":"idle",
  "statusChangedAt":"2026-01-01T00:00:00.000Z","gitBranch":"main","mainRepoRoot":"/r","prompts":[],"paneTitle":"codex"},
 {"id":"n6","nativeSessionId":"n6","cwd":"/r","status":"idle","statusChangedAt":"2026-01-01T00:00:00.000Z",
  "gitBranch":"main","mainRepoRoot":"/r","prompts":[],"paneTitle":"claude"},
 {"id":"s7","cwd":"/r","status":"starting","prompts":[],"paneTitle":"tab\there"}
]`

// reported holds the self-reported states the ccmuxJSON cases use.
func reported() map[string]live {
	changed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return map[string]live{
		"n5": {State: agents.Idle, At: changed.Add(time.Minute)},     // newer than ccmux: wins over the pane title
		"n6": {State: agents.Working, At: changed.Add(-time.Minute)}, // older than ccmux: ignored
		// codex_pane6's pane: the newest report names its session.
		"c6-old": {State: agents.Idle, At: changed.Add(-time.Hour), Pane: "%6"},
		"c6":     {State: agents.Waiting, At: changed.Add(time.Minute), Pane: "%6"},
		"c7":     {State: agents.Idle, At: changed.Add(2 * time.Minute), Pane: "%7"},
	}
}

func TestParse(t *testing.T) {
	got, err := parse([]byte(ccmuxJSON), reported())
	if err != nil {
		t.Fatal(err)
	}
	want := []agents.Agent{
		{Path: "/r/.claude/worktrees/wicpa", Status: "completed", Provider: "claude", SessionID: "01a14609-3a63-7c02-bd2b-c313bedc3c37", Target: "d48f", Branch: "update/3068", Repo: "/r", Title: "Fix license headings"},
		{Path: "/nowhere", Status: "working", Target: "e19a", Branch: "main", Title: "Fix the queue timeout"},
		{Path: "/r", Status: "idle", Target: "f2c0", Branch: "main", Repo: "/r", Title: "✳ Claude Code"},
		{Path: "/r", Status: "waiting", Provider: "codex", Target: "codex_pane1", Branch: "main", Repo: "/r", Title: "Fix editor | r"},
		{Path: "/r", Status: "working", Provider: "codex", Target: "codex_pane2", Branch: "main", Repo: "/r", Title: "Fix editor | r"},
		{Path: "/r", Status: "idle", Provider: "codex", Target: "codex_pane3", Branch: "main", Repo: "/r", Title: "Fix editor | r"},
		{Path: "/r", Status: "waiting", Provider: "codex", Target: "codex_pane4", Branch: "main", Repo: "/r", Title: "Fix editor | r"},
		{Path: "/r", Status: "idle", Provider: "codex", SessionID: "n5", Target: "codex_pane5", Branch: "main", Repo: "/r", Title: "Fix editor | r"},
		{Path: "/r", Status: "waiting", Provider: "codex", SessionID: "c6", Target: "codex_pane6", Branch: "main", Repo: "/r", Title: "codex"},
		{Path: "/r", Status: "idle", SessionID: "n6", Target: "n6", Branch: "main", Repo: "/r", Title: "claude"},
		{Path: "/r", Status: "starting", Target: "s7", Title: "tab here"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("session %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if _, err := parse([]byte("not json"), nil); err == nil {
		t.Fatal("want a parse error")
	}
}

// TestListWithoutCcmux checks that a missing ccmux names where to get it,
// since asanamate shows the error as is.
func TestListWithoutCcmux(t *testing.T) {
	saved := command
	t.Cleanup(func() { command = saved })
	command = []string{"asanamate-test-no-such-ccmux", "show", "--json"}
	_, err := List(context.Background(), t.TempDir())
	want := "asanamate-test-no-such-ccmux not installed; see " + homepage
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// TestOutputThroughAgentsCommand feeds the adapter's output through
// agents.command, as asanamate runs it, and checks each session gets the
// state, branch, and repo the built-in ccmux preset gave it (TestParse's want
// for the preset's cases). A status ccmux reports that is no state name still
// maps through agents.states.
func TestOutputThroughAgentsCommand(t *testing.T) {
	list, err := parse([]byte(ccmuxJSON), reported())
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Print(&out, list); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "agents.tsv")
	if err := os.WriteFile(file, []byte(out.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := agents.Fetch(context.Background(), "cat '"+file+"'", map[string][]string{"working": {"starting"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []agents.State{
		agents.Completed, agents.Working, agents.Idle, agents.Waiting, agents.Working,
		agents.Idle, agents.Waiting, agents.Idle, agents.Waiting, agents.Idle, agents.Working,
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, w := range want {
		g := got[i]
		if g.State != w {
			t.Errorf("agent %d state = %q, want %q", i, g.State, w)
		}
		if g.State = ""; g != list[i] {
			t.Errorf("agent %d = %+v, want %+v", i, g, list[i])
		}
	}
}

func TestHookRecordsSessionState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	hook := func(event, id string) {
		t.Helper()
		if err := WriteHook(strings.NewReader(`{"hook_event_name":"`+event+`","session_id":"`+id+`"}`), dir, "%3"); err != nil {
			t.Fatal(err)
		}
	}
	state := func(id string) agents.State {
		got := map[string]live{}
		readHookFiles(dir, got)
		return got[id].State
	}
	hook("UserPromptSubmit", "s1")
	if got := state("s1"); got != agents.Working {
		t.Fatalf("after prompt = %q", got)
	}
	got := map[string]live{}
	readHookFiles(dir, got)
	if got["s1"].Pane != "%3" {
		t.Fatalf("pane = %q, want %%3", got["s1"].Pane)
	}
	hook("PermissionRequest", "s1")
	if got := state("s1"); got != agents.Waiting {
		t.Fatalf("after permission request = %q", got)
	}
	hook("PostCompact", "s1") // untracked: no change
	hook("Stop", "s1")
	if got := state("s1"); got != agents.Idle {
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

// RunHook, which `asanamate hook codex` and `asanamate-ccmux hook codex`
// both serve, writes where List reads.
func TestRunHookWritesUnderStateDir(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	RunHook([]string{"codex"}, strings.NewReader(`{"hook_event_name":"PermissionRequest","session_id":"s1"}`))
	RunHook([]string{"claude"}, strings.NewReader(`{"hook_event_name":"Stop","session_id":"s2"}`)) // unknown tool: ignored
	got := map[string]live{}
	readHookFiles(HookDir(filepath.Join(state, "asanamate")), got)
	if len(got) != 1 || got["s1"].State != agents.Waiting {
		t.Fatalf("got %+v", got)
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
	if len(got) != 1 || got["a"].State != agents.Working || !got["a"].At.Equal(time.UnixMilli(1790000000000)) {
		t.Fatalf("got %+v", got)
	}
}
