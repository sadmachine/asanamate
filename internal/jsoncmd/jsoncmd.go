// Package jsoncmd runs provider commands that read one JSON request on stdin
// and write one JSON response on stdout.
package jsoncmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Run executes command with /bin/sh -c and returns its stdout. Data goes
// through stdin, never into user-authored shell text. label prefixes errors,
// such as "time provider".
func Run(ctx context.Context, label, command string, req any) ([]byte, error) {
	input, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return nil, fmt.Errorf("%s: %s: %w", label, message, err)
		}
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return out, nil
}

// Limits bounds the bytes captured from each output stream. Both must be positive.
type Limits struct{ Stdout, Stderr int }

// ErrOutputLimit means the response is incomplete and must not be decoded.
var ErrOutputLimit = errors.New("command output exceeds limit")

// RunBounded is Run with bounded capture and process-group cleanup. Cancelling
// ctx or overflowing either stream kills the shell and its discovery children.
// Run retains its existing capture and process behavior for other protocols.
func RunBounded(ctx context.Context, label, command string, req any, limits Limits) ([]byte, error) {
	if limits.Stdout <= 0 || limits.Stderr <= 0 {
		return nil, fmt.Errorf("%s: output limits must be positive", label)
	}
	input, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "/bin/sh", "-c", command)
	cmd.Stdin = bytes.NewReader(input)
	return runBounded(ctx, label, cmd, cancel, limits)
}

// RunArgsBounded runs a known argument vector without a shell or stdin.
func RunArgsBounded(ctx context.Context, label string, args []string, limits Limits) ([]byte, error) {
	if len(args) == 0 || limits.Stdout <= 0 || limits.Stderr <= 0 {
		return nil, fmt.Errorf("%s: arguments and positive output limits required", label)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	return runBounded(ctx, label, exec.CommandContext(runCtx, args[0], args[1:]...), cancel, limits)
}

func runBounded(ctx context.Context, label string, cmd *exec.Cmd, cancel context.CancelFunc, limits Limits) ([]byte, error) {
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
		return nil, fmt.Errorf("%s: %w (%s: %d bytes)", label, ErrOutputLimit, stream, limit)
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s: %w", label, ctx.Err())
	}
	if err != nil {
		message := strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
				return ' '
			}
			return r
		}, ansi.Strip(stderr.String())))
		if message != "" {
			return nil, fmt.Errorf("%s: %s: %w", label, message, err)
		}
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return stdout.Bytes(), nil
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
