package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadmachine/asanamate/internal/feed"
	"github.com/sadmachine/asanamate/internal/projectlinks"
	"github.com/sadmachine/asanamate/internal/timetracking"
)

// TestMain runs the test binary as asanamate-hrvst when TestProviderProtocol
// starts it as a provider command, pointed at a fake Harvest.
func TestMain(m *testing.M) {
	if endpoint := os.Getenv("ASANAMATE_HRVST_TEST_ENDPOINT"); endpoint != "" {
		harvestEndpoint = endpoint
		os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
	dir, err := os.MkdirTemp("", "harvest-tests-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	os.Unsetenv("HARVEST_ACCESS_TOKEN")
	os.Unsetenv("HARVEST_ACCOUNT_ID")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// TestProviderProtocol runs asanamate-hrvst as time_tracking.command through
// asanamate's provider client, like the protocol test in
// internal/timetracking: form, log, then summary.
func TestProviderProtocol(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprintf("native=%v", native), func(t *testing.T) { exerciseProviderProtocol(t, native) })
	}
}

func exerciseProviderProtocol(t *testing.T, native bool) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, ".hrvst"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".hrvst", "config.json"), []byte(`{"accessToken":"test-token","accountId":"789"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if native {
		t.Setenv("HARVEST_ACCESS_TOKEN", "test-token")
		t.Setenv("HARVEST_ACCOUNT_ID", "789")
		writeProviderSettings(t, "task_id = \"456\"\n")
	}
	var logged atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/me/project_assignments" {
			fmt.Fprintf(w, `{"project_assignments":%s,"next_page":null}`, assignmentJSON)
			return
		}
		if r.Method == http.MethodPost {
			var payload harvestEntryRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.ProjectID != "123" || payload.TaskID != "456" || payload.SpentDate != "2026-10-05" || payload.Hours != 1.25 || payload.ExternalReference.ID != "42" {
				t.Errorf("Harvest payload = %+v, err = %v", payload, err)
			}
			logged.Store(true)
			fmt.Fprint(w, `{"id":987,"external_reference":{"id":"42","permalink":"https://app.asana.com/0/7/42"}}`)
			return
		}
		if r.URL.Path == "/users/me" {
			fmt.Fprint(w, `{"id":555}`)
			return
		}
		fmt.Fprintf(w, `{"time_entries":[{"hours":3.75,"spent_date":%q,"is_running":false,"created_at":"2026-10-12T18:05:00Z","project":{"name":"Web"},"external_reference":{"id":"42"}}],"next_page":null}`, time.Now().Format(time.DateOnly))
	}))
	defer server.Close()
	t.Setenv("PATH", dir)
	t.Setenv("HOME", home)
	t.Setenv("ASANAMATE_HRVST_TEST_ENDPOINT", server.URL)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := "'" + exe + "'"
	if !native {
		command += " --task-id 456"
	}
	provider := timetracking.Provider{Command: command}
	ctx := context.Background()
	asana := timetracking.Asana{TaskGID: "42", ProjectGID: "7", Title: "Fix login", URL: "https://app.asana.com/0/7/42"}

	spec, err := provider.Form(ctx, asana, []projectlinks.Link{{Type: projectlinks.HarvestProject, Value: "123"}})
	if err != nil || len(spec.Fields) != 4 || spec.Fields[0].Options[0].ID != "123" || spec.Fields[0].Default != "123" {
		t.Fatalf("form = %+v, err = %v", spec, err)
	}
	if err := provider.Log(ctx, spec, map[string]string{"project_id": "123", "task_id": "456", "hours": "1.25", "spent_date": "2026-10-05"}, asana); err != nil || !logged.Load() {
		t.Fatalf("log err = %v, logged = %v", err, logged.Load())
	}
	summary, err := provider.Summary(ctx, asana)
	if err != nil || summary.TotalSeconds != 13500 || summary.TodaySeconds == nil || *summary.TodaySeconds != 13500 {
		t.Fatalf("summary = %+v, err = %v", summary, err)
	}
	// The same command answers the feed protocol.
	reply, err := feed.Fetch(ctx, provider.Command, time.Now())
	if err != nil || reply.Source != "hrvst" || reply.Summary != "3h 45m logged" || len(reply.Items) != 1 || reply.Items[0].TicketGID != "42" {
		t.Fatalf("feed = %+v, err = %v", reply, err)
	}
}

func TestRunRejectsArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--task-id", "456", "extra"}, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "unexpected arguments") {
		t.Fatalf("code = %d, stderr = %s", code, errOut.String())
	}
	if code := run([]string{"-h"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "asanamate-hrvst check") {
		t.Fatalf("help code = %d, stdout = %s", code, out.String())
	}
}
