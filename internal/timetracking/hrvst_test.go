package timetracking

import (
	"bytes"
	"context"
	"encoding/json"
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
	capture := filepath.Join(dir, "args")
	projectCall := filepath.Join(dir, "project-args")
	body := "#!/bin/sh\nif [ \"$1\" = users ]; then printf '%s\\n' \"$@\" > '" + projectCall + "'; echo 'update warning' >&2; printf '%s' '" + assignmentJSON + "'; else printf '%s\\n' \"$@\" > '" + capture + "'; fi\n"
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
	if err := RunHarvest(context.Background(), strings.NewReader(request), &out, "456"); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(capture)
	for _, want := range []string{"time-entries\ncreate\n", "--project_id\n123\n", "--task_id\n456\n", "--hours\n1.25\n", "--notes\nFix login\n", "--external_reference[id]\n1\n", "--external_reference[group_id]\n2\n", "--external_reference[permalink]\nhttps://app.asana.com/0/2/1\n"} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("args = %s; missing %q", args, want)
		}
	}
	if err := RunHarvest(context.Background(), strings.NewReader(request), &out, ""); err == nil {
		t.Fatal("missing default task ID accepted")
	}
}
