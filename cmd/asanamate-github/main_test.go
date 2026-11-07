package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sadmachine/asanamate/internal/prhost"
	"github.com/sadmachine/asanamate/internal/pullrequests"
)

// fakeGH puts testdata/gh on PATH and returns the file it logs calls to.
func fakeGH(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("PATH", dir+":/bin:/usr/bin")
	t.Setenv("FAKE_LOG", log)
	return log
}

func calls(log string) []string {
	data, _ := os.ReadFile(log)
	return slices.DeleteFunc(strings.Split(string(data), "\n"), func(s string) bool { return s == "" })
}

func lookup(t *testing.T, request string) (pullrequests.Reply, string, int) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := prhost.Main(host, nil, strings.NewReader(request), &out, &stderr)
	var reply pullrequests.Reply
	if code == 0 {
		if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
			t.Fatalf("reply %q: %v", out.String(), err)
		}
	}
	return reply, stderr.String(), code
}

func request(repos string) string {
	return `{"version":1,"operation":"lookup","tickets":[
	  {"gid":"1201","url":"https://app.asana.com/0/0/1201","branch":"feature/login","repos":[` + repos + `]},
	  {"gid":"1300","url":"https://app.asana.com/0/0/1300","branch":"feature/other","repos":[` + repos + `]}]}`
}

const (
	web     = `{"path":"/code/web","remotes":[{"name":"origin","url":"git@github.com:acme/web.git"}]}`
	gitlab  = `{"path":"/code/api","remotes":[{"name":"origin","url":"https://gitlab.com/acme/api.git"}]}`
	expired = `{"path":"/code/old","remotes":[{"name":"origin","url":"https://expired.example.com/acme/old"}]}`
)

func TestLookupMapsGhOutput(t *testing.T) {
	log := fakeGH(t)
	reply, stderr, code := lookup(t, request(web+","+gitlab))
	if code != 0 || stderr != "" {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	want := []pullrequests.PullRequest{
		{TicketGID: "1201", URL: "https://github.com/acme/web/pull/42", Number: 42, Title: "Fix login redirect", State: "open",
			Checks: "failing", Review: "changes", Author: "zed", UpdatedAt: time.Date(2026, 11, 5, 14, 5, 0, 0, time.UTC), Repo: "/code/web", Branch: "feature/login"},
		{TicketGID: "1201", URL: "https://github.com/acme/web/pull/40", Number: 40, Title: "Old login fix", State: "merged",
			Checks: "passing", Review: "approved", Author: "zed", UpdatedAt: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC), Repo: "/code/web", Branch: "feature/login"},
		{TicketGID: "1201", URL: "https://github.com/acme/web/pull/43", Number: 43, Title: "Draft redirect tests", State: "draft",
			Checks: "pending", Review: "pending", Author: "amy", UpdatedAt: time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC), Repo: "/code/web", Branch: "tests"},
		{TicketGID: "1300", URL: "https://github.com/acme/web/pull/41", Number: 41, Title: "Abandoned", State: "closed",
			Author: "zed", UpdatedAt: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC), Repo: "/code/web", Branch: "feature/other"},
	}
	if !slices.Equal(reply.PullRequests, want) {
		t.Fatalf("reply:\n%+v\nwant:\n%+v", reply.PullRequests, want)
	}
	// One list call for the GitHub repo, none for the GitLab one.
	got := calls(log)
	if len(got) != 1 || !strings.HasPrefix(got[0], "pr list --repo github.com/acme/web --state all --limit 100 --json ") {
		t.Fatalf("gh calls = %q", got)
	}
}

func TestNoGitHubRepoCallsNothing(t *testing.T) {
	log := fakeGH(t)
	reply, _, code := lookup(t, request(gitlab))
	if code != 0 || len(reply.PullRequests) != 0 || len(calls(log)) != 0 {
		t.Fatalf("code %d, reply %+v, calls %q", code, reply, calls(log))
	}
}

func TestPartialHostFailureAndMissingCLI(t *testing.T) {
	fakeGH(t)
	reply, stderr, code := lookup(t, request(web+","+expired))
	if code != 0 || len(reply.PullRequests) != 4 || !strings.Contains(stderr, "expired.example.com/acme/old: ") || !strings.Contains(stderr, "Bad credentials") {
		t.Fatalf("code %d, %d pull requests, stderr %q", code, len(reply.PullRequests), stderr)
	}
	if _, stderr, code := lookup(t, request(expired)); code != 1 || !strings.Contains(stderr, "every host failed") {
		t.Fatalf("every host failed: code %d, stderr %q", code, stderr)
	}
	if _, stderr, code := lookup(t, `{"version":1,"operation":"create"}`); code != 1 || !strings.Contains(stderr, "unknown operation") {
		t.Fatalf("unknown operation: code %d, stderr %q", code, stderr)
	}
	t.Setenv("PATH", t.TempDir())
	if _, stderr, code := lookup(t, request(web)); code != 1 || !strings.Contains(stderr, "gh is not installed") {
		t.Fatalf("missing gh: code %d, stderr %q", code, stderr)
	}
}

func TestCheck(t *testing.T) {
	log := fakeGH(t)
	var out, stderr bytes.Buffer
	if code := prhost.Main(host, []string{"check"}, nil, &out, &stderr); code != 0 || !strings.Contains(out.String(), "gh is installed and logged in") {
		t.Fatalf("check: code %d, out %q, stderr %q", code, out.String(), stderr.String())
	}
	if got := calls(log); !slices.Equal(got, []string{"auth status"}) {
		t.Fatalf("check calls = %q", got)
	}
}
