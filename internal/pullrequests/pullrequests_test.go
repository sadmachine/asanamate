package pullrequests

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sadmachine/asanamate/internal/repo"
)

var login = Ticket{GID: "1201", URL: "https://app.asana.com/0/0/1201", Branch: "feature/login",
	Repos: []Repo{{Path: "/code/web", Remotes: []repo.Remote{{Name: "origin", URL: "https://git.example.com/acme/web.git"}}}}}

// TestProviderFixture runs testdata/provider.sh, the provider
// docs/extensions.md quotes, and checks the request it received, so a
// protocol change fails here.
func TestProviderFixture(t *testing.T) {
	script, err := filepath.Abs("testdata/provider.sh")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "requests.jsonl")
	command := "/bin/sh '" + script + "' '" + log + "'"
	other := Ticket{GID: "1202", URL: "https://app.asana.com/0/0/1202", Branch: "feature/x",
		Repos: []Repo{{Path: "/code/api", Remotes: []repo.Remote{{Name: "origin", URL: "git@github.com:acme/api.git"}}}}}
	prs, _, err := Lookup(context.Background(), command, []Ticket{login, other})
	want := PullRequest{TicketGID: "1201", URL: "https://git.example.com/acme/web/pulls/42", Number: 42, Title: "Fix login redirect",
		State: StateOpen, Checks: ChecksFailing, Review: ReviewChanges, Author: "zed",
		UpdatedAt: time.Date(2026, 11, 5, 14, 5, 0, 0, time.UTC), Repo: "/code/web", Branch: "feature/login"}
	if err != nil || len(prs) != 1 || prs[0] != want || prs[0].Label() != "#42" {
		t.Fatalf("prs = %+v, err = %v", prs, err)
	}
	if prs, _, err := Lookup(context.Background(), command, []Ticket{other}); err != nil || len(prs) != 0 {
		t.Fatalf("other host = %+v, %v", prs, err)
	}
	data, _ := os.ReadFile(log)
	first, _, _ := strings.Cut(string(data), "\n")
	if wantReq := `{"version":1,"operation":"lookup","tickets":[{"gid":"1201","url":"https://app.asana.com/0/0/1201","branch":"feature/login","repos":[{"path":"/code/web","remotes":[{"name":"origin","url":"https://git.example.com/acme/web.git"}]}]},` +
		`{"gid":"1202","url":"https://app.asana.com/0/0/1202","branch":"feature/x","repos":[{"path":"/code/api","remotes":[{"name":"origin","url":"git@github.com:acme/api.git"}]}]}]}`; first != wantReq {
		t.Fatalf("request = %s\nwant      %s", first, wantReq)
	}
}

func TestLookupValidates(t *testing.T) {
	item := func(fields string) string {
		return `{"ticket_gid":"1201","url":"https://h/p/1","title":"t","state":"open"` + fields + `}`
	}
	body := `{"pull_requests":[` + strings.Join([]string{
		item(`,"checks":"exploded","review":"maybe","updated_at":"yesterday","ref":"!7","author":"a\tb"`),
		`{"ticket_gid":"999","url":"https://h/p/2","title":"t","state":"open"}`,
		`{"ticket_gid":"1201","url":"javascript:alert(1)","title":"t","state":"open"}`,
		`{"ticket_gid":"1201","url":"https://h/p/3","title":" ","state":"open"}`,
		`{"ticket_gid":"1201","url":"https://h/p/4","title":"t","state":"reopened"}`,
		`{"ticket_gid":"1201","url":"https://h/p/5","title":"two\nlines\u001b[31m","state":"merged"}`,
	}, ",") + `]}`
	prs, _, err := Lookup(context.Background(), "printf '%s' '"+body+"'", []Ticket{login})
	if err != nil || len(prs) != 2 {
		t.Fatalf("prs = %+v, err = %v", prs, err)
	}
	if p := prs[0]; p.Checks != "" || p.Review != "" || !p.UpdatedAt.IsZero() || p.Label() != "!7" || p.Author != "a b" || !p.Active() {
		t.Fatalf("first = %+v", p)
	}
	if p := prs[1]; p.Title != "two lines" || p.Active() || p.Label() != "" {
		t.Fatalf("second = %+v", p)
	}
	for name, command := range map[string]string{
		"not json":   "printf nope",
		"no list":    "printf '{}'",
		"exit":       "echo gh is not logged in >&2; exit 1",
		"bad number": `printf '{"pull_requests":[{"number":"x"}]}'`,
	} {
		if _, _, err := Lookup(context.Background(), command, []Ticket{login}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, stderr, err := Lookup(context.Background(), `printf '{"pull_requests":[]}'; echo one host failed >&2`, nil); err != nil || stderr != "one host failed" {
		t.Fatalf("stderr = %q, err = %v", stderr, err)
	}
}

func TestMergeKeepsFirstPerTicket(t *testing.T) {
	a := PullRequest{TicketGID: "1", URL: "u1", Title: "first"}
	b := PullRequest{TicketGID: "1", URL: "u1", Title: "second"}
	c := PullRequest{TicketGID: "2", URL: "u1", Title: "other ticket"}
	d := PullRequest{TicketGID: "1", URL: "u2", Title: "next"}
	got := Merge([][]PullRequest{{a}, {b, c, d}})
	if !slices.Equal(got, []PullRequest{a, c, d}) {
		t.Fatalf("Merge = %+v", got)
	}
}

func TestWithRemotes(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", "https://github.com/acme/web.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v %s", err, out)
	}
	missing := filepath.Join(dir, "missing")
	got := WithRemotes(context.Background(), []Ticket{
		{GID: "1", Repos: []Repo{{Path: dir}, {Path: missing}}},
		{GID: "2", Repos: []Repo{{Path: dir}}},
	})
	want := []repo.Remote{{Name: "origin", URL: "https://github.com/acme/web.git"}}
	if !slices.Equal(got[0].Repos[0].Remotes, want) || !slices.Equal(got[1].Repos[0].Remotes, want) {
		t.Fatalf("remotes = %+v", got)
	}
	if r := got[0].Repos[1].Remotes; r == nil || len(r) != 0 {
		t.Fatalf("missing repo remotes = %#v, want empty, not null", r)
	}
}
