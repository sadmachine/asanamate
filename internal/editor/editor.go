// Package editor opens files in the user's external editor.
package editor

import (
	"cmp"
	"os"
	"os/exec"
	"strings"
)

// Name returns $VISUAL, else $EDITOR, else fallback. The value may carry
// flags, such as "code --wait".
func Name(fallback string) string {
	return cmp.Or(strings.TrimSpace(os.Getenv("VISUAL")), strings.TrimSpace(os.Getenv("EDITOR")), fallback)
}

// Command runs editor on path. The shell splits the editor's flags; the path
// is passed as an argument, never interpolated into shell code.
func Command(editor, path string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", "exec "+editor+` "$1"`, "asanamate-editor", path)
}
