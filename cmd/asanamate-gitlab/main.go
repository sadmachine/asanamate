// Command asanamate-gitlab is asanamate's pull request provider for GitLab
// merge requests, on GitLab.com or self-managed GitLab, with the glab CLI.
// Add it to pull_requests.commands to show each ticket's merge requests
// (docs/extensions.md#pull-requests).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/sadmachine/asanamate/internal/prhost"
	"github.com/sadmachine/asanamate/internal/pullrequests"
)

const usage = `usage:
  asanamate-gitlab         answer one pull requests lookup read on stdin
                           (pull_requests.commands = ["asanamate-gitlab"])
  asanamate-gitlab check   report whether glab is installed and logged in

It serves repos with a remote on gitlab.com, or on a self-managed GitLab
host glab is logged in to (glab auth login --hostname <host>), and skips the
rest. asanamate calls merge requests pull requests.
`

var host = prhost.Host{
	Name:    "asanamate-gitlab",
	CLI:     "glab",
	Usage:   usage,
	Hosts:   []string{"gitlab.com"},
	Foreign: []string{"github.com", "bitbucket.org"},
	List:    list,
	Detail:  detail,
}

func main() { os.Exit(prhost.Main(host, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

// mr is a merge request as glab prints it, from GitLab's REST API.
type mr struct {
	IID                 int       `json:"iid"`
	Title               string    `json:"title"`
	WebURL              string    `json:"web_url"`
	State               string    `json:"state"`
	Draft               bool      `json:"draft"`
	SourceBranch        string    `json:"source_branch"`
	Description         string    `json:"description"`
	UpdatedAt           time.Time `json:"updated_at"`
	DetailedMergeStatus string    `json:"detailed_merge_status"`
	Author              struct {
		Username string `json:"username"`
	} `json:"author"`
	HeadPipeline *struct {
		Status string `json:"status"`
	} `json:"head_pipeline"`
}

// list lists the project's 100 most recent merge requests with one glab
// call. The list has no pipelines; detail adds them.
func list(ctx context.Context, hostname, path string) ([]prhost.Candidate, error) {
	out, err := prhost.Run(ctx, "glab", "mr", "list", "--repo", "https://"+hostname+"/"+path, "--all", "--per-page", "100", "--output", "json")
	if err != nil {
		return nil, err
	}
	var mrs []mr
	if err := json.Unmarshal(out, &mrs); err != nil {
		return nil, fmt.Errorf("reading glab mr list: %w", err)
	}
	candidates := make([]prhost.Candidate, len(mrs))
	for i, m := range mrs {
		candidates[i] = prhost.Candidate{Body: m.Description, PullRequest: pullrequests.PullRequest{
			URL: m.WebURL, Number: m.IID, Ref: "!" + strconv.Itoa(m.IID), Title: m.Title, State: state(m),
			Checks: pipeline(m), Review: reviews[m.DetailedMergeStatus], Author: m.Author.Username,
			UpdatedAt: m.UpdatedAt, Branch: m.SourceBranch,
		}}
	}
	return candidates, nil
}

// detail reads a matched open merge request's pipeline, with one glab call.
func detail(ctx context.Context, hostname, path string, c *prhost.Candidate) error {
	endpoint := "projects/" + url.PathEscape(path) + "/merge_requests/" + strconv.Itoa(c.Number)
	out, err := prhost.Run(ctx, "glab", "api", "--hostname", hostname, endpoint)
	if err != nil {
		return err
	}
	var m mr
	if err := json.Unmarshal(out, &m); err != nil {
		return fmt.Errorf("reading glab api: %w", err)
	}
	c.Checks, c.Review = pipeline(m), reviews[m.DetailedMergeStatus]
	return nil
}

func state(m mr) string {
	switch m.State {
	case "merged":
		return pullrequests.StateMerged
	case "closed", "locked":
		return pullrequests.StateClosed
	}
	if m.Draft {
		return pullrequests.StateDraft
	}
	return pullrequests.StateOpen
}

// reviews maps GitLab's merge status to reviews. GitLab reports no approval
// here, so a merge request is never "approved".
var reviews = map[string]string{
	"requested_changes": pullrequests.ReviewChanges,
	"not_approved":      pullrequests.ReviewPending,
}

// pipeline maps the head pipeline's status to checks; "" without one.
func pipeline(m mr) string {
	if m.HeadPipeline == nil {
		return ""
	}
	switch m.HeadPipeline.Status {
	case "success":
		return pullrequests.ChecksPassing
	case "failed":
		return pullrequests.ChecksFailing
	case "created", "waiting_for_resource", "preparing", "pending", "running", "scheduled":
		return pullrequests.ChecksPending
	}
	return ""
}
