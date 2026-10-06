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

	"github.com/sadmachine/asanamate/internal/config"
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

// CollapseHome replaces a leading home directory with "~", undoing ExpandHome.
func CollapseHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return filepath.Join("~", rest)
	}
	return p
}

// Candidates returns the repo picker's paths: one per output line of
// src.Command, run with /bin/sh, or when it is empty the repos directly
// inside src.Root.
func Candidates(ctx context.Context, src config.RepoSource) ([]string, error) {
	if strings.TrimSpace(src.Command) == "" {
		return List(src.Root)
	}
	out, err := exec.CommandContext(ctx, "/bin/sh", "-c", src.Command).Output()
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

// List returns the directories directly inside root that hold a .git entry,
// a directory for a clone or a file for a worktree. Empty root lists nothing.
func List(root string) ([]string, error) {
	if root == "" {
		return nil, nil
	}
	dir := ExpandHome(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("repo_source.root: %w", err)
	}
	var paths []string
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			paths = append(paths, p)
		}
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
	if errors.Is(err, exec.ErrNotFound) {
		return "", errors.New("git is not installed; repo actions need it")
	}
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
