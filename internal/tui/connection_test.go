package tui

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
)

func connectionFailure() error {
	return &url.Error{Op: "Get", URL: "https://app.asana.com/api/1.0/tasks", Err: &net.DNSError{Err: "no such host", Name: "app.asana.com"}}
}

func openConnectionMenu(t *testing.T, m *Model) {
	t.Helper()
	m.Update(key(","))
	m.Update(key("c"))
	if m.modal == nil || m.modal.title != "Connection status" {
		t.Fatal("connection menu did not open from Settings & tools")
	}
}

func connectionMenuText(m *Model) string {
	var text strings.Builder
	for _, item := range m.modal.items {
		fmt.Fprintln(&text, item.Label, item.Help)
	}
	return text.String()
}

func TestConnectionFailureAndRecoveryStayOutOfNotifications(t *testing.T) {
	m := splitModel(t)
	now := time.Now()
	m.now = func() time.Time { return now }
	for range 3 {
		m.Update(tasksMsg{err: connectionFailure()})
	}
	if len(m.notices) != 0 || len(m.statusHistory) != 0 || m.status != "" || m.toast != nil {
		t.Fatal("background connection failures leaked into ordinary notifications")
	}
	openConnectionMenu(t, m)
	if text := connectionMenuText(m); !strings.Contains(text, "Reconnecting") || !strings.Contains(text, "3 failed attempts") || !strings.Contains(text, "no such host") {
		t.Fatalf("connection details = %s", text)
	}
	now = now.Add(time.Minute)
	m.Update(tasksMsg{err: connectionFailure()})
	if text := connectionMenuText(m); !strings.Contains(text, "Unavailable") || !strings.Contains(text, "4 failed attempts") {
		t.Fatalf("persistent outage = %s", text)
	}
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	if text := connectionMenuText(m); !strings.Contains(text, "Connected") || !strings.Contains(text, "Restored") || strings.Contains(text, "Asana: Unavailable") {
		t.Fatalf("recovery details = %s", text)
	}
	if len(m.notices) != 0 || len(m.statusHistory) != 0 {
		t.Fatal("recovery created an ordinary notification")
	}
}

func TestConnectionIndicatorAcrossSymbolSetsAndViews(t *testing.T) {
	for name, symbols := range symbolSets {
		t.Run(name, func(t *testing.T) {
			m := splitModel(t)
			m.sym = symbols
			now := testToday
			m.now = func() time.Time { return now }
			m.Update(tasksMsg{err: connectionFailure()})
			for _, view := range []string{"list", "ticket", "activity"} {
				m.deps.Ticket = ""
				m.activityOpen = view == "activity"
				if view == "ticket" {
					m.deps.Ticket = "1"
				}
				for _, width := range []int{24, 45, 120} {
					m.width = width
					bar := ansi.Strip(m.statusline())
					want := symbols.icon(iconNetwork) + symbols.connection[connectionReconnecting]
					if !strings.Contains(bar, want) || ansi.StringWidth(bar) > width {
						t.Fatalf("%s width=%d missing indicator %q: %s", view, width, want, bar)
					}
				}
			}
			now = now.Add(time.Minute)
			m.Update(connectionTickMsg{})
			if got := ansi.Strip(m.connectionIndicator()); got != symbols.icon(iconNetwork)+symbols.connection[connectionUnavailable] {
				t.Fatalf("persistent failure indicator = %q", got)
			}
		})
	}
}

func TestConnectionKeepsOtherFailuresVisible(t *testing.T) {
	for _, failure := range []error{
		errors.New("invalid configuration"),
		&asana.APIError{Status: 401, Message: "invalid token"},
		&asana.APIError{Status: 403, Message: "permission denied"},
		&url.Error{Op: "Get", URL: "https://app.asana.com", Err: errors.New("certificate is not trusted")},
	} {
		m := splitModel(t)
		m.Update(tasksMsg{err: failure})
		if len(m.notices) != 1 || len(m.statusHistory) != 1 {
			t.Fatalf("nonconnection error disappeared: %v", failure)
		}
	}
	m := splitModel(t)
	m.Update(editDoneMsg{what: "update ticket", err: connectionFailure()})
	if len(m.notices) != 1 || !strings.Contains(m.status, "update ticket failed") {
		t.Fatal("write failure must still demand attention")
	}
}

func TestAgentTimeoutDoesNotChangeAsanaConnection(t *testing.T) {
	m := splitModel(t)
	m.deps.Config.Agents.Command = "agents"
	m.Update(agentsMsg{err: fmt.Errorf("agents timed out: %w", context.DeadlineExceeded)})
	if len(m.notices) != 0 || len(m.statusHistory) != 0 {
		t.Fatal("agent polling timeout created ordinary notifications")
	}
	openConnectionMenu(t, m)
	text := connectionMenuText(m)
	if !strings.Contains(text, "Asana: Connected") || !strings.Contains(text, "Agents: Reconnecting") {
		t.Fatalf("independent service health = %s", text)
	}
}

func TestConnectionGraceAndBoundedOutages(t *testing.T) {
	m := splitModel(t)
	now := testToday
	m.now = func() time.Time { return now }
	var delay time.Duration
	m.connectionTimer = func(after time.Duration, msg connectionTickMsg) tea.Cmd {
		delay = after
		return func() tea.Msg { return msg }
	}
	m.Update(tasksMsg{err: connectionFailure()})
	if delay != 30*time.Second {
		t.Fatalf("grace redraw scheduled after %s", delay)
	}
	now = now.Add(10 * time.Second)
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	if len(m.asanaConnection.outages) != 0 {
		t.Fatal("brief network switch left an outage in history")
	}
	for range 25 {
		m.Update(tasksMsg{err: connectionFailure()})
		now = now.Add(time.Minute)
		m.Update(tasksMsg{tasks: []asana.Task{openTask}})
		now = now.Add(time.Minute)
	}
	if len(m.asanaConnection.outages) != 20 {
		t.Fatalf("retained %d outages, want latest 20", len(m.asanaConnection.outages))
	}
}

func TestConnectionRetryRunsWithoutClosingMenu(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/me/user_task_list" {
			fmt.Fprint(w, `{"data":{"gid":"7"}}`)
		} else {
			fmt.Fprint(w, `{"data":[]}`)
		}
	}))
	defer server.Close()
	m := splitModel(t)
	m.deps.Client = asana.New("test")
	m.deps.Client.BaseURL = server.URL
	m.Update(tasksMsg{err: connectionFailure()})
	openConnectionMenu(t, m)
	p := m.modal
	_, cmd := m.Update(key("r"))
	if cmd == nil || m.modal != p {
		t.Fatal("retry must run while keeping connection details open")
	}
	if _, duplicate := m.Update(key("r")); duplicate != nil {
		t.Fatal("retry overlapped an in-flight task refresh")
	}
	loaded := false
	for _, msg := range commandMessages(cmd) {
		if tasks, ok := msg.(tasksMsg); ok {
			loaded = true
			if tasks.err != nil {
				t.Fatal(tasks.err)
			}
			m.Update(tasks)
		}
	}
	if !loaded || m.modal != p || !strings.Contains(connectionMenuText(m), "Asana: Connected") {
		t.Fatal("successful retry did not update the open connection menu")
	}
}

func TestConnectionMenuAllowsAutomaticRecovery(t *testing.T) {
	m := splitModel(t)
	m.Update(tasksMsg{err: connectionFailure()})
	openConnectionMenu(t, m)
	m.Update(refreshTickMsg{seq: m.refreshSeq})
	if !m.loading {
		t.Fatal("connection menu paused automatic refresh")
	}
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	if !strings.Contains(connectionMenuText(m), "Asana: Connected") {
		t.Fatal("automatic recovery did not update the open menu")
	}
}

func TestConnectionErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		quiet bool
	}{
		{"DNS", connectionFailure(), true},
		{"deadline", fmt.Errorf("request: %w", context.DeadlineExceeded), true},
		{"truncated response", io.ErrUnexpectedEOF, true},
		{"server unavailable", &asana.APIError{Status: 503}, true},
		{"rate limited", &asana.APIError{Status: 429}, true},
		{"canceled", context.Canceled, false},
		{"TLS certificate", &url.Error{Err: x509.UnknownAuthorityError{}}, false},
		{"authentication", &asana.APIError{Status: 401}, false},
		{"permission", &asana.APIError{Status: 403}, false},
		{"invalid response", errors.New("invalid JSON"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := connectionFailureError(tc.err); got != tc.quiet {
				t.Fatalf("classified %v as connection failure: %v", tc.err, got)
			}
		})
	}
}

func TestPinnedTaskConnectionErrorUsesConnectionStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/me/user_task_list":
			fmt.Fprint(w, `{"data":{"gid":"7"}}`)
		case "/tasks/2":
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"errors":[{"message":"temporarily unavailable"}]}`)
		default:
			fmt.Fprint(w, `{"data":[]}`)
		}
	}))
	defer server.Close()
	client := asana.New("test")
	client.BaseURL = server.URL
	msg := loadTasks(client, "1", nil, []string{"2"})().(tasksMsg)
	if msg.err != nil || msg.warning != "" || msg.pinConnectionErr == nil {
		t.Fatalf("pinned failure escaped connection handling: %+v", msg)
	}
	m := splitModel(t)
	m.Update(msg)
	if len(m.notices) != 0 || len(m.statusHistory) != 0 || m.asanaConnection.active() == nil {
		t.Fatal("pinned task failure did not use connection status")
	}
}
