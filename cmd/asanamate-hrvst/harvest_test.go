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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sadmachine/asanamate/internal/feed"
	"github.com/sadmachine/asanamate/internal/form"
	"github.com/sadmachine/asanamate/internal/timetracking"
)

const assignmentJSON = `[{"is_active":true,"project":{"id":123,"name":"Web"},"task_assignments":[{"is_active":true,"task":{"id":456,"name":"Development"}}]},` +
	`{"is_active":true,"project":{"id":123,"name":"Web"},"task_assignments":[{"is_active":true,"task":{"id":456,"name":"Development"}}]},` +
	`{"is_active":true,"project":{"id":321,"name":"Ops"},"task_assignments":[{"is_active":true,"task":{"id":7,"name":"Support"}},{"is_active":false,"task":{"id":456,"name":"Development"}}]},` +
	`{"is_active":false,"project":{"id":999,"name":"Old"},"task_assignments":[{"is_active":true,"task":{"id":456,"name":"Development"}}]}]`

// Only active projects with the configured task are offered, under the
// task's real name.
func TestHarvestAssignmentOptions(t *testing.T) {
	var all assignments
	if err := json.Unmarshal([]byte(assignmentJSON), &all); err != nil {
		t.Fatal(err)
	}
	projects, task, err := all.options("456")
	if err != nil || len(projects) != 1 || projects[0] != (form.Option{ID: "123", Name: "Web"}) || task != (form.Option{ID: "456", Name: "Development"}) {
		t.Fatalf("projects=%+v task=%+v err=%v", projects, task, err)
	}
	if _, _, err := all.options("8"); err == nil || !strings.Contains(err.Error(), "Harvest task 8 is not active") {
		t.Fatalf("unassigned: %v", err)
	}
	if _, _, err := (assignments{}).options("456"); err == nil || !strings.Contains(err.Error(), "no active projects") {
		t.Fatalf("empty: %v", err)
	}
}

func TestHarvestAdapterUsesExternalReference(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	request := `{"operation":"log","values":{"project_id":"123","task_id":"456","hours":"1.25","spent_date":"2026-10-05"},"asana":{"task_gid":"1","project_gid":"2","title":"Fix login","url":"https://app.asana.com/0/2/1"}}`
	credentials := filepath.Join(dir, "config.json")
	if err := os.WriteFile(credentials, []byte(`{"accessToken":"test-token","accountId":"789"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Harvest-Account-ID") != "789" {
			t.Errorf("Harvest request headers = %+v", r.Header)
		}
		if r.Method == http.MethodGet {
			if r.URL.Path != "/users/me/project_assignments" {
				t.Errorf("unexpected path %s", r.URL.Path)
			}
			fmt.Fprintf(w, `{"project_assignments":%s,"next_page":null}`, assignmentJSON)
			return
		}
		var payload struct {
			ProjectID         string  `json:"project_id"`
			TaskID            string  `json:"task_id"`
			SpentDate         string  `json:"spent_date"`
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
		if payload.ProjectID != "123" || payload.TaskID != "456" || payload.SpentDate != "2026-10-05" || payload.Hours != 1.25 || payload.Notes != "Fix login" || payload.ExternalReference.ID != "1" || payload.ExternalReference.GroupID != "2" || payload.ExternalReference.Permalink != "https://app.asana.com/0/2/1" {
			t.Errorf("Harvest payload = %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":987,"external_reference":{"id":"1","permalink":"https://app.asana.com/0/2/1"}}`))
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := runHarvest(context.Background(), strings.NewReader(`{"operation":"form"}`), &out, "456", credentials, server.URL); err != nil {
		t.Fatal(err)
	}
	var spec form.Spec
	if err := json.Unmarshal(out.Bytes(), &spec); err != nil || len(spec.Fields) != 4 || spec.Fields[3].Type != form.Date || spec.Fields[0].Options[0].ID != "123" || spec.Fields[1].Options[0] != (form.Option{ID: "456", Name: "Development"}) || !spec.Fields[0].Remember || !spec.Fields[1].Remember {
		t.Fatalf("response = %s, err = %v", out.String(), err)
	}
	out.Reset()
	// The first suggested Harvest project that is offered becomes the default.
	if err := runHarvest(context.Background(), strings.NewReader(`{"operation":"form","links":[{"type":"harvest_project","value":"999"},{"type":"harvest_project","value":"123"}]}`), &out, "456", credentials, server.URL); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &spec); err != nil || spec.Fields[0].Default != "123" || spec.Fields[1].Default != "" {
		t.Fatalf("suggested response = %s, err = %v", out.String(), err)
	}
	if err := runHarvest(context.Background(), strings.NewReader(request), &out, "456", credentials, server.URL); err != nil {
		t.Fatal(err)
	}
	if err := runHarvest(context.Background(), strings.NewReader(request), &out, "", credentials, server.URL); err == nil {
		t.Fatal("missing default task ID accepted")
	}
	for _, date := range []string{"", "+1 day", "2026-02-30"} {
		bad := strings.Replace(request, `"2026-10-05"`, strconv.Quote(date), 1)
		if err := runHarvest(context.Background(), strings.NewReader(bad), &out, "456", credentials, "http://unused.invalid"); err == nil || !strings.Contains(err.Error(), "spent_date") {
			t.Errorf("spent_date %q: err = %v", date, err)
		}
	}
}

func TestHarvestRejectsCreatedEntryWithoutAsanaLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":987,"external_reference":null}`))
	}))
	defer server.Close()
	err := (harvestCredentials{AccessToken: "test-token", AccountID: "789"}).createEntry(context.Background(), server.URL, "123", "456", "2026-10-05", 1.25, timetracking.Asana{TaskGID: "1", ProjectGID: "2", URL: "https://app.asana.com/0/2/1"})
	if err == nil || !strings.Contains(err.Error(), "without the Asana link") || !strings.Contains(err.Error(), "do not submit again") {
		t.Fatalf("missing link error = %v", err)
	}
}

func TestHarvestSummaryFiltersTicketAndPaginates(t *testing.T) {
	today := time.Now().Format(time.DateOnly)
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
			fmt.Fprintf(w, `{"time_entries":[{"hours":1.25,"rounded_hours":2,"spent_date":%q,"is_running":false,"external_reference":{"id":"42","group_id":"1"}},{"hours":99,"spent_date":%q,"is_running":false,"external_reference":{"id":"99"}},{"hours":10,"spent_date":%q,"is_running":true,"external_reference":{"id":"42"}},{"hours":20,"spent_date":%q,"is_running":false,"external_reference":null}],"next_page":2}`, today, today, today, today)
		case "2":
			fmt.Fprint(w, `{"time_entries":[{"hours":2.5,"spent_date":"2000-01-01","is_running":false,"external_reference":{"id":"42","group_id":"2"}}],"next_page":null}`)
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
	if out.String() != "{\"total_seconds\":13500,\"today_seconds\":4500}\n" || strings.Join(pages, ",") != "1,2" {
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
		`{"time_entries":[{"hours":1e100,"spent_date":"2000-01-01","is_running":false,"external_reference":{"id":"42"}}],"next_page":null}`,
		`{"time_entries":[{"hours":1,"is_running":false,"external_reference":{"id":"42"}}],"next_page":null}`,
		`{"time_entries":[{"hours":1,"spent_date":"2026-02-30","is_running":false,"external_reference":{"id":"42"}}],"next_page":null}`,
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
			fmt.Fprint(w, `{"time_entries":[{"hours":1,"spent_date":"2000-01-01","is_running":false,"external_reference":{"id":"42"}}],"next_page":2}`)
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
		{`{"time_entries":[{"hours":0.0002777778,"spent_date":"2000-01-01","is_running":false,"external_reference":{"id":"42"}}],"next_page":null}`, 1},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.response) }))
		summary, err := harvestSummary(context.Background(), credentials, server.URL, "42")
		server.Close()
		if err != nil || summary.TotalSeconds != tc.seconds || summary.TodaySeconds == nil || *summary.TodaySeconds != 0 {
			t.Fatalf("summary = %+v, err = %v; want %d seconds", summary, err, tc.seconds)
		}
	}
}

func TestHarvestSummaryTodayAcrossPagesAndDates(t *testing.T) {
	credentials := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(credentials, []byte(`{"accessToken":"test-token","accountId":"789"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	today := now.Format(time.DateOnly)
	yesterday := now.AddDate(0, 0, -1).Format(time.DateOnly)
	tomorrow := now.AddDate(0, 0, 1).Format(time.DateOnly)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			fmt.Fprintf(w, `{"time_entries":[{"hours":1,"spent_date":%q,"is_running":false,"external_reference":{"id":"42","group_id":"1"}},{"hours":2,"spent_date":%q,"is_running":false,"external_reference":{"id":"42"}},{"hours":3,"spent_date":%q,"is_running":false,"external_reference":{"id":"42"}}],"next_page":2}`, today, yesterday, tomorrow)
		} else {
			fmt.Fprintf(w, `{"time_entries":[{"hours":0.0002777778,"spent_date":%q,"is_running":false,"external_reference":{"id":"42","group_id":"2"}}],"next_page":null}`, today)
		}
	}))
	defer server.Close()
	summary, err := harvestSummary(context.Background(), credentials, server.URL, "42")
	if err != nil || summary.TotalSeconds != 21601 || summary.TodaySeconds == nil || *summary.TodaySeconds != 3601 {
		t.Fatalf("summary = %+v, err = %v", summary, err)
	}
}

// The provider follows the protocol rule it documents: it ignores
// the version and any request fields it does not know.
func TestHarvestIgnoresVersionAndUnknownFields(t *testing.T) {
	var out bytes.Buffer
	request := `{"version":1,"operation":"nope","future":{"x":1}}`
	err := runHarvest(context.Background(), strings.NewReader(request), &out, "", "", "")
	if err == nil || !strings.Contains(err.Error(), `unknown time provider operation "nope"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestHarvestFeedListsTheDaysEntries(t *testing.T) {
	credentials := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(credentials, []byte(`{"accessToken":"test-token","accountId":"789"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/me" {
			fmt.Fprint(w, `{"id":555}`)
			return
		}
		query := r.URL.Query()
		if query.Get("user_id") != "555" || query.Get("from") != "2026-10-12" || query.Get("to") != "2026-10-12" {
			t.Errorf("feed filter = %v", query)
		}
		switch query.Get("page") {
		case "1":
			fmt.Fprint(w, `{"time_entries":[
				{"hours":1.5,"is_running":false,"notes":"Fix login","created_at":"2026-10-12T18:05:00Z","project":{"name":"MOCPA"},"external_reference":{"id":"42","permalink":"https://app.asana.com/0/7/42"}},
				{"hours":0.75,"is_running":false,"notes":"standup\nnotes","created_at":"2026-10-12T13:00:00Z","project":{"name":"Internal"},"external_reference":null}],"next_page":2}`)
		default:
			fmt.Fprint(w, `{"time_entries":[{"hours":0.9166,"is_running":true,"notes":"","created_at":"2026-10-12T19:00:00Z","project":{"name":"Fuse"}}],"next_page":null}`)
		}
	}))
	defer server.Close()
	reply, err := harvestFeed(context.Background(), credentials, server.URL, "2026-10-12")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Source != "hrvst" || reply.Summary != "3h 10m logged" || reply.Icon != "clock" || len(reply.Items) != 3 {
		t.Fatalf("reply = %+v", reply)
	}
	linked, unlinked, running := reply.Items[0], reply.Items[1], reply.Items[2]
	if linked.Text != "1.5h · MOCPA · Fix login" || linked.TicketGID != "42" || linked.URL != "https://app.asana.com/0/7/42" || linked.Tone != feed.ToneOK {
		t.Errorf("linked = %+v", linked)
	}
	if unlinked.Text != "0.75h · Internal · standup notes" || unlinked.TicketGID != "" {
		t.Errorf("unlinked = %+v", unlinked)
	}
	if running.Text != "running · 0.92h · Fuse" || running.Tone != feed.ToneWarn {
		t.Errorf("running = %+v", running)
	}
	if _, err := harvestFeed(context.Background(), credentials, server.URL, "today"); err == nil {
		t.Error("bad date accepted")
	}
}

func TestDurationLabel(t *testing.T) {
	for hours, want := range map[float64]string{0: "0m", 0.75: "45m", 1: "1h", 3.1666: "3h 10m"} {
		if got := durationLabel(hours); got != want {
			t.Errorf("durationLabel(%v) = %q, want %q", hours, got, want)
		}
	}
}
