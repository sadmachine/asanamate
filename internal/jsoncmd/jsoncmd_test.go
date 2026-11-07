package jsoncmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBoundedCapture(t *testing.T) {
	limits := Limits{Stdout: 32, Stderr: 16}
	for _, tc := range []struct {
		name, command string
		wantLimit     bool
	}{
		{"exact stdout", `printf '%032d' 0`, false},
		{"stdout overflow", `printf '%033d' 0`, true},
		{"stderr overflow on success", `printf '%017d' 0 >&2`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := RunBounded(context.Background(), "fixture", tc.command, nil, limits)
			if errors.Is(err, ErrOutputLimit) != tc.wantLimit {
				t.Fatalf("output = %q, error = %v", out, err)
			}
			if tc.wantLimit && out != nil {
				t.Fatal("overflow returned truncated output")
			}
		})
	}
	if _, err := RunBounded(context.Background(), "fixture", "exit 4", nil, limits); err == nil {
		t.Fatal("missing command failure")
	}
	if _, err := RunBounded(context.Background(), "fixture", "true", nil, Limits{}); err == nil {
		t.Fatal("zero limits accepted")
	}
}

func TestBoundedCancellationKillsDiscoveryChildren(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	t.Setenv("PID_FILE", pidFile)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := RunBounded(ctx, "fixture", `sleep 60 & child=$!; printf '%s' "$child" > "$PID_FILE"; wait`, nil, Limits{Stdout: 32, Stderr: 32})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Fatalf("elapsed = %s, error = %v", time.Since(start), err)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); ; {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			break
		}
		// Linux can retain a killed orphan as a zombie until PID 1 reaps it.
		if stat, _ := os.ReadFile("/proc/" + string(data) + "/stat"); strings.Contains(string(stat), ") Z ") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("discovery subprocess %d survived cancellation", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The background sleep outlives the shell; returning well before it ends,
// with no deadline set, proves Wait does not hang on its inherited pipes. The
// bound is generous so a loaded CI runner does not fail it.
func TestBoundedDoesNotHangOnInheritedPipes(t *testing.T) {
	start := time.Now()
	out, err := RunBounded(context.Background(), "fixture", `sleep 60 & printf '{}'`, nil, Limits{Stdout: 32, Stderr: 32})
	if err == nil || out != nil || time.Since(start) > 30*time.Second {
		t.Fatalf("output = %q, elapsed = %s, error = %v", out, time.Since(start), err)
	}
}

func TestBoundedErrorIsDisplaySafe(t *testing.T) {
	command := `printf '\033[31mbroken\033[0m\n' >&2; exit 3`
	for name, run := range map[string]func() error{
		"RunBounded": func() error {
			_, err := RunBounded(context.Background(), "fixture", command, nil, Limits{Stdout: 32, Stderr: 64})
			return err
		},
		"Run":      func() error { _, err := Run(context.Background(), "fixture", command, nil); return err },
		"RunShell": func() error { _, err := RunShell(context.Background(), "fixture", command, DefaultLimits); return err },
	} {
		if err := run(); err == nil || strings.ContainsRune(err.Error(), '\x1b') || !strings.Contains(err.Error(), "fixture: broken") {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	out, err := Run(context.Background(), "run", `cat; printf '%0100d' 0`, map[string]int{"version": 1})
	if err != nil || !strings.HasPrefix(string(out), `{"version":1}`) || len(out) != 113 {
		t.Fatalf("run output length = %d, error = %v", len(out), err)
	}
}

func TestRunAndRunShellDoNotHangOnInheritedPipes(t *testing.T) {
	for name, run := range map[string]func(context.Context) error{
		"Run": func(ctx context.Context) error { _, err := Run(ctx, "fixture", `sleep 60 & cat`, nil); return err },
		"RunShell": func(ctx context.Context) error {
			_, err := RunShell(ctx, "fixture", `sleep 60 & cat`, DefaultLimits)
			return err
		},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		start := time.Now()
		err := run(ctx)
		cancel()
		if err == nil || time.Since(start) > 30*time.Second {
			t.Fatalf("%s: elapsed = %s, error = %v", name, time.Since(start), err)
		}
	}
}

func TestRunShellGivesNoStdin(t *testing.T) {
	out, err := RunShell(context.Background(), "fixture", `cat; printf done`, DefaultLimits)
	if err != nil || string(out) != "done" {
		t.Fatalf("output = %q, error = %v", out, err)
	}
}

func TestBoundedStderrOnSuccess(t *testing.T) {
	out, stderr, err := RunBoundedStderr(context.Background(), "fixture", `printf ok; printf 'one host\033[31m failed\n' >&2`, nil, DefaultLimits)
	if err != nil || string(out) != "ok" || stderr != "one host failed" {
		t.Fatalf("out = %q, stderr = %q, err = %v", out, stderr, err)
	}
}
