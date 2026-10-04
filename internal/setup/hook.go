package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/prompt"
)

// CodexHome is Codex's config directory: $CODEX_HOME, else ~/.codex.
func CodexHome() (string, error) {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// OfferCodexHook adds `asanamate hook codex` to Codex's hooks.json, after
// asking, when Codex is installed and the hook is missing. It does nothing
// when o.CodexHome or o.Executable is unset.
func OfferCodexHook(o Options) error {
	if o.CodexHome == "" || o.Executable == "" {
		return nil
	}
	if info, err := os.Stat(o.CodexHome); err != nil || !info.IsDir() {
		return nil
	}
	path := filepath.Join(o.CodexHome, "hooks.json")
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	file := map[string]any{}
	if len(old) > 0 {
		if err := json.Unmarshal(old, &file); err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
	}
	hooks, ok := file["hooks"].(map[string]any)
	if file["hooks"] != nil && !ok {
		return fmt.Errorf("%s: \"hooks\" is not an object", path)
	}
	if hooks == nil {
		hooks = map[string]any{}
	}
	if hasCodexHook(hooks) {
		return nil
	}
	lipgloss.Fprintln(o.Out, "\n"+warning.Render("Warning: Codex agent status needs a hook")+"\n"+codexHookWarning)
	install, err := prompt.Confirm(o.In, o.Out, "Add asanamate's Codex hook to "+path+"?")
	if err != nil {
		return err
	}
	if !install {
		fmt.Fprintln(o.Out, "Skipped. Run `asanamate setup hooks` to add it later.")
		return nil
	}
	command := shellQuote(o.Executable) + " hook codex"
	for _, event := range agents.HookEvents() {
		groups, _ := hooks[event].([]any)
		hooks[event] = append(groups, map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": command}},
		})
	}
	file["hooks"] = hooks
	var data bytes.Buffer
	enc := json.NewEncoder(&data)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(file); err != nil {
		return err
	}
	backup := ""
	if old != nil {
		backup = path + ".bak"
		if err := writePrivate(backup, old); err != nil {
			return err
		}
	}
	if err := writePrivate(path, data.Bytes()); err != nil {
		return err
	}
	fmt.Fprintf(o.Out, "Added the hook to %s.", path)
	if backup != "" {
		fmt.Fprintf(o.Out, " The previous file is saved as %s.", backup)
	}
	fmt.Fprintln(o.Out, "\nCodex asks you to trust the new hooks the next time it starts.")
	return nil
}

// warning highlights the heading of a setup warning. lipgloss.Fprintln drops
// the color when the output is not a terminal or NO_COLOR is set.
var warning = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Yellow)

const codexHookWarning = `  Without asanamate's Codex hook, Codex agent status will be
  inaccurate or wrong: ccmux can show a busy Codex session as idle.
  The hook records each session's real state.
`

// hasCodexHook reports whether any hook in hooks runs `asanamate hook codex`.
func hasCodexHook(hooks map[string]any) bool {
	for _, groups := range hooks {
		groups, _ := groups.([]any)
		for _, g := range groups {
			g, _ := g.(map[string]any)
			list, _ := g["hooks"].([]any)
			for _, h := range list {
				h, _ := h.(map[string]any)
				cmd, _ := h["command"].(string)
				if strings.Contains(cmd, "asanamate") && strings.HasSuffix(cmd, " hook codex") {
					return true
				}
			}
		}
	}
	return false
}

// shellQuote quotes s for /bin/sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
