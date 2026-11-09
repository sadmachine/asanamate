package tui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/filedialog"
	"github.com/sadmachine/asanamate/internal/repo"
	"github.com/sadmachine/asanamate/internal/writeback"
)

// The composer's footer rows, which tab reaches after the comment text
// (footer -1).
const (
	footerFiles = iota
	footerAssignee
	footerSection
	footerMore
	footerRows
)

// fileChoice is a Files menu item: an action, or an attached file to remove.
type fileChoice struct {
	action string // "search", "dialog", or "remove"
	path   string
}

// editComposerFiles opens the Files menu: search for files, choose them in
// the system dialog where one is available, or remove an attached file.
func (m *Model) editComposerFiles() {
	c := m.composer
	items := []pickItem{{Label: "Search files…", Key: "s", Value: fileChoice{action: "search"},
		Help: "Search the ticket's repo, Downloads, Desktop, and Documents, or drop files on the terminal."}}
	if fileDialogAvailable() {
		items = append(items, pickItem{Label: "Choose with system dialog…", Key: "d", Value: fileChoice{action: "dialog"}})
	}
	if len(c.files) > 0 {
		items = append(items, pickItem{Header: true, Label: "Attached"})
		for _, f := range c.files {
			items = append(items, pickItem{Label: filepath.Base(f), Hint: "enter to remove", Value: fileChoice{action: "remove", path: f}})
		}
	}
	p := newPicker(pickValue(func(choice fileChoice) tea.Cmd {
		switch choice.action {
		case "search":
			return m.openFileFinder()
		case "dialog":
			return m.chooseFilesInDialog()
		}
		c.files = slices.DeleteFunc(c.files, func(f string) bool { return f == choice.path })
		m.returnToDraft()
		return nil
	}), "Files", items)
	p.keySelect = true
	m.input, m.modal = nil, p
}

// addFiles attaches paths not already attached, keeping their order.
func (c *commentComposer) addFiles(paths []string) {
	for _, p := range paths {
		if !slices.Contains(c.files, p) {
			c.files = append(c.files, p)
		}
	}
}

// ticketFolders returns a func listing the folders where the composer's
// ticket keeps its files: the worktree of its branch, then its repo; none
// when it has no repo.
func (m *Model) ticketFolders() func() []string {
	t := *m.composer.task
	path, _ := m.effectiveRepo(t)
	if path == "" {
		return func() []string { return nil }
	}
	branch := action.Branch(t, m.deps.State.TaskBranches[t.GID], m.deps.Config.BranchField, m.rowContext().preferred)
	// Finding the worktree runs git, so callers run this off the UI loop.
	return func() []string {
		if wt := repo.Worktree(path, branch); wt != "" && wt != path {
			return []string{wt, path}
		}
		return []string{path}
	}
}

// fileScanMsg delivers the files found for the finder p.
type fileScanMsg struct {
	picker *picker
	items  []pickItem
}

// openFileFinder lists files to attach, those in the ticket's repo first,
// to search and check several of. Typed or dropped paths work too.
func (m *Model) openFileFinder() tea.Cmd {
	c := m.composer
	p := newMultiPicker(nil, "Attach files", nil, map[int]bool{})
	p.onPick = func(res pickResult) tea.Cmd {
		var paths []string
		for _, it := range res.items {
			paths = append(paths, it.Value.(string))
		}
		if len(paths) == 0 {
			// Nothing checked: use dropped or typed paths, else the
			// highlighted file.
			if dropped, err := droppedFiles(p.input.Value()); err == nil {
				paths = dropped
			} else if it := p.current(); it != nil {
				paths = []string{it.Value.(string)}
			} else {
				p.err = err.Error()
				return nil
			}
		}
		c.addFiles(paths)
		m.returnToDraft()
		return nil
	}
	p.searching = true
	p.input.Placeholder = "search, or drop files here"
	p.progress = "Scanning files…"
	m.input, m.modal = nil, p
	folders := m.ticketFolders()
	return tea.Batch(p.input.Focus(), m.startSpinner(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fileScanTime)
		defer cancel()
		roots := folders()
		if home, err := os.UserHomeDir(); err == nil {
			for _, dir := range []string{"Downloads", "Desktop", "Documents"} {
				roots = append(roots, filepath.Join(home, dir))
			}
		}
		return fileScanMsg{picker: p, items: scanFiles(ctx, roots)}
	})
}

// fileScanned fills the finder with the files found, unless it closed.
func (m *Model) fileScanned(msg fileScanMsg) {
	p := msg.picker
	if m.modal != p {
		return
	}
	p.items, p.progress = msg.items, ""
	p.refilter()
	if len(p.items) == 0 {
		p.notice = "No files found. Type a path or drop files here."
	}
}

const (
	// fileScanLimit caps the files listed from each folder, and fileScanTime
	// the whole scan, so a large folder cannot stall the finder.
	fileScanLimit = 5000
	fileScanTime  = 3 * time.Second
)

// scanFiles lists the regular files under roots as finder items, each root's
// newest first, skipping hidden entries and dependency folders. A file
// under two roots is listed once, under the first.
func scanFiles(ctx context.Context, roots []string) []pickItem {
	type found struct {
		path string
		mod  time.Time
	}
	seen := map[string]bool{}
	var items []pickItem
	for _, root := range roots {
		var files []found
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			switch {
			case ctx.Err() != nil:
				return fs.SkipAll
			case err != nil:
				return nil // an unreadable entry; WalkDir skips a folder it cannot list
			case path != root && strings.HasPrefix(d.Name(), "."):
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			case d.IsDir():
				if d.Name() == "node_modules" || d.Name() == "vendor" {
					return fs.SkipDir
				}
				return nil
			case !d.Type().IsRegular() || seen[path]:
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			seen[path] = true
			files = append(files, found{path, info.ModTime()})
			if len(files) >= fileScanLimit {
				return fs.SkipAll
			}
			return nil
		})
		slices.SortStableFunc(files, func(a, b found) int { return b.mod.Compare(a.mod) })
		for _, f := range files {
			items = append(items, pickItem{Label: filepath.Base(f.path), Hint: repo.CollapseHome(filepath.Dir(f.path)), Search: f.path, Value: f.path})
		}
	}
	return items
}

// fileDialogAvailable reports whether the system file dialog can open; tests
// replace it.
var fileDialogAvailable = filedialog.Available

// chooseFiles opens the system file dialog in dir; tests replace it.
var chooseFiles = filedialog.Choose

// fileDialogMsg is the system dialog's result for composer c.
type fileDialogMsg struct {
	composer *commentComposer
	paths    []string
	err      error
}

// chooseFilesInDialog opens the system file dialog in the ticket's repo and
// attaches the files chosen there.
func (m *Model) chooseFilesInDialog() tea.Cmd {
	c := m.composer
	m.returnToDraft()
	c.box.err, c.box.progress = "", "Waiting for the file dialog…"
	folders := m.ticketFolders()
	return tea.Batch(m.startSpinner(), func() tea.Msg {
		dir := ""
		if f := folders(); len(f) > 0 {
			dir = f[0]
		}
		paths, err := chooseFiles(context.Background(), dir)
		return fileDialogMsg{composer: c, paths: paths, err: err}
	})
}

// filesChosen attaches the dialog's files, or explains how else to attach
// them when it failed.
func (m *Model) filesChosen(msg fileDialogMsg) {
	c := msg.composer
	if m.composer != c {
		return
	}
	c.box.progress = ""
	switch {
	case errors.Is(msg.err, filedialog.ErrCanceled):
	case msg.err != nil:
		c.setError(fmt.Sprintf("The system file dialog did not work (%v). Use Search files, or drop files on the terminal.", msg.err))
	default:
		c.addFiles(msg.paths)
	}
}

// droppedFiles resolves typed or pasted text to the absolute paths of files
// to upload. A terminal pastes several dropped files as shell words, so the
// text is tried as one path first, then split as a shell would.
func droppedFiles(text string) ([]string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("check files with space, or type or drop a path")
	}
	path, err := droppedFile(text)
	if err == nil {
		return []string{path}, nil
	}
	fields := shellFields(text)
	if len(fields) < 2 {
		return nil, err
	}
	paths := make([]string, len(fields))
	for i, field := range fields {
		if paths[i], err = droppedFile(field); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

// droppedFile resolves one typed or pasted path to an absolute file path to
// upload. Terminals paste a dropped file's path quoted, shell-escaped, or as
// a file:// URL, so each form is tried after the text as typed.
func droppedFile(text string) (string, error) {
	candidates := []string{text}
	if len(text) >= 2 && (text[0] == '\'' || text[0] == '"') && text[len(text)-1] == text[0] {
		candidates = append(candidates, text[1:len(text)-1])
	}
	if u, err := url.Parse(text); err == nil && u.Scheme == "file" {
		candidates = append(candidates, u.Path)
	}
	if fields := shellFields(text); len(fields) == 1 {
		candidates = append(candidates, fields[0])
	}
	var first error
	for _, candidate := range candidates {
		path, err := writeback.LocalFile(candidate)
		if err == nil {
			return filepath.Abs(path)
		}
		if first == nil {
			first = err
		}
	}
	return "", first
}

// shellFields splits text into words as a shell does: whitespace separates
// them, a backslash escapes the next character, and quotes group.
func shellFields(text string) []string {
	var fields []string
	var b strings.Builder
	started := false
	var quote byte
	for i := 0; i < len(text); i++ {
		ch := text[i]
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			} else {
				b.WriteByte(ch)
			}
		case ch == '\\' && i+1 < len(text):
			i++
			b.WriteByte(text[i])
			started = true
		case ch == '\'' || ch == '"':
			quote, started = ch, true
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			if started {
				fields = append(fields, b.String())
				b.Reset()
				started = false
			}
		default:
			b.WriteByte(ch)
			started = true
		}
	}
	if started {
		fields = append(fields, b.String())
	}
	return fields
}
