package writeback

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
)

const taskJSON = `{"data":{"gid":"1","name":"Fix login",
 "memberships":[{"project":{"gid":"p1","name":"Web"}},{"project":{"gid":"p2","name":"API"}}],
 "custom_fields":[
  {"gid":"f1","name":"Branch Name ","resource_subtype":"text"},
  {"gid":"f2","name":"Points","resource_subtype":"number"},
  {"gid":"f3","name":"Stage","resource_subtype":"enum","enum_options":[{"gid":"o1","name":"Review"}]},
  {"gid":"f4","name":"When","resource_subtype":"date"}]}}`

type write struct {
	method, path string
	body         map[string]any
}

func fake(t *testing.T) (*asana.Client, *[]write) {
	t.Helper()
	var writes []write
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/1":
			io.WriteString(w, taskJSON)
		case r.Method == http.MethodGet && r.URL.Path == "/projects/p1/sections":
			io.WriteString(w, `{"data":[{"gid":"s1","name":"To Do"},{"gid":"s2","name":"In Review"}]}`)
		case r.Method != http.MethodGet:
			var body struct {
				Data map[string]any `json:"data"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			writes = append(writes, write{r.Method, r.URL.Path, body.Data})
			io.WriteString(w, `{"data":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := asana.New("tok")
	c.BaseURL = srv.URL
	return c, &writes
}

var ctx = context.Background()

func TestNeedsConfirm(t *testing.T) {
	cases := []struct {
		yes      bool
		env      string
		def, out bool
	}{
		{true, "1", true, false},
		{false, "0", true, false},
		{false, "1", false, true},
		{false, "", true, true},
		{false, "", false, false},
	}
	for _, c := range cases {
		if got := NeedsConfirm(c.yes, c.env, c.def); got != c.out {
			t.Errorf("NeedsConfirm(%v, %q, %v) = %v", c.yes, c.env, c.def, got)
		}
	}
}

func TestCommentPosts(t *testing.T) {
	c, writes := fake(t)
	if err := (Service{Client: c}).Comment(ctx, "1", "  PR: https://x  \n"); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0].path != "/tasks/1/stories" || (*writes)[0].body["text"] != "PR: https://x" {
		t.Fatalf("writes = %+v", *writes)
	}
}

func TestDeclineBlocksWrite(t *testing.T) {
	c, writes := fake(t)
	var asked string
	svc := Service{Client: c, Confirm: func(p string) (bool, error) { asked = p; return false, nil }}
	if err := svc.Comment(ctx, "1", "hi"); !errors.Is(err, ErrDeclined) {
		t.Fatalf("err = %v", err)
	}
	if len(*writes) != 0 || !strings.Contains(asked, "Fix login") {
		t.Fatalf("writes = %+v, prompt = %q", *writes, asked)
	}
}

func TestConfirmErrorBlocksWrite(t *testing.T) {
	c, writes := fake(t)
	svc := Service{Client: c, Confirm: func(string) (bool, error) { return false, ErrNoTTY }}
	if err := svc.SetField(ctx, "1", "points", "3"); !errors.Is(err, ErrNoTTY) {
		t.Fatalf("err = %v", err)
	}
	if len(*writes) != 0 {
		t.Fatalf("writes = %+v", *writes)
	}
}

func TestMoveNeedsProjectWhenAmbiguous(t *testing.T) {
	c, _ := fake(t)
	err := (Service{Client: c}).Move(ctx, "1", "In Review", "")
	if err == nil || !strings.Contains(err.Error(), "--project") || !strings.Contains(err.Error(), "Web (p1)") {
		t.Fatalf("err = %v", err)
	}
}

func TestMoveMatchesSectionCaseInsensitively(t *testing.T) {
	c, writes := fake(t)
	if err := (Service{Client: c}).Move(ctx, "1", "in review", "p1"); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0].path != "/sections/s2/addTask" || (*writes)[0].body["task"] != "1" {
		t.Fatalf("writes = %+v", *writes)
	}
	if err := (Service{Client: c}).Move(ctx, "1", "Shipped", "p1"); err == nil || !strings.Contains(err.Error(), "To Do") {
		t.Fatalf("err = %v, want list of sections", err)
	}
}

func TestSetFieldValues(t *testing.T) {
	cases := []struct {
		field, value string
		want         any
	}{
		{"branch name", "feat/x", "feat/x"},
		{"Points", "3.5", 3.5},
		{"stage", "review", "o1"},
		{"Points", "", nil},
	}
	for _, tc := range cases {
		c, writes := fake(t)
		if err := (Service{Client: c}).SetField(ctx, "1", tc.field, tc.value); err != nil {
			t.Fatalf("%s=%q: %v", tc.field, tc.value, err)
		}
		fields := (*writes)[0].body["custom_fields"].(map[string]any)
		for _, got := range fields {
			if got != tc.want {
				t.Errorf("%s=%q: sent %v, want %v", tc.field, tc.value, got, tc.want)
			}
		}
	}
}

func TestSetFieldRejectsBadInput(t *testing.T) {
	c, writes := fake(t)
	svc := Service{Client: c}
	for _, args := range [][2]string{{"Points", "many"}, {"Stage", "Shipped"}, {"When", "2026-01-01"}, {"Nope", "x"}} {
		if err := svc.SetField(ctx, "1", args[0], args[1]); err == nil {
			t.Errorf("SetField(%q, %q) succeeded", args[0], args[1])
		}
	}
	if len(*writes) != 0 {
		t.Fatalf("writes = %+v", *writes)
	}
}
