package tui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/drafts"
	"github.com/sadmachine/asanamate/internal/filedialog"
)

// writeFile writes a small file at path, making its folders, and dates it
// age before now.
func writeFile(t *testing.T, path string, age time.Duration) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	return path
}

// tempFile writes a small file named name in a directory whose name has a
// space, as dropped paths often do.
func tempFile(t *testing.T, name string) string {
	t.Helper()
	return writeFile(t, filepath.Join(t.TempDir(), "my docs", name), 0)
}

// noFileDialog hides the system file dialog for the test.
func noFileDialog(t *testing.T) {
	t.Helper()
	available := fileDialogAvailable
	fileDialogAvailable = func() bool { return false }
	t.Cleanup(func() { fileDialogAvailable = available })
}

// finderModel is an editModel whose ticket links a git repo holding
// notes.txt, with HOME holding Downloads/report.pdf.
func finderModel(t *testing.T) (m *Model, writes *[]string, notes, report string) {
	t.Helper()
	noFileDialog(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	report = writeFile(t, filepath.Join(home, "Downloads", "report.pdf"), 0)
	repoDir := filepath.Join(t.TempDir(), "repo")
	notes = writeFile(t, filepath.Join(repoDir, "notes.txt"), time.Hour)
	if out, err := exec.Command("git", "init", "-q", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if resolved, err := filepath.EvalSymlinks(repoDir); err == nil {
		repoDir = resolved
		notes = filepath.Join(repoDir, "notes.txt")
	}
	m, writes = editModel(t)
	m.deps.State.TaskRepos["1"] = repoDir
	return m, writes, notes, report
}

func TestFinderAttachesSeveralFilesRepoFirst(t *testing.T) {
	m, writes, notes, report := finderModel(t)
	press(m, "c")
	m.input.area.SetValue("See attached")
	press(m, "tab", "enter", "s")
	p := m.modal
	if p == nil || p.title != "Attach files" || p.progress != "" || len(p.items) != 2 {
		t.Fatalf("finder = %+v", p)
	}
	if p.items[0].Value != notes || p.items[1].Value != report {
		t.Fatalf("finder lists %v, %v; want the repo's file first", p.items[0].Value, p.items[1].Value)
	}
	// tab checks while searching, where space types.
	press(m, "tab", "down", "tab", "enter")
	if m.input != m.composer.box || !slices.Equal(m.composer.files, []string{notes, report}) {
		t.Fatalf("files = %v", m.composer.files)
	}
	if view := ansi.Strip(m.composer.view(80, 30, m.modalLook())); !strings.Contains(view, "notes.txt, report.pdf ●") {
		t.Fatalf("composer does not list the files:\n%s", view)
	}
	send(m, ctrlS)
	if m.composer != nil || len(*writes) != 3 || !strings.HasPrefix((*writes)[0], "POST /tasks/1/stories ") ||
		!strings.Contains((*writes)[1], "filename=notes.txt") || !strings.Contains((*writes)[2], "filename=report.pdf") {
		t.Fatalf("writes = %v", *writes)
	}
}

func TestFinderAttachesHighlightedOrDroppedFiles(t *testing.T) {
	m, _, notes, _ := finderModel(t)
	press(m, "c", "tab", "enter", "s", "enter")
	if !slices.Equal(m.composer.files, []string{notes}) {
		t.Fatalf("enter without checks attached %v, want the highlighted file", m.composer.files)
	}
	a, b := tempFile(t, "a b.txt"), tempFile(t, "c.txt")
	press(m, "enter", "s")
	send(m, tea.PasteMsg{Content: strings.ReplaceAll(a, " ", `\ `) + " " + strings.ReplaceAll(b, " ", `\ `)})
	press(m, "enter")
	if !slices.Equal(m.composer.files, []string{notes, a, b}) {
		t.Fatalf("dropped files = %v", m.composer.files)
	}
}

func TestComposerRemovesFilesAndNeedsText(t *testing.T) {
	noFileDialog(t)
	m, writes := editModel(t)
	a, b := tempFile(t, "a.txt"), tempFile(t, "b.txt")
	press(m, "c")
	m.composer.files = []string{a, b}
	press(m, "tab", "enter")
	if m.modal == nil || m.modal.title != "Files" || len(m.modal.items) != 4 {
		t.Fatalf("files menu = %+v", m.modal.items)
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

func TestFileDialog(t *testing.T) {
	m, _, _, _ := finderModel(t)
	fileDialogAvailable = func() bool { return true }
	chosen := []string{tempFile(t, "x.pdf"), tempFile(t, "y.pdf")}
	var result error
	var dir string
	chooseFiles = func(_ context.Context, d string) ([]string, error) { dir = d; return chosen, result }
	t.Cleanup(func() { chooseFiles = filedialog.Choose })

	press(m, "c", "tab", "enter", "d")
	if !slices.Equal(m.composer.files, chosen) || m.composer.box.progress != "" || dir != m.deps.State.TaskRepos["1"] {
		t.Fatalf("files = %v, progress = %q, dir = %q", m.composer.files, m.composer.box.progress, dir)
	}
	m.composer.files = nil
	result = filedialog.ErrCanceled
	press(m, "enter", "d")
	if len(m.composer.files) != 0 || m.composer.box.err != "" {
		t.Fatalf("cancel: files = %v, err = %q", m.composer.files, m.composer.box.err)
	}
	result = errors.New("exit status 1: Not authorized to send Apple events (-1743)")
	press(m, "enter", "d")
	if err := m.composer.box.err; !strings.Contains(err, "-1743") || !strings.Contains(err, "Use Search files, or drop files on the terminal") {
		t.Fatalf("failure: err = %q", err)
	}

	noFileDialog(t)
	press(m, "enter")
	for _, it := range m.modal.items {
		if strings.Contains(it.Label, "system dialog") {
			t.Fatalf("menu offers an unavailable dialog: %+v", m.modal.items)
		}
	}
}

func TestScanFiles(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	old := writeFile(t, filepath.Join(root, "old.txt"), time.Hour)
	recent := writeFile(t, filepath.Join(root, "sub", "new.txt"), 0)
	writeFile(t, filepath.Join(root, ".hidden.txt"), 0)
	writeFile(t, filepath.Join(root, ".git", "config"), 0)
	writeFile(t, filepath.Join(root, "node_modules", "dep.js"), 0)
	writeFile(t, filepath.Join(root, "vendor", "lib.go"), 0)
	elsewhere := writeFile(t, filepath.Join(other, "else.txt"), 0)
	items := scanFiles(context.Background(), []string{root, filepath.Join(root, "sub"), other, filepath.Join(root, "missing")})
	var got []string
	for _, it := range items {
		got = append(got, it.Value.(string))
	}
	if want := []string{recent, old, elsewhere}; !slices.Equal(got, want) {
		t.Fatalf("scanFiles = %v, want %v", got, want)
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

func TestDroppedFiles(t *testing.T) {
	notes, spaced := tempFile(t, "notes.txt"), tempFile(t, "two words.pdf")
	for _, tt := range []struct {
		text string
		want []string
	}{
		{notes, []string{notes}},
		{"  " + spaced + "\n", []string{spaced}},
		{"'" + spaced + "'", []string{spaced}},
		{`"` + spaced + `"`, []string{spaced}},
		{strings.ReplaceAll(spaced, " ", `\ `), []string{spaced}},
		{"file://" + strings.ReplaceAll(spaced, " ", "%20"), []string{spaced}},
		{strings.ReplaceAll(notes, " ", `\ `) + " " + strings.ReplaceAll(spaced, " ", `\ `), []string{notes, spaced}},
		{"'" + notes + "' '" + spaced + "'", []string{notes, spaced}},
	} {
		if got, err := droppedFiles(tt.text); err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("droppedFiles(%q) = %q, %v", tt.text, got, err)
		}
	}
	for _, text := range []string{"", "report", filepath.Dir(notes), notes + ".missing", strings.ReplaceAll(notes, " ", `\ `) + " missing.txt"} {
		if got, err := droppedFiles(text); err == nil {
			t.Errorf("droppedFiles(%q) = %q, want an error", text, got)
		}
	}
}
