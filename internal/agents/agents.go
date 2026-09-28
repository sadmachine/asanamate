// Package agents reads running coding agents from an external tool (ccmux,
// agent-deck, dmux, or a script) and links them to tickets by git branch.
package agents

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Agent is one running agent as reported by the agents command.
type Agent struct {
	Path   string // working directory the agent runs in
	Status string // free-form status from the tool, e.g. "running"
	Target string // optional id the tool uses to jump to the agent
	Branch string // git branch checked out at Path, "" if not a repo
	Repo   string // main repository of Path (worktrees resolve to their repo)
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

// Match returns the first agent on branch whose repository is one of repos,
// or on branch in any repository when repos is empty.
func Match(agents []Agent, branch string, repos []string) *Agent {
	if branch == "" {
		return nil
	}
	for i, a := range agents {
		if a.Branch != branch {
			continue
		}
		if len(repos) == 0 {
			return &agents[i]
		}
		for _, r := range repos {
			if a.Repo == r {
				return &agents[i]
			}
		}
	}
	return nil
}
