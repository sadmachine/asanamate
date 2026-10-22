package asana

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
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
	c.sleep = func(context.Context, time.Duration) error { return nil }
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
	c.sleep = func(_ context.Context, d time.Duration) error { slept = d; return nil }
	me, err := c.Me(ctx)
	if err != nil || me.Name != "Me" || calls != 2 || slept != 2*time.Second {
		t.Fatalf("me = %+v, err = %v, calls = %d, slept = %v", me, err, calls, slept)
	}
}

func TestRateLimitWaitStopsWhenContextEnds(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c.sleep = sleep
	timeout, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Me(timeout)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("err = %v after %v; want the deadline error without waiting out Retry-After", err, time.Since(start))
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

func TestStoriesWithoutSinceLoadsWholeTimeline(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Has("created_after") || !strings.Contains(q.Get("opt_fields"), "is_editable") || !strings.Contains(q.Get("opt_fields"), "reaction_summary") {
			t.Errorf("unexpected query %s", r.URL)
		}
		io.WriteString(w, `{"data":[{"gid":"1","type":"system"},{"gid":"2","type":"comment","is_editable":true,"html_text":"<body>hi</body>"}]}`)
	})
	stories, err := c.Stories(ctx, "9", time.Time{})
	if err != nil || len(stories) != 2 || stories[1].GID != "2" || !stories[1].IsEditable {
		t.Fatalf("stories = %+v, err = %v", stories, err)
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

func TestUploadAttachmentSendsMultipart(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nrest")
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/attachments" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if r.FormValue("parent") != "1" {
			t.Errorf("parent = %q", r.FormValue("parent"))
		}
		f, h, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(f)
		if h.Filename != "shot.png" || h.Header.Get("Content-Type") != "image/png" || string(data) != string(png) {
			t.Errorf("file = %q %q %q", h.Filename, h.Header.Get("Content-Type"), data)
		}
		io.WriteString(w, `{"data":{"gid":"7","name":"shot.png"}}`)
	})
	a, err := c.UploadAttachment(ctx, "1", "shot.png", png)
	if err != nil || a.GID != "7" {
		t.Fatalf("attachment = %+v, err = %v", a, err)
	}
}

func TestTaskUpdatesSendFields(t *testing.T) {
	var got map[string]map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/tasks/1" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		got = nil
		json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, `{"data":{}}`)
	})
	cases := []struct {
		write func() error
		want  map[string]any
	}{
		{func() error { return c.SetAssignee(ctx, "1", "u1") }, map[string]any{"assignee": "u1"}},
		{func() error { return c.SetAssignee(ctx, "1", "") }, map[string]any{"assignee": nil}},
		{func() error { return c.SetMyTasksSection(ctx, "1", "s1") }, map[string]any{"assignee_section": "s1"}},
		{func() error { return c.SetCompleted(ctx, "1", true) }, map[string]any{"completed": true}},
		{func() error { return c.SetCompleted(ctx, "1", false) }, map[string]any{"completed": false}},
	}
	for i, tc := range cases {
		if err := tc.write(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got["data"], tc.want) {
			t.Errorf("case %d: body = %v, want %v", i, got["data"], tc.want)
		}
	}
}

func TestCreateTasksSendFields(t *testing.T) {
	var path string
	var got map[string]map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Query().Get("opt_fields") == "" {
			t.Errorf("%s %s", r.Method, r.URL)
		}
		path, got = r.URL.Path, nil
		json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, `{"data":{"gid":"9","name":"New"}}`)
	})
	cases := []struct {
		create   func() (Task, error)
		wantPath string
		want     map[string]any
	}{
		{func() (Task, error) {
			return c.CreateTask(ctx, NewTask{Name: "New", Workspace: "w", Assignee: "me", DueOn: "2026-01-02", HTMLNotes: "<body>x</body>"})
		}, "/tasks", map[string]any{"name": "New", "workspace": "w", "assignee": "me", "due_on": "2026-01-02", "html_notes": "<body>x</body>"}},
		{func() (Task, error) { return c.CreateTask(ctx, NewTask{Name: "New", Project: "p1"}) },
			"/tasks", map[string]any{"name": "New", "projects": []any{"p1"}}},
		{func() (Task, error) { return c.CreateTask(ctx, NewTask{Name: "New", Project: "p1", Section: "s1"}) },
			"/tasks", map[string]any{"name": "New", "projects": []any{"p1"}, "memberships": []any{map[string]any{"project": "p1", "section": "s1"}}}},
		{func() (Task, error) { return c.CreateSubtask(ctx, "1", NewTask{Name: "New"}) },
			"/tasks/1/subtasks", map[string]any{"name": "New"}},
	}
	for i, tc := range cases {
		task, err := tc.create()
		if err != nil || task.GID != "9" {
			t.Fatalf("case %d: task = %+v, err = %v", i, task, err)
		}
		if path != tc.wantPath || !reflect.DeepEqual(got["data"], tc.want) {
			t.Errorf("case %d: %s %v, want %s %v", i, path, got["data"], tc.wantPath, tc.want)
		}
	}
}

func TestValidGID(t *testing.T) {
	for s, want := range map[string]bool{"123": true, "": false, "12a": false, "../1": false} {
		if ValidGID(s) != want {
			t.Errorf("ValidGID(%q) = %v", s, !want)
		}
	}
}

func TestTaskFieldPrefersProjectThenValue(t *testing.T) {
	v := func(s string) *string { return &s }
	task := Task{CustomFields: []CustomField{
		{GID: "a", Name: "Branch Name ", DisplayValue: v("")},
		{GID: "b", Name: "branch name", DisplayValue: v("feat/b")},
		{GID: "c", Name: "Other", DisplayValue: v("x")},
	}}
	if f, ok := task.Field("Branch Name", nil); !ok || f.GID != "b" {
		t.Fatalf("no preference: want first with a value (b), got %+v", f)
	}
	if f, _ := task.Field("BRANCH NAME", map[string]bool{"a": true}); f.GID != "a" {
		t.Fatalf("project preference: want a, got %+v", f)
	}
	if _, ok := task.Field("missing", nil); ok {
		t.Fatal("missing field found")
	}
}

func TestProjectFieldGIDs(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/9/custom_field_settings" {
			t.Errorf("path %s", r.URL.Path)
		}
		io.WriteString(w, `{"data":[{"custom_field":{"gid":"f1"}},{"custom_field":{"gid":"f2"}}]}`)
	})
	got, err := c.ProjectFieldGIDs(ctx, "9")
	if err != nil || !got["f1"] || !got["f2"] || len(got) != 2 {
		t.Fatalf("got %v, err %v", got, err)
	}
}

func TestProjectFieldsReturnsNamesWithoutTaskValues(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		want       []Ref
	}{
		{"configured", `[{"custom_field":{"gid":"f1","name":"Priority"}},{"custom_field":{"gid":"f2","name":"Priority"}}]`, []Ref{{GID: "f1", Name: "Priority"}, {GID: "f2", Name: "Priority"}}},
		{"no fields", `[]`, []Ref{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/projects/9/custom_field_settings" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if !strings.Contains(r.URL.Query().Get("opt_fields"), "custom_field.name") {
					t.Error("metadata request lacks field names")
				}
				io.WriteString(w, `{"data":`+tc.data+`}`)
			})
			got, err := c.ProjectFields(ctx, "9")
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("fields = %v, err = %v, want %v", got, err, tc.want)
			}
		})
	}
}

// TestTaskGIDFromURL shares its cases with asana-sessions, so the two parsers
// cannot drift apart.
func TestTaskGIDFromURL(t *testing.T) {
	data, err := os.ReadFile("../../packages/asana-sessions/test/fixtures/task-urls.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ URL, GID string }
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) == 0 {
		t.Fatalf("fixtures: %v", err)
	}
	for _, tc := range cases {
		if got := TaskGIDFromURL(tc.URL); got != tc.GID {
			t.Errorf("TaskGIDFromURL(%q) = %q, want %q", tc.URL, got, tc.GID)
		}
	}
}
