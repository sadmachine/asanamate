package writeback

import (
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
)

func TestCommentMarkdown(t *testing.T) {
	cases := []struct {
		in, want string
		mentions int
	}{
		{"**bold** *italic* ~~gone~~", "<body><strong>bold</strong> <em>italic</em> <s>gone</s></body>", 0},
		{"```go\n<x> & @Sam Lee\n```", "<body><pre>&lt;x&gt; &amp; @Sam Lee\n</pre></body>", 0},
		{"`@Sam Lee` **@Sam Lee**", "<body><code>@Sam Lee</code> <strong><a data-asana-gid=\"1\"/></strong></body>", 1},
		{"- one\n- two", "<body><ul><li>one</li><li>two</li></ul></body>", 0},
		{"1. one\n2. two", "<body><ol><li>one</li><li>two</li></ol></body>", 0},
		{"[go](https://example.com/?a=1&b=2)", "<body><a href=\"https://example.com/?a=1&amp;b=2\">go</a></body>", 0},
		{"[bad](javascript:alert(1))", "<body>bad</body>", 0},
		{"[bad](https:relative)", "<body>bad</body>", 0},
		{"<https://example.com>", "<body><a href=\"https://example.com\">https://example.com</a></body>", 0},
		{"<sam@example.com>", "<body><a href=\"mailto:sam@example.com\">sam@example.com</a></body>", 0},
		{"# Header", "<body><strong>Header</strong></body>", 0},
		{"<script>bad()</script>", "<body>&lt;script&gt;bad()&lt;/script&gt;</body>", 0},
		{"one\n\ntwo", "<body>one\n\ntwo</body>", 0},
		{"\\*literal\\* &amp;", "<body>*literal* &amp;</body>", 0},
		{"    a < b\n", "<body><pre>a &lt; b\n</pre></body>", 0},
		{"> quoted", "<body><blockquote>quoted</blockquote></body>", 0},
		{"> - one\n> - two", "<body><blockquote>- one\n- two</blockquote></body>", 0},
		{"- one\n\n  ```\n  x\n  ```", "<body><ul><li>one\n\n<code>x\n</code></li></ul></body>", 0},
		{"- > quoted", "<body><ul><li>quoted</li></ul></body>", 0},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, mentioned := CommentHTML(c.in, []asana.Ref{{GID: "1", Name: "Sam Lee"}})
			if got != c.want || len(mentioned) != c.mentions {
				t.Fatalf("CommentHTML(%q) = %q, %v; want %q, %d mentions", c.in, got, mentioned, c.want, c.mentions)
			}
			decoder := xml.NewDecoder(strings.NewReader(got))
			for {
				_, err := decoder.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("invalid Asana XML %q: %v", got, err)
				}
			}
		})
	}
}

func TestCommentMarkdownPostsHTML(t *testing.T) {
	client, writes := fake(t)
	const markdown = "**Values**\n\n```json\n{\"enabled\": true}\n```"
	const want = "<body><strong>Values</strong>\n\n<pre>{\"enabled\": true}\n</pre></body>"
	var asked string
	service := Service{Client: client, Confirm: func(prompt string) (bool, error) { asked = prompt; return true, nil }}
	if err := service.Comment(ctx, "1", markdown); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0].method != "POST" || (*writes)[0].path != "/tasks/1/stories" || (*writes)[0].body["html_text"] != want || (*writes)[0].body["text"] != nil {
		t.Fatalf("writes = %+v", *writes)
	}
	if !strings.Contains(asked, markdown) {
		t.Fatalf("confirmation omits Markdown: %q", asked)
	}
}

func TestCommentMarkdownCodeMentions(t *testing.T) {
	client, writes := fake(t)
	const markdown = "`@Sam Lee` [@Sam Lee](https://example.com)\n\n```\n@Sam Lee\n```"
	var asked string
	service := Service{Client: client, Users: []asana.Ref{{GID: "1", Name: "Sam Lee"}}, Confirm: func(prompt string) (bool, error) { asked = prompt; return true, nil }}
	if err := service.Comment(ctx, "1", markdown); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 {
		t.Fatalf("writes = %+v", *writes)
	}
	body, ok := (*writes)[0].body["html_text"].(string)
	if !ok || strings.Contains(body, "data-asana-gid") || strings.Contains(asked, "Mentions:") {
		t.Fatalf("code/link text creates mentions: body = %q, prompt = %q", body, asked)
	}
}
