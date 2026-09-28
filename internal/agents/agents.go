// Package agents reads running coding agents from an external tool (ccmux,
// agent-deck, dmux, or a script) and links them to tickets by git branch.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// State is asanamate's normalized view of an agent's status.
type State string

// Agent states, most urgent first.
const (
	Waiting   State = "waiting"   // needs you: a question or a permission prompt
	Working   State = "working"   // busy
	Completed State = "completed" // finished, and you have not looked yet
	Idle      State = "idle"      // nothing happening
	Unknown   State = "unknown"   // a status the mapping does not know
)

// States lists every state in display order.
var States = []State{Waiting, Working, Completed, Idle, Unknown}

// Agent is one running agent as reported by the agents source.
type Agent struct {
	Path   string // working directory the agent runs in
	Status string // raw status from the tool, e.g. "running"
	Target string // optional id the tool uses to jump to the agent
	Branch string // git branch checked out at Path, "" if not a repo
	Repo   string // main repository of Path (worktrees resolve to their repo)
	State  State  // Status mapped to a State by Classify
}

// Fetch lists agents from the ccmux preset or from command, then classifies
// their statuses with states (state name -> raw statuses).
func Fetch(ctx context.Context, preset, command string, states map[string][]string) ([]Agent, error) {
	var list []Agent
	var err error
	if preset == "ccmux" {
		list, err = CCMux(ctx)
	} else {
		list, err = List(ctx, command)
	}
	if err != nil {
		return nil, err
	}
	Classify(list, states)
	return list, nil
}

// Classify sets each agent's State. A raw status equal to a state name maps
// to that state; extra adds more raw values per state (case-insensitive).
// Anything else is Unknown.
func Classify(list []Agent, extra map[string][]string) {
	for i := range list {
		list[i].State = classify(list[i].Status, extra)
	}
}

func classify(raw string, extra map[string][]string) State {
	for _, s := range States {
		if s != Unknown && strings.EqualFold(raw, string(s)) {
			return s
		}
		for _, v := range extra[string(s)] {
			if strings.EqualFold(raw, v) {
				return s
			}
		}
	}
	return Unknown
}

// ccmuxCommand is the command the ccmux preset runs.
var ccmuxCommand = []string{"ccmux", "show", "--json"}

// CCMux lists sessions from ccmux. A session that finished a turn you have
// not looked at yet (idle and unread) reports "completed".
func CCMux(ctx context.Context) ([]Agent, error) {
	out, err := exec.CommandContext(ctx, ccmuxCommand[0], ccmuxCommand[1:]...).Output()
	if err != nil {
		return nil, fmt.Errorf("ccmux show --json failed: %w", err)
	}
	return parseCCMux(ctx, out)
}

func parseCCMux(ctx context.Context, data []byte) ([]Agent, error) {
	var sessions []struct {
		ID             string  `json:"id"`
		Cwd            string  `json:"cwd"`
		Status         string  `json:"status"`
		AttentionState *string `json:"attentionState"`
		GitBranch      *string `json:"gitBranch"`
		MainRepoRoot   *string `json:"mainRepoRoot"`
	}
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, fmt.Errorf("parsing ccmux output: %w", err)
	}
	list := make([]Agent, 0, len(sessions))
	for _, s := range sessions {
		a := Agent{Path: s.Cwd, Status: s.Status, Target: s.ID}
		if s.Status == "idle" && s.AttentionState != nil && *s.AttentionState == "unread" {
			a.Status = string(Completed)
		}
		if s.GitBranch != nil && s.MainRepoRoot != nil {
			a.Branch, a.Repo = *s.GitBranch, *s.MainRepoRoot
		} else {
			a.Branch, a.Repo = gitInfo(ctx, s.Cwd)
			if a.Branch == "" && s.GitBranch != nil {
				a.Branch = *s.GitBranch
			}
		}
		list = append(list, a)
	}
	return list, nil
}

// List runs command with /bin/sh and parses "<path>\t<status>[\t<target>]"
// lines. Lines without a tab are skipped. Each path's branch and repository
// come from git, so worktrees resolve to the repository they belong to.
func List(ctx context.Context, command string) ([]Agent, error) {
	out, err := exec.CommandContext(ctx, "/bin/sh", "-c", command).Output()
	if err != nil {
		return nil, fmt.Errorf("agents command failed: %w", err)
	}
	var agents []Agent
	for _, line := range strings.Split(string(out), "\n") {
		cols := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(cols) < 2 || strings.TrimSpace(cols[0]) == "" {
			continue
		}
		a := Agent{Path: strings.TrimSpace(cols[0]), Status: strings.TrimSpace(cols[1])}
		if len(cols) > 2 {
			a.Target = strings.TrimSpace(cols[2])
		}
		a.Branch, a.Repo = gitInfo(ctx, a.Path)
		agents = append(agents, a)
	}
	return agents, nil
}

func gitInfo(ctx context.Context, path string) (branch, repo string) {
	out, err := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		return "", ""
	}
	common := filepath.Clean(lines[1])
	if filepath.Base(common) == ".git" {
		common = filepath.Dir(common)
	}
	return lines[0], common
}

// MatchAll returns the agents on branch whose repository is one of repos (any
// repository when repos is empty), most urgent state first.
func MatchAll(list []Agent, branch string, repos []string) []Agent {
	if branch == "" {
		return nil
	}
	var out []Agent
	for _, a := range list {
		if a.Branch == branch && (len(repos) == 0 || slices.Contains(repos, a.Repo)) {
			out = append(out, a)
		}
	}
	slices.SortStableFunc(out, func(a, b Agent) int { return rank(a.State) - rank(b.State) })
	return out
}

func rank(s State) int {
	if i := slices.Index(States, s); i >= 0 {
		return i
	}
	return len(States)
}
