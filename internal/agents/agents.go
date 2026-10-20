// Package agents reads running coding agents from an agents command (an
// adapter such as asanamate-ccmux, or a script) and links them to tickets by
// git branch.
package agents

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/sync/errgroup"

	"github.com/sadmachine/asanamate/internal/jsoncmd"
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
	Branch string // branch to link by: the source's, else git's at Path; "" if none
	Repo   string // main repository to link by (worktrees resolve to their repo)
	State  State  // Status mapped to a State by Classify
	Title  string // optional short description from the tool
}

const maxTitle = 60

// Title turns text into a short single-line title.
func Title(s string) string {
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

// Fetch lists agents from command, then classifies their statuses with
// states (state name -> raw statuses).
func Fetch(ctx context.Context, command string, states map[string][]string) ([]Agent, error) {
	list, err := List(ctx, command)
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

// List runs command with /bin/sh and parses
// "<path>\t<status>[\t<target>[\t<title>[\t<branch>\t<repo>]]]" lines.
// Lines without a tab are skipped. A line that sets both branch and repo
// links by them; otherwise git reads them at path, so worktrees resolve to
// the repository they belong to, and a set branch is kept only when git finds
// none.
func List(ctx context.Context, command string) ([]Agent, error) {
	out, err := exec.CommandContext(ctx, "/bin/sh", "-c", command).Output()
	if err != nil {
		return nil, fmt.Errorf("agents command failed: %w", err)
	}
	return parseList(ctx, out), nil
}

// FetchArgs is the bounded, shell-free path for known read-only agent sources.
// Provider diagnostics are withheld because they can contain secrets.
func FetchArgs(ctx context.Context, args []string, states map[string][]string) ([]Agent, error) {
	out, err := jsoncmd.RunArgsBounded(ctx, "agents", args, jsoncmd.Limits{Stdout: 64 << 10, Stderr: 64 << 10})
	if err != nil {
		return nil, fmt.Errorf("local agent check failed; run asanamate extensions for verification guidance")
	}
	out = []byte(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, ansi.Strip(string(out))))
	list := parseList(ctx, out)
	Classify(list, states)
	return list, nil
}

func parseList(ctx context.Context, out []byte) []Agent {
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
			a.Title = Title(cols[3])
		}
		if len(cols) > 5 {
			a.Branch, a.Repo = strings.TrimSpace(cols[4]), strings.TrimSpace(cols[5])
		}
		agents = append(agents, a)
	}
	inParallel(len(agents), func(i int) {
		a := &agents[i]
		if a.Branch != "" && a.Repo != "" {
			return
		}
		fallback := a.Branch
		if a.Branch, a.Repo = gitInfo(ctx, a.Path); a.Branch == "" {
			a.Branch = fallback
		}
	})
	return agents
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
