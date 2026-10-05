package timetracking

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/form"
)

const sampleForm = `{"fields":[{"id":"project_id","label":"Project","type":"select","options":[{"id":"opaque","name":"Project"}],"remember":true},{"id":"hours","label":"Hours","type":"hours"}]}`

func TestCommandProtocol(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")
	script := filepath.Join(t.TempDir(), "provider.sh")
	body := "#!/bin/sh\ncat > '" + path + "'\nprintf '%s' '" + sampleForm + "'\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	provider := Provider{Command: script}
	spec, err := provider.Form(context.Background())
	if err != nil || len(spec.Fields) != 2 || spec.Fields[0].Options[0].ID != "opaque" {
		t.Fatalf("form = %+v, err = %v", spec, err)
	}
	request, _ := os.ReadFile(path)
	if !strings.Contains(string(request), `"operation":"form"`) {
		t.Fatalf("request = %s", request)
	}
	values := map[string]string{"project_id": "opaque", "hours": "1.25"}
	if err := provider.Log(context.Background(), spec, values, Asana{TaskGID: "1", ProjectGID: "2", Title: "Fix", URL: "https://app.asana.com/0/2/1"}); err != nil {
		t.Fatal(err)
	}
	request, _ = os.ReadFile(path)
	for _, want := range []string{`"operation":"log"`, `"project_id":"opaque"`, `"hours":"1.25"`, `"task_gid":"1"`, `"project_gid":"2"`} {
		if !strings.Contains(string(request), want) {
			t.Fatalf("request = %s; missing %s", request, want)
		}
	}
}

func TestProviderRejectsBadDataAndFailures(t *testing.T) {
	for _, response := range []string{`{}`, `{"fields":[{"id":"x","label":"X","type":"select","options":[]}]}`, `bad`} {
		provider := Provider{Command: "printf '%s' '" + response + "'"}
		if _, err := provider.Form(context.Background()); err == nil {
			t.Fatalf("response %s should fail", response)
		}
	}
	provider := Provider{Command: "echo unavailable >&2; exit 1"}
	if _, err := provider.Form(context.Background()); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("error = %v", err)
	}
	spec := form.Spec{Fields: []form.Field{{ID: "hours", Label: "Hours", Type: form.Hours}}}
	if err := provider.Log(context.Background(), spec, map[string]string{"hours": "0"}, Asana{}); err == nil {
		t.Fatal("zero hours accepted")
	}
}

func TestSummaryProtocol(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")
	provider := Provider{Command: "cat > '" + path + "'; printf '%s' '{\"total_seconds\":13500}'"}
	summary, err := provider.Summary(context.Background(), Asana{TaskGID: "42", URL: "https://app.asana.com/0/1/42"})
	if err != nil || summary.TotalSeconds != 13500 {
		t.Fatalf("summary = %+v, err = %v", summary, err)
	}
	request, _ := os.ReadFile(path)
	if !strings.Contains(string(request), `"operation":"summary"`) || !strings.Contains(string(request), `"task_gid":"42"`) || strings.Contains(string(request), `"values"`) {
		t.Fatalf("request = %s", request)
	}
}

func TestSummaryRejectsInvalidResponses(t *testing.T) {
	for _, response := range []string{`{}`, `null`, `bad`, `{"total_seconds":null}`, `{"total_seconds":-1}`, `{"total_seconds":1.5}`, `{"total_seconds":9223372036854775808}`} {
		provider := Provider{Command: "printf '%s' '" + response + "'"}
		if _, err := provider.Summary(context.Background(), Asana{TaskGID: "42"}); err == nil {
			t.Fatalf("invalid summary accepted: %s", response)
		}
	}
	provider := Provider{Command: "printf '%s' '{\"total_seconds\":0}'"}
	if summary, err := provider.Summary(context.Background(), Asana{TaskGID: "42"}); err != nil || summary.TotalSeconds != 0 {
		t.Fatalf("zero summary = %+v, err = %v", summary, err)
	}
	if _, err := provider.Summary(context.Background(), Asana{}); err == nil {
		t.Fatal("missing ticket accepted")
	}
	provider.Command = "printf '%s' '{\"unsupported\":true}'"
	if _, err := provider.Summary(context.Background(), Asana{TaskGID: "42"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported error = %v", err)
	}
	provider.Command = "echo unknown operation >&2; exit 1"
	if _, err := provider.Summary(context.Background(), Asana{TaskGID: "42"}); err == nil {
		t.Fatal("legacy provider failure treated as zero")
	}
}
