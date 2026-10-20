// Package ccmux is the agent adapter for ccmux: it lists ccmux's sessions as
// asanamate agent lines (docs/extensions.md, "Agent sources"), correcting
// their states with what Claude Code and Codex report themselves.
package ccmux

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
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
// other than these pass through as ccmux reports them.
func List(ctx context.Context, hookDir string) ([]agents.Agent, error) {
	if _, err := exec.LookPath(command[0]); err != nil {
		return nil, fmt.Errorf("%s not installed; see %s", command[0], homepage)
	}
	out, err := exec.CommandContext(ctx, command[0], command[1:]...).Output()
	if err != nil {
		return nil, fmt.Errorf("ccmux show --json failed: %w", err)
	}
	return parse(out, liveStates(hookDir))
}

func parse(data []byte, reported map[string]live) ([]agents.Agent, error) {
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
		NativeID       string   `json:"nativeSessionId"`
		ChangedAt      *string  `json:"statusChangedAt"`
		AgentType      string   `json:"agentType"`
		TrackingMode   string   `json:"trackingMode"`
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
			if r, ok := reported[s.NativeID]; ok && r.At.After(changedAt(s.ChangedAt)) {
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
	if rest, ok := strings.CutPrefix(pane, codexPaneWaiting); ok {
		return string(agents.Waiting), agents.Title(strings.TrimPrefix(strings.TrimSpace(rest), "|"))
	}
	if r, size := utf8.DecodeRuneInString(pane); r >= 0x2800 && r <= 0x28FF {
		return string(agents.Working), agents.Title(pane[size:])
	}
	return "", agents.Title(pane)
}
