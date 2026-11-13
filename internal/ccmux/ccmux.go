// Package ccmux is the agent adapter for ccmux: it lists ccmux's sessions as
// asanamate agent lines (docs/extensions.md, "Agent sources"), correcting
// their states with what Claude Code and Codex report themselves.
package ccmux

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sadmachine/asanamate/internal/agents"
)

// command is the ccmux command the adapter runs.
var command = []string{"ccmux", "show", "--json"}

// homepage is where to get ccmux, named when it is not installed.
const homepage = "https://github.com/motherskitchenblr2/ccmux"

// List lists sessions from ccmux. A session that finished a turn you have not
// looked at yet (idle and unread) reports "completed". ccmux can leave a busy
// session "idle", so an idle session takes the state its agent last reported
// itself (Claude Code's session files, the Codex hook in hookDir) when that
// report is newer than ccmux's, else a Codex pane title's state. Statuses
// other than these pass through as ccmux reports them. ccmux knows no session
// ID for a pane-tracked Codex session, so it takes the one the Codex hook last
// reported from that tmux pane, else the one Codex was resumed with when the
// pane title still shows that thread (see resumedThread).
func List(ctx context.Context, hookDir string) ([]agents.Agent, error) {
	if _, err := exec.LookPath(command[0]); err != nil {
		return nil, fmt.Errorf("%s not installed; see %s", command[0], homepage)
	}
	out, err := exec.CommandContext(ctx, command[0], command[1:]...).Output()
	if err != nil {
		return nil, fmt.Errorf("ccmux show --json failed: %w", err)
	}
	codexHome, _ := CodexHome()
	return parse(out, liveStates(hookDir), func(pid int, title string) string {
		return resumedThread(processArgs(ctx, pid), title, codexHome)
	})
}

// parse reads ccmux's sessions. resumed names a pane-tracked Codex session's
// thread from its process and pane title, "" when unsure; nil skips it.
func parse(data []byte, reported map[string]live, resumed func(pid int, paneTitle string) string) ([]agents.Agent, error) {
	var sessions []struct {
		ID             string   `json:"id"`
		Cwd            string   `json:"cwd"`
		Status         string   `json:"status"`
		AttentionState *string  `json:"attentionState"`
		GitBranch      *string  `json:"gitBranch"`
		MainRepoRoot   *string  `json:"mainRepoRoot"`
		Summary        *string  `json:"summary"`
		Prompts        []string `json:"prompts"`
		PaneTitle      string   `json:"paneTitle"`
		TmuxPane       string   `json:"tmuxPane"`
		NativeID       string   `json:"nativeSessionId"`
		ChangedAt      *string  `json:"statusChangedAt"`
		AgentType      string   `json:"agentType"`
		TrackingMode   string   `json:"trackingMode"`
		PID            int      `json:"pid"`
	}
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, fmt.Errorf("parsing ccmux output: %w", err)
	}
	list := make([]agents.Agent, 0, len(sessions))
	for _, s := range sessions {
		a := agents.Agent{Provider: s.AgentType, SessionID: s.NativeID, Path: s.Cwd, Status: s.Status, Target: s.ID, Title: agents.Title(s.PaneTitle)}
		var paneStatus string
		if s.AgentType == "codex" && s.TrackingMode == "pane" {
			paneStatus, a.Title = codexPane(s.PaneTitle)
			if a.SessionID == "" {
				a.SessionID = paneSession(reported, s.TmuxPane)
			}
			if a.SessionID == "" && resumed != nil {
				a.SessionID = resumed(s.PID, s.PaneTitle)
			}
		}
		if len(s.Prompts) > 0 && agents.Title(s.Prompts[0]) != "" {
			a.Title = agents.Title(s.Prompts[0])
		}
		if s.Summary != nil && agents.Title(*s.Summary) != "" {
			a.Title = agents.Title(*s.Summary)
		}
		if s.Status == "idle" && s.AttentionState != nil && *s.AttentionState == "unread" {
			a.Status = string(agents.Completed)
		}
		if a.Status == string(agents.Idle) {
			if r, ok := reported[a.SessionID]; ok && r.At.After(changedAt(s.ChangedAt)) {
				a.Status = string(r.State)
			} else if paneStatus != "" {
				a.Status = paneStatus
			}
		}
		if s.GitBranch != nil {
			a.Branch = *s.GitBranch
		}
		if s.MainRepoRoot != nil {
			a.Repo = *s.MainRepoRoot
		}
		list = append(list, a)
	}
	return list, nil
}

// paneSession is the session that last reported from tmux pane, "" when none
// did. A pane can outlive its session, so the newest report wins.
func paneSession(reported map[string]live, pane string) string {
	var id string
	var at time.Time
	for sid, r := range reported {
		if pane != "" && r.Pane == pane && (id == "" || r.At.After(at)) {
			id, at = sid, r.At
		}
	}
	return id
}

// Print writes list as agents command lines,
// path, status, target, title, branch, repo, task GID, provider, session ID.
// ccmux's branch and repo link a session the way ccmux sees it, such as a nested repo
// linking to the project around it.
func Print(w io.Writer, list []agents.Agent) error {
	for _, a := range list {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", field(a.Path), field(a.Status), field(a.Target), field(a.Title), field(a.Branch), field(a.Repo), field(a.TaskGID), field(a.Provider), field(a.SessionID)); err != nil {
			return err
		}
	}
	return nil
}

// field keeps a value from splitting its line or column.
var field = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace

// changedAt parses ccmux's statusChangedAt, the zero time when unset.
func changedAt(s *string) time.Time {
	if s == nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, *s)
	return t
}

// codexPaneWaiting prefixes a Codex terminal title while Codex waits on you.
const codexPaneWaiting = "[ ! ] Action Required"

// codexPane splits a pane-tracked Codex session's terminal title into the
// status its prefix shows and the thread title. Codex prefixes the title with
// a braille spinner while working and with codexPaneWaiting while it waits;
// status is "" without a prefix. ccmux can leave these sessions "idle" while
// Codex works, so the prefix overrides only an "idle" status.
func codexPane(pane string) (status, rest string) {
	status, rest = codexPaneRaw(pane)
	return status, agents.Title(rest)
}

// codexPaneRaw is codexPane without shortening the title.
func codexPaneRaw(pane string) (status, rest string) {
	if rest, ok := strings.CutPrefix(pane, codexPaneWaiting); ok {
		return string(agents.Waiting), strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "|"))
	}
	if r, size := utf8.DecodeRuneInString(pane); r >= 0x2800 && r <= 0x28FF {
		return string(agents.Working), strings.TrimSpace(pane[size:])
	}
	return "", strings.TrimSpace(pane)
}

// resumedThread is the thread a Codex process with command line args was
// resumed with (`codex resume <id>`), "" when its pane title,
// "<thread name> | <dir>", no longer shows one of that thread's names in
// codexHome. Codex keeps its arguments after /new switches threads, and its
// app-server daemon runs hooks outside the pane, so the title is the only
// check left that the pane still shows the thread.
func resumedThread(args []string, paneTitle, codexHome string) string {
	id := resumeArg(args)
	if id == "" || codexHome == "" {
		return ""
	}
	_, title := codexPaneRaw(paneTitle)
	if i := strings.LastIndex(title, " | "); i >= 0 {
		title = title[:i]
	}
	if !showsName(title, threadNames(filepath.Join(codexHome, "session_index.jsonl"), id)) {
		return ""
	}
	return id
}

// threadID matches a Codex thread ID.
var threadID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// resumeArg is the thread ID in `codex resume [options] <id>` arguments, ""
// for any other command.
func resumeArg(args []string) string {
	if len(args) < 3 || filepath.Base(args[0]) != "codex" || args[1] != "resume" {
		return ""
	}
	for _, a := range args[2:] {
		if threadID.MatchString(a) {
			return a
		}
	}
	return ""
}

// threadNames lists every name thread id has had in Codex's session index,
// one JSON object per line, appended on each rename. A running Codex can show
// a name from before a rename.
func threadNames(path, id string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var names []string
	lines := bufio.NewScanner(f)
	lines.Buffer(nil, 1<<20)
	for lines.Scan() {
		var e struct {
			ID   string `json:"id"`
			Name string `json:"thread_name"`
		}
		if json.Unmarshal(lines.Bytes(), &e) == nil && e.ID == id && e.Name != "" {
			names = append(names, e.Name)
		}
	}
	return names
}

// showsName reports whether title is one of names, or the start of a longer
// one followed by "...", as Codex shortens long names.
func showsName(title string, names []string) bool {
	short, cut := strings.CutSuffix(title, "...")
	for _, n := range names {
		if n == title || cut && short != "" && len(n) > len(short) && strings.HasPrefix(n, short) {
			return true
		}
	}
	return false
}
