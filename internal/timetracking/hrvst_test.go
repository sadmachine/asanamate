package timetracking

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
	"testing"

	"github.com/sadmachine/asanamate/internal/form"
)

const assignmentJSON = `[{"is_active":true,"project":{"id":123,"name":"Web"},"task_assignments":[{"is_active":true,"task":{"id":456,"name":"Development"}}]},` +
	`{"is_active":true,"project":{"id":123,"name":"Web"},"task_assignments":[{"is_active":true,"task":{"id":456,"name":"Development"}}]},` +
	`{"is_active":true,"project":{"id":321,"name":"Ops"},"task_assignments":[{"is_active":true,"task":{"id":7,"name":"Support"}},{"is_active":false,"task":{"id":456,"name":"Development"}}]},` +
	`{"is_active":false,"project":{"id":999,"name":"Old"},"task_assignments":[{"is_active":true,"task":{"id":456,"name":"Development"}}]}]`

// Only active projects with the configured task are offered, under the
// task's real name.
func TestParseHarvestAssignments(t *testing.T) {
	projects, task, err := parseAssignments([]byte(assignmentJSON), "456")
	if err != nil || len(projects) != 1 || projects[0] != (form.Option{ID: "123", Name: "Web"}) || task != (form.Option{ID: "456", Name: "Development"}) {
		t.Fatalf("projects = %+v, task = %+v, err = %v", projects, task, err)
	}
	if _, _, err := parseAssignments([]byte(assignmentJSON), "8"); err == nil || !strings.Contains(err.Error(), "Harvest task 8 is not active") {
		t.Fatalf("unassigned task err = %v", err)
	}
	if _, _, err := parseAssignments([]byte(`[]`), "456"); err == nil || !strings.Contains(err.Error(), "no active projects") {
		t.Fatalf("no assignments err = %v", err)
	}
	if _, _, err := parseAssignments([]byte("not JSON"), "456"); err == nil {
		t.Fatal("unrecognized assignment output accepted")
	}
}

func TestHarvestAdapterUsesExternalReference(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hrvst")
	projectCall := filepath.Join(dir, "project-args")
	body := "#!/bin/sh\nif [ \"$1\" = users ]; then printf '%s\\n' \"$@\" > '" + projectCall + "'; echo 'update warning' >&2; printf '%s' '" + assignmentJSON + "'; else exit 1; fi\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	var out bytes.Buffer
	if err := RunHarvest(context.Background(), strings.NewReader(`{"operation":"form"}`), &out, "456"); err != nil {
		t.Fatal(err)
	}
	var spec form.Spec
	if err := json.Unmarshal(out.Bytes(), &spec); err != nil || len(spec.Fields) != 3 || spec.Fields[0].Options[0].ID != "123" || spec.Fields[1].Options[0] != (form.Option{ID: "456", Name: "Development"}) || !spec.Fields[0].Remember || !spec.Fields[1].Remember {
		t.Fatalf("response = %s, err = %v", out.String(), err)
	}
	projectArgs, _ := os.ReadFile(projectCall)
	if string(projectArgs) != "users\nproject-assignments\nme\n--output=json\n--page=all\n" {
		t.Fatalf("assignment command args = %s", projectArgs)
	}
	request := `{"operation":"log","values":{"project_id":"123","task_id":"456","hours":"1.25"},"asana":{"task_gid":"1","project_gid":"2","title":"Fix login","url":"https://app.asana.com/0/2/1"}}`
	credentials := filepath.Join(dir, "config.json")
	if err := os.WriteFile(credentials, []byte(`{"accessToken":"test-token","accountId":"789"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Harvest-Account-ID") != "789" {
			t.Errorf("Harvest request headers = %+v", r.Header)
		}
		var payload struct {
			ProjectID         string  `json:"project_id"`
			TaskID            string  `json:"task_id"`
			Hours             float64 `json:"hours"`
			Notes             string  `json:"notes"`
			ExternalReference struct {
				ID        string `json:"id"`
				GroupID   string `json:"group_id"`
				Permalink string `json:"permalink"`
			} `json:"external_reference"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.ProjectID != "123" || payload.TaskID != "456" || payload.Hours != 1.25 || payload.Notes != "Fix login" || payload.ExternalReference.ID != "1" || payload.ExternalReference.GroupID != "2" || payload.ExternalReference.Permalink != "https://app.asana.com/0/2/1" {
			t.Errorf("Harvest payload = %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":987,"external_reference":{"id":"1","permalink":"https://app.asana.com/0/2/1"}}`))
	}))
	defer server.Close()
	if err := runHarvest(context.Background(), strings.NewReader(request), &out, "456", credentials, server.URL); err != nil {
		t.Fatal(err)
	}
	if err := runHarvest(context.Background(), strings.NewReader(request), &out, "", credentials, server.URL); err == nil {
		t.Fatal("missing default task ID accepted")
	}
}

func TestHarvestRejectsCreatedEntryWithoutAsanaLink(t *testing.T) {
	dir := t.TempDir()
	credentials := filepath.Join(dir, "config.json")
	if err := os.WriteFile(credentials, []byte(`{"accessToken":"test-token","accountId":"789"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":987,"external_reference":null}`))
	}))
	defer server.Close()
	err := createHarvestEntry(context.Background(), credentials, server.URL, "123", "456", 1.25, Asana{TaskGID: "1", ProjectGID: "2", URL: "https://app.asana.com/0/2/1"})
	if err == nil || !strings.Contains(err.Error(), "without the Asana link") || !strings.Contains(err.Error(), "do not submit again") {
		t.Fatalf("missing link error = %v", err)
	}
}

func TestHarvestSummaryFiltersTicketAndPaginates(t *testing.T) {
	credentials := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(credentials, []byte(`{"accessToken":"test-token","accountId":"789"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Harvest-Account-ID") != "789" {
			t.Errorf("unexpected request: %s %+v", r.Method, r.Header)
		}
		query := r.URL.Query()
		if query.Get("external_reference_id") != "42" || query.Get("is_running") != "false" || query.Get("per_page") != "100" || query.Has("project_id") || query.Has("from") || query.Has("user_id") {
			t.Errorf("summary filter = %v", query)
		}
		pages = append(pages, query.Get("page"))
		switch query.Get("page") {
		case "1":
			fmt.Fprint(w, `{"time_entries":[{"hours":1.25,"rounded_hours":2,"is_running":false,"external_reference":{"id":"42","group_id":"1"}},{"hours":99,"is_running":false,"external_reference":{"id":"99"}},{"hours":10,"is_running":true,"external_reference":{"id":"42"}},{"hours":20,"is_running":false,"external_reference":null}],"next_page":2}`)
		case "2":
			fmt.Fprint(w, `{"time_entries":[{"hours":2.5,"is_running":false,"external_reference":{"id":"42","group_id":"2"}}],"next_page":null}`)
		default:
			t.Errorf("unexpected page: %v", query)
			http.Error(w, "bad page", 400)
		}
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := runHarvest(context.Background(), strings.NewReader(`{"operation":"summary","asana":{"task_gid":"42"}}`), &out, "", credentials, server.URL); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{\"total_seconds\":13500}\n" || strings.Join(pages, ",") != "1,2" {
		t.Fatalf("summary = %s, pages = %v", out.String(), pages)
	}
}

func TestHarvestSummaryRejectsPartialOrInvalidData(t *testing.T) {
	credentials := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(credentials, []byte(`{"accessToken":"test-token","accountId":"789"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, response := range []string{
		`{}`, `bad`, `{"time_entries":[]}`,
		`{"time_entries":[],"next_page":1}`,
		`{"time_entries":[{"hours":-1,"is_running":false,"external_reference":{"id":"42"}}],"next_page":null}`,
		`{"time_entries":[{"is_running":false,"external_reference":{"id":"42"}}],"next_page":null}`,
		`{"time_entries":[{"hours":1,"external_reference":{"id":"42"}}],"next_page":null}`,
		`{"time_entries":[{"hours":1e100,"is_running":false,"external_reference":{"id":"42"}}],"next_page":null}`,
	} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, response) }))
			defer server.Close()
			if _, err := harvestSummary(context.Background(), credentials, server.URL, "42"); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			fmt.Fprint(w, `{"time_entries":[{"hours":1,"is_running":false,"external_reference":{"id":"42"}}],"next_page":2}`)
		} else {
			http.Error(w, "denied", http.StatusForbidden)
		}
	}))
	defer server.Close()
	if _, err := harvestSummary(context.Background(), credentials, server.URL, "42"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("partial result returned: %v", err)
	}
}

func TestHarvestSummaryZeroAndUnitConversion(t *testing.T) {
	credentials := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(credentials, []byte(`{"accessToken":"test-token","accountId":"789"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		response string
		seconds  int64
	}{
		{`{"time_entries":[],"next_page":null}`, 0},
		{`{"time_entries":[{"hours":0.0002777778,"is_running":false,"external_reference":{"id":"42"}}],"next_page":null}`, 1},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.response) }))
		summary, err := harvestSummary(context.Background(), credentials, server.URL, "42")
		server.Close()
		if err != nil || summary.TotalSeconds != tc.seconds {
			t.Fatalf("summary = %+v, err = %v; want %d seconds", summary, err, tc.seconds)
		}
	}
}
