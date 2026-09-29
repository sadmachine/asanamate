// Package agents reads running coding agents from an external tool (ccmux,
// agent-deck, dmux, or a script) and links them to tickets by git branch.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/sync/errgroup"
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
	Title  string // short description: the tool's title, else its first prompt
}

const maxTitle = 60

// title turns text into a short single-line title.
func title(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxTitle {
		s = string(r[:maxTitle-1]) + "…"
	}
	return s
}

// presets are the built-in agent sources, by agents.preset name.
var presets = map[string]func(context.Context) ([]Agent, error){
	"ccmux": CCMux,
}

// Presets returns the built-in agent source names, sorted.
func Presets() []string { return slices.Sorted(maps.Keys(presets)) }

// Fetch lists agents from preset when set, else from command, then classifies
// their statuses with states (state name -> raw statuses).
func Fetch(ctx context.Context, preset, command string, states map[string][]string) ([]Agent, error) {
	var list []Agent
	var err error
	if source, ok := presets[preset]; ok {
		list, err = source(ctx)
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
		ID             string   `json:"id"`
		Cwd            string   `json:"cwd"`
		Status         string   `json:"status"`
		AttentionState *string  `json:"attentionState"`
		GitBranch      *string  `json:"gitBranch"`
		MainRepoRoot   *string  `json:"mainRepoRoot"`
		Summary        *string  `json:"summary"`
		Prompts        []string `json:"prompts"`
		PaneTitle      string   `json:"paneTitle"`
	}
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, fmt.Errorf("parsing ccmux output: %w", err)
	}
	list := make([]Agent, 0, len(sessions))
	for _, s := range sessions {
		a := Agent{Path: s.Cwd, Status: s.Status, Target: s.ID, Title: title(s.PaneTitle)}
		if len(s.Prompts) > 0 && title(s.Prompts[0]) != "" {
			a.Title = title(s.Prompts[0])
		}
		if s.Summary != nil && title(*s.Summary) != "" {
			a.Title = title(*s.Summary)
		}
		if s.Status == "idle" && s.AttentionState != nil && *s.AttentionState == "unread" {
			a.Status = string(Completed)
		}
		if s.GitBranch != nil && s.MainRepoRoot != nil {
			a.Branch, a.Repo = *s.GitBranch, *s.MainRepoRoot
		}
		list = append(list, a)
	}
	inParallel(len(list), func(i int) {
		s, a := sessions[i], &list[i]
		if s.GitBranch != nil && s.MainRepoRoot != nil {
			return
		}
		a.Branch, a.Repo = gitInfo(ctx, s.Cwd)
		if a.Branch == "" && s.GitBranch != nil {
			a.Branch = *s.GitBranch
		}
	})
	return list, nil
}

// List runs command with /bin/sh and parses
// "<path>\t<status>[\t<target>[\t<title>]]" lines. Lines without a tab are skipped. Each path's branch and repository
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
		if len(cols) > 3 {
			a.Title = title(cols[3])
		}
		agents = append(agents, a)
	}
	inParallel(len(agents), func(i int) { agents[i].Branch, agents[i].Repo = gitInfo(ctx, agents[i].Path) })
	return agents, nil
}

// maxGit caps the git processes run at once while resolving agents.
const maxGit = 8

// inParallel calls fn for 0..n-1, at most maxGit at a time, and waits.
func inParallel(n int, fn func(i int)) {
	var g errgroup.Group
	g.SetLimit(maxGit)
	for i := range n {
		g.Go(func() error { fn(i); return nil })
	}
	_ = g.Wait()
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

// Unlinked returns the agents in repos that are not on branch: the ones a
// ticket without linked agents most likely meant to match. It returns nil
// when repos is empty, since any repo would qualify.
func Unlinked(list []Agent, branch string, repos []string) []Agent {
	var out []Agent
	for _, a := range list {
		if a.Branch != branch && slices.Contains(repos, a.Repo) {
			out = append(out, a)
		}
	}
	return out
}

// Hint explains why a is not linked to a ticket on branch.
func Hint(a Agent, branch string) string {
	on := "on " + a.Branch
	switch a.Branch {
	case "HEAD":
		on = "on a detached HEAD"
	case "":
		on = "not on a git branch"
	}
	return fmt.Sprintf("%s is %s, not %s", filepath.Base(a.Path), on, branch)
}

func rank(s State) int {
	if i := slices.Index(States, s); i >= 0 {
		return i
	}
	return len(States)
}
