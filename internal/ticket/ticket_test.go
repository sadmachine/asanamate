package ticket

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
)

func sample() Ticket {
	due, cpa := "2026-10-01", "CPA-2491"
	return Ticket{
		Task: asana.Task{
			GID: "1", Name: "Fix \x1b[31mlogin\u009b", PermalinkURL: "https://app.asana.com/t/1", DueOn: &due,
			Memberships:  []asana.Membership{{Project: asana.Ref{GID: "p", Name: "Web"}, Section: &asana.Ref{Name: "Doing"}}},
			CustomFields: []asana.CustomField{{Name: "CPA", DisplayValue: &cpa}, {Name: "Branch Name ", DisplayValue: nil}},
			HTMLNotes:    "<body>Use <strong>SSO</strong>.</body>",
		},
		Comments:    []asana.Story{{CreatedAt: "2026-09-20T10:00:00Z", CreatedBy: &asana.Ref{Name: "Sam"}, HTMLText: "<body>Looks good</body>"}},
		Attachments: []asana.Attachment{{Name: "shot.png", PermanentURL: "https://app.asana.com/a/1"}},
		Subtasks:    []asana.Task{{Name: "Write test", Completed: true}},
	}
}

func TestMarkdownSections(t *testing.T) {
	md := sample().Markdown()
	for _, want := range []string{
		"- **Status:** open",
		"- **Due:** 2026-10-01",
		"- **Project:** Web / Doing",
		"## Fields",
		"- **CPA:** CPA-2491",
		"Use **SSO**.",
		"- [x] Write test",
		"1. [shot.png](https://app.asana.com/a/1)",
		"### Sam · 2026-09-20",
		"Looks good",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "Branch Name") {
		t.Error("empty custom fields must be omitted")
	}
}

func TestMarkdownStripsControlCharacters(t *testing.T) {
	md := sample().Markdown()
	if strings.ContainsAny(md, "\x1b\u009b") {
		t.Fatalf("control characters leaked: %q", md)
	}
	if !strings.HasPrefix(md, "# Fix [31mlogin\n") {
		t.Fatalf("title line = %q", strings.SplitN(md, "\n", 2)[0])
	}
}

func TestHTMLToMarkdownAutolinksURLText(t *testing.T) {
	for in, want := range map[string]string{
		`<a href="https://google.com">https://google.com</a>`: "<https://google.com>",
		`<a href="https://g.com/a_b">https://g.com/a_b</a>`:   "<https://g.com/a_b>",
		`<a href="https://google.com">Google</a>`:             "[Google](https://google.com)",
		`<a href="/docs">/docs</a>`:                           "[/docs](/docs)",
	} {
		if got := HTMLToMarkdown("<body>" + in + "</body>"); got != want {
			t.Errorf("HTMLToMarkdown(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestHTMLToMarkdownCodeBlocks(t *testing.T) {
	for _, tt := range []struct {
		name, html, want string
	}{
		{"standalone after prose", "Before\n\n<code>[fees] ; early = [&quot;ED&quot;]</code>", "Before\n\n```\n[fees] ; early = [\"ED\"]\n```"},
		{"standalone before prose", "<code>x</code>\n\nAfter", "```\nx\n```\n\nAfter"},
		{"multiline", "<code>one\n  two\n\nthree</code>", "```\none\n  two\n\nthree\n```"},
		{"paragraph", "<p><code>x</code></p>", "```\nx\n```"},
		{"line breaks", "Before<br><code>x</code><br>After", "Before\n\n```\nx\n```\n\nAfter"},
		{"nested pre", "<pre><code>one\n  two</code></pre>", "```\none\n  two\n```"},
		{"embedded fence", "<code>```\nx\n```</code>", "````\n```\nx\n```\n````"},
		{"inline", "Use <code>x</code> now.", "Use `x` now."},
		{"inline at start", "<code>x</code> now.", "`x` now."},
		{"inline at end", "Use <code>x</code>", "Use `x`"},
		{"formatted inline", "Use <strong><code>x</code></strong> now.", "Use **`x`** now."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := HTMLToMarkdown("<body>" + tt.html + "</body>"); got != tt.want {
				t.Fatalf("HTMLToMarkdown(%q) = %q, want %q", tt.html, got, tt.want)
			}
		})
	}
}

func TestJSONFlattensTaskAndIncludesExtras(t *testing.T) {
	b, err := json.Marshal(sample())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"gid":"1"`, `"comments":[`, `"attachments":[`, `"subtasks":[`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("json missing %s: %s", want, b)
		}
	}
}

func TestFetchCombinesEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tasks/1":
			io.WriteString(w, `{"data":{"gid":"1","name":"T"}}`)
		case "/tasks/1/stories":
			io.WriteString(w, `{"data":[{"gid":"s","type":"comment","html_text":"<body>c</body>"}]}`)
		case "/attachments":
			io.WriteString(w, `{"data":[{"gid":"a","name":"f.pdf"}]}`)
		case "/tasks/1/subtasks":
			io.WriteString(w, `{"data":[{"gid":"2","name":"sub"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := asana.New("tok")
	c.BaseURL = srv.URL
	tk, err := Fetch(context.Background(), c, "1")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Name != "T" || len(tk.Comments) != 1 || len(tk.Attachments) != 1 || len(tk.Subtasks) != 1 {
		t.Fatalf("ticket = %+v", tk)
	}
}

func TestListPicksViewEndpoint(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/users/me/user_task_list" {
			io.WriteString(w, `{"data":{"gid":"55"}}`)
			return
		}
		if r.URL.Query().Get("completed_since") == "" {
			t.Error("missing completed_since")
		}
		io.WriteString(w, `{"data":[{"gid":"1","name":"T"}]}`)
	}))
	defer srv.Close()
	c := asana.New("tok")
	c.BaseURL = srv.URL
	if tasks, err := List(context.Background(), c, "w", ""); err != nil || len(tasks) != 1 {
		t.Fatalf("my tasks: %v %v", tasks, err)
	}
	if _, err := List(context.Background(), c, "w", "9"); err != nil {
		t.Fatal(err)
	}
	want := "/users/me/user_task_list,/user_task_lists/55/tasks,/projects/9/tasks"
	if got := strings.Join(paths, ","); got != want {
		t.Fatalf("paths = %s, want %s", got, want)
	}
}

func TestSplitImages(t *testing.T) {
	html := `<body>Before<img data-asana-gid="111" src="https://a/1.png" alt="one.png">` +
		`<ul><li><img data-asana-gid="222" src="https://a/2.png"></li></ul><img src="https://a/3.png"></body>`
	parts := SplitImages(html)
	want := []RichPart{
		{Markdown: "Before"},
		{Markdown: "![one.png](https://a/1.png)", ImageGID: "111"},
		{Markdown: "![](https://a/2.png)", ImageGID: "222"},
		{Markdown: "![](https://a/3.png)"},
	}
	if !slices.Equal(parts, want) {
		t.Fatalf("parts = %#v", parts)
	}
	if gids := ImageGIDs(html); !slices.Equal(gids, []string{"111", "222"}) {
		t.Fatalf("gids = %v", gids)
	}
}
