package writeback

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/dateinput"
	"github.com/sadmachine/asanamate/internal/ticket"
)

const taskJSON = `{"data":{"gid":"1","name":"Fix login",
 "memberships":[{"project":{"gid":"p1","name":"Web"}},{"project":{"gid":"p2","name":"API"}}],
 "custom_fields":[
  {"gid":"f1","name":"Branch Name ","resource_subtype":"text"},
  {"gid":"f2","name":"Points","resource_subtype":"number"},
  {"gid":"f3","name":"Stage","resource_subtype":"enum","enum_options":[{"gid":"o1","name":"Review"}]},
  {"gid":"f4","name":"When","resource_subtype":"date"},
  {"gid":"f5","name":"Areas","resource_subtype":"multi_enum","enum_options":[{"gid":"o2","name":"Frontend"},{"gid":"o3","name":"Backend"}]},
  {"gid":"f6","name":"Reviewers","resource_subtype":"people"}]}}`

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
		case r.Method == http.MethodGet && r.URL.Path == "/stories/9":
			io.WriteString(w, `{"data":{"gid":"9","type":"comment","is_editable":true,"text":"Original","created_by":{"gid":"7","name":"Amy"},"target":{"gid":"1"}}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/projects/p1/sections":
			io.WriteString(w, `{"data":[{"gid":"s1","name":"To Do"},{"gid":"s2","name":"In Review"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/2":
			io.WriteString(w, `{"data":{"gid":"2","name":"Mine","assignee_section":{"gid":"m1","name":"Inbox"}}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/users/me/user_task_list":
			io.WriteString(w, `{"data":{"gid":"mt"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/projects/mt/sections":
			io.WriteString(w, `{"data":[{"gid":"m1","name":"Inbox"},{"gid":"m2","name":"Later"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/1/stories":
			io.WriteString(w, `{"data":[{"gid":"st1","resource_subtype":"attachment_added","text":"Ann attached shot.png"},{"gid":"st2","resource_subtype":"attachment_added","text":"Ann attached other.png"},{"gid":"st3","resource_subtype":"comment_added","text":"shot.png"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/projects/p3":
			io.WriteString(w, `{"data":{"gid":"p3","name":"Ops"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/attachments":
			_, h, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			writes = append(writes, write{r.Method, r.URL.Path, map[string]any{"parent": r.FormValue("parent"), "name": h.Filename}})
			io.WriteString(w, `{"data":{"gid":"a`+strconv.Itoa(len(writes))+`"}}`)
		case r.Method != http.MethodGet:
			var body struct {
				Data map[string]any `json:"data"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			writes = append(writes, write{r.Method, r.URL.Path, body.Data})
			io.WriteString(w, `{"data":{"gid":"new"}}`)
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

func TestEditTitleAndDescription(t *testing.T) {
	c, writes := fake(t)
	var question string
	s := Service{Client: c, Confirm: func(p string) (bool, error) { question = p; return true, nil }}
	if err := s.SetTitle(ctx, "1", "  New title  "); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(question, "Fix login") || !strings.Contains(question, "New title") {
		t.Fatalf("confirmation = %q", question)
	}
	original := ticket.Ticket{Task: asana.Task{HTMLNotes: `<body><strong>Before</strong> <a data-asana-gid="7" data-asana-type="user">Amy</a><img data-asana-gid="123" src="https://expired.test/image"/></body>`}}
	text := strings.Replace(original.DescriptionMarkdown(), "Before", "After", 1)
	if err := s.SetDescription(ctx, "1", text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(question, "Fix login") || !strings.Contains(question, "After") {
		t.Fatalf("confirmation = %q", question)
	}
	if err := s.SetDescription(ctx, "1", ""); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 3 {
		t.Fatalf("writes = %+v", *writes)
	}
	for _, w := range *writes {
		if w.method != http.MethodPut || w.path != "/tasks/1" || len(w.body) != 1 {
			t.Fatalf("must update only the requested field: %+v", w)
		}
	}
	if (*writes)[0].body["name"] != "New title" || (*writes)[2].body["html_notes"] != "<body></body>" {
		t.Fatalf("writes = %+v", *writes)
	}
	html := (*writes)[1].body["html_notes"].(string)
	for _, want := range []string{"<strong>After</strong>", `<a data-asana-gid="7"/>`, `<img data-asana-gid="123"/>`} {
		if !strings.Contains(html, want) {
			t.Errorf("description = %q, missing %q", html, want)
		}
	}
}

func TestEditTitleAndDescriptionRejectBeforeWriting(t *testing.T) {
	c, writes := fake(t)
	s := Service{Client: c}
	for _, title := range []string{"", " \n ", "two\nlines", "two\rlines"} {
		if err := s.SetTitle(ctx, "1", title); err == nil {
			t.Errorf("accepted title %q", title)
		}
	}
	if err := s.SetDescription(ctx, "1", "![local](missing.png)"); err == nil {
		t.Error("silently accepted an unsupported image upload")
	}
	s.Confirm = func(string) (bool, error) { return false, nil }
	if err := s.SetTitle(ctx, "1", "New"); !errors.Is(err, ErrDeclined) {
		t.Errorf("title decline = %v", err)
	}
	if err := s.SetDescription(ctx, "1", ""); !errors.Is(err, ErrDeclined) {
		t.Errorf("description decline = %v", err)
	}
	if len(*writes) != 0 {
		t.Fatalf("writes = %+v", *writes)
	}
}

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

func TestDeleteComment(t *testing.T) {
	for _, mode := range []string{"no confirmation", "confirmed", "declined", "confirmation error"} {
		t.Run(mode, func(t *testing.T) {
			c, writes := fake(t)
			svc := Service{Client: c}
			var question string
			confirmErr := errors.New("confirmation failed")
			if mode != "no confirmation" {
				svc.Confirm = func(p string) (bool, error) {
					question = p
					if mode == "confirmation error" {
						return false, confirmErr
					}
					return mode == "confirmed", nil
				}
			}
			err := svc.DeleteComment(ctx, "9")
			if mode == "declined" || mode == "confirmation error" {
				want := ErrDeclined
				if mode == "confirmation error" {
					want = confirmErr
				}
				if !errors.Is(err, want) || len(*writes) != 0 {
					t.Fatalf("err = %v, writes = %+v", err, *writes)
				}
			} else if err != nil || len(*writes) != 1 || (*writes)[0].method != http.MethodDelete || (*writes)[0].path != "/stories/9" {
				t.Fatalf("err = %v, writes = %+v", err, *writes)
			}
			if mode != "no confirmation" && (!strings.Contains(question, "Amy") || !strings.Contains(question, "Original")) {
				t.Fatalf("confirmation = %q", question)
			}
		})
	}
}

func TestDeleteCommentChecksPermissionAndPropagatesErrors(t *testing.T) {
	for _, tc := range []struct {
		name, story, failedMethod, wantError string
		wantWrites                           int
	}{
		{"not editable", `{"type":"comment","is_editable":false}`, "", "cannot be deleted", 0},
		{"system story", `{"type":"system","is_editable":true}`, "", "cannot be deleted", 0},
		{"read denied", "", http.MethodGet, "Denied", 0},
		{"delete denied", `{"type":"comment","is_editable":true}`, http.MethodDelete, "Denied", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes++
				}
				if r.Method == tc.failedMethod {
					w.WriteHeader(http.StatusForbidden)
					io.WriteString(w, `{"errors":[{"message":"Denied"}]}`)
					return
				}
				io.WriteString(w, `{"data":`+tc.story+`}`)
			}))
			t.Cleanup(srv.Close)
			c := asana.New("tok")
			c.BaseURL = srv.URL
			if err := (Service{Client: c}).DeleteComment(ctx, "9"); err == nil || !strings.Contains(err.Error(), tc.wantError) || writes != tc.wantWrites {
				t.Fatalf("err = %v, writes = %d", err, writes)
			}
		})
	}
}

func TestEditCommentPreservesStoryAndRichContent(t *testing.T) {
	c, writes := fake(t)
	original := asana.Story{HTMLText: `<body><strong>Before</strong> <a data-asana-gid="7" data-asana-type="user" href="https://app.asana.com/0/7/list">Amy</a><img data-asana-gid="123" src="https://expired.test/image"/></body>`}
	text := strings.Replace(ticket.CommentMarkdown(original), "Before", "After", 1)
	var question string
	svc := Service{Client: c, Confirm: func(p string) (bool, error) { question = p; return true, nil }}
	if err := svc.EditComment(ctx, "9", "  "+text+"\n"); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 {
		t.Fatalf("writes = %+v", *writes)
	}
	w := (*writes)[0]
	if w.method != http.MethodPut || w.path != "/stories/9" || len(w.body) != 1 {
		t.Fatalf("edit must update only the existing story's text: %+v", w)
	}
	html, _ := w.body["html_text"].(string)
	for _, want := range []string{"<strong>After</strong>", `<a data-asana-gid="7"/>`, `<img data-asana-gid="123"/>`} {
		if !strings.Contains(html, want) {
			t.Errorf("html = %q, missing %q (Markdown %q)", html, want, text)
		}
	}
	if !strings.Contains(question, "Amy") || !strings.Contains(question, "Original") || !strings.Contains(question, "After") {
		t.Fatalf("confirmation = %q", question)
	}
}

func TestEditCommentDeclineAndEmptyDoNotWrite(t *testing.T) {
	c, writes := fake(t)
	svc := Service{Client: c, Confirm: func(string) (bool, error) { return false, nil }}
	if err := svc.EditComment(ctx, "9", "new"); !errors.Is(err, ErrDeclined) {
		t.Fatalf("decline = %v", err)
	}
	if err := svc.EditComment(ctx, "9", " \n "); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty = %v", err)
	}
	if len(*writes) != 0 {
		t.Fatalf("writes = %+v", *writes)
	}
}

func TestEditCommentChecksCurrentPermission(t *testing.T) {
	for _, story := range []string{
		`{"type":"comment","is_editable":false}`,
		`{"type":"system","is_editable":true}`,
	} {
		t.Run(story, func(t *testing.T) {
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes++
				}
				io.WriteString(w, `{"data":`+story+`}`)
			}))
			t.Cleanup(srv.Close)
			c := asana.New("tok")
			c.BaseURL = srv.URL
			svc := Service{Client: c, Images: map[string][]byte{"shot.png": []byte("\x89PNG\r\n\x1a\nimage")}}
			if err := svc.EditComment(ctx, "9", "![](shot.png)"); err == nil || !strings.Contains(err.Error(), "cannot be edited") || writes != 0 {
				t.Fatalf("err = %v, writes = %d", err, writes)
			}
		})
	}
}

func TestEditCommentUploadsImagesToItsTask(t *testing.T) {
	c, writes := fake(t)
	svc := Service{Client: c, Images: map[string][]byte{"shot.png": []byte("\x89PNG\r\n\x1a\nimage")}}
	if err := svc.EditComment(ctx, "9", "![](asana:123) ![](shot.png)"); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 3 || (*writes)[0].path != "/attachments" || (*writes)[0].body["parent"] != "1" || (*writes)[1].method != http.MethodPut || (*writes)[1].path != "/stories/9" || (*writes)[2].path != "/stories/st1" {
		t.Fatalf("writes = %+v", *writes)
	}
	html := (*writes)[1].body["html_text"].(string)
	if !strings.Contains(html, `data-asana-gid="123"`) || !strings.Contains(html, `data-asana-gid="a1"`) {
		t.Fatalf("images = %q", html)
	}
}

func TestCommentSavedImages(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(strconv.FormatBool(valid), func(t *testing.T) {
			c, writes := fake(t)
			image := []byte("\x89PNG\r\n\x1a\nimage")
			if !valid {
				image = []byte("private text must never upload")
			}
			svc := Service{Client: c, Images: map[string][]byte{"/missing/image.png": image}}
			err := svc.Comment(ctx, "1", "![](/missing/image.png)")
			if valid {
				if err != nil || len(*writes) != 2 || (*writes)[0].path != "/attachments" || !strings.Contains((*writes)[1].body["html_text"].(string), `data-asana-gid="a1"`) {
					t.Fatalf("saved image: %v, %+v", err, *writes)
				}
			} else if err == nil || len(*writes) != 0 {
				t.Fatalf("invalid saved image uploaded: %v, %+v", err, *writes)
			}
		})
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
	if err := svc.SetField(ctx, "1", "points", "3", ""); !errors.Is(err, ErrNoTTY) {
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

func TestMoveBySectionGID(t *testing.T) {
	c, writes := fake(t)
	if err := (Service{Client: c}).Move(ctx, "1", "s2", "p1"); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0].path != "/sections/s2/addTask" {
		t.Fatalf("writes = %+v", *writes)
	}
}

func TestMoveWithinMyTasks(t *testing.T) {
	c, writes := fake(t)
	svc := Service{Client: c, Workspace: "w"}
	if err := svc.Move(ctx, "2", "later", MyTasks); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0].path != "/tasks/2" || (*writes)[0].body["assignee_section"] != "m2" {
		t.Fatalf("writes = %+v", *writes)
	}
	if err := svc.Move(ctx, "2", "Someday", MyTasks); err == nil || !strings.Contains(err.Error(), "My Tasks; sections: Inbox, Later") {
		t.Fatalf("unknown section: err = %v", err)
	}
	if err := svc.Move(ctx, "1", "Inbox", MyTasks); err == nil || !strings.Contains(err.Error(), "only its assignee") {
		t.Fatalf("someone else's task: err = %v", err)
	}
	if len(*writes) != 1 {
		t.Fatalf("writes = %+v", *writes)
	}
}

func TestAssign(t *testing.T) {
	users := []asana.Ref{{GID: "9", Name: "Ann Lee"}, {GID: "8", Name: "Bo"}}
	for user, want := range map[string]any{"Bo": "8", " ann lee ": "9", "9": "9", "me": "me", "": nil} {
		c, writes := fake(t)
		if err := (Service{Client: c, Users: users}).Assign(ctx, "1", user); err != nil {
			t.Fatalf("Assign(%q): %v", user, err)
		}
		if len(*writes) != 1 || (*writes)[0].path != "/tasks/1" || (*writes)[0].body["assignee"] != want {
			t.Errorf("Assign(%q): writes = %+v, want assignee %v", user, *writes, want)
		}
	}
}

func TestAssignRejectsUnknownOrAmbiguousUsers(t *testing.T) {
	c, writes := fake(t)
	svc := Service{Client: c, Users: []asana.Ref{{GID: "9", Name: "Ann"}, {GID: "7", Name: "ann"}, {GID: "8", Name: "Bo"}}}
	for user, want := range map[string]string{"Ann": "several", "Cy": "no user"} {
		if err := svc.Assign(ctx, "1", user); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Assign(%q) = %v, want error containing %q", user, err, want)
		}
	}
	if len(*writes) != 0 {
		t.Fatalf("writes = %+v", *writes)
	}
}

func TestSetDue(t *testing.T) {
	for date, want := range map[string]any{"2026-10-01": "2026-10-01", " 2026-10-01\n": "2026-10-01", "": nil} {
		c, writes := fake(t)
		if err := (Service{Client: c}).SetDue(ctx, "1", date); err != nil {
			t.Fatalf("SetDue(%q): %v", date, err)
		}
		if len(*writes) != 1 || (*writes)[0].path != "/tasks/1" || (*writes)[0].body["due_on"] != want {
			t.Errorf("SetDue(%q): writes = %+v, want due_on %v", date, *writes, want)
		}
	}
}

func TestSetDueRejectsBadDates(t *testing.T) {
	c, writes := fake(t)
	for _, date := range []string{"nonsense", "2026-02-30"} {
		if err := (Service{Client: c}).SetDue(ctx, "1", date); err == nil || !strings.Contains(err.Error(), "due date") {
			t.Errorf("SetDue(%q) = %v, want due date error", date, err)
		}
	}
	if len(*writes) != 0 {
		t.Fatalf("writes = %+v", *writes)
	}
}

func TestDateWritesResolveRelativeDates(t *testing.T) {
	if _, err := dateinput.Resolve(ctx, "today"); err != nil {
		t.Skipf("no GNU date: %v", err)
	}
	tomorrow := time.Now().AddDate(0, 0, 1).Format(time.DateOnly)
	c, writes := fake(t)
	if err := (Service{Client: c}).SetDue(ctx, "1", "+1 day"); err != nil {
		t.Fatal(err)
	}
	if err := (Service{Client: c}).SetField(ctx, "1", "When", "+1 day", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := (Service{Client: c}).Create(ctx, NewTicket{Name: "New", Parent: "1", Due: "+1 day"}); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 3 || (*writes)[0].body["due_on"] != tomorrow || (*writes)[2].body["due_on"] != tomorrow ||
		!reflect.DeepEqual((*writes)[1].body["custom_fields"], map[string]any{"f4": map[string]any{"date": tomorrow}}) {
		t.Fatalf("writes = %+v, want %s", *writes, tomorrow)
	}
}

func TestComplete(t *testing.T) {
	for _, done := range []bool{true, false} {
		c, writes := fake(t)
		if err := (Service{Client: c}).Complete(ctx, "1", done); err != nil {
			t.Fatalf("Complete(%v): %v", done, err)
		}
		if len(*writes) != 1 || (*writes)[0].path != "/tasks/1" || (*writes)[0].body["completed"] != done {
			t.Errorf("Complete(%v): writes = %+v", done, *writes)
		}
	}
}

func TestProjectMembership(t *testing.T) {
	c, writes := fake(t)
	svc := Service{Client: c}
	if err := svc.AddProject(ctx, "1", "p3"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveProject(ctx, "1", "p1"); err != nil {
		t.Fatal(err)
	}
	want := []write{
		{"POST", "/tasks/1/addProject", map[string]any{"project": "p3"}},
		{"POST", "/tasks/1/removeProject", map[string]any{"project": "p1"}},
	}
	if !reflect.DeepEqual(*writes, want) {
		t.Fatalf("writes = %+v", *writes)
	}
	if svc.AddProject(ctx, "1", "") == nil || svc.RemoveProject(ctx, "1", "") == nil {
		t.Fatal("an empty project gid must fail")
	}
}

func TestNewWritesConfirmWithNames(t *testing.T) {
	c, writes := fake(t)
	var asked []string
	svc := Service{Client: c, Users: []asana.Ref{{GID: "8", Name: "Bo"}}, Confirm: func(p string) (bool, error) {
		asked = append(asked, p)
		return false, nil
	}}
	for _, err := range []error{
		svc.Assign(ctx, "1", "Bo"),
		svc.Assign(ctx, "1", ""),
		svc.SetDue(ctx, "1", "2026-10-01"),
		svc.Complete(ctx, "1", true),
		svc.Complete(ctx, "1", false),
		svc.AddProject(ctx, "1", "p3"),
		svc.RemoveProject(ctx, "1", "p2"),
	} {
		if !errors.Is(err, ErrDeclined) {
			t.Fatalf("err = %v", err)
		}
	}
	want := []string{
		`Assign "Fix login" to Bo?`,
		`Unassign "Fix login"?`,
		`Set the due date of "Fix login" to 2026-10-01?`,
		`Mark "Fix login" complete?`,
		`Reopen "Fix login"?`,
		`Add "Fix login" to project Ops?`,
		`Remove "Fix login" from project API?`,
	}
	if !reflect.DeepEqual(asked, want) || len(*writes) != 0 {
		t.Fatalf("asked = %q, writes = %+v", asked, *writes)
	}
	if err := svc.RemoveProject(ctx, "1", "p3"); err == nil || !strings.Contains(err.Error(), "not in project") {
		t.Fatalf("remove from a project the task is not in: err = %v", err)
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
		{"When", "2026-01-01", map[string]any{"date": "2026-01-01"}},
	}
	for _, tc := range cases {
		c, writes := fake(t)
		if err := (Service{Client: c}).SetField(ctx, "1", tc.field, tc.value, ""); err != nil {
			t.Fatalf("%s=%q: %v", tc.field, tc.value, err)
		}
		fields := (*writes)[0].body["custom_fields"].(map[string]any)
		for _, got := range fields {
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s=%q: sent %v, want %v", tc.field, tc.value, got, tc.want)
			}
		}
	}
}

func TestSetFieldListValues(t *testing.T) {
	users := []asana.Ref{{GID: "9", Name: "Ann Lee"}, {GID: "8", Name: "Bo"}}
	cases := []struct {
		field, value string
		want         any
	}{
		{"areas", "backend, frontend", []any{"o3", "o2"}},
		{"Areas", "Frontend,frontend", []any{"o2"}},
		{"reviewers", "ann lee, Bo", []any{"9", "8"}},
		{"Reviewers", "", nil},
	}
	for _, tc := range cases {
		c, writes := fake(t)
		if err := (Service{Client: c, Users: users}).SetField(ctx, "1", tc.field, tc.value, ""); err != nil {
			t.Fatalf("%s=%q: %v", tc.field, tc.value, err)
		}
		fields := (*writes)[0].body["custom_fields"].(map[string]any)
		if got := fields[map[string]string{"areas": "f5", "Areas": "f5", "reviewers": "f6", "Reviewers": "f6"}[tc.field]]; !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s=%q: sent %v, want %v", tc.field, tc.value, got, tc.want)
		}
	}
}

func TestSetFieldRejectsUnknownOrAmbiguousPeople(t *testing.T) {
	c, writes := fake(t)
	svc := Service{Client: c, Users: []asana.Ref{{GID: "9", Name: "Ann"}, {GID: "7", Name: "Ann"}, {GID: "8", Name: "Bo"}}}
	for value, want := range map[string]string{"Ann": "several", "Cy": "no user", "Bo,": "empty"} {
		if err := svc.SetField(ctx, "1", "Reviewers", value, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("SetField(Reviewers, %q) = %v, want error containing %q", value, err, want)
		}
	}
	if err := svc.SetField(ctx, "1", "Areas", "Frontend, Ops", ""); err == nil || !strings.Contains(err.Error(), "Backend") {
		t.Errorf("unknown option err = %v, want list of options", err)
	}
	if len(*writes) != 0 {
		t.Fatalf("writes = %+v", *writes)
	}
}

func TestSetFieldRejectsBadInput(t *testing.T) {
	c, writes := fake(t)
	svc := Service{Client: c}
	for _, args := range [][2]string{{"Points", "many"}, {"Stage", "Shipped"}, {"When", "nonsense"}, {"Nope", "x"}} {
		if err := svc.SetField(ctx, "1", args[0], args[1], ""); err == nil {
			t.Errorf("SetField(%q, %q) succeeded", args[0], args[1])
		}
	}
	if len(*writes) != 0 {
		t.Fatalf("writes = %+v", *writes)
	}
}

func TestMessagesStripControlCharacters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tasks/1":
			io.WriteString(w, `{"data":{"gid":"1","name":"T","memberships":[{"project":{"gid":"p1","name":"Web\u001b]0;x\u0007"}},{"project":{"gid":"p2","name":"API"}}]}}`)
		case "/projects/p1/sections":
			io.WriteString(w, `{"data":[{"gid":"s1","name":"To Do\u001b[2J"}]}`)
		}
	}))
	defer srv.Close()
	c := asana.New("tok")
	c.BaseURL = srv.URL
	svc := Service{Client: c}
	for _, err := range []error{svc.Move(ctx, "1", "Nope", "p1"), svc.Move(ctx, "1", "Nope", "")} {
		if err == nil || strings.ContainsAny(err.Error(), "\x1b\x07") {
			t.Errorf("err = %q", err)
		}
	}
}

func TestSetFieldSameNameNeedsProject(t *testing.T) {
	var puts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/tasks/1" && r.Method == http.MethodGet:
			io.WriteString(w, `{"data":{"gid":"1","name":"T","custom_fields":[
				{"gid":"f1","name":"Branch Name ","resource_subtype":"text"},
				{"gid":"f5","name":"Branch name","resource_subtype":"text"}]}}`)
		case r.URL.Path == "/projects/p2/custom_field_settings":
			io.WriteString(w, `{"data":[{"custom_field":{"gid":"f5"}}]}`)
		case r.URL.Path == "/projects/p3/custom_field_settings":
			io.WriteString(w, `{"data":[{"custom_field":{"gid":"zz"}}]}`)
		case r.Method == http.MethodPut:
			var body struct {
				Data struct {
					CustomFields map[string]any `json:"custom_fields"`
				} `json:"data"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			for gid := range body.Data.CustomFields {
				puts = append(puts, gid)
			}
			io.WriteString(w, `{"data":{}}`)
		}
	}))
	defer srv.Close()
	c := asana.New("tok")
	c.BaseURL = srv.URL
	svc := Service{Client: c}
	if err := svc.SetField(ctx, "1", "branch name", "x", ""); err == nil || !strings.Contains(err.Error(), "--project") {
		t.Fatalf("no project: err = %v", err)
	}
	if err := svc.SetField(ctx, "1", "branch name", "x", "p3"); err == nil {
		t.Fatal("project without either field must fail")
	}
	if err := svc.SetField(ctx, "1", "branch name", "x", "p2"); err != nil || len(puts) != 1 || puts[0] != "f5" {
		t.Fatalf("err = %v, puts = %v", err, puts)
	}
	if err := svc.SetField(ctx, "1", "f1", "x", ""); err != nil || len(puts) != 2 || puts[1] != "f1" {
		t.Fatalf("by gid: err = %v, puts = %v", err, puts)
	}
}

func TestSetFieldTakesGIDs(t *testing.T) {
	users := []asana.Ref{{GID: "9", Name: "Ann"}, {GID: "7", Name: "Ann"}}
	cases := []struct {
		field, value string
		want         any
	}{
		{"f3", "o1", "o1"},
		{"Areas", "o3,o2", []any{"o3", "o2"}},
		{"Reviewers", "7", []any{"7"}},
	}
	for _, tc := range cases {
		c, writes := fake(t)
		if err := (Service{Client: c, Users: users}).SetField(ctx, "1", tc.field, tc.value, ""); err != nil {
			t.Fatalf("%s=%q: %v", tc.field, tc.value, err)
		}
		for _, got := range (*writes)[0].body["custom_fields"].(map[string]any) {
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s=%q: sent %v, want %v", tc.field, tc.value, got, tc.want)
			}
		}
	}
}

func TestCommentHTML(t *testing.T) {
	users := []asana.Ref{
		{GID: "10", Name: "Victoria"},
		{GID: "11", Name: "Victoria Andersen"},
		{GID: "12", Name: "Sam Lee"},
		{GID: "13", Name: "Sam Lee"},
		{GID: "x<", Name: "Bad Gid"},
	}
	cases := []struct {
		text, html string
		gids       []string
	}{
		{"hi @victoria andersen, see <this> & that", `<body>hi <a data-asana-gid="11"/>, see &lt;this&gt; &amp; that</body>`, []string{"11"}},
		{"@Victoria said\n@VICTORIA ANDERSEN", "<body><a data-asana-gid=\"10\"/> said\n<a data-asana-gid=\"11\"/></body>", []string{"10", "11"}},
		{"@Victoria Andersenx", `<body><a data-asana-gid="10"/> Andersenx</body>`, []string{"10"}},
		{"mail me@victoria or @nobody", "<body>mail me@victoria or @nobody</body>", nil},
		{"ask @Sam Lee or @Bad Gid", "<body>ask @Sam Lee or @Bad Gid</body>", nil},
	}
	for _, c := range cases {
		html, mentioned := CommentHTML(c.text, users, nil)
		var gids []string
		for _, u := range mentioned {
			gids = append(gids, u.GID)
		}
		if html != c.html || !reflect.DeepEqual(gids, c.gids) {
			t.Errorf("CommentHTML(%q) = %q, %v; want %q, %v", c.text, html, gids, c.html, c.gids)
		}
	}
}

func TestCommentMentionsPostHTML(t *testing.T) {
	c, writes := fake(t)
	var asked string
	svc := Service{Client: c, Users: []asana.Ref{{GID: "11", Name: "Victoria Andersen"}},
		Confirm: func(p string) (bool, error) { asked = p; return true, nil }}
	if err := svc.Comment(ctx, "1", "thanks @victoria andersen"); err != nil {
		t.Fatal(err)
	}
	want := `<body>thanks <a data-asana-gid="11"/></body>`
	if len(*writes) != 1 || (*writes)[0].body["html_text"] != want || (*writes)[0].body["text"] != nil {
		t.Fatalf("writes = %+v", *writes)
	}
	if !strings.Contains(asked, "Mentions: Victoria Andersen") {
		t.Fatalf("prompt = %q", asked)
	}
}

func TestCreate(t *testing.T) {
	cases := []struct {
		name     string
		nt       NewTicket
		wantPath string
		want     map[string]any
	}{
		{"my tasks by default", NewTicket{Name: " New "}, "/tasks",
			map[string]any{"name": "New", "workspace": "w", "assignee": "me"}},
		{"project and section", NewTicket{Name: "New", Project: "p1", Section: "in review", Due: "2026-01-02"}, "/tasks",
			map[string]any{"name": "New", "projects": []any{"p1"}, "memberships": []any{map[string]any{"project": "p1", "section": "s2"}}, "due_on": "2026-01-02"}},
		{"subtask", NewTicket{Name: "New", Parent: "1", Assignee: "amy", Notes: "**hi**"}, "/tasks/1/subtasks",
			map[string]any{"name": "New", "assignee": "u1", "html_notes": "<body><strong>hi</strong></body>"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, writes := fake(t)
			svc := Service{Client: c, Workspace: "w", Users: []asana.Ref{{GID: "u1", Name: "Amy"}}}
			task, err := svc.Create(ctx, tc.nt)
			if err != nil || task.GID != "new" {
				t.Fatalf("task = %+v, err = %v", task, err)
			}
			if len(*writes) != 1 || (*writes)[0].path != tc.wantPath || !reflect.DeepEqual((*writes)[0].body, tc.want) {
				t.Fatalf("writes = %+v", *writes)
			}
		})
	}
}

func TestCreateRejects(t *testing.T) {
	cases := []struct {
		nt   NewTicket
		want string
	}{
		{NewTicket{Name: "New", Parent: "1", Project: "p1"}, "not both"},
		{NewTicket{Name: "New", Project: "p1", Section: "Later"}, `no section "Later" in project p1; sections: To Do, In Review`},
		{NewTicket{Name: "New", Section: "To Do"}, "needs a project"},
		{NewTicket{Name: " "}, "title is empty"},
		{NewTicket{Name: "a\nb"}, "one line"},
		{NewTicket{Name: "New", Due: "soon"}, "due date"},
	}
	for _, tc := range cases {
		c, writes := fake(t)
		_, err := (Service{Client: c, Workspace: "w"}).Create(ctx, tc.nt)
		if err == nil || !strings.Contains(err.Error(), tc.want) || len(*writes) != 0 {
			t.Errorf("%+v: err = %v, writes = %+v", tc.nt, err, *writes)
		}
	}
}

func TestCreateConfirmsPlacement(t *testing.T) {
	c, writes := fake(t)
	var asked string
	svc := Service{Client: c, Confirm: func(p string) (bool, error) { asked = p; return false, nil }}
	if _, err := svc.Create(ctx, NewTicket{Name: "New", Parent: "1", Due: "2026-01-02"}); !errors.Is(err, ErrDeclined) {
		t.Fatalf("err = %v", err)
	}
	if len(*writes) != 0 || asked != "Create \"New\" as a subtask of \"Fix login\"?\nDue: 2026-01-02\n" {
		t.Fatalf("writes = %+v, prompt = %q", *writes, asked)
	}
}

// fakeStories serves comment s1, whose reactions follow the reactions
// written to it, and comment s2, which ignores them.
func fakeStories(t *testing.T) (*asana.Client, *[]write) {
	t.Helper()
	var writes []write
	reacted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			var body struct {
				Data map[string]any `json:"data"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			writes = append(writes, write{r.Method, r.URL.Path, body.Data})
			if rs, ok := body.Data["reactions"].([]any); ok && r.URL.Path == "/stories/s1" {
				reacted = rs[0].(map[string]any)["reacted"].(bool)
			}
			io.WriteString(w, `{"data":{}}`)
			return
		}
		summary := `[]`
		if reacted && r.URL.Path == "/stories/s1" {
			summary = `[{"emoji_base":"👍","variant":"👍🏽","count":2,"reacted":true}]`
		}
		io.WriteString(w, `{"data":{"type":"comment","created_by":{"name":"Bo"},"text":"Looks good to me, shipping it after lunch today","reaction_summary":`+summary+`}}`)
	}))
	t.Cleanup(srv.Close)
	c := asana.New("tok")
	c.BaseURL = srv.URL
	return c, &writes
}

func TestReact(t *testing.T) {
	c, writes := fakeStories(t)
	svc := Service{Client: c}
	if err := svc.React(ctx, "s1", " 👍 ", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.React(ctx, "s1", "👍", false); err != nil {
		t.Fatal(err)
	}
	want := []write{
		{"PUT", "/stories/s1", map[string]any{"reactions": []any{map[string]any{"emoji": "👍", "reacted": true}}}},
		{"PUT", "/stories/s1", map[string]any{"reactions": []any{map[string]any{"emoji": "👍", "reacted": false}}}},
	}
	if !reflect.DeepEqual(*writes, want) {
		t.Fatalf("writes = %+v", *writes)
	}
	if err := svc.React(ctx, "s1", "", true); err == nil {
		t.Fatal("an empty emoji must fail")
	}
}

func TestReactIgnoredFails(t *testing.T) {
	c, _ := fakeStories(t)
	if err := (Service{Client: c}).React(ctx, "s2", "👍", true); !errors.Is(err, ErrReactionIgnored) {
		t.Fatalf("err = %v", err)
	}
}

func TestReactConfirms(t *testing.T) {
	c, writes := fakeStories(t)
	var asked []string
	svc := Service{Client: c, Confirm: func(p string) (bool, error) { asked = append(asked, p); return false, nil }}
	for _, reacted := range []bool{true, false} {
		if err := svc.React(ctx, "s1", "👍", reacted); !errors.Is(err, ErrDeclined) {
			t.Fatalf("err = %v", err)
		}
	}
	want := []string{
		`React 👍 to Bo's comment "Looks good to me, shipping it after lun…"?`,
		`Remove your 👍 from Bo's comment "Looks good to me, shipping it after lun…"?`,
	}
	if !reflect.DeepEqual(asked, want) || len(*writes) != 0 {
		t.Fatalf("asked = %q, writes = %+v", asked, *writes)
	}
}
