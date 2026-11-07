package prhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/sadmachine/asanamate/internal/pullrequests"
	"github.com/sadmachine/asanamate/internal/repo"
)

func TestLinksTask(t *testing.T) {
	for body, want := range map[string]bool{
		"Fixes https://app.asana.com/0/42/1201":                              true,
		"see https://app.asana.com/0/42/1201/f for details":                  true,
		"(https://app.asana.com/1/9/project/42/task/1201?focus=true)":        true,
		"https://app.asana.com/0/42/12011":                                   false,
		"https://example.com/0/42/1201":                                      false,
		"1201":                                                               false,
		"[task](https://app.asana.com/0/0/1201#comment) and other text here": true,
	} {
		if got := LinksTask(body, "1201"); got != want {
			t.Errorf("LinksTask(%q) = %v, want %v", body, got, want)
		}
	}
}

// fakeCLI puts a CLI named name on PATH that logs its arguments and accepts
// "auth status --hostname" only for the hosts in ok.
func fakeCLI(t *testing.T, name string, ok ...string) (log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\ncase \"$*\" in\n"
	for _, h := range ok {
		script += "*'--hostname " + h + "') exit 0 ;;\n"
	}
	script += "esac\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/bin:/usr/bin")
	return log
}

func calls(log string) []string {
	data, _ := os.ReadFile(log)
	return strings.Fields(strings.ReplaceAll(string(data), " ", "_"))
}

func ticket(gid, branch string, repos ...pullrequests.Repo) pullrequests.Ticket {
	return pullrequests.Ticket{GID: gid, URL: "https://app.asana.com/0/0/" + gid, Branch: branch, Repos: repos}
}

func at(path string, urls ...string) pullrequests.Repo {
	r := pullrequests.Repo{Path: path}
	for i, u := range urls {
		r.Remotes = append(r.Remotes, repo.Remote{Name: []string{"origin", "upstream"}[i], URL: u})
	}
	return r
}

func lookup(t *testing.T, h Host, tickets ...pullrequests.Ticket) (pullrequests.Reply, string, int) {
	t.Helper()
	in, _ := json.Marshal(pullrequests.Request{Version: 1, Operation: "lookup", Tickets: tickets})
	var out, stderr bytes.Buffer
	code := Main(h, nil, bytes.NewReader(in), &out, &stderr)
	var reply pullrequests.Reply
	if code == 0 {
		if err := json.Unmarshal(out.Bytes(), &reply); err != nil || reply.PullRequests == nil {
			t.Fatalf("reply %q: %v", out.String(), err)
		}
	}
	return reply, stderr.String(), code
}

// testHost lists the pull requests in prs by "host/repo", recording each call.
func testHost(prs map[string][]Candidate) (Host, *[]string) {
	var mu sync.Mutex
	var listed []string
	return Host{
		Name: "asanamate-test", CLI: "tcli", Usage: "usage\n", Hosts: []string{"code.example.com"}, Foreign: []string{"other.example.com"},
		List: func(_ context.Context, host, path string) ([]Candidate, error) {
			mu.Lock()
			defer mu.Unlock()
			listed = append(listed, host+"/"+path)
			if host == "broken.example.com" {
				return nil, errors.New("HTTP 401: login expired")
			}
			return prs[host+"/"+path], nil
		},
	}, &listed
}

func candidate(url, branch, state, body string) Candidate {
	return Candidate{PullRequest: pullrequests.PullRequest{URL: url, Title: url, State: state, Branch: branch}, Body: body}
}

// Each ticket gets its branch matches first, then link matches, once each,
// across its repos; each repo is listed once however many tickets share it.
func TestMatchOrdersBranchThenLink(t *testing.T) {
	fakeCLI(t, "tcli")
	h, listed := testHost(map[string][]Candidate{
		"code.example.com/acme/web": {
			candidate("u/link", "other", "open", "Fixes https://app.asana.com/0/0/1"),
			candidate("u/both", "feat/a", "merged", "https://app.asana.com/0/0/1"),
			candidate("u/b", "feat/b", "open", ""),
		},
		"code.example.com/acme/api": {candidate("u/api", "feat/a", "draft", "")},
	})
	web := at("/code/web", "git@code.example.com:acme/web.git")
	api := at("/code/api", "https://code.example.com/acme/api")
	reply, _, code := lookup(t, h, ticket("1", "feat/a", web, api), ticket("2", "feat/b", web))
	var got []string
	for _, p := range reply.PullRequests {
		got = append(got, p.TicketGID+" "+p.URL+" "+p.Repo)
	}
	want := []string{"1 u/both /code/web", "1 u/api /code/api", "1 u/link /code/web", "2 u/b /code/web"}
	if code != 0 || !slices.Equal(got, want) {
		t.Fatalf("code %d, got %q, want %q", code, got, want)
	}
	slices.Sort(*listed)
	if !slices.Equal(*listed, []string{"code.example.com/acme/api", "code.example.com/acme/web"}) {
		t.Fatalf("listed = %q", *listed)
	}
}

// The first remote on a served host picks the repo's host. Other hosts are
// asked about once each, foreign ones never, and repos nobody here serves
// are skipped silently.
func TestHostSelection(t *testing.T) {
	log := fakeCLI(t, "tcli", "ghe.example.com")
	h, listed := testHost(nil)
	reply, stderr, code := lookup(t, h,
		ticket("1", "x",
			at("/a", "git@other.example.com:acme/a.git", "https://code.example.com/acme/a"),
			at("/b", "https://ghe.example.com/acme/b"),
			at("/c", "https://gitea.example.com/acme/c"),
			at("/d")),
		ticket("2", "y", at("/e", "https://gitea.example.com/acme/e")))
	if code != 0 || len(reply.PullRequests) != 0 || stderr != "" {
		t.Fatalf("code %d, reply %+v, stderr %q", code, reply, stderr)
	}
	slices.Sort(*listed)
	if !slices.Equal(*listed, []string{"code.example.com/acme/a", "ghe.example.com/acme/b"}) {
		t.Fatalf("listed = %q", *listed)
	}
	if got := calls(log); !slices.Equal(got, []string{"auth_status_--hostname_ghe.example.com", "auth_status_--hostname_gitea.example.com"}) {
		t.Fatalf("CLI calls = %q", got)
	}
}

func TestNoServedRepoNeedsNoCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no CLI at all
	h, listed := testHost(nil)
	reply, _, code := lookup(t, h, ticket("1", "x", at("/a", "https://other.example.com/a")), ticket("2", "y", at("/b")))
	if code != 0 || len(reply.PullRequests) != 0 || len(*listed) != 0 {
		t.Fatalf("code %d, reply %+v, listed %q", code, reply, *listed)
	}
}

func TestFailureRules(t *testing.T) {
	fakeCLI(t, "tcli", "broken.example.com")
	h, _ := testHost(map[string][]Candidate{"code.example.com/acme/web": {candidate("u/1", "x", "open", "")}})
	web, broken := at("/web", "https://code.example.com/acme/web"), at("/old", "https://broken.example.com/acme/old")

	reply, stderr, code := lookup(t, h, ticket("1", "x", web, broken))
	if code != 0 || len(reply.PullRequests) != 1 || !strings.Contains(stderr, "broken.example.com/acme/old: HTTP 401: login expired") {
		t.Fatalf("partial failure: code %d, reply %+v, stderr %q", code, reply, stderr)
	}
	if _, stderr, code := lookup(t, h, ticket("1", "x", broken)); code == 0 || !strings.Contains(stderr, "every host failed: broken.example.com") {
		t.Fatalf("every host failed: code %d, stderr %q", code, stderr)
	}
	t.Setenv("PATH", t.TempDir())
	if _, stderr, code := lookup(t, h, ticket("1", "x", web)); code == 0 || !strings.Contains(stderr, "tcli is not installed") {
		t.Fatalf("missing CLI: code %d, stderr %q", code, stderr)
	}
	var out, errb bytes.Buffer
	if code := Main(h, nil, strings.NewReader(`{"version":1,"operation":"merge"}`), &out, &errb); code != 1 || !strings.Contains(errb.String(), "unknown operation") {
		t.Fatalf("unknown operation: code %d, stderr %q", code, errb.String())
	}
	if code := Main(h, []string{"help"}, nil, &out, &errb); code != 0 || !strings.Contains(out.String(), "usage") {
		t.Fatalf("help: code %d", code)
	}
	if code := Main(h, []string{"nope"}, nil, &out, &errb); code != 2 {
		t.Fatalf("bad args: code %d", code)
	}
}

// Detail runs once per matched open pull request, never for merged ones or
// ones no ticket matched.
func TestDetailOnlyForMatchedOpenPullRequests(t *testing.T) {
	fakeCLI(t, "tcli")
	h, _ := testHost(map[string][]Candidate{"code.example.com/acme/web": {
		candidate("u/open", "x", "open", ""), candidate("u/merged", "x", "merged", ""), candidate("u/other", "z", "open", ""),
	}})
	var detailed []string
	h.Detail = func(_ context.Context, host, path string, c *Candidate) error {
		detailed = append(detailed, c.URL)
		c.Checks = pullrequests.ChecksFailing
		return nil
	}
	web := at("/web", "https://code.example.com/acme/web")
	reply, _, code := lookup(t, h, ticket("1", "x", web), ticket("2", "y", pullrequests.Repo{Path: "/web", Remotes: web.Remotes}))
	if code != 0 || !slices.Equal(detailed, []string{"u/open"}) || reply.PullRequests[0].Checks != pullrequests.ChecksFailing {
		t.Fatalf("code %d, detailed %q, reply %+v", code, detailed, reply)
	}
}
