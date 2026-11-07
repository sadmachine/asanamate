// Command asanamate-github is asanamate's pull request provider for GitHub
// and GitHub Enterprise, with the gh CLI. Add it to pull_requests.commands
// to show each ticket's pull requests (docs/extensions.md#pull-requests).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/sadmachine/asanamate/internal/prhost"
	"github.com/sadmachine/asanamate/internal/pullrequests"
)

const usage = `usage:
  asanamate-github         answer one pull requests lookup read on stdin
                           (pull_requests.commands = ["asanamate-github"])
  asanamate-github check   report whether gh is installed and logged in

It serves repos with a remote on github.com, or on a GitHub Enterprise host
gh is logged in to (gh auth login --hostname <host>), and skips the rest.
`

var host = prhost.Host{
	Name:    "asanamate-github",
	CLI:     "gh",
	Usage:   usage,
	Hosts:   []string{"github.com"},
	Foreign: []string{"gitlab.com", "bitbucket.org"},
	List:    list,
}

func main() { os.Exit(prhost.Main(host, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

// pr is a pull request as gh pr list --json prints it.
type pr struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	State       string    `json:"state"`
	IsDraft     bool      `json:"isDraft"`
	HeadRefName string    `json:"headRefName"`
	Body        string    `json:"body"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
	ReviewDecision    string  `json:"reviewDecision"`
	StatusCheckRollup []check `json:"statusCheckRollup"`
}

// check is a check run (status, conclusion) or a commit status (state).
type check struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}

// list lists the repo's 100 most recent pull requests with one gh call.
func list(ctx context.Context, hostname, path string) ([]prhost.Candidate, error) {
	out, err := prhost.Run(ctx, "gh", "pr", "list", "--repo", hostname+"/"+path, "--state", "all", "--limit", "100",
		"--json", "number,title,url,state,isDraft,headRefName,body,updatedAt,author,reviewDecision,statusCheckRollup")
	if err != nil {
		return nil, err
	}
	var prs []pr
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("reading gh pr list: %w", err)
	}
	candidates := make([]prhost.Candidate, len(prs))
	for i, p := range prs {
		candidates[i] = prhost.Candidate{Body: p.Body, PullRequest: pullrequests.PullRequest{
			URL: p.URL, Number: p.Number, Title: p.Title, State: state(p), Checks: checks(p.StatusCheckRollup),
			Review: reviews[p.ReviewDecision], Author: p.Author.Login, UpdatedAt: p.UpdatedAt, Branch: p.HeadRefName,
		}}
	}
	return candidates, nil
}

func state(p pr) string {
	switch {
	case p.State == "MERGED":
		return pullrequests.StateMerged
	case p.State == "CLOSED":
		return pullrequests.StateClosed
	case p.IsDraft:
		return pullrequests.StateDraft
	}
	return pullrequests.StateOpen
}

var reviews = map[string]string{
	"APPROVED":          pullrequests.ReviewApproved,
	"CHANGES_REQUESTED": pullrequests.ReviewChanges,
	"REVIEW_REQUIRED":   pullrequests.ReviewPending,
}

// checks rolls a pull request's checks up: failing if any failed, else
// pending if any is still running, else passing; "" with no checks.
func checks(rollup []check) string {
	if len(rollup) == 0 {
		return ""
	}
	pending := false
	for _, c := range rollup {
		switch {
		case c.Conclusion == "FAILURE" || c.Conclusion == "TIMED_OUT" || c.Conclusion == "CANCELLED" ||
			c.Conclusion == "ACTION_REQUIRED" || c.Conclusion == "STARTUP_FAILURE" || c.State == "FAILURE" || c.State == "ERROR":
			return pullrequests.ChecksFailing
		case c.Status != "" && c.Status != "COMPLETED", c.State == "PENDING" || c.State == "EXPECTED":
			pending = true
		}
	}
	if pending {
		return pullrequests.ChecksPending
	}
	return pullrequests.ChecksPassing
}
