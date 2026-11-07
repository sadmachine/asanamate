package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/pullrequests"
)

// Doctor runs each pull request command for the ticket and shows its stderr,
// whether the command failed or explained a partial answer.
func TestDoctorChecksPullRequests(t *testing.T) {
	var out bytes.Buffer
	ticket := pullrequests.Ticket{GID: "42", URL: "https://app.asana.com/0/0/42", Branch: "feat/x"}
	checkPullRequests(context.Background(), &out, []string{"x"}, ticket, nil)
	if !strings.Contains(out.String(), "link a repo first") {
		t.Fatalf("no repo: %q", out.String())
	}
	out.Reset()
	commands := []string{
		`echo "gh: not logged in" >&2; exit 1`,
		`cat >/dev/null; echo "ghe.example.com: login expired" >&2; printf '%s' '{"pull_requests":[{"ticket_gid":"42","url":"https://github.com/acme/web/pull/7","number":7,"title":"Fix","state":"open"}]}'`,
	}
	checkPullRequests(context.Background(), &out, commands, ticket, []string{filepath.Join(t.TempDir(), "web")})
	for _, want := range []string{"failed: pull requests: gh: not logged in", "1 found", "stderr: ghe.example.com: login expired", "#7 open Fix  https://github.com/acme/web/pull/7"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("doctor output lacks %q:\n%s", want, out.String())
		}
	}
}
