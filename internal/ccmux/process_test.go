package ccmux

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"testing"
)

func TestProcessArgsAndDescends(t *testing.T) {
	ctx := context.Background()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	other := cmd.Process.Pid

	if got := processArgs(ctx, other); !slices.Equal(got, []string{"sleep", "30"}) {
		t.Errorf("processArgs = %q", got)
	}
	if got := processArgs(ctx, 0); got != nil {
		t.Errorf("processArgs(0) = %q", got)
	}
	if !descends(ctx, os.Getpid(), os.Getppid()) || !descends(ctx, other, os.Getpid()) {
		t.Error("want a process to descend from its parent")
	}
	if descends(ctx, os.Getpid(), other) {
		t.Error("want no descent from an unrelated process")
	}
}

// hookPane keeps $TMUX_PANE only for the pane this process runs under, so a
// Codex daemon started from another pane records none.
func TestHookPane(t *testing.T) {
	if pane := os.Getenv("TMUX_PANE"); pane != "" && os.Getenv("TMUX") != "" {
		if got := hookPane(); got != pane {
			t.Errorf("own pane: hookPane = %q, want %q", got, pane)
		}
	}
	t.Setenv("TMUX_PANE", "%999999")
	if got := hookPane(); got != "" {
		t.Errorf("missing pane: hookPane = %q, want none", got)
	}
}
