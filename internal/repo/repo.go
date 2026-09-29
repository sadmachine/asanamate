// Package repo lists candidate repositories and validates git working trees.
package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExpandHome replaces a leading "~" with the user's home directory.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// Candidates runs command with /bin/sh and returns one path per output line.
func Candidates(ctx context.Context, command string) ([]string, error) {
	if strings.TrimSpace(command) == "" {
		return nil, nil
	}
	out, err := exec.CommandContext(ctx, "/bin/sh", "-c", command).Output()
	if err != nil {
		return nil, fmt.Errorf("repo_source command failed: %w", err)
	}
	seen := map[string]bool{}
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		p := ExpandHome(strings.TrimSpace(line))
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	return paths, nil
}

// Resolve returns the top-level directory of the git working tree containing path.
func Resolve(path string) (string, error) {
	p := ExpandHome(strings.TrimSpace(path))
	if p == "" {
		return "", errors.New("no path given")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	out, err := exec.Command("git", "-C", abs, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", abs)
	}
	return strings.TrimSpace(string(out)), nil
}

// Worktree returns the working tree of the repository at repoPath that has
// branch checked out, the main one included, or "" when none does.
func Worktree(repoPath, branch string) string {
	if branch == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return ""
	}
	var path string
	for _, line := range strings.Split(string(out), "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			path = p
		} else if line == "branch refs/heads/"+branch {
			return path
		}
	}
	return ""
}
