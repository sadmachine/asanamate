package agents

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// live is a state an agent reported itself, and when. It corrects ccmux,
// which can leave a busy session "idle".
type live struct {
	State State
	At    time.Time
}

// HookDir is where `asanamate hook` keeps one state file per session, under
// asanamate's state directory.
func HookDir(stateDir string) string { return filepath.Join(stateDir, "agents") }

// hookStates maps Codex hook events to the state they leave a session in.
// SessionEnd removes the session's file instead.
var hookStates = map[string]State{
	"SessionStart":      Idle,
	"UserPromptSubmit":  Working,
	"PreToolUse":        Working,
	"PostToolUse":       Working,
	"PermissionRequest": Waiting,
	"Stop":              Idle,
	"Interrupt":         Idle,
}

// HookEvents lists the hook events WriteHook records, for installing the hook.
func HookEvents() []string {
	events := append(slices.Collect(maps.Keys(hookStates)), "SessionEnd")
	slices.Sort(events)
	return events
}

// WriteHook records the session state that the hook event JSON on in implies,
// as a file named for the session in dir. Events it does not track are ignored.
func WriteHook(in io.Reader, dir string) error {
	var ev struct {
		SessionID string `json:"session_id"`
		Event     string `json:"hook_event_name"`
	}
	if err := json.NewDecoder(in).Decode(&ev); err != nil {
		return err
	}
	if !validSessionID(ev.SessionID) {
		return nil
	}
	path := filepath.Join(dir, ev.SessionID)
	if ev.Event == "SessionEnd" {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	s, ok := hookStates[ev.Event]
	if !ok {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(string(s) + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// validSessionID reports whether id is safe to use as a file name.
func validSessionID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// liveStates returns the states agents reported themselves, by session id:
// Claude Code's own session files, then the Codex hook's files in hookDir.
func liveStates(hookDir string) map[string]live {
	states := map[string]live{}
	if dir := claudeDir(); dir != "" {
		readClaudeSessions(filepath.Join(dir, "sessions"), states)
	}
	readHookFiles(hookDir, states)
	return states
}

// claudeDir is Claude Code's config directory, "" when unknown.
func claudeDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// claudeStates maps the statuses Claude Code writes in its session files.
var claudeStates = map[string]State{"busy": Working, "waiting": Waiting, "idle": Idle}

// readClaudeSessions reads the <pid>.json files Claude Code keeps for each
// running session. The format is Claude Code's own, so unreadable files are
// skipped.
func readClaudeSessions(dir string, into map[string]live) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s struct {
			SessionID       string `json:"sessionId"`
			Status          string `json:"status"`
			StatusUpdatedAt int64  `json:"statusUpdatedAt"`
		}
		if json.Unmarshal(data, &s) != nil || s.SessionID == "" {
			continue
		}
		if state, ok := claudeStates[s.Status]; ok {
			into[s.SessionID] = live{State: state, At: time.UnixMilli(s.StatusUpdatedAt)}
		}
	}
}

// readHookFiles reads the files WriteHook leaves in dir.
func readHookFiles(dir string, into map[string]live) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !validSessionID(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		info, ierr := e.Info()
		if err != nil || ierr != nil {
			continue
		}
		if s := State(strings.TrimSpace(string(data))); s != Unknown && rank(s) < len(States) {
			into[e.Name()] = live{State: s, At: info.ModTime()}
		}
	}
}
