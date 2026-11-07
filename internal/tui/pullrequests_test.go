package tui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/filter"
	"github.com/sadmachine/asanamate/internal/pullrequests"
)

// provider is a pull requests command that logs its request to log and
// answers reply.
func provider(log, reply string) string {
	return "cat >> '" + log + "'; printf '%s' '" + reply + "'"
}

// prModel is editModel with ticket 1 linked to a repo and two pull request
// commands, sized so the reader shows.
func prModel(t *testing.T, commands ...string) *Model {
	t.Helper()
	m, _ := editModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	m.deps.Config.PullRequests.Commands = commands
	m.deps.State.LinkRepo("p1", "/code/web")
	tk := m.details["1"]
	tk.HTMLNotes = "<body>Notes</body>"
	m.details["1"] = tk
	return m
}

func TestPullRequestLookupMergesCommands(t *testing.T) {
	log := filepath.Join(t.TempDir(), "requests.jsonl")
	open := `{"ticket_gid":"1","url":"https://github.com/acme/web/pull/42","number":42,"title":"Fix login","state":"open","checks":"failing","review":"changes"}`
	merged := `{"ticket_gid":"1","url":"https://gitlab.com/acme/web/-/merge_requests/7","ref":"!7","title":"Old fix","state":"merged","checks":"failing"}`
	m := prModel(t,
		provider(log, `{"pull_requests":[`+open+`]}`),
		provider(log, `{"pull_requests":[`+open+`,`+merged+`]}`))
	drain(m, m.checkPullRequests())
	data, _ := os.ReadFile(log)
	if n := strings.Count(string(data), `"operation":"lookup"`); n != 2 || !strings.Contains(string(data), `"repos":[{"path":"/code/web","remotes":[]}]`) {
		t.Fatalf("requests:\n%s", data)
	}
	got := m.feed.prs["1"]
	if len(got) != 2 || got[0].Number != 42 || got[1].Label() != "!7" {
		t.Fatalf("merged pull requests = %+v", got)
	}
	env := m.filterEnv()
	for query, want := range map[string]bool{
		"pr:open": true, "pr:merged": true, "pr:draft": false, "pr:any": true,
		"checks:failing": true, "review:changes": true, "review:approved": false, "-pr:none": true,
	} {
		if got := filter.Parse(query).Match(m.tasks[0], env); got != want {
			t.Errorf("%s matches = %v, want %v", query, got, want)
		}
	}

	// A merged pull request's failing checks do not count.
	m.gotPullRequests(prMsg{index: 0, seq: m.feed.prSeq, prs: nil})
	m.gotPullRequests(prMsg{index: 1, seq: m.feed.prSeq, prs: []pullrequests.PullRequest{{TicketGID: "1", URL: "u", Title: "t", State: "merged", Checks: "failing"}}})
	if env := m.filterEnv(); filter.Parse("checks:any").Match(m.tasks[0], env) || !filter.Parse("pr:merged").Match(m.tasks[0], env) {
		t.Fatal("checks: must skip merged pull requests")
	}
}

func TestPullRequestFailureKeepsResults(t *testing.T) {
	m := prModel(t, "a", "b")
	m.feed.prRuns = make([]prRun, 2)
	m.feed.prSeq = 1
	a := pullrequests.PullRequest{TicketGID: "1", URL: "https://h/1", Title: "A", State: "open"}
	b := pullrequests.PullRequest{TicketGID: "1", URL: "https://h/2", Title: "B", State: "draft"}
	m.Update(prMsg{index: 0, seq: 1, prs: []pullrequests.PullRequest{a}})
	m.Update(prMsg{index: 1, seq: 1, prs: []pullrequests.PullRequest{b}})
	m.feed.prSeq = 2
	m.Update(prMsg{index: 0, seq: 2, err: errors.New("gh: not logged in")})
	if got := m.feed.prs["1"]; !slices.Equal(got, []pullrequests.PullRequest{a, b}) {
		t.Fatalf("after failure = %+v", got)
	}
	if s := m.feedSummary(); !strings.Contains(s, "pull requests failed") {
		t.Fatalf("summary = %q", s)
	}
	m.Update(prMsg{index: 0, seq: 1, prs: nil}) // older than the newest answer
	if len(m.feed.prs["1"]) != 2 {
		t.Fatal("a stale answer replaced a newer one")
	}
	m.Update(prMsg{index: 0, seq: 2, prs: []pullrequests.PullRequest{a}})
	if s := m.feedSummary(); strings.Contains(s, "pull requests failed") {
		t.Fatalf("summary after recovery = %q", s)
	}
}

func TestPullRequestLookupOff(t *testing.T) {
	if m := prModel(t); m.checkPullRequests() != nil {
		t.Fatal("no commands must run nothing")
	}
	m := prModel(t, "unused")
	m.deps.State.Repos = nil
	if m.checkPullRequests() != nil {
		t.Fatal("no ticket with a linked repo must run nothing")
	}
}

func TestReaderShowsPullRequestsInEveryLayout(t *testing.T) {
	m := prModel(t, "unused")
	m.feed.prRuns = make([]prRun, 1)
	url := "https://github.com/acme/web/pull/42"
	m.gotPullRequests(prMsg{prs: []pullrequests.PullRequest{
		{TicketGID: "1", URL: url, Number: 42, Title: "Fix login redirect", State: "open", Checks: "failing", Review: "changes"},
	}})
	want := "#42 open · checks failing · changes requested  Fix login redirect"
	for _, view := range []string{config.ViewCards, config.ViewStreamlined} {
		m.readerView, m.fieldKey = view, ""
		m.renderDetail(false)
		text := readerText(m)
		line := strings.Split(text, "\n")[m.fieldLines[prKeyPrefix+url]]
		if !strings.Contains(line, want) || strings.Index(text, "Pull requests") > strings.Index(text, "Description") {
			t.Fatalf("%s: pull request line %q in:\n%s", view, line, text)
		}
		i := slices.Index(m.cardTargets, prSectionKey)
		if i < 0 || m.cardTargets[i+1] != prKeyPrefix+url {
			t.Fatalf("%s: targets = %q", view, m.cardTargets)
		}
		m.openJumpMenu()
		if p := m.modal; p == nil || !slices.ContainsFunc(p.items, func(it pickItem) bool { return it.Value == prSectionKey && it.Key == "p" }) {
			t.Fatalf("%s: jump menu lacks Pull requests", view)
		}
		m.modal = nil
		m.focusReader, m.fieldKey = true, prKeyPrefix+url
		if _, _, hints := m.mode(); !slices.ContainsFunc(hints, func(h [2]string) bool { return h[1] == "open pull request" }) {
			t.Fatalf("%s: hints = %q", view, hints)
		}
		if cmd := m.open(tea.KeyPressMsg{}); cmd == nil {
			t.Fatalf("%s: enter on a pull request must open it", view)
		}
	}
	m.readerView = config.ViewMarkdown
	m.renderDetail(false)
	if text := readerText(m); !strings.Contains(text, "## Pull requests") || !strings.Contains(text, "#42 "+url+" open · checks failing") ||
		strings.Index(text, "Pull requests") > strings.Index(text, "Description") {
		t.Fatalf("markdown:\n%s", text)
	}
}

func TestBuiltinGroupingsUsePullRequests(t *testing.T) {
	m := prModel(t, "unused")
	m.feed.prRuns = make([]prRun, 1)
	m.gotPullRequests(prMsg{prs: []pullrequests.PullRequest{{TicketGID: "1", URL: "https://h/1", Title: "t", State: "draft", Review: "changes"}}})
	g, _ := m.deps.Config.Grouping("state")
	if label := matchSection(t, m, g); label != "In review" {
		t.Fatalf("state section = %q", label)
	}
	g, _ = m.deps.Config.Grouping(config.NowGrouping)
	if label := matchSection(t, m, g); label != "Review feedback" {
		t.Fatalf("now section = %q", label)
	}
}

// matchSection is the label of g's first section ticket 1 matches.
func matchSection(t *testing.T, m *Model, g config.Grouping) string {
	t.Helper()
	env := m.filterEnv()
	for _, s := range g.Sections {
		for _, q := range s.Match {
			if filter.Parse(q).Match(m.tasks[0], env) {
				return s.Label
			}
		}
	}
	return ""
}
