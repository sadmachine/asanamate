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

// fakeGlab puts testdata/glab on PATH and returns the file it logs calls to.
func fakeGlab(t *testing.T) string {
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
	web     = `{"path":"/code/web","remotes":[{"name":"origin","url":"git@gitlab.example.com:acme/web.git"}]}`
	github  = `{"path":"/code/api","remotes":[{"name":"origin","url":"https://github.com/acme/api.git"}]}`
	expired = `{"path":"/code/old","remotes":[{"name":"origin","url":"https://expired.example.com/acme/old"}]}`
)

func TestLookupMapsGlabOutput(t *testing.T) {
	log := fakeGlab(t)
	reply, stderr, code := lookup(t, request(web+","+github))
	if code != 0 || stderr != "" {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	est := time.FixedZone("", -5*3600)
	want := []pullrequests.PullRequest{
		{TicketGID: "1201", URL: "https://gitlab.example.com/acme/web/-/merge_requests/12", Number: 12, Ref: "!12", Title: "Fix login redirect",
			State: "open", Checks: "failing", Review: "changes", Author: "zed", UpdatedAt: time.Date(2026, 11, 5, 9, 5, 0, 0, est), Repo: "/code/web", Branch: "feature/login"},
		{TicketGID: "1201", URL: "https://gitlab.example.com/acme/web/-/merge_requests/10", Number: 10, Ref: "!10", Title: "Old login fix",
			State: "merged", Author: "zed", UpdatedAt: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC), Repo: "/code/web", Branch: "other"},
		{TicketGID: "1300", URL: "https://gitlab.example.com/acme/web/-/merge_requests/9", Number: 9, Ref: "!9", Title: "Abandoned",
			State: "closed", Author: "zed", UpdatedAt: time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC), Repo: "/code/web", Branch: "feature/other"},
	}
	if len(reply.PullRequests) != len(want) {
		t.Fatalf("reply = %+v", reply.PullRequests)
	}
	for i, p := range reply.PullRequests {
		w := want[i]
		if !p.UpdatedAt.Equal(w.UpdatedAt) {
			t.Errorf("%d: updated_at %v, want %v", i, p.UpdatedAt, w.UpdatedAt)
		}
		p.UpdatedAt, w.UpdatedAt = time.Time{}, time.Time{}
		if p != w {
			t.Errorf("%d:\n%+v\nwant\n%+v", i, p, w)
		}
	}
	// One list call for the GitLab repo, then one detail call for the only
	// open merge request a ticket matched; none for the GitHub repo.
	got := calls(log)
	if len(got) != 3 || got[0] != "auth status --hostname gitlab.example.com" ||
		got[1] != "mr list --repo https://gitlab.example.com/acme/web --all --per-page 100 --output json" ||
		got[2] != "api --hostname gitlab.example.com projects/acme%2Fweb/merge_requests/12" {
		t.Fatalf("glab calls = %q", got)
	}
}

func TestNoGitLabRepoCallsNothing(t *testing.T) {
	log := fakeGlab(t)
	reply, _, code := lookup(t, request(github))
	if code != 0 || len(reply.PullRequests) != 0 || len(calls(log)) != 0 {
		t.Fatalf("code %d, reply %+v, calls %q", code, reply, calls(log))
	}
}

func TestPartialHostFailureAndMissingCLI(t *testing.T) {
	fakeGlab(t)
	reply, stderr, code := lookup(t, request(web+","+expired))
	if code != 0 || len(reply.PullRequests) != 3 || !strings.Contains(stderr, "expired.example.com/acme/old: ") || !strings.Contains(stderr, "401 Unauthorized") {
		t.Fatalf("code %d, %d merge requests, stderr %q", code, len(reply.PullRequests), stderr)
	}
	if _, stderr, code := lookup(t, request(expired)); code != 1 || !strings.Contains(stderr, "every host failed") {
		t.Fatalf("every host failed: code %d, stderr %q", code, stderr)
	}
	if _, stderr, code := lookup(t, `{"version":1,"operation":"create"}`); code != 1 || !strings.Contains(stderr, "unknown operation") {
		t.Fatalf("unknown operation: code %d, stderr %q", code, stderr)
	}
	t.Setenv("PATH", t.TempDir())
	if _, stderr, code := lookup(t, request(web)); code != 1 || !strings.Contains(stderr, "glab is not installed") {
		t.Fatalf("missing glab: code %d, stderr %q", code, stderr)
	}
}

func TestCheck(t *testing.T) {
	log := fakeGlab(t)
	var out, stderr bytes.Buffer
	if code := prhost.Main(host, []string{"check"}, nil, &out, &stderr); code != 0 || !strings.Contains(out.String(), "glab is installed and logged in") {
		t.Fatalf("check: code %d, out %q, stderr %q", code, out.String(), stderr.String())
	}
	if got := calls(log); !slices.Equal(got, []string{"auth status"}) {
		t.Fatalf("check calls = %q", got)
	}
}
