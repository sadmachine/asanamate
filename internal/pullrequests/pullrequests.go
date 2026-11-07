// Package pullrequests runs pull request providers: commands that return
// each ticket's pull requests, which GitLab calls merge requests
// (docs/extensions.md).
package pullrequests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/sadmachine/asanamate/internal/display"
	"github.com/sadmachine/asanamate/internal/jsoncmd"
	"github.com/sadmachine/asanamate/internal/repo"
)

// ProtocolVersion is the pull requests protocol version every request
// carries. It changes only when existing providers would break; new optional
// fields keep it, so providers must ignore request fields they do not know.
const ProtocolVersion = 1

// OperationLookup is the only operation: return the tickets' pull requests.
const OperationLookup = "lookup"

// Request is sent as JSON on provider stdin.
type Request struct {
	Version   int      `json:"version"`
	Operation string   `json:"operation"`
	Tickets   []Ticket `json:"tickets"`
}

// Ticket is a ticket to look up: its Asana task, its branch, and the repos
// linked to it.
type Ticket struct {
	GID    string `json:"gid"`
	URL    string `json:"url"`
	Branch string `json:"branch"`
	Repos  []Repo `json:"repos"`
}

// Repo is a linked repo and its remotes, so a provider picks the repos on
// its host without running git.
type Repo struct {
	Path    string        `json:"path"`
	Remotes []repo.Remote `json:"remotes"`
}

// States, checks, and reviews a provider reports. An empty Checks or Review
// is none or unknown.
const (
	StateOpen   = "open"
	StateDraft  = "draft"
	StateMerged = "merged"
	StateClosed = "closed"

	ChecksPassing = "passing"
	ChecksFailing = "failing"
	ChecksPending = "pending"

	ReviewApproved = "approved"
	ReviewChanges  = "changes"
	ReviewPending  = "pending"
)

// PullRequest is one pull request of a ticket.
type PullRequest struct {
	TicketGID string    `json:"ticket_gid"`
	URL       string    `json:"url"`
	Number    int       `json:"number,omitempty"`
	Ref       string    `json:"ref,omitempty"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Checks    string    `json:"checks,omitempty"`
	Review    string    `json:"review,omitempty"`
	Author    string    `json:"author,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
	Repo      string    `json:"repo,omitempty"`
	Branch    string    `json:"branch,omitempty"`
}

// Active reports whether p is open or a draft, the pull requests whose
// checks and review still matter.
func (p PullRequest) Active() bool { return p.State == StateOpen || p.State == StateDraft }

// Label is p's display reference, such as "#42" or "!42"; "" when the
// provider gave neither a ref nor a number.
func (p PullRequest) Label() string {
	switch {
	case p.Ref != "":
		return p.Ref
	case p.Number > 0:
		return "#" + strconv.Itoa(p.Number)
	}
	return ""
}

// Reply is a provider's answer.
type Reply struct {
	PullRequests []PullRequest `json:"pull_requests"`
}

// Lookup runs command once for tickets and returns the valid pull requests
// it found, and its stderr, where a provider explains partial results.
func Lookup(ctx context.Context, command string, tickets []Ticket) ([]PullRequest, string, error) {
	req := Request{Version: ProtocolVersion, Operation: OperationLookup, Tickets: tickets}
	out, stderr, err := jsoncmd.RunBoundedStderr(ctx, "pull requests", command, req, jsoncmd.DefaultLimits)
	if err != nil {
		return nil, "", err
	}
	var reply struct {
		PullRequests *[]wireItem `json:"pull_requests"`
	}
	if err := json.Unmarshal(out, &reply); err != nil {
		return nil, stderr, fmt.Errorf("pull requests: %w", err)
	}
	if reply.PullRequests == nil {
		return nil, stderr, errors.New("pull requests: missing pull_requests")
	}
	gids := map[string]bool{}
	for _, t := range tickets {
		gids[t.GID] = true
	}
	var prs []PullRequest
	for _, item := range *reply.PullRequests {
		if p, ok := item.valid(gids); ok {
			prs = append(prs, p)
		}
	}
	return prs, stderr, nil
}

// wireItem is a reply item as sent, before validation.
type wireItem struct {
	PullRequest
	UpdatedAt string `json:"updated_at"`
}

// valid cleans item into a pull request of one of gids. It drops an item
// for another ticket, without an http(s) URL or a title, or with an unknown
// state, and blanks unknown checks, reviews, and times, so providers can add
// values later.
func (item wireItem) valid(gids map[string]bool) (PullRequest, bool) {
	p := item.PullRequest
	u, err := url.Parse(p.URL)
	if !gids[p.TicketGID] || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return p, false
	}
	switch p.State {
	case StateOpen, StateDraft, StateMerged, StateClosed:
	default:
		return p, false
	}
	switch p.Checks {
	case ChecksPassing, ChecksFailing, ChecksPending:
	default:
		p.Checks = ""
	}
	switch p.Review {
	case ReviewApproved, ReviewChanges, ReviewPending:
	default:
		p.Review = ""
	}
	if at, err := time.Parse(time.RFC3339, item.UpdatedAt); err == nil {
		p.UpdatedAt = at.UTC()
	}
	p.Title, p.Author, p.Ref = display.Line(p.Title), display.Line(p.Author), display.Line(p.Ref)
	p.Repo, p.Branch = display.Line(p.Repo), display.Line(p.Branch)
	return p, p.Title != ""
}

// Merge joins the replies of several commands, in command order, dropping a
// pull request an earlier one already gave for the same ticket.
func Merge(replies [][]PullRequest) []PullRequest {
	type key struct{ gid, url string }
	seen := map[key]bool{}
	var out []PullRequest
	for _, reply := range replies {
		for _, p := range reply {
			k := key{p.TicketGID, p.URL}
			if !seen[k] {
				seen[k] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// WithRemotes fills in the remotes of each ticket's repos, reading each
// repo once. A repo whose remotes git cannot read gets none.
func WithRemotes(ctx context.Context, tickets []Ticket) []Ticket {
	remotes := map[string][]repo.Remote{}
	out := make([]Ticket, len(tickets))
	for i, t := range tickets {
		out[i] = t
		out[i].Repos = make([]Repo, len(t.Repos))
		for j, r := range t.Repos {
			list, ok := remotes[r.Path]
			if !ok {
				list, _ = repo.Remotes(ctx, r.Path)
				if list == nil {
					list = []repo.Remote{}
				}
				remotes[r.Path] = list
			}
			out[i].Repos[j] = Repo{Path: r.Path, Remotes: list}
		}
	}
	return out
}
