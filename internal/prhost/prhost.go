// Package prhost is what the bundled pull request adapters, asanamate-github
// and asanamate-gitlab, share: the command layout, picking each repo's host
// from its remotes, running the host's CLI, and matching its pull requests
// to tickets (docs/extensions.md#pull-requests). Each adapter supplies only
// its CLI calls and their mapping, in a Host.
package prhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sadmachine/asanamate/internal/display"
	"github.com/sadmachine/asanamate/internal/jsoncmd"
	"github.com/sadmachine/asanamate/internal/pullrequests"
	"github.com/sadmachine/asanamate/internal/repo"
)

// Timeouts keep a run inside asanamate's one-minute budget: each CLI call
// gets CallTimeout, and the whole run budget.
const (
	CallTimeout = 20 * time.Second
	budget      = 50 * time.Second
)

// parallel bounds the repos listed at once.
const parallel = 4

// Host is one adapter: its git host's CLI and how to read pull requests
// with it.
type Host struct {
	// Name is the adapter's command, such as "asanamate-github".
	Name string
	// CLI is the host's command line tool, such as "gh".
	CLI string
	// Usage is the adapter's help text.
	Usage string
	// Hosts are the hosts served without asking the CLI, such as
	// "github.com". The CLI's "auth status --hostname <host>" decides any
	// other host, once per run, except Foreign ones.
	Hosts []string
	// Foreign are hosts never served, because another provider does, such
	// as "gitlab.com" for GitHub.
	Foreign []string
	// List returns the recent pull requests of the repo at path on host,
	// with one CLI call.
	List func(ctx context.Context, host, path string) ([]Candidate, error)
	// Detail, when set, fills in what List lacks for an open or draft pull
	// request that matched a ticket, with at most one CLI call.
	Detail func(ctx context.Context, host, path string, c *Candidate) error
}

// Candidate is a pull request as the host lists it, before matching: its
// fields but TicketGID and Repo, and its description, which can link tickets.
type Candidate struct {
	pullrequests.PullRequest
	Body string
}

// Main runs the adapter with args and returns its exit status: lookup with
// no arguments, check, or help.
func Main(h Host, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0:
		reply, err := h.lookup(stdin, stderr)
		if err != nil {
			fmt.Fprintln(stderr, h.Name+":", err)
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(reply); err != nil {
			fmt.Fprintln(stderr, h.Name+":", err)
			return 1
		}
	case len(args) == 1 && args[0] == "check":
		ctx, cancel := context.WithTimeout(context.Background(), CallTimeout)
		defer cancel()
		if _, err := Run(ctx, h.CLI, "auth", "status"); err != nil {
			fmt.Fprintln(stderr, h.Name+":", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s is installed and logged in\n", h.CLI)
	case len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help"):
		fmt.Fprint(stdout, h.Usage)
	default:
		fmt.Fprint(stderr, h.Usage)
		return 2
	}
	return 0
}

// Run runs a CLI with an argument array, never a shell, and returns its
// stdout. Errors carry its stderr, made safe to print.
func Run(ctx context.Context, cli string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	return jsoncmd.RunArgsBounded(ctx, cli, append([]string{cli}, args...), jsoncmd.DefaultLimits)
}

// served is a request repo on this adapter's host: where it lives there.
type served struct {
	path       string // the local repo
	host, repo string // such as "github.com" and "acme/web"
}

// lookup answers one request on in, explaining failed hosts on stderr.
func (h Host) lookup(in io.Reader, stderr io.Writer) (pullrequests.Reply, error) {
	var req pullrequests.Request
	if err := json.NewDecoder(in).Decode(&req); err != nil {
		return pullrequests.Reply{}, fmt.Errorf("reading the request: %w", err)
	}
	if req.Operation != pullrequests.OperationLookup {
		return pullrequests.Reply{}, errors.New("unknown operation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	repos, err := h.pick(ctx, req.Tickets)
	if err != nil {
		return pullrequests.Reply{}, err
	}
	found, failed := h.listAll(ctx, repos, stderr)
	if len(repos) > 0 && len(failed) == hostCount(repos) {
		return pullrequests.Reply{}, fmt.Errorf("every host failed: %s", strings.Join(failed, ", "))
	}
	prs := match(req.Tickets, repos, found)
	if h.Detail != nil {
		h.detail(ctx, repos, prs, stderr)
	}
	return pullrequests.Reply{PullRequests: prs}, nil
}

// pick finds the request's repos on this adapter's hosts, each once: the
// first of its remotes on a served host wins. It asks the CLI about each
// unknown host once, and fails when the CLI is needed but missing.
func (h Host) pick(ctx context.Context, tickets []pullrequests.Ticket) ([]served, error) {
	asked := map[string]bool{}
	var repos []served
	seen := map[string]bool{}
	for _, t := range tickets {
		for _, r := range t.Repos {
			if seen[r.Path] {
				continue
			}
			seen[r.Path] = true
			for _, remote := range r.Remotes {
				host, path := repo.HostPath(remote.URL)
				if host == "" || slices.Contains(h.Foreign, host) {
					continue
				}
				if _, err := exec.LookPath(h.CLI); err != nil {
					return nil, fmt.Errorf("%s is not installed: %w", h.CLI, err)
				}
				ok, known := asked[host]
				if !known {
					ok = slices.Contains(h.Hosts, host)
					if !ok {
						_, err := Run(ctx, h.CLI, "auth", "status", "--hostname", host)
						ok = err == nil
					}
					asked[host] = ok
				}
				if ok {
					repos = append(repos, served{path: r.Path, host: host, repo: path})
					break
				}
			}
		}
	}
	return repos, nil
}

// listAll lists each repo's pull requests, a few at a time, by local path.
// It explains each failure on stderr and returns the hosts where every
// call failed.
func (h Host) listAll(ctx context.Context, repos []served, stderr io.Writer) (map[string][]Candidate, []string) {
	results := make([][]Candidate, len(repos))
	errs := make([]error, len(repos))
	var wg sync.WaitGroup
	slots := make(chan struct{}, parallel)
	for i, r := range repos {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			results[i], errs[i] = h.List(ctx, r.host, r.repo)
		})
	}
	wg.Wait()
	found := map[string][]Candidate{}
	ok := map[string]bool{}
	var hosts []string
	for i, r := range repos {
		if !slices.Contains(hosts, r.host) {
			hosts = append(hosts, r.host)
		}
		if errs[i] != nil {
			fmt.Fprintf(stderr, "%s: %s/%s: %s\n", h.Name, r.host, r.repo, display.Line(errs[i].Error()))
			continue
		}
		ok[r.host] = true
		found[r.path] = results[i]
	}
	var failed []string
	for _, host := range hosts {
		if !ok[host] {
			failed = append(failed, host)
		}
	}
	return found, failed
}

func hostCount(repos []served) int {
	var hosts []string
	for _, r := range repos {
		if !slices.Contains(hosts, r.host) {
			hosts = append(hosts, r.host)
		}
	}
	return len(hosts)
}

// match gives each ticket the pull requests in its served repos whose head
// branch is its branch, then those whose description links its task,
// without repeating a URL.
func match(tickets []pullrequests.Ticket, repos []served, found map[string][]Candidate) []pullrequests.PullRequest {
	prs := []pullrequests.PullRequest{}
	for _, t := range tickets {
		var mine []served
		for _, r := range repos {
			if slices.ContainsFunc(t.Repos, func(tr pullrequests.Repo) bool { return tr.Path == r.path }) {
				mine = append(mine, r)
			}
		}
		var urls []string
		add := func(r served, c Candidate) {
			if slices.Contains(urls, c.URL) {
				return
			}
			urls = append(urls, c.URL)
			p := c.PullRequest
			p.TicketGID, p.Repo = t.GID, r.path
			prs = append(prs, p)
		}
		for _, r := range mine {
			for _, c := range found[r.path] {
				if t.Branch != "" && c.Branch == t.Branch {
					add(r, c)
				}
			}
		}
		for _, r := range mine {
			for _, c := range found[r.path] {
				if LinksTask(c.Body, t.GID) {
					add(r, c)
				}
			}
		}
	}
	return prs
}

// detail runs Host.Detail once per matched open or draft pull request.
func (h Host) detail(ctx context.Context, repos []served, prs []pullrequests.PullRequest, stderr io.Writer) {
	done := map[string]pullrequests.PullRequest{}
	for i, p := range prs {
		if !p.Active() {
			continue
		}
		if d, ok := done[p.URL]; ok {
			prs[i].Checks, prs[i].Review = d.Checks, d.Review
			continue
		}
		r := repos[slices.IndexFunc(repos, func(r served) bool { return r.path == p.Repo })]
		c := Candidate{PullRequest: p}
		if err := h.Detail(ctx, r.host, r.repo, &c); err != nil {
			fmt.Fprintf(stderr, "%s: %s: %s\n", h.Name, p.URL, display.Line(err.Error()))
		}
		prs[i].Checks, prs[i].Review = c.Checks, c.Review
		done[p.URL] = prs[i]
	}
}

// asanaURL finds Asana links in a description.
var asanaURL = regexp.MustCompile(`https?://app\.asana\.com/[^\s)\]>"'<]*`)

// LinksTask reports whether body links the Asana task gid: an app.asana.com
// URL with gid as a whole path segment, as in /0/<project>/<task>,
// /0/<project>/<task>/f, or /1/<workspace>/project/<p>/task/<task>.
func LinksTask(body, gid string) bool {
	if gid == "" {
		return false
	}
	for _, u := range asanaURL.FindAllString(body, -1) {
		path, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://"), "?")
		path, _, _ = strings.Cut(path, "#")
		if slices.Contains(strings.Split(path, "/")[1:], gid) {
			return true
		}
	}
	return false
}
