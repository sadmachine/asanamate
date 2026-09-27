package listing

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

var due = "2026-10-01"

var tasks = []asana.Task{
	{
		GID: "1", Name: "Fix\tlogin\nnow\x1b", DueOn: &due, PermalinkURL: "https://app.asana.com/t/1",
		AssigneeSection: &asana.Ref{Name: "Today"},
		Memberships:     []asana.Membership{{Project: asana.Ref{GID: "p", Name: "Web"}, Section: &asana.Ref{Name: "Doing"}}},
	},
	{GID: "2", Name: "Plain"},
}

func TestTasksTSV(t *testing.T) {
	var b strings.Builder
	if err := Tasks(&b, FormatTSV, tasks, ""); err != nil {
		t.Fatal(err)
	}
	want := "1\tToday\t2026-10-01\tFix login now\thttps://app.asana.com/t/1\n2\t\t\tPlain\t\n"
	if b.String() != want {
		t.Fatalf("got %q\nwant %q", b.String(), want)
	}
	for _, line := range strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n") {
		if n := len(strings.Split(line, "\t")); n != 5 {
			t.Fatalf("line %q has %d columns", line, n)
		}
	}
	b.Reset()
	Tasks(&b, FormatTSV, tasks[:1], "p")
	if !strings.HasPrefix(b.String(), "1\tDoing\t") {
		t.Fatalf("project section not used: %q", b.String())
	}
}

func TestTasksJSONL(t *testing.T) {
	var b strings.Builder
	if err := Tasks(&b, FormatJSONL, tasks, ""); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	var first asana.Task
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil || first.GID != "1" || first.SectionFor("p") != "Doing" {
		t.Fatalf("first = %+v, err = %v", first, err)
	}
}

func TestTicketFormats(t *testing.T) {
	tk := ticket.Ticket{Task: asana.Task{GID: "1", Name: "T"}}
	var md, js strings.Builder
	if err := Ticket(&md, FormatMarkdown, tk); err != nil || !strings.HasPrefix(md.String(), "# T\n") {
		t.Fatalf("md = %q, err = %v", md.String(), err)
	}
	if err := Ticket(&js, FormatJSON, tk); err != nil || !strings.Contains(js.String(), `"comments"`) {
		t.Fatalf("json = %q, err = %v", js.String(), err)
	}
}

func TestUnknownFormats(t *testing.T) {
	if err := Tasks(io.Discard, "csv", tasks, ""); err == nil {
		t.Error("Tasks accepted csv")
	}
	if err := Ticket(io.Discard, "html", ticket.Ticket{}); err == nil {
		t.Error("Ticket accepted html")
	}
}
