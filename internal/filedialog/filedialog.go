// Package filedialog opens the system's file chooser: Finder's on macOS, or
// zenity's or kdialog's on a Linux desktop.
package filedialog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ErrCanceled means the dialog closed without choosing files.
var ErrCanceled = errors.New("no files chosen")

// chooseScript runs Finder's chooser, starting in argv's directory when it
// is not empty, and prints each chosen path on its own line.
const chooseScript = `on run argv
	set startDir to item 1 of argv
	if startDir is "" then
		set chosen to choose file with prompt "Attach files" with multiple selections allowed
	else
		set chosen to choose file with prompt "Attach files" default location (POSIX file startDir) with multiple selections allowed
	end if
	set out to ""
	repeat with f in chosen
		set out to out & POSIX path of f & linefeed
	end repeat
	return out
end run`

// Available reports whether Choose can open a dialog in this session.
func Available() bool {
	_, ok := command(runtime.GOOS, os.Getenv, exec.LookPath, "")
	return ok
}

// Choose opens the dialog in dir, or the dialog's default when dir is "",
// and returns the chosen files' paths, or ErrCanceled.
func Choose(ctx context.Context, dir string) ([]string, error) {
	args, ok := command(runtime.GOOS, os.Getenv, exec.LookPath, dir)
	if !ok {
		return nil, errors.New("no system file dialog is available here")
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	return parse(out, stderr.String(), err)
}

// command returns the dialog command for goos, or false when this session
// cannot show one: macOS outside SSH, or a Linux desktop with zenity or
// kdialog installed.
func command(goos string, getenv func(string) string, lookPath func(string) (string, error), dir string) ([]string, bool) {
	has := func(name string) bool { _, err := lookPath(name); return err == nil }
	switch {
	case goos == "darwin":
		// Over SSH the dialog would open on the remote Mac's screen.
		if getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != "" || !has("osascript") {
			return nil, false
		}
		return []string{"osascript", "-e", chooseScript, dir}, true
	case getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "":
		return nil, false
	case has("zenity"):
		args := []string{"zenity", "--file-selection", "--multiple", "--separator=\n", "--title=Attach files"}
		if dir != "" {
			args = append(args, "--filename="+strings.TrimSuffix(dir, "/")+"/")
		}
		return args, true
	case has("kdialog"):
		return []string{"kdialog", "--title", "Attach files", "--getopenfilename", dir, "--multiple", "--separate-output"}, true
	}
	return nil, false
}

// parse turns a dialog's output into paths. Every dialog exits non-zero on
// cancel; AppleScript names its cancel error -128.
func parse(out []byte, stderr string, err error) ([]string, error) {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if exit.ExitCode() == 1 && (strings.TrimSpace(stderr) == "" || strings.Contains(stderr, "-128")) {
			return nil, ErrCanceled
		}
		if msg := strings.TrimSpace(stderr); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	if len(paths) == 0 {
		return nil, ErrCanceled
	}
	return paths, nil
}
