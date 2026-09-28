package asana

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var ctx = context.Background()

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("tok")
	c.BaseURL = srv.URL
	c.sleep = func(time.Duration) {}
	return c
}

func TestAuthHeaderAndErrorMessage(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"errors":[{"message":"Not a member"}]}`)
	})
	_, err := c.Me(ctx)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 403 || apiErr.Message != "Not a member" {
		t.Fatalf("err = %v", err)
	}
}

func TestPaginationFollowsOffset(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "100" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		switch r.URL.Query().Get("offset") {
		case "":
			io.WriteString(w, `{"data":[{"gid":"1","name":"a"}],"next_page":{"offset":"p2"}}`)
		case "p2":
			io.WriteString(w, `{"data":[{"gid":"2","name":"b"}],"next_page":null}`)
		default:
			t.Errorf("unexpected offset %q", r.URL.Query().Get("offset"))
		}
	})
	secs, err := c.Sections(ctx, "9")
	if err != nil || len(secs) != 2 || secs[1].Name != "b" {
		t.Fatalf("secs = %+v, err = %v", secs, err)
	}
}

func TestRetriesOnceAfter429(t *testing.T) {
	calls := 0
	var slept time.Duration
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `{"data":{"gid":"1","name":"Me"}}`)
	})
	c.sleep = func(d time.Duration) { slept = d }
	me, err := c.Me(ctx)
	if err != nil || me.Name != "Me" || calls != 2 || slept != 2*time.Second {
		t.Fatalf("me = %+v, err = %v, calls = %d, slept = %v", me, err, calls, slept)
	}
}

func TestMyTasksResolvesTaskList(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/me/user_task_list":
			if r.URL.Query().Get("workspace") != "77" {
				t.Errorf("workspace = %q", r.URL.Query().Get("workspace"))
			}
			io.WriteString(w, `{"data":{"gid":"55"}}`)
		case "/user_task_lists/55/tasks":
			if r.URL.Query().Get("completed_since") == "" {
				t.Error("missing completed_since")
			}
			if !strings.Contains(r.URL.Query().Get("opt_fields"), "custom_fields.display_value") {
				t.Error("list must fetch custom fields for list display")
			}
			io.WriteString(w, `{"data":[{"gid":"1","name":"Fix","assignee_section":{"gid":"s","name":"Today"},"memberships":[{"project":{"gid":"p","name":"Web"},"section":{"gid":"x","name":"Doing"}}]}]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	tasks, err := c.MyTasks(ctx, "77", time.Now())
	if err != nil || len(tasks) != 1 || tasks[0].SectionFor("") != "Today" || tasks[0].SectionFor("p") != "Doing" || tasks[0].SectionIn("zz") != "" {
		t.Fatalf("tasks = %+v, err = %v", tasks, err)
	}
}

func TestMemberProjectsFiltersByUser(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("archived") != "false" {
			t.Errorf("archived = %q", r.URL.Query().Get("archived"))
		}
		io.WriteString(w, `{"data":[{"gid":"a","name":"Mine","members":[{"gid":"me"}]},{"gid":"b","name":"Other","members":[{"gid":"x"}]}]}`)
	})
	projects, err := c.MemberProjects(ctx, "w", "me")
	if err != nil || len(projects) != 1 || projects[0].Name != "Mine" {
		t.Fatalf("projects = %+v, err = %v", projects, err)
	}
}

func TestCommentsKeepsOnlyComments(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[{"gid":"1","type":"system"},{"gid":"2","type":"comment","html_text":"<body>hi</body>"}]}`)
	})
	comments, err := c.Comments(ctx, "9")
	if err != nil || len(comments) != 1 || comments[0].GID != "2" {
		t.Fatalf("comments = %+v, err = %v", comments, err)
	}
}

func TestWritesWrapBodyInData(t *testing.T) {
	var got map[string]map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tasks/1/stories" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, `{"data":{}}`)
	})
	if err := c.AddComment(ctx, "1", "hi"); err != nil {
		t.Fatal(err)
	}
	if got["data"]["text"] != "hi" {
		t.Fatalf("body = %v", got)
	}
}

func TestValidGID(t *testing.T) {
	for s, want := range map[string]bool{"123": true, "": false, "12a": false, "../1": false} {
		if ValidGID(s) != want {
			t.Errorf("ValidGID(%q) = %v", s, !want)
		}
	}
}
