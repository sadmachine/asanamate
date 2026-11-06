package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, response string) (Source, string) {
	t.Helper()
	dir := t.TempDir()
	request, reply := filepath.Join(dir, "request.json"), filepath.Join(dir, "response.json")
	t.Setenv("REQUEST_FILE", request)
	t.Setenv("RESPONSE_FILE", reply)
	if err := os.WriteFile(reply, []byte(response), 0o600); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("testdata/source.sh")
	if err != nil {
		t.Fatal(err)
	}
	return Source{Command: "/bin/sh '" + script + "'"}, request
}

const validRecord = `{"provider":"codex","session_id":"native-id","task_gid":"42","title":"Saved [asana:42]","activity":"unknown"}`

func TestSourceFixture(t *testing.T) {
	var record map[string]any
	if err := json.Unmarshal([]byte(validRecord), &record); err != nil {
		t.Fatal(err)
	}
	record["cwd"] = "/deleted/worktree"
	record["branch"] = "feature/sorting"
	record["updated_at"] = "2026-10-15T16:00:00Z"
	record["archived"] = true
	record["future"] = map[string]bool{"ignored": true}
	body, _ := json.Marshal(map[string]any{"version": 1, "conversations": []any{record, record}, "future": true,
		"diagnostics": []any{map[string]string{"provider": "claude", "code": "unavailable", "severity": "info", "message": "History is not installed"}}})
	source, request := fixture(t, string(body))
	result, err := source.List(context.Background(), "42")
	if err != nil || len(result.Conversations) != 1 || !result.Complete() || len(result.Diagnostics) != 1 {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	r := result.Conversations[0]
	if r.SessionID != "native-id" || r.CWD != "/deleted/worktree" || !r.Archived || r.Branch != "feature/sorting" || r.UpdatedAt != "2026-10-15T16:00:00Z" {
		t.Fatalf("record = %+v", r)
	}
	data, err := os.ReadFile(request)
	if err != nil || string(data) != `{"version":1,"operation":"list","asana":{"task_gid":"42"}}` {
		t.Fatalf("request = %s, error = %v", data, err)
	}
}

func TestSourceEmptyAndActivity(t *testing.T) {
	source, _ := fixture(t, `{"version":1,"conversations":[]}`)
	result, err := source.List(context.Background(), "42")
	if err != nil || result.Conversations == nil || len(result.Conversations) != 0 || !result.Complete() {
		t.Fatalf("complete empty lookup = %+v, error = %v", result, err)
	}
	for _, activity := range []string{"active", "inactive", "unknown"} {
		source, _ := fixture(t, `{"version":1,"conversations":[`+strings.Replace(validRecord, "unknown", activity, 1)+`]}`)
		result, err := source.List(context.Background(), "42")
		if err != nil || len(result.Conversations) != 1 || result.Conversations[0].Activity != activity || result.Conversations[0].Archived {
			t.Fatalf("activity = %s, result = %+v, error = %v", activity, result, err)
		}
	}
}

func TestSourceRejectsMalformedResults(t *testing.T) {
	for name, body := range map[string]string{
		"version":          `{"version":2,"conversations":[]}`,
		"missing rows":     `{"version":1}`,
		"null rows":        `{"version":1,"conversations":null}`,
		"not object":       `[]`,
		"extra object":     `{"version":1,"conversations":[]} {}`,
		"bad record":       `{"version":1,"conversations":[false]}`,
		"null record":      `{"version":1,"conversations":[null]}`,
		"null diagnostics": `{"version":1,"conversations":[],"diagnostics":null}`,
		"bad diagnostic":   `{"version":1,"conversations":[],"diagnostics":[{"provider":"claude","code":"unavailable","message":"missing"}]}`,
		"bad severity":     `{"version":1,"conversations":[],"diagnostics":[{"provider":"claude","code":"unavailable","severity":"fatal","message":"missing"}]}`,
		"bad code":         `{"version":1,"conversations":[],"diagnostics":[{"provider":"claude","code":"oops","severity":"error","message":"missing"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			source, _ := fixture(t, body)
			if _, err := source.List(context.Background(), "42"); err == nil {
				t.Fatal("malformed response accepted")
			}
		})
	}
	for key, values := range map[string][]any{
		"provider": {nil, "", "claude\x00"}, "session_id": {nil, "", 12, "id\n"},
		"task_gid": {nil, "../42", "43", 42}, "title": {nil, false},
		"activity": {nil, "idle", ""}, "cwd": {nil, "relative", ""},
		"updated_at": {nil, "yesterday", ""}, "archived": {nil, "yes"},
		"branch": {nil, 123},
	} {
		for _, value := range values {
			t.Run(key, func(t *testing.T) {
				var record map[string]any
				json.Unmarshal([]byte(validRecord), &record)
				record[key] = value
				body, _ := json.Marshal(map[string]any{"version": 1, "conversations": []any{record}})
				source, _ := fixture(t, string(body))
				if _, err := source.List(context.Background(), "42"); err == nil {
					t.Fatalf("accepted %s = %#v", key, value)
				}
			})
		}
	}
	for _, key := range []string{"provider", "session_id", "task_gid", "title", "activity"} {
		t.Run("missing "+key, func(t *testing.T) {
			var record map[string]any
			json.Unmarshal([]byte(validRecord), &record)
			delete(record, key)
			body, _ := json.Marshal(map[string]any{"version": 1, "conversations": []any{record}})
			source, _ := fixture(t, string(body))
			if _, err := source.List(context.Background(), "42"); err == nil {
				t.Fatal("missing required field accepted")
			}
		})
	}
}

func TestSourceConflictingIdentity(t *testing.T) {
	other := strings.Replace(validRecord, "Saved", "Changed", 1)
	source, _ := fixture(t, `{"version":1,"conversations":[`+validRecord+`,`+other+`]}`)
	if _, err := source.List(context.Background(), "42"); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("error = %v", err)
	}
	other = strings.Replace(validRecord, "codex", "claude", 1)
	source, _ = fixture(t, `{"version":1,"conversations":[`+validRecord+`,`+other+`]}`)
	if result, err := source.List(context.Background(), "42"); err != nil || len(result.Conversations) != 2 {
		t.Fatalf("provider-scoped identity: result = %+v, error = %v", result, err)
	}
}

func TestSourceDiagnosticsAndDisplay(t *testing.T) {
	for _, severity := range []string{"info", "warning", "error"} {
		body, _ := json.Marshal(map[string]any{"version": 1, "conversations": []any{}, "diagnostics": []any{
			map[string]string{"provider": "claude", "code": "incomplete", "severity": severity, "message": "\x1b[31mFailure\x1b[0m\n" + strings.Repeat("x", 1500)},
		}})
		source, _ := fixture(t, string(body))
		result, err := source.List(context.Background(), "42")
		if err != nil || result.Complete() != (severity == "info") {
			t.Fatalf("result = %+v, error = %v", result, err)
		}
		message := result.Diagnostics[0].Message
		if strings.ContainsAny(message, "\x1b\n") || len([]rune(message)) > MaxDiagnosticMessage || !strings.HasPrefix(message, "Failure ") {
			t.Fatalf("unsafe or unbounded message = %q", message)
		}
	}
}

func TestSourceFailureAndCancellation(t *testing.T) {
	source, request := fixture(t, `{"version":1,"conversations":[]}`)
	for _, gid := range []string{"", "../42", "42; touch bad"} {
		if _, err := source.List(context.Background(), gid); err == nil {
			t.Fatal("invalid GID accepted")
		}
	}
	if _, err := os.Stat(request); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid request launched source")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.List(ctx, "42"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := (Source{Command: "sleep 60"}).List(ctx, "42"); !errors.Is(err, context.DeadlineExceeded) || Failure(err).Code != "incomplete" {
		t.Fatalf("error = %v", err)
	}
	for _, command := range []string{"", "printf 'unavailable' >&2; exit 3", "printf '%4194305d' 0", "printf '%65537d' 0 >&2"} {
		if _, err := (Source{Command: command}).List(context.Background(), "42"); err == nil {
			t.Fatal("command failure accepted")
		}
	}
}

func TestFailureClassifiesWholeLookupErrors(t *testing.T) {
	cases := map[string]string{
		"printf 'no history' >&2; exit 3":           "unavailable",
		"printf '%4194305d' 0":                      "incomplete",
		`printf '{"version":2,"conversations":[]}'`: "unsupported_version",
		`printf '{"version":1}'`:                    "provider_error",
	}
	for command, code := range cases {
		_, err := (Source{Command: "cat >/dev/null; " + command}).List(context.Background(), "42")
		d := Failure(err)
		if err == nil || d.Code != code || d.Provider != SourceProvider || d.Severity != "error" || Guidance(d.Code) == "" {
			t.Fatalf("%s: diagnostic = %+v, error = %v", command, d, err)
		}
	}
}

func TestSortAndFind(t *testing.T) {
	records := []Record{
		{Provider: "codex", SessionID: "b"},
		{Provider: "codex", SessionID: "old", UpdatedAt: "2026-10-01T00:00:00Z"},
		{Provider: "claude", SessionID: "z"},
		{Provider: "codex", SessionID: "a", UpdatedAt: "2026-10-15T00:00:00+02:00"},
		{Provider: "claude", SessionID: "a", UpdatedAt: "2026-10-14T22:00:00Z"},
	}
	Sort(records)
	var got []string
	for _, r := range records {
		got = append(got, r.Provider+"/"+r.SessionID)
	}
	if strings.Join(got, " ") != "claude/a codex/a codex/old claude/z codex/b" {
		t.Fatalf("order = %v", got)
	}
	result := Result{Conversations: records}
	if r, ok := result.Find("codex", "a"); !ok || r.UpdatedAt == "" {
		t.Fatal("exact identity not found")
	}
	if _, ok := result.Find("claude", "old"); ok {
		t.Fatal("identity matched across providers")
	}
}
