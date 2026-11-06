package action

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/conversations"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func TestConversationSnapshotAndCommand(t *testing.T) {
	t.Setenv("ASANAMATE_CONVERSATION_JSON", "/stale")
	t.Setenv("ASANAMATE_CONVERSATION_ID", "stale")
	t.Setenv("ASANAMATE_STALE", "old")
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	record := conversations.Record{Provider: "codex", SessionID: "id; $(touch " + marker + ") ' \"", TaskGID: "42", Title: "\x1b[31mRaw title", CWD: "/deleted/worktree", Branch: "other-branch", Activity: "unknown", Archived: true}
	c := Context{Ticket: ticket.Ticket{Task: asana.Task{GID: "42", Name: "Selected ticket"}}, Repo: "/not/the/provider/cwd", Worktree: "/not/the/worktree"}
	script, err := filepath.Abs("testdata/conversation.sh")
	if err != nil {
		t.Fatal(err)
	}
	command := "/bin/sh '" + script + "'"
	if c.Files, err = WriteFiles(dir, c.Ticket); err != nil {
		t.Fatal(err)
	}
	cmd, err := ConversationCommand(dir, command, c, record)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != "" || cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil || cmd.Args[len(cmd.Args)-1] != command {
		t.Fatalf("not a foreground command in current directory: %+v", cmd)
	}
	var snapshot string
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "ASANAMATE_STALE=") {
			t.Fatal("inherited action variable leaked")
		}
		if value, ok := strings.CutPrefix(kv, "ASANAMATE_CONVERSATION_JSON="); ok {
			snapshot = value
		}
	}
	if !filepath.IsAbs(snapshot) {
		t.Fatalf("snapshot path = %q", snapshot)
	}
	for path, mode := range map[string]os.FileMode{snapshot: 0o600, filepath.Dir(snapshot): 0o700, filepath.Join(filepath.Dir(snapshot), "ticket.json"): 0o600, filepath.Join(filepath.Dir(snapshot), "ticket.md"): 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s: info = %v, error = %v", path, info, err)
		}
	}
	data, _ := os.ReadFile(snapshot)
	var saved conversations.OpenRequest
	if err := json.Unmarshal(data, &saved); err != nil || saved.Version != 1 || saved.Operation != "open" || saved.Asana.TaskGID != "42" || saved.Conversation != record {
		t.Fatalf("snapshot = %s, error = %v", data, err)
	}
	cmd.Stdin = strings.NewReader("input stays separate\n")
	out, err := cmd.Output()
	if err != nil || !strings.Contains(string(out), "input=input stays separate\n") || !strings.Contains(string(out), "id="+record.SessionID+"\n") {
		t.Fatalf("output = %q, error = %v", out, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("record metadata executed as shell text")
	}
	if filepath.Dir(snapshot) != filepath.Dir(c.Files.JSON) {
		t.Fatalf("snapshot %s is not beside the ticket exports %s", snapshot, c.Files.JSON)
	}
	// Without ticket exports (the CLI path), each open still gets its own run.
	bare := Context{Ticket: ticket.Ticket{Task: asana.Task{GID: "42"}}}
	first, err := ConversationCommand(dir, command, bare, record)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ConversationCommand(dir, command, bare, record)
	if err != nil || slices.Equal(second.Env, first.Env) {
		t.Fatal("open runs reused snapshot")
	}
	if !slices.Contains(first.Env, "ASANAMATE_TICKET_JSON=") {
		t.Fatal("absent ticket exports are not empty")
	}
	for _, kv := range Command(config.Action{Command: "true"}, c).Env {
		if strings.HasPrefix(kv, "ASANAMATE_CONVERSATION_") && !strings.HasSuffix(kv, "=") {
			t.Fatalf("conversation selection leaked to unrelated action: %s", kv)
		}
	}
	for _, bad := range []conversations.Record{{}, {Provider: "codex", SessionID: "id", TaskGID: "43", Title: "other", Activity: "unknown"}} {
		if _, err := ConversationCommand(dir, command, c, bad); err == nil {
			t.Fatal("invalid selection accepted")
		}
	}
	if _, err := ConversationCommand(dir, " ", c, record); err == nil {
		t.Fatal("blank opener accepted")
	}
}

func TestConversationFixtureGetsTerminalInput(t *testing.T) {
	script, err := exec.LookPath("script")
	if err != nil {
		t.Fatal("terminal fixture requires system script command")
	}
	opener, _ := filepath.Abs("testdata/conversation.sh")
	c := Context{Ticket: ticket.Ticket{Task: asana.Task{GID: "42"}}}
	record := conversations.Record{Provider: "fixture", SessionID: "native", TaskGID: "42", Title: "Saved", Activity: "unknown"}
	for _, input := range []string{"fixture terminal input", "fail"} {
		cmd, err := ConversationCommand(t.TempDir(), "/bin/sh '"+opener+"'", c, record)
		if err != nil {
			t.Fatal(err)
		}
		var terminal *exec.Cmd
		if runtime.GOOS == "darwin" {
			terminal = exec.Command(script, append([]string{"-q", "/dev/null"}, cmd.Args...)...)
		} else {
			terminal = exec.Command(script, "-q", "-e", "-c", cmd.Args[len(cmd.Args)-1], "/dev/null")
		}
		terminal.Env = cmd.Env
		// Keep the pipe open until script exits. BSD script otherwise sends an
		// EOF character ahead of the queued line when stdin closes at startup.
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		defer writer.Close()
		terminal.Stdin = reader
		if _, err := writer.WriteString(input + "\n"); err != nil {
			t.Fatal(err)
		}
		out, err := terminal.CombinedOutput()
		if (err != nil) != (input == "fail") || !strings.Contains(string(out), "terminal=yes") || !strings.Contains(string(out), "input="+input) {
			t.Fatalf("terminal output = %q, error = %v", out, err)
		}
	}
}

func TestConversationSnapshotFailureKeepsCallerRunDir(t *testing.T) {
	dir := t.TempDir()
	c := Context{Ticket: ticket.Ticket{Task: asana.Task{GID: "42", Name: "Selected ticket"}}}
	var err error
	if c.Files, err = WriteFiles(dir, c.Ticket); err != nil {
		t.Fatal(err)
	}
	// A directory where the snapshot goes makes its write fail.
	if err := os.Mkdir(filepath.Join(filepath.Dir(c.Files.JSON), "conversation.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	record := conversations.Record{Provider: "codex", SessionID: "id", TaskGID: "42", Activity: "unknown"}
	if _, err := ConversationCommand(dir, "true", c, record); err == nil {
		t.Fatal("want the snapshot write to fail")
	}
	if _, err := os.Stat(c.Files.JSON); err != nil {
		t.Fatalf("the caller's run directory was removed: %v", err)
	}
}
