package timetracking

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/form"
)

const assignmentJSON = `[{"is_active":true,"project":{"id":123,"name":"Web"}},{"is_active":true,"project":{"id":123,"name":"Web"}},{"is_active":false,"project":{"id":999,"name":"Old"}}]`

func TestParseHarvestAssignments(t *testing.T) {
	projects, err := parseAssignments([]byte(assignmentJSON))
	if err != nil || len(projects) != 1 || projects[0] != (form.Option{ID: "123", Name: "Web"}) {
		t.Fatalf("projects = %+v, err = %v", projects, err)
	}
	if _, err := parseAssignments([]byte("not JSON")); err == nil {
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
	if err := json.Unmarshal(out.Bytes(), &spec); err != nil || len(spec.Fields) != 3 || spec.Fields[0].Options[0].ID != "123" || spec.Fields[1].Options[0].ID != "456" || !spec.Fields[0].Remember || !spec.Fields[1].Remember {
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
