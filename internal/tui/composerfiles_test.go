package tui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/drafts"
)

// tempFile writes a small file named name in a directory whose name has a
// space, as dropped paths often do.
func tempFile(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "my docs")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestComposerAttachesFilesAfterTheComment(t *testing.T) {
	m, writes := editModel(t)
	notes := tempFile(t, "notes.txt")
	press(m, "c")
	m.input.area.SetValue("See attached")
	press(m, "tab", "enter")
	if m.input == nil || m.input.title != "Attach file" {
		t.Fatalf("enter on Files did not ask for a path: %+v", m.input)
	}
	m.input.area.SetValue(strings.ReplaceAll(notes, " ", `\ `))
	send(m, ctrlS)
	if m.input != m.composer.box || !slices.Equal(m.composer.files, []string{notes}) {
		t.Fatalf("files = %v", m.composer.files)
	}
	if view := ansi.Strip(m.composer.view(80, 30, m.modalLook())); !strings.Contains(view, "Files") || !strings.Contains(view, "notes.txt ●") {
		t.Fatalf("composer does not list the file:\n%s", view)
	}
	send(m, ctrlS)
	if m.composer != nil || len(*writes) != 2 || !strings.HasPrefix((*writes)[0], "POST /tasks/1/stories ") ||
		!strings.HasPrefix((*writes)[1], "POST /attachments ") || !strings.Contains((*writes)[1], "filename=notes.txt") {
		t.Fatalf("writes = %v", *writes)
	}
}

func TestComposerRemovesFilesAndNeedsText(t *testing.T) {
	m, writes := editModel(t)
	a, b := tempFile(t, "a.txt"), tempFile(t, "b.txt")
	press(m, "c")
	m.composer.files = []string{a, b}
	press(m, "tab", "enter")
	if m.modal == nil || len(m.modal.items) != 3 {
		t.Fatalf("files menu = %+v", m.modal)
	}
	press(m, "down", "enter")
	if m.input != m.composer.box || !slices.Equal(m.composer.files, []string{b}) {
		t.Fatalf("files = %v", m.composer.files)
	}
	send(m, ctrlS)
	if m.composer == nil || !strings.Contains(m.composer.box.err, "add comment text") || len(*writes) != 0 {
		t.Fatalf("files posted without a comment: %v", *writes)
	}
}

func TestComposerFilesSurviveDrafts(t *testing.T) {
	m, writes := editModel(t)
	m.width, m.height = 140, 40
	notes := tempFile(t, "notes.txt")
	press(m, "c")
	m.composer.files = []string{notes}
	send(m, ctrlD)
	if m.input != nil || len(*writes) != 0 || !slices.Equal(savedComment(t, m).Files, []string{notes}) {
		t.Fatal("saving the draft lost its files")
	}
	press(m, "c")
	if !slices.Equal(m.composer.files, []string{notes}) || !strings.Contains(m.input.title, "draft") {
		t.Fatalf("reopened files = %v", m.composer.files)
	}
}

func TestComposerClosesWhenAttachingFailsAfterPosting(t *testing.T) {
	m, _ := editModel(t)
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writes = append(writes, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/attachments" {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"errors":[{"message":"upload rejected"}]}`)
			return
		}
		io.WriteString(w, `{"data":{"gid":"9"}}`)
	}))
	t.Cleanup(srv.Close)
	m.deps.Client = asana.New("tok")
	m.deps.Client.BaseURL = srv.URL
	press(m, "c")
	m.input.area.SetValue("hi")
	m.composer.files = []string{tempFile(t, "notes.txt")}
	send(m, ctrlS)
	if m.composer != nil || !strings.Contains(m.status, "comment posted, but attaching") || len(writes) < 2 || !slices.Equal(writes[:2], []string{"POST /tasks/1/stories", "POST /attachments"}) {
		t.Fatalf("composer = %v, status = %q, writes = %v", m.composer != nil, m.status, writes)
	}
	if drafts.HasComment(m.deps.StateDir, m.deps.Config.Workspace, "1") {
		t.Fatal("the posted comment's draft was kept, so it could post twice")
	}
}

func TestDroppedFile(t *testing.T) {
	notes := tempFile(t, "notes.txt")
	for _, text := range []string{
		notes,
		"  " + notes + "\n",
		"'" + notes + "'",
		`"` + notes + `"`,
		strings.ReplaceAll(notes, " ", `\ `),
		"file://" + strings.ReplaceAll(notes, " ", "%20"),
	} {
		if got, err := droppedFile(text); err != nil || got != notes {
			t.Errorf("droppedFile(%q) = %q, %v", text, got, err)
		}
	}
	for _, text := range []string{"", filepath.Dir(notes), notes + ".missing"} {
		if _, err := droppedFile(text); err == nil {
			t.Errorf("droppedFile(%q) accepted", text)
		}
	}
}
