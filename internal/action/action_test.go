package action

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func TestEnvIncludesTicketFields(t *testing.T) {
	due, branch := "2026-10-01", "feat/x"
	tk := ticket.Ticket{Task: asana.Task{
		GID: "42", Name: "Fix Login!", DueOn: &due, PermalinkURL: "https://app.asana.com/t/42",
		Assignee:        &asana.Ref{Name: "Ann"},
		Tags:            []asana.Ref{{Name: "bug"}, {Name: "p1"}},
		AssigneeSection: &asana.Ref{Name: "Today"},
		Memberships:     []asana.Membership{{Project: asana.Ref{GID: "7", Name: "Web"}, Section: &asana.Ref{Name: "Doing"}}},
		CustomFields:    []asana.CustomField{{Name: "Branch Name ", DisplayValue: &branch}},
	}}
	env := Env(Context{Ticket: tk, Project: &asana.Ref{GID: "7", Name: "Web"}, Repo: "/r", ConfirmWrites: true, Files: Files{JSON: "/j", Markdown: "/m"}})
	for _, want := range []string{
		"ASANAMATE_GID=42", "ASANAMATE_TITLE=Fix Login!", "ASANAMATE_URL=https://app.asana.com/t/42",
		"ASANAMATE_SLUG=fix-login", "ASANAMATE_COMPLETED=false", "ASANAMATE_ASSIGNEE=Ann",
		"ASANAMATE_DUE=2026-10-01", "ASANAMATE_TAGS=bug,p1", "ASANAMATE_MY_SECTION=Today",
		"ASANAMATE_PROJECT=Web", "ASANAMATE_PROJECT_GID=7", "ASANAMATE_SECTION=Doing",
		"ASANAMATE_REPO=/r", "ASANAMATE_TICKET_JSON=/j", "ASANAMATE_TICKET_MD=/m",
		"ASANAMATE_CONFIRM_WRITES=1", "ASANAMATE_FIELD_BRANCH_NAME=feat/x",
	} {
		if !slices.Contains(env, want) {
			t.Errorf("env missing %q", want)
		}
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Fix Login!":              "fix-login",
		"  Ünïcode & stuff ":      "n-code-stuff",
		strings.Repeat("ab-", 30): strings.TrimRight(strings.Repeat("ab-", 17)[:50], "-"),
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultProject(t *testing.T) {
	web := asana.Membership{Project: asana.Ref{GID: "1", Name: "Web"}}
	api := asana.Membership{Project: asana.Ref{GID: "2", Name: "API"}}
	if p := DefaultProject(asana.Task{Memberships: []asana.Membership{web, api}}, "2"); p == nil || p.GID != "2" {
		t.Errorf("viewed project: got %v", p)
	}
	if p := DefaultProject(asana.Task{Memberships: []asana.Membership{web}}, ""); p == nil || p.GID != "1" {
		t.Errorf("only project: got %v", p)
	}
	if p := DefaultProject(asana.Task{Memberships: []asana.Membership{web, api}}, ""); p != nil {
		t.Errorf("ambiguous: got %v, want nil", p)
	}
}

func TestWriteFiles(t *testing.T) {
	dir := t.TempDir()
	files, err := WriteFiles(dir, ticket.Ticket{Task: asana.Task{GID: "9", Name: "T"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{files.JSON, files.Markdown} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: info = %v, err = %v", p, info, err)
		}
	}
	var decoded map[string]any
	data, _ := os.ReadFile(files.JSON)
	if err := json.Unmarshal(data, &decoded); err != nil || decoded["gid"] != "9" {
		t.Fatalf("json = %s, err = %v", data, err)
	}
	if _, err := WriteFiles(dir, ticket.Ticket{Task: asana.Task{GID: "../x"}}); err == nil {
		t.Fatal("expected rejection of a non-numeric gid")
	}
}

func TestCommandIsInjectionSafe(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ASANAMATE_FIELD_STALE", "old")
	tk := ticket.Ticket{Task: asana.Task{GID: "1", Name: `$(touch pwned) "; touch pwned2 '`}}
	cmd := Command(config.Action{Command: `printf '%s' "$ASANAMATE_TITLE"; env | grep -c ASANAMATE_FIELD_STALE || true`}, Context{Ticket: tk, Repo: dir})
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := tk.Name + "0\n"; string(out) != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
	for _, name := range []string{"pwned", "pwned2"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Fatalf("%s was created: ticket text was executed", name)
		}
	}
	if cmd.Dir != dir {
		t.Fatalf("Dir = %q, want %q", cmd.Dir, dir)
	}
}

func TestRunBackgroundLogsOutput(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "actions.log")
	if err := RunBackground(exec.Command("/bin/sh", "-c", "echo hello"), logPath); err != nil {
		t.Fatal(err)
	}
	if err := RunBackground(exec.Command("/bin/sh", "-c", "exit 3"), logPath); err == nil {
		t.Fatal("want an error for a non-zero exit")
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("log = %q", data)
	}
}

func TestRunBackgroundStartsNewSession(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 1")
	done := make(chan error, 1)
	go func() { done <- RunBackground(cmd, filepath.Join(t.TempDir(), "actions.log")) }()
	for i := 0; i < 100 && cmd.Process == nil; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if cmd.Process == nil {
		t.Fatal("process did not start")
	}
	child, err := unix.Getsid(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	parent, _ := unix.Getsid(0)
	if child == parent {
		t.Fatal("background action shares asanamate's session, so it can open /dev/tty and block on a prompt")
	}
	<-done
}

func TestExecReplacesProcess(t *testing.T) {
	if os.Getenv("ASANAMATE_TEST_EXEC") == "1" {
		cmd := exec.Command("/bin/sh", "-c", "trap '' INT; sleep 0.5; exit 7")
		cmd.Env = os.Environ()
		err := Exec(cmd)
		t.Fatalf("Exec returned: %v", err)
	}
	helper := exec.Command(os.Args[0], "-test.run=^TestExecReplacesProcess$")
	helper.Env = append(os.Environ(), "ASANAMATE_TEST_EXEC=1")
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	helper.Process.Signal(os.Interrupt)
	err := helper.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("want the command's exit code 7 after Ctrl+C, got %v", err)
	}
}
