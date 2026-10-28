package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/activity"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/state"
)

var branchHook = config.Hook{On: config.HookTicketOpen, When: "is:open branch:none", Once: true, Run: "edit:branch"}

// hookModel is editModel, wide enough for the reader beside the list, with
// hooks.
func hookModel(t *testing.T, hooks ...config.Hook) *Model {
	t.Helper()
	m, _ := editModel(t)
	m.deps.Config.Hooks = hooks
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return m
}

func branchAsked(m *Model) bool {
	return m.input != nil && m.input.title == "Branch"
}

func TestTicketOpenHookAsksOnceWhenTheReaderFocuses(t *testing.T) {
	for _, k := range []string{"2", "tab", "enter"} {
		t.Run(k, func(t *testing.T) {
			m := hookModel(t, branchHook)
			if branchAsked(m) {
				t.Fatal("asked before the reader had focus")
			}
			press(m, k)
			if !branchAsked(m) {
				t.Fatalf("%s did not ask for a branch (input %+v)", k, m.input)
			}
			press(m, "esc", "1", k)
			if branchAsked(m) {
				t.Fatal("asked again after esc")
			}
			st, err := state.Load(m.deps.State.Path())
			if err != nil || !st.HookRan(branchHook.Key(), "1") {
				t.Fatalf("once record not saved: %v", err)
			}
		})
	}
}

func TestTicketOpenHookSkipsBranchesAndBusyReaders(t *testing.T) {
	m := hookModel(t, branchHook)
	m.deps.State.SetTaskBranch("1", "feature/fix")
	press(m, "2")
	if branchAsked(m) {
		t.Fatal("asked for a ticket with a saved branch")
	}

	m = hookModel(t, branchHook)
	m.modal = newPicker(nil, "Busy", []pickItem{{Label: "x"}})
	press(m, "2")
	if branchAsked(m) || m.deps.State.HookRan(branchHook.Key(), "1") {
		t.Fatal("prompted over a modal, or marked the skipped hook")
	}
	m.modal = nil
	press(m, "1", "2")
	if !branchAsked(m) {
		t.Fatal("skipped hook did not ask next time")
	}
}

func TestTicketOpenHookIgnoresListMovesAndComments(t *testing.T) {
	m := hookModel(t, branchHook)
	press(m, "j", "k")
	if branchAsked(m) {
		t.Fatal("moving the list selection asked")
	}
	m.openCommentComposer(m.details["1"].Task)
	send(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	if branchAsked(m) || m.deps.State.HookRan(branchHook.Key(), "1") {
		t.Fatal("starting a comment ran the ticket_open hook")
	}
}

func TestTicketOpenHookFiresFromTicketSearch(t *testing.T) {
	m := hookModel(t, branchHook)
	send(m, m.openTicketSearch())
	send(m, key("enter"))
	if !branchAsked(m) {
		t.Fatalf("opening from ticket search did not ask (modal %v)", m.modal)
	}
}

// Activity, the Feed panel, and Asana task links open tickets through
// showTicket.
func TestTicketOpenHookFiresFromShowTicket(t *testing.T) {
	m := hookModel(t, branchHook)
	cmd := m.showTicket(m.details["1"].Task)
	send(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	drain(m, cmd)
	if !branchAsked(m) {
		t.Fatal("showTicket did not ask for a branch")
	}
}

func TestHookOpenedEditRaisesNoEvents(t *testing.T) {
	m := hookModel(t,
		config.Hook{On: config.HookTicketOpen, Run: "edit:assign"},
		assignLogTime,
	)
	m.deps.Config.ConfirmWrites = false
	m.deps.Config.TimeTracking = config.TimeTracking{ID: "hrvst", Command: "printf '%s' '" + timeFormJSON + "'"}
	tk := m.details["1"]
	tk.PermalinkURL = "https://app.asana.com/0/p1/1"
	m.details["1"] = tk
	press(m, "2")
	if m.modal == nil {
		t.Fatal("hook did not open the assignee picker")
	}
	press(m, "j", "j", "enter")
	if m.form != nil {
		t.Fatal("an edit a hook opened ran the assign hook")
	}
}

func TestNewActivityEvents(t *testing.T) {
	found := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	entry := func(gid string, mine, automatic bool, foundAt time.Time) activity.Entry {
		return activity.Entry{Task: asana.Task{GID: "t" + gid}, Story: asana.Story{GID: gid, ResourceSubtype: "comment_added"}, Mine: mine, Automatic: automatic, FoundAt: foundAt}
	}
	entries := []activity.Entry{
		entry("new", false, false, found),
		entry("known", false, false, found),
		entry("mine", true, false, found),
		entry("auto", false, true, found),
		entry("seed", false, false, time.Time{}),
	}
	events := newActivityEvents(entries, map[string]bool{"known": true})
	if len(events) != 1 || events[0].task.GID != "tnew" || events[0].name != config.HookNewActivity ||
		events[0].comment == nil || events[0].comment.GID != "new" {
		t.Fatalf("events = %+v", events)
	}
}

func TestNewActivityHookRunsBackgroundAction(t *testing.T) {
	m := hookModel(t, config.Hook{On: config.HookNewActivity, When: "fix", Run: "action:log"})
	out := filepath.Join(t.TempDir(), "event")
	m.deps.Config.Actions = []config.Action{{Name: "Log", Key: "l", Mode: config.ModeBackground, File: "log",
		Command: `printf '%s %s %s' "$ASANAMATE_EVENT" "$ASANAMATE_GID" "$ASANAMATE_COMMENT_GID" > ` + out}}
	cmd := m.runHooks([]hookEvent{
		{name: config.HookNewActivity, task: asana.Task{GID: "1", Name: "Fix"}, comment: &asana.Story{GID: "s1"}},
		{name: config.HookNewActivity, task: asana.Task{GID: "2", Name: "Other"}},
	})
	if cmd == nil {
		t.Fatal("no action ran")
	}
	drain(m, cmd)
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(out)
		if err == nil && strings.TrimSpace(string(data)) == "new_activity 1 s1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("action output = %q, %v", data, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
