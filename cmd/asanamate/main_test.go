package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/writeback"
)

// fakeWriter is a writeback service backed by a fake Asana that creates task
// 9 and records each request body's data.
func fakeWriter(t *testing.T, bodies *[]map[string]any) func(bool) (writeback.Service, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Data map[string]any `json:"data"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		*bodies = append(*bodies, body.Data)
		io.WriteString(w, `{"data":{"gid":"9","name":"New"}}`)
	}))
	t.Cleanup(srv.Close)
	c := asana.New("tok")
	c.BaseURL = srv.URL
	return func(bool) (writeback.Service, error) {
		return writeback.Service{Client: c, Workspace: "w"}, nil
	}
}

func TestCreatePrintsOnlyTheGID(t *testing.T) {
	var bodies []map[string]any
	var out strings.Builder
	err := runCreate([]string{"--yes", "--notes", "-", "New"}, strings.NewReader("hi"), &out, fakeWriter(t, &bodies))
	if err != nil || out.String() != "9\n" {
		t.Fatalf("out = %q, err = %v", out.String(), err)
	}
	if len(bodies) != 1 || bodies[0]["assignee"] != "me" || bodies[0]["html_notes"] != "<body>hi</body>" {
		t.Fatalf("bodies = %v", bodies)
	}
}

func TestTitleAndDescriptionCLI(t *testing.T) {
	for _, tt := range []struct {
		command, input, stdin, key, want string
	}{
		{"title", " New title ", "", "name", "New title"},
		{"description", "**Details**", "", "html_notes", "<body><strong>Details</strong></body>"},
		{"description", "-", "**Details**", "html_notes", "<body><strong>Details</strong></body>"},
		{"description", "", "", "html_notes", "<body></body>"},
	} {
		t.Run(tt.command+tt.input, func(t *testing.T) {
			var bodies []map[string]any
			base := fakeWriter(t, &bodies)
			writer := func(yes bool) (writeback.Service, error) {
				if !yes {
					t.Error("--yes not forwarded")
				}
				return base(yes)
			}
			if err := runWrite(tt.command, []string{"--yes", "1", tt.input}, strings.NewReader(tt.stdin), writer); err != nil {
				t.Fatal(err)
			}
			if len(bodies) != 1 || len(bodies[0]) != 1 || bodies[0][tt.key] != tt.want {
				t.Fatalf("bodies = %+v", bodies)
			}
		})
	}
}

func TestTitleAndDescriptionCLIRejectsBadArguments(t *testing.T) {
	for _, name := range []string{"title", "description"} {
		for _, args := range [][]string{{}, {"1"}, {"1", "text", "extra"}, {"bad", "text"}} {
			writer := func(bool) (writeback.Service, error) {
				t.Fatal("invalid arguments loaded writer")
				return writeback.Service{}, nil
			}
			if err := runWrite(name, args, strings.NewReader(""), writer); err == nil {
				t.Errorf("%s accepted %q", name, args)
			}
		}
	}
}

func TestCreateRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"a", "b"},
		{"--project", "web", "New"},
		{"--parent", "1", "--project", "2", "New"},
	} {
		var bodies []map[string]any
		var out strings.Builder
		if err := runCreate(args, strings.NewReader(""), &out, fakeWriter(t, &bodies)); err == nil || out.Len() > 0 || len(bodies) > 0 {
			t.Errorf("%q: err = %v, out = %q, bodies = %v", args, err, out.String(), bodies)
		}
	}
}

func TestEditCommentCLI(t *testing.T) {
	for _, input := range []string{"**Edited**", "-"} {
		t.Run(input, func(t *testing.T) {
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/stories/9" {
					t.Errorf("path = %q", r.URL.Path)
				}
				if r.Method == http.MethodGet {
					io.WriteString(w, `{"data":{"type":"comment","is_editable":true}}`)
					return
				}
				writes++
				var body struct {
					Data map[string]any `json:"data"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPut || body.Data["html_text"] != "<body><strong>Edited</strong></body>" {
					t.Errorf("%s: %+v", r.Method, body)
				}
				io.WriteString(w, `{"data":{}}`)
			}))
			t.Cleanup(srv.Close)
			client := asana.New("tok")
			client.BaseURL = srv.URL
			writer := func(yes bool) (writeback.Service, error) {
				if !yes {
					t.Error("--yes not forwarded")
				}
				return writeback.Service{Client: client}, nil
			}
			if err := runWrite("edit-comment", []string{"--yes", "9", input}, strings.NewReader("**Edited**"), writer); err != nil || writes != 1 {
				t.Fatalf("err = %v, writes = %d", err, writes)
			}
		})
	}
}

func TestDeleteCommentCLI(t *testing.T) {
	for _, yes := range []bool{false, true} {
		writes := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/stories/9" {
				t.Errorf("path = %q", r.URL.Path)
			}
			if r.Method == http.MethodGet {
				io.WriteString(w, `{"data":{"type":"comment","is_editable":true}}`)
				return
			}
			writes++
			if r.Method != http.MethodDelete {
				t.Errorf("method = %s", r.Method)
			}
			io.WriteString(w, `{"data":{}}`)
		}))
		t.Cleanup(srv.Close)
		client := asana.New("tok")
		client.BaseURL = srv.URL
		writer := func(gotYes bool) (writeback.Service, error) {
			if gotYes != yes {
				t.Errorf("yes = %v, want %v", gotYes, yes)
			}
			return writeback.Service{Client: client}, nil
		}
		args := []string{"9"}
		if yes {
			args = append([]string{"--yes"}, args...)
		}
		if err := runWrite("delete-comment", args, strings.NewReader(""), writer); err != nil || writes != 1 {
			t.Fatalf("err = %v, writes = %d", err, writes)
		}
	}
}

func TestDeleteCommentCLIRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"9", "extra"}, {"bad"}, {"../9"}} {
		writer := func(bool) (writeback.Service, error) {
			t.Fatal("invalid arguments loaded writer")
			return writeback.Service{}, nil
		}
		if err := runWrite("delete-comment", args, strings.NewReader(""), writer); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
}

func TestEditCommentCLIRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"9"}, {"9", "text", "extra"}, {"bad", "text"}} {
		writer := func(bool) (writeback.Service, error) {
			t.Fatal("invalid arguments loaded writer")
			return writeback.Service{}, nil
		}
		if err := runWrite("edit-comment", args, strings.NewReader(""), writer); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
}

func TestCheckFeedsRunsEachFeed(t *testing.T) {
	var out bytes.Buffer
	checkFeeds(context.Background(), &out, []string{
		`printf '%s' '{"source":"ci","summary":"1 failed","items":[{"time":"2026-10-12T09:00:00Z","text":"build"}]}'`,
		"exit 3",
	})
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "ok: ") || !strings.HasSuffix(lines[0], "ci, 1 item today, 1 failed") || !strings.Contains(lines[1], "warning: ") {
		t.Fatalf("doctor feeds:\n%s", out.String())
	}
	out.Reset()
	checkFeeds(context.Background(), &out, nil)
	if !strings.Contains(out.String(), "none: set feed.commands") {
		t.Fatalf("no feeds = %q", out.String())
	}
}

func TestReadNeedsOneTaskGID(t *testing.T) {
	for _, args := range [][]string{nil, {"1", "2"}, {"abc"}} {
		if err := runRead(args); err == nil {
			t.Fatalf("read %v: no error", args)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := runRead([]string{"--exit-on-action", "1"}); !errors.Is(err, config.ErrNotConfigured) {
		t.Fatalf("read error = %v", err)
	}
}
