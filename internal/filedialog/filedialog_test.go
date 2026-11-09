package filedialog

import (
	"errors"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestCommand(t *testing.T) {
	installed := func(names ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			if slices.Contains(names, name) {
				return "/usr/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		}
	}
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	for _, tt := range []struct {
		name  string
		goos  string
		env   map[string]string
		tools []string
		want  string // the command, or "" when unavailable
	}{
		{"mac", "darwin", nil, []string{"osascript"}, "osascript"},
		{"mac over ssh", "darwin", map[string]string{"SSH_CONNECTION": "1.2.3.4 5 6.7.8.9 22"}, []string{"osascript"}, ""},
		{"linux zenity", "linux", map[string]string{"DISPLAY": ":0"}, []string{"zenity", "kdialog"}, "zenity"},
		{"linux kdialog on wayland", "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, []string{"kdialog"}, "kdialog"},
		{"linux without a display", "linux", nil, []string{"zenity"}, ""},
		{"linux without a dialog tool", "linux", map[string]string{"DISPLAY": ":0"}, nil, ""},
		{"windows", "windows", nil, nil, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args, ok := command(tt.goos, env(tt.env), installed(tt.tools...), "/repo")
			if got := ""; ok {
				got = args[0]
				if got != tt.want {
					t.Fatalf("command = %v, want %s", args, tt.want)
				}
			} else if tt.want != "" {
				t.Fatalf("unavailable, want %s", tt.want)
			}
		})
	}
	args, _ := command("linux", env(map[string]string{"DISPLAY": ":0"}), installed("zenity"), "/repo")
	if !slices.Contains(args, "--filename=/repo/") || !slices.Contains(args, "--multiple") {
		t.Fatalf("zenity args = %v", args)
	}
}

func TestParse(t *testing.T) {
	paths, err := parse([]byte("/a/one.pdf\n/b/two files.txt\n\n"), "", nil)
	if err != nil || !slices.Equal(paths, []string{"/a/one.pdf", "/b/two files.txt"}) {
		t.Fatalf("paths = %q, err = %v", paths, err)
	}
	if _, err := parse(nil, "", nil); !errors.Is(err, ErrCanceled) {
		t.Fatalf("empty output: err = %v", err)
	}
	exit := exitError(t, 1)
	if _, err := parse(nil, "execution error: User canceled. (-128)", exit); !errors.Is(err, ErrCanceled) {
		t.Fatalf("AppleScript cancel: err = %v", err)
	}
	if _, err := parse(nil, "", exit); !errors.Is(err, ErrCanceled) {
		t.Fatalf("zenity cancel: err = %v", err)
	}
	if _, err := parse(nil, "Not authorized to send Apple events (-1743)", exit); err == nil || errors.Is(err, ErrCanceled) || !strings.Contains(err.Error(), "-1743") {
		t.Fatalf("failure: err = %v", err)
	}
}

// exitError returns the error of a process that exited with code.
func exitError(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run()
	if err == nil {
		t.Fatal("expected an exit error")
	}
	return err
}
