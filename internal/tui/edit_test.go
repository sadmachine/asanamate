package tui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

var ctrlS = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}

// editModel is a model on one ticket of yours, backed by a fake Asana that
// records every write as "METHOD path body".
func editModel(t *testing.T) (*Model, *[]string) {
	t.Helper()
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/users/me":
			io.WriteString(w, `{"data":{"gid":"u1"}}`)
		case r.URL.Path == "/projects":
			io.WriteString(w, `{"data":[{"gid":"p1","name":"Web","members":[{"gid":"u1"}]},{"gid":"p2","name":"API","members":[{"gid":"u1"}]}]}`)
		case r.URL.Path == "/workspaces/w/users":
			io.WriteString(w, `{"data":[{"gid":"u2","name":"zed"},{"gid":"u1","name":"Amy"}]}`)
		case r.URL.Path == "/users/me/user_task_list":
			io.WriteString(w, `{"data":{"gid":"mt"}}`)
		case r.URL.Path == "/projects/mt/sections":
			io.WriteString(w, `{"data":[{"gid":"m1","name":"Inbox"},{"gid":"m2","name":"Later"}]}`)
		case r.URL.Path == "/projects/p1/sections":
			io.WriteString(w, `{"data":[{"gid":"s1","name":"Todo"},{"gid":"s2","name":"Done"}]}`)
		case r.Method != http.MethodGet:
			b, _ := io.ReadAll(r.Body)
			writes = append(writes, r.Method+" "+r.URL.Path+" "+string(b))
			io.WriteString(w, `{"data":{}}`)
		default:
			io.WriteString(w, `{"data":[]}`)
		}
	}))
	t.Cleanup(srv.Close)
	m, _ := testModel(t, config.Config{Workspace: "w"})
	m.deps.Client = asana.New("tok")
	m.deps.Client.BaseURL = srv.URL
	branch := "old"
	tk := ticket.Ticket{Task: asana.Task{
		GID: "1", Name: "Fix",
		Assignee:        &asana.Ref{GID: "u1", Name: "Amy"},
		AssigneeSection: &asana.Ref{GID: "m1", Name: "Inbox"},
		Memberships:     []asana.Membership{{Project: asana.Ref{GID: "p1", Name: "Web"}, Section: &asana.Ref{GID: "s1", Name: "Todo"}}},
		CustomFields: []asana.CustomField{
			{GID: "f1", Name: "Branch", ResourceSubtype: "text", DisplayValue: &branch},
			{GID: "f2", Name: "Due", ResourceSubtype: "date"},
			{GID: "f3", Name: "Scope", ResourceSubtype: "multi_enum",
				EnumOptions:     []asana.EnumOption{{GID: "o1", Name: "FE"}, {GID: "o2", Name: "BE"}},
				MultiEnumValues: []asana.EnumOption{{GID: "o1", Name: "FE"}}},
			{GID: "f4", Name: "Reviewers", ResourceSubtype: "people"},
			{GID: "f5", Name: "Formula", ResourceSubtype: "formula"},
		},
	}}
	m.tasks, m.visible, m.loading = []asana.Task{tk.Task}, []asana.Task{tk.Task}, false
	m.details["1"] = tk
	return m, &writes
}

// send delivers msg and then the messages its commands produce, stopping at
// the list reload a finished write starts and its loading spinner.
func send(m *Model, msg tea.Msg) {
	_, cmd := m.Update(msg)
	drain(m, cmd)
}

func drain(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil, tasksMsg, spinnerTickMsg:
	case tea.BatchMsg:
		for _, c := range msg {
			drain(m, c)
		}
	default:
		send(m, msg)
	}
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		send(m, key(k))
	}
}

func TestEditWrites(t *testing.T) {
	cases := []struct {
		name string
		do   func(m *Model)
		want string
	}{
		{"comment", func(m *Model) {
			press(m, "e", "c")
			m.input.area.SetValue(" hello ")
			send(m, ctrlS)
		}, `POST /tasks/1/stories {"data":{"text":"hello"}}`},
		{"project section", func(m *Model) {
			m.viewProject = &asana.Ref{GID: "p1"}
			press(m, "e", "s", "enter", "down", "enter")
		}, `POST /sections/s2/addTask {"data":{"task":"1"}}`},
		{"my tasks section", func(m *Model) {
			press(m, "e", "s", "enter", "down", "enter")
		}, `PUT /tasks/1 {"data":{"assignee_section":"m2"}}`},
		{"add to project", func(m *Model) {
			press(m, "e", "p", "enter")
		}, `POST /tasks/1/addProject {"data":{"project":"p2"}}`},
		{"remove from project", func(m *Model) {
			press(m, "e", "r", "enter")
		}, `POST /tasks/1/removeProject {"data":{"project":"p1"}}`},
		{"text field", func(m *Model) {
			press(m, "e", "f", "enter")
			if got := m.input.area.Value(); got != "old" {
				t.Errorf("prefill = %q", got)
			}
			m.input.area.SetValue("feat/x")
			send(m, ctrlS)
		}, `PUT /tasks/1 {"data":{"custom_fields":{"f1":"feat/x"}}}`},
		{"date field", func(m *Model) {
			press(m, "e", "f", "down", "enter")
			m.input.area.SetValue("2026-10-01")
			send(m, ctrlS)
		}, `PUT /tasks/1 {"data":{"custom_fields":{"f2":{"date":"2026-10-01"}}}}`},
		{"due date", func(m *Model) {
			press(m, "e", "d")
			m.input.area.SetValue("2026-10-01")
			send(m, ctrlS)
		}, `PUT /tasks/1 {"data":{"due_on":"2026-10-01"}}`},
		{"clear due date", func(m *Model) {
			date := "2026-09-30"
			tk := m.details["1"]
			tk.DueOn = &date
			m.details["1"] = tk
			press(m, "e", "d")
			if got := m.input.area.Value(); got != date {
				t.Errorf("prefill = %q", got)
			}
			m.input.area.SetValue("")
			send(m, ctrlS)
		}, `PUT /tasks/1 {"data":{"due_on":null}}`},
		{"multi-select field", func(m *Model) {
			press(m, "e", "f", "down", "down", "enter", "down", "enter")
			send(m, ctrlS)
		}, `PUT /tasks/1 {"data":{"custom_fields":{"f3":["o1","o2"]}}}`},
		{"people field", func(m *Model) {
			press(m, "e", "f", "down", "down", "down", "enter", "down", "enter")
			send(m, ctrlS)
		}, `PUT /tasks/1 {"data":{"custom_fields":{"f4":["u2"]}}}`},
		{"assign", func(m *Model) {
			press(m, "e", "a", "down", "down", "enter")
		}, `PUT /tasks/1 {"data":{"assignee":"u2"}}`},
		{"unassign", func(m *Model) {
			press(m, "e", "a", "enter")
		}, `PUT /tasks/1 {"data":{"assignee":null}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, writes := editModel(t)
			tc.do(m)
			if len(*writes) != 1 || strings.TrimSpace((*writes)[0]) != tc.want {
				t.Fatalf("writes = %q, want %q (status %q)", *writes, tc.want, m.status)
			}
			if !strings.HasSuffix(m.status, ": done") {
				t.Errorf("status = %q", m.status)
			}
		})
	}
}

func TestAddProjectExcludesExistingMemberships(t *testing.T) {
	m, _ := editModel(t)
	press(m, "e", "p")
	if m.modal == nil || len(m.modal.items) != 1 || m.modal.items[0].Value.(asana.Ref).GID != "p2" {
		t.Fatalf("add project choices = %v", m.modal)
	}
}

func TestEditRejectsBadDates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		keys  []string
		value string
	}{
		{"date field", []string{"e", "f", "down", "enter"}, "tomorrow"},
		{"due date word", []string{"e", "d"}, "tomorrow"},
		{"due date impossible", []string{"e", "d"}, "2026-02-30"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, writes := editModel(t)
			press(m, tc.keys...)
			m.input.area.SetValue(tc.value)
			send(m, ctrlS)
			if len(*writes) != 0 || !strings.Contains(m.status, "YYYY-MM-DD") {
				t.Fatalf("writes = %q, status = %q", *writes, m.status)
			}
		})
	}
}

func TestEditCancelWritesNothing(t *testing.T) {
	m, writes := editModel(t)
	press(m, "e", "c")
	send(m, key("esc"))
	if len(*writes) != 0 || m.edit != nil || m.input != nil {
		t.Fatalf("writes = %q, edit = %v, input = %v", *writes, m.edit, m.input)
	}
}

func TestEditSkipsUnsupportedFields(t *testing.T) {
	m, _ := editModel(t)
	press(m, "e", "f")
	for _, it := range m.modal.items {
		if it.Label == "Formula" {
			t.Fatal("formula fields are read-only")
		}
	}
}

func TestTabCyclesPanes(t *testing.T) {
	m, _ := editModel(t)
	press(m, "tab")
	if !m.focusReader || m.fieldKey != "assignee" {
		t.Fatalf("tab from list: focusReader = %v, fieldKey = %q", m.focusReader, m.fieldKey)
	}
	press(m, "j", "tab")
	if m.focusReader || m.fieldKey != "" {
		t.Fatalf("tab from reader: focusReader = %v, fieldKey = %q", m.focusReader, m.fieldKey)
	}
	press(m, "shift+tab")
	if !m.focusReader {
		t.Fatal("shift+tab from list should focus the reader")
	}
	m.Update(tea.WindowSizeMsg{Width: wideWidth, Height: 20})
	press(m, "tab")
	if !m.focusNav || m.focusReader {
		t.Fatalf("tab from reader on a wide screen: focusNav = %v, focusReader = %v", m.focusNav, m.focusReader)
	}
	press(m, "shift+tab")
	if !m.focusReader || m.focusNav {
		t.Fatalf("shift+tab from views: focusNav = %v, focusReader = %v", m.focusNav, m.focusReader)
	}
}

// cardLines renders the cards at width and checks each field target's
// recorded line holds that field's row.
func cardLines(t *testing.T, m *Model, tk ticket.Ticket, width int) []string {
	t.Helper()
	lines := strings.Split(ansi.Strip(m.renderCards(tk, width)), "\n")
	labels := map[string]string{"assignee": "Assignee", "project:p1": "Project", "my_tasks": "My Tasks", "field:f1": "Branch", commentKey: "Add comment"}
	for _, key := range m.fieldTargets(tk) {
		if l := lines[m.fieldLines[key]]; !strings.Contains(l, labels[key]) {
			t.Errorf("%s line %d = %q", key, m.fieldLines[key], l)
		}
	}
	return lines
}

func TestFieldLinesMatchRows(t *testing.T) {
	m, _ := editModel(t)
	tk := m.details["1"]
	cardLines(t, m, tk, 60)
	m.fieldKey = commentKey
	selected := strings.Split(ansi.Strip(m.renderCards(tk, 60)), "\n")
	line := m.fieldLines[commentKey]
	if !strings.Contains(selected[line-1], "Comments 0") || !strings.Contains(selected[line], "+ Add comment") {
		t.Fatalf("heading = %q, add row = %q", selected[line-1], selected[line])
	}
}

func TestWideCardsPutSubtasksBesideDetails(t *testing.T) {
	m, _ := editModel(t)
	tk := m.details["1"]
	tk.Subtasks = []asana.Task{{Name: "Repro", Completed: true}, {Name: "Patch"}}
	lines := cardLines(t, m, tk, 120)
	edge := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "Details") })
	if edge < 0 || !strings.Contains(lines[edge], "Subtasks ▰▰▱▱▱ 1/2") || !strings.Contains(lines[edge+1], "✓ Repro") {
		t.Fatalf("subtasks not beside details:\n%s", strings.Join(lines, "\n"))
	}
	if !slices.Contains(m.cardTargets, "section:subtasks") || !strings.Contains(lines[m.fieldLines["section:subtasks"]], "Subtasks") {
		t.Fatal("side-by-side subtasks heading is not selectable")
	}
}

func TestReaderFieldEdits(t *testing.T) {
	cases := []struct {
		name string
		do   func(m *Model)
		want string
	}{
		{"assign", func(m *Model) {
			press(m, "2", "enter", "down", "down", "enter")
		}, `PUT /tasks/1 {"data":{"assignee":"u2"}}`},
		{"project section", func(m *Model) {
			press(m, "2", "j", "enter", "down", "enter")
		}, `POST /sections/s2/addTask {"data":{"task":"1"}}`},
		{"due date", func(m *Model) {
			date := "2026-09-30"
			tk := m.details["1"]
			tk.DueOn = &date
			m.details["1"] = tk
			press(m, "2", "j", "enter")
			if got := m.input.area.Value(); got != date {
				t.Errorf("prefill = %q", got)
			}
			m.input.area.SetValue("2026-10-01")
			send(m, ctrlS)
		}, `PUT /tasks/1 {"data":{"due_on":"2026-10-01"}}`},
		{"my tasks section", func(m *Model) {
			press(m, "2", "j", "j", "enter", "down", "enter")
		}, `PUT /tasks/1 {"data":{"assignee_section":"m2"}}`},
		{"text field", func(m *Model) {
			press(m, "2", "j", "j", "j", "enter")
			if got := m.input.area.Value(); got != "old" {
				t.Errorf("prefill = %q", got)
			}
			m.input.area.SetValue("feat/x")
			send(m, ctrlS)
		}, `PUT /tasks/1 {"data":{"custom_fields":{"f1":"feat/x"}}}`},
		{"comment", func(m *Model) {
			press(m, "2", "G", "enter")
			m.input.area.SetValue("hello")
			send(m, ctrlS)
		}, `POST /tasks/1/stories {"data":{"text":"hello"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, writes := editModel(t)
			tc.do(m)
			if len(*writes) != 1 || strings.TrimSpace((*writes)[0]) != tc.want {
				t.Fatalf("writes = %q, want %q (status %q)", *writes, tc.want, m.status)
			}
		})
	}
}

func TestEscLeavesFields(t *testing.T) {
	m, _ := editModel(t)
	press(m, "2", "esc")
	if m.focusReader || m.fieldKey != "" {
		t.Fatalf("focusReader = %v, fieldKey = %q", m.focusReader, m.fieldKey)
	}
}

func TestTabTogglesInMarkdownView(t *testing.T) {
	m, _ := editModel(t)
	m.readerView = config.ViewMarkdown
	press(m, "tab")
	if !m.focusReader || m.fieldKey != "" {
		t.Fatalf("focusReader = %v, fieldKey = %q", m.focusReader, m.fieldKey)
	}
	press(m, "tab")
	if m.focusReader {
		t.Fatal("tab should return focus to the list")
	}
}
