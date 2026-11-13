package ccmux

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
)

// processArgs is process pid's command line split on spaces, nil when unknown.
// An argument containing a space splits too, so callers match whole tokens.
func processArgs(ctx context.Context, pid int) []string {
	if pid <= 0 {
		return nil
	}
	out, err := exec.CommandContext(ctx, "ps", "-o", "args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}

// descends reports whether process pid is root or runs under it.
func descends(ctx context.Context, pid, root int) bool {
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		return false
	}
	parent := map[int]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		p, err1 := strconv.Atoi(f[0])
		pp, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			parent[p] = pp
		}
	}
	for seen := map[int]bool{}; pid > 0 && !seen[pid]; pid = parent[pid] {
		if pid == root {
			return true
		}
		seen[pid] = true
	}
	return false
}
