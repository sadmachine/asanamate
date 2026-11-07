// Package jsoncmd runs provider commands that read one JSON request on stdin
// and write one JSON response on stdout.
package jsoncmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/sadmachine/asanamate/internal/display"
)

// Run executes command with /bin/sh -c and returns its stdout. Data goes
// through stdin, never into user-authored shell text. label prefixes errors,
// such as "time provider". It is RunBounded with DefaultLimits.
func Run(ctx context.Context, label, command string, req any) ([]byte, error) {
	return RunBounded(ctx, label, command, req, DefaultLimits)
}

// Limits bounds the bytes captured from each output stream. Both must be positive.
type Limits struct{ Stdout, Stderr int }

// DefaultLimits bounds commands whose protocols set no limit of their own.
var DefaultLimits = Limits{Stdout: 16 << 20, Stderr: 64 << 10}

// ErrOutputLimit means the response is incomplete and must not be decoded.
var ErrOutputLimit = errors.New("command output exceeds limit")

// RunBounded runs command with bounded capture and process-group cleanup.
// Cancelling ctx or overflowing either stream kills the shell and its
// children. Errors carry stderr with escape sequences and controls removed.
func RunBounded(ctx context.Context, label, command string, req any, limits Limits) ([]byte, error) {
	stdout, _, err := RunBoundedStderr(ctx, label, command, req, limits)
	return stdout, err
}

// RunBoundedStderr is RunBounded that also returns stderr on success, with
// escape sequences and controls removed, for providers that explain partial
// results there.
func RunBoundedStderr(ctx context.Context, label, command string, req any, limits Limits) (stdout []byte, stderr string, err error) {
	input, err := json.Marshal(req)
	if err != nil {
		return nil, "", err
	}
	return runShell(ctx, label, command, bytes.NewReader(input), limits)
}

// RunShell runs command with /bin/sh -c, no stdin, and RunBounded's limits
// and cleanup, for line-oriented sources that take no request.
func RunShell(ctx context.Context, label, command string, limits Limits) ([]byte, error) {
	out, _, err := runShell(ctx, label, command, nil, limits)
	return out, err
}

func runShell(ctx context.Context, label, command string, stdin io.Reader, limits Limits) ([]byte, string, error) {
	if limits.Stdout <= 0 || limits.Stderr <= 0 {
		return nil, "", fmt.Errorf("%s: output limits must be positive", label)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "/bin/sh", "-c", command)
	cmd.Stdin = stdin
	return runBounded(ctx, label, cmd, cancel, limits)
}

// RunArgsBounded runs a known argument vector without a shell or stdin.
func RunArgsBounded(ctx context.Context, label string, args []string, limits Limits) ([]byte, error) {
	if len(args) == 0 || limits.Stdout <= 0 || limits.Stderr <= 0 {
		return nil, fmt.Errorf("%s: arguments and positive output limits required", label)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	out, _, err := runBounded(ctx, label, exec.CommandContext(runCtx, args[0], args[1:]...), cancel, limits)
	return out, err
}

func runBounded(ctx context.Context, label string, cmd *exec.Cmd, cancel context.CancelFunc, limits Limits) ([]byte, string, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	killGroup := func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.Cancel = killGroup
	// A shell that exits while descendants retain its pipes must not hang Wait.
	cmd.WaitDelay = 250 * time.Millisecond
	stdout := boundedBuffer{limit: limits.Stdout, cancel: cancel}
	stderr := boundedBuffer{limit: limits.Stderr, cancel: cancel}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if cmd.Process != nil {
		_ = killGroup()
	}
	if stdout.overflow || stderr.overflow {
		stream, limit := "stdout", limits.Stdout
		if !stdout.overflow {
			stream, limit = "stderr", limits.Stderr
		}
		return nil, "", fmt.Errorf("%s: %w (%s: %d bytes)", label, ErrOutputLimit, stream, limit)
	}
	if ctx.Err() != nil {
		return nil, "", fmt.Errorf("%s: %w", label, ctx.Err())
	}
	message := display.Line(stderr.String())
	if err != nil {
		if message != "" {
			return nil, "", fmt.Errorf("%s: %s: %w", label, message, err)
		}
		return nil, "", fmt.Errorf("%s: %w", label, err)
	}
	return stdout.Bytes(), message, nil
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (b *boundedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *boundedBuffer) String() string { return b.buffer.String() }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.buffer.Len()
	if n > remaining {
		b.buffer.Write(p[:remaining])
		b.overflow = true
		b.cancel()
	} else {
		b.buffer.Write(p)
	}
	return n, nil
}
