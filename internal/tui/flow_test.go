package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/repo"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	resolved, err := repo.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

var exitRepoAction = config.Action{Name: "Go", Key: "x", Mode: config.ModeExit, Repo: true, Command: "true"}

func flowModel(t *testing.T, memberships ...asana.Membership) (*Model, *state.State) {
	t.Helper()
	m, st := testModel(t, config.Config{
		Actions:    []config.Action{exitRepoAction},
		RepoSource: config.RepoSource{Command: `printf '/definitely/missing\n'`},
	})
	tk := ticket.Ticket{Task: asana.Task{GID: "1", Name: "Fix", Memberships: memberships}}
	m.tasks, m.visible = []asana.Task{tk.Task}, []asana.Task{tk.Task}
	m.details["1"] = tk
	m.openActionMenu()
	return m, st
}

var (
	web = asana.Membership{Project: asana.Ref{GID: "p1", Name: "Web"}}
	api = asana.Membership{Project: asana.Ref{GID: "p2", Name: "API"}}
)

func TestSeveralProjectsAskWhichOne(t *testing.T) {
	m, _ := flowModel(t, web, api)
	m.pickedAction(0)
	if m.modal == nil || m.modal.title != "Which project's repo?" || len(m.modal.items) != 2 {
		t.Fatalf("modal = %+v", m.modal)
	}
}

func TestLinkedRepoRunsImmediately(t *testing.T) {
	dir := gitRepo(t)
	m, st := flowModel(t, web)
	st.LinkRepo("p1", dir)
	m.pickedAction(0)
	cmd := m.ExitCommand()
	if cmd == nil || cmd.Dir != dir || !slices.Contains(cmd.Env, "ASANAMATE_REPO="+dir) || !slices.Contains(cmd.Env, "ASANAMATE_PROJECT=Web") {
		t.Fatalf("exit command = %+v", cmd)
	}
	if _, err := os.Stat(filepath.Join(m.deps.StateDir, "tickets", "1", "ticket.md")); err != nil {
		t.Fatalf("ticket.md not written: %v", err)
	}
}

func TestUnlinkedRepoPromptsAndSaves(t *testing.T) {
	dir := gitRepo(t)
	m, st := flowModel(t, web)
	msg := m.pickedAction(0)().(candidatesMsg)
	m.Update(msg)
	if m.modal == nil || !strings.HasPrefix(m.modal.title, "Repo for") || !m.modal.allowFree {
		t.Fatalf("modal = %+v", m.modal)
	}
	m.modal.input.SetValue(t.TempDir())
	m.Update(key("tab"))
	if m.modal == nil || !strings.Contains(m.modal.err, "not a git repository") {
		t.Fatalf("invalid path must keep the picker open with an error; modal = %+v", m.modal)
	}
	m.modal.input.SetValue(dir)
	m.Update(key("tab"))
	if m.modal != nil || m.ExitCommand() == nil || m.ExitCommand().Dir != dir {
		t.Fatalf("modal = %+v, exit = %+v", m.modal, m.ExitCommand())
	}
	again, _ := state.Load(st.Path())
	if again.Repos["p1"] != dir {
		t.Fatalf("saved repos = %v", again.Repos)
	}
}

func TestStaleRepoLinkReprompts(t *testing.T) {
	m, st := flowModel(t, web)
	st.LinkRepo("p1", t.TempDir())
	cmd := m.pickedAction(0)
	if cmd == nil || m.ExitCommand() != nil || !strings.Contains(m.status, "no longer a git repository") {
		t.Fatalf("status = %q, exit = %v", m.status, m.ExitCommand())
	}
	if _, ok := cmd().(candidatesMsg); !ok {
		t.Fatal("want the repo picker to load candidates")
	}
}

func TestNoProjectRepoIsNotSaved(t *testing.T) {
	dir := gitRepo(t)
	m, st := flowModel(t)
	m.Update(m.pickedAction(0)())
	m.pickedRepo(dir)
	if m.ExitCommand() == nil || len(st.Repos) != 0 {
		t.Fatalf("exit = %v, repos = %v", m.ExitCommand(), st.Repos)
	}
}

func TestNonRepoActionUsesViewedProject(t *testing.T) {
	m, _ := testModel(t, config.Config{Actions: []config.Action{{Name: "Go", Key: "x", Mode: config.ModeExit, Command: "true"}}})
	tk := ticket.Ticket{Task: asana.Task{GID: "1", Name: "Fix", Memberships: []asana.Membership{web, api}}}
	m.viewProject = &asana.Ref{GID: "p2", Name: "API"}
	m.tasks, m.visible = []asana.Task{tk.Task}, []asana.Task{tk.Task}
	m.details["1"] = tk
	m.openActionMenu()
	m.Update(key("x"))
	if cmd := m.ExitCommand(); cmd == nil || !slices.Contains(cmd.Env, "ASANAMATE_PROJECT_GID=p2") || cmd.Dir != "" {
		t.Fatalf("exit command = %+v", cmd)
	}
}

func TestRepoLinkInsideParentRepoReprompts(t *testing.T) {
	outer := gitRepo(t)
	inner := filepath.Join(outer, "proj")
	os.Mkdir(inner, 0o755)
	m, st := flowModel(t, web)
	st.LinkRepo("p1", inner)
	cmd := m.pickedAction(0)
	if m.ExitCommand() != nil || cmd == nil || !strings.Contains(m.status, "no longer a git repository") {
		t.Fatalf("ran in parent repo: exit = %+v, status = %q", m.ExitCommand(), m.status)
	}
}

func inputModel(t *testing.T) *Model {
	t.Helper()
	m, _ := testModel(t, config.Config{Actions: []config.Action{{Name: "Go", Key: "x", Mode: config.ModeExit, Input: "Extra context", Command: "true"}}})
	tk := ticket.Ticket{Task: asana.Task{GID: "1", Name: "Fix"}}
	m.tasks, m.visible = []asana.Task{tk.Task}, []asana.Task{tk.Task}
	m.details["1"] = tk
	m.openActionMenu()
	m.Update(key("x"))
	if m.input == nil || m.ExitCommand() != nil {
		t.Fatalf("want the input box before running; input = %v, exit = %v", m.input, m.ExitCommand())
	}
	return m
}

func inputFile(t *testing.T, cmd *exec.Cmd) string {
	t.Helper()
	for _, kv := range cmd.Env {
		if path, ok := strings.CutPrefix(kv, "ASANAMATE_INPUT_FILE="); ok {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			return string(data)
		}
	}
	t.Fatal("ASANAMATE_INPUT_FILE not set")
	return ""
}

func TestInputActionPassesTypedText(t *testing.T) {
	m := inputModel(t)
	m.input.area.SetValue("  tested on UAT\nnot local  ")
	m.Update(key("enter"))
	if m.ExitCommand() != nil {
		t.Fatal("enter must insert a newline, not run the action")
	}
	m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	cmd := m.ExitCommand()
	if m.input != nil || cmd == nil {
		t.Fatalf("input = %v, exit = %v", m.input, cmd)
	}
	if got := inputFile(t, cmd); got != "tested on UAT\nnot local" {
		t.Fatalf("input file = %q", got)
	}
}

func TestInputActionCancels(t *testing.T) {
	m := inputModel(t)
	m.Update(key("esc"))
	if m.input != nil || m.run != nil || m.ExitCommand() != nil {
		t.Fatalf("input = %v, run = %v, exit = %v", m.input, m.run, m.ExitCommand())
	}
}

// linksModel is a model viewing Web, with Web, API, and Zed loaded.
func linksModel(t *testing.T) (*Model, *state.State) {
	t.Helper()
	m, st := testModel(t, config.Config{RepoSource: config.RepoSource{Command: `printf '/definitely/missing\n'`}})
	m.projects = []asana.Project{{GID: "p3", Name: "Zed"}, {GID: "p1", Name: "Web"}, {GID: "p2", Name: "API"}}
	m.viewProject, m.loading = &asana.Ref{GID: "p1", Name: "Web"}, false
	return m, st
}

func TestRepoLinksListLinkedFirstFromViewedProject(t *testing.T) {
	m, st := linksModel(t)
	st.LinkRepo("p2", "/code/api")
	st.LinkRepo("gone", "/code/old")
	m.Update(key("L"))
	if m.modal == nil || m.modal.title != "Repo links" {
		t.Fatalf("modal = %+v", m.modal)
	}
	var got []string
	for _, it := range m.modal.items {
		got = append(got, it.Label+"="+it.Hint)
	}
	want := []string{"API=/code/api", "gone=/code/old", "Web=not linked", "Zed=not linked"}
	if !slices.Equal(got, want) {
		t.Fatalf("items = %q, want %q", got, want)
	}
	if it := m.modal.items[m.modal.matches[m.modal.cursor]]; it.Label != "Web" {
		t.Fatalf("cursor on %q, want the viewed project", it.Label)
	}
}

// editLink opens the repo picker for the project under the links cursor.
func editLink(t *testing.T, m *Model) {
	t.Helper()
	m.Update(key("L"))
	_, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("want the repo picker to load candidates")
	}
	m.Update(cmd())
	if m.modal == nil || m.modal.title != "Repo for Web" || !m.modal.allowFree {
		t.Fatalf("modal = %+v", m.modal)
	}
}

func TestRepoLinksRelinkSaves(t *testing.T) {
	dir := gitRepo(t)
	m, st := linksModel(t)
	editLink(t, m)
	if len(m.modal.items) != 1 {
		t.Fatalf("unlinked project must not offer unlink; items = %+v", m.modal.items)
	}
	m.linked = map[string][]agents.Agent{}
	m.modal.input.SetValue(dir)
	m.Update(key("tab"))
	if m.modal != nil || m.linked != nil {
		t.Fatalf("modal = %+v, linked = %v", m.modal, m.linked)
	}
	again, _ := state.Load(st.Path())
	if again.Repos["p1"] != dir {
		t.Fatalf("saved repos = %v", again.Repos)
	}
}

func TestRepoLinksUnlinkSaves(t *testing.T) {
	m, st := linksModel(t)
	st.LinkRepo("p1", "/code/web")
	editLink(t, m)
	if m.modal.items[0].Label != "unlink" {
		t.Fatalf("items = %+v", m.modal.items)
	}
	m.Update(key("enter"))
	again, _ := state.Load(st.Path())
	if _, ok := again.Repos["p1"]; ok || m.modal != nil {
		t.Fatalf("saved repos = %v, modal = %+v", again.Repos, m.modal)
	}
}
