package tui

import (
	"io"
	"net/http"
	"net/http/httptest"
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

func TestEditRejectsBadDate(t *testing.T) {
	m, writes := editModel(t)
	press(m, "e", "f", "down", "enter")
	m.input.area.SetValue("tomorrow")
	send(m, ctrlS)
	if len(*writes) != 0 || !strings.Contains(m.status, "YYYY-MM-DD") {
		t.Fatalf("writes = %q, status = %q", *writes, m.status)
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

func TestTabCyclesFields(t *testing.T) {
	m, _ := editModel(t)
	var got []string
	for range 6 {
		press(m, "tab")
		got = append(got, m.fieldKey)
	}
	press(m, "shift+tab", "shift+tab")
	got = append(got, m.fieldKey)
	want := []string{"assignee", "project:p1", "my_tasks", "field:f1", commentKey, "assignee", "field:f1"}
	if !m.focusReader || strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("keys = %q, want %q (focusReader %v)", got, want, m.focusReader)
	}
}

func TestFieldLinesMatchRows(t *testing.T) {
	m, _ := editModel(t)
	tk := m.details["1"]
	lines := strings.Split(ansi.Strip(m.renderCards(tk, 60)), "\n")
	labels := map[string]string{"assignee": "Assignee", "project:p1": "Project", "my_tasks": "My Tasks", "field:f1": "Branch", commentKey: "Comments"}
	for _, key := range fieldTargets(tk) {
		if l := lines[m.fieldLines[key]]; !strings.Contains(l, labels[key]) {
			t.Errorf("%s line %d = %q", key, m.fieldLines[key], l)
		}
	}
}

func TestTabFieldEdits(t *testing.T) {
	cases := []struct {
		name string
		do   func(m *Model)
		want string
	}{
		{"assign", func(m *Model) {
			press(m, "tab", "enter", "down", "down", "enter")
		}, `PUT /tasks/1 {"data":{"assignee":"u2"}}`},
		{"project section", func(m *Model) {
			press(m, "tab", "tab", "enter", "down", "enter")
		}, `POST /sections/s2/addTask {"data":{"task":"1"}}`},
		{"my tasks section", func(m *Model) {
			press(m, "tab", "tab", "tab", "enter", "down", "enter")
		}, `PUT /tasks/1 {"data":{"assignee_section":"m2"}}`},
		{"text field", func(m *Model) {
			press(m, "shift+tab", "shift+tab", "enter")
			if got := m.input.area.Value(); got != "old" {
				t.Errorf("prefill = %q", got)
			}
			m.input.area.SetValue("feat/x")
			send(m, ctrlS)
		}, `PUT /tasks/1 {"data":{"custom_fields":{"f1":"feat/x"}}}`},
		{"comment", func(m *Model) {
			press(m, "shift+tab", "enter")
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
	press(m, "tab", "esc")
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
