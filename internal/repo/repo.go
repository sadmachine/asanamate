// Package repo lists candidate repositories and validates git working trees.
package repo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/jsoncmd"
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
	out, err := jsoncmd.RunShell(ctx, "repo_source command failed", src.Command, jsoncmd.DefaultLimits)
	if err != nil {
		return nil, err
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
// Children it cannot inspect are reported in the error, alongside the paths
// it found.
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
	var unreadable []string
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		_, err := os.Stat(filepath.Join(p, ".git"))
		switch {
		case err == nil:
			paths = append(paths, p)
		case errors.Is(err, fs.ErrPermission):
			unreadable = append(unreadable, e.Name())
		}
	}
	if len(unreadable) > 0 {
		return paths, fmt.Errorf("repo_source.root: cannot read %s in %s", strings.Join(unreadable, ", "), dir)
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

// Pending is the work in a working tree that is not on its remote yet.
type Pending struct {
	Uncommitted int       // changed or untracked files
	ChangedAt   time.Time // newest modification time among them
	Unpushed    int       // commits ahead of the upstream
	NoUpstream  bool      // the branch tracks no upstream, so nothing is pushed
	CommitAt    time.Time // time of the newest commit, when unpushed or untracked
}

// Empty reports whether nothing is waiting to be committed or pushed.
func (p Pending) Empty() bool { return p.Uncommitted == 0 && p.Unpushed == 0 && !p.NoUpstream }

// PendingWork reports the uncommitted files and unpushed commits of the
// working tree at path.
func PendingWork(ctx context.Context, path string) (Pending, error) {
	git := func(args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", path}, args...)...).Output()
		return string(out), err
	}
	var p Pending
	status, err := git("status", "--porcelain", "-z")
	if err != nil {
		return p, fmt.Errorf("git status in %s: %w", path, err)
	}
	fields := strings.Split(status, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		p.Uncommitted++
		if f[0] == 'R' || f[0] == 'C' {
			i++ // -z puts a rename's source in the next field
		}
		if info, err := os.Lstat(filepath.Join(path, f[3:])); err == nil && info.ModTime().After(p.ChangedAt) {
			p.ChangedAt = info.ModTime()
		}
	}
	if ahead, err := git("rev-list", "--count", "@{upstream}..HEAD"); err != nil {
		p.NoUpstream = true
	} else if p.Unpushed, err = strconv.Atoi(strings.TrimSpace(ahead)); err != nil {
		return p, fmt.Errorf("git rev-list in %s: %w", path, err)
	}
	if p.Unpushed > 0 || p.NoUpstream {
		when, err := git("log", "-1", "--format=%cI")
		if err != nil {
			// A branch with no commits has nothing to push.
			p.NoUpstream = false
		} else if p.CommitAt, err = time.Parse(time.RFC3339, strings.TrimSpace(when)); err != nil {
			return p, fmt.Errorf("git log in %s: %w", path, err)
		}
	}
	return p, nil
}

// Matching returns the paths with a git remote equal to one of urls, ignoring
// the scheme, user, port, and a trailing ".git", so ssh and https forms match.
// Clones come before worktrees, which share their remotes.
func Matching(ctx context.Context, paths, urls []string) []string {
	want := map[string]bool{}
	for _, u := range urls {
		if key := remoteKey(u); key != "" {
			want[key] = true
		}
	}
	if len(want) == 0 {
		return nil
	}
	var clones, worktrees []string
	for _, p := range paths {
		out, err := exec.CommandContext(ctx, "git", "-C", p, "remote", "-v").Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			if fields := strings.Fields(line); len(fields) >= 2 && want[remoteKey(fields[1])] {
				if info, err := os.Stat(filepath.Join(p, ".git")); err == nil && !info.IsDir() {
					worktrees = append(worktrees, p)
				} else {
					clones = append(clones, p)
				}
				break
			}
		}
	}
	return append(clones, worktrees...)
}

// remoteKey reduces a git remote URL to lowercase "host/path", or "" when it
// names no host and path. It accepts URLs with a scheme and scp-like
// "user@host:path" remotes.
func remoteKey(u string) string {
	u = strings.TrimSpace(u)
	if _, rest, ok := strings.Cut(u, "://"); ok {
		u = rest
	} else if host, path, ok := strings.Cut(u, ":"); ok && !strings.Contains(host, "/") {
		u = host + "/" + path
	}
	host, path, _ := strings.Cut(u, "/")
	if _, h, ok := strings.Cut(host, "@"); ok {
		host = h
	}
	host, _, _ = strings.Cut(host, ":")
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if host == "" || path == "" {
		return ""
	}
	return strings.ToLower(host + "/" + path)
}
