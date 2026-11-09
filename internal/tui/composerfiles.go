package tui

import (
	"errors"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

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

// editComposerFiles asks for a file to attach, or, once the comment has
// some, offers to add another or remove one.
func (m *Model) editComposerFiles() {
	c := m.composer
	if len(c.files) == 0 {
		m.promptComposerFile()
		return
	}
	items := []pickItem{{Label: "Add file…", Key: "a", Value: ""}}
	for _, f := range c.files {
		items = append(items, pickItem{Label: filepath.Base(f), Hint: "enter to remove", Search: f, Value: f})
	}
	p := newPicker(pickValue(func(path string) tea.Cmd {
		if path == "" {
			m.promptComposerFile()
			return nil
		}
		c.files = slices.DeleteFunc(c.files, func(f string) bool { return f == path })
		m.returnToDraft()
		return nil
	}), "Files to attach", items)
	p.keySelect = true
	m.input, m.modal = nil, p
}

// promptComposerFile asks for the path of a file to attach. A file dropped
// on the terminal pastes its path, which works too.
func (m *Model) promptComposerFile() {
	c := m.composer
	b := newInputBox("Attach file", "file path, or drop a file here")
	b.oneLine = true
	b.area.KeyMap.InsertNewline.SetEnabled(false)
	b.onSubmit = func(value string) tea.Cmd {
		path, err := droppedFile(value)
		if err != nil {
			b.err = err.Error()
			return nil
		}
		if !slices.Contains(c.files, path) {
			c.files = append(c.files, path)
		}
		m.returnToDraft()
		return nil
	}
	m.input, m.modal = b, nil
}

// droppedFile resolves a typed or pasted path to an absolute file path to
// upload. Terminals paste a dropped file's path quoted, shell-escaped, or as
// a file:// URL, so each form is tried after the text as typed.
func droppedFile(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("enter a file path")
	}
	candidates := []string{text}
	if len(text) >= 2 && (text[0] == '\'' || text[0] == '"') && text[len(text)-1] == text[0] {
		candidates = append(candidates, text[1:len(text)-1])
	}
	if u, err := url.Parse(text); err == nil && u.Scheme == "file" {
		candidates = append(candidates, u.Path)
	}
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) {
			i++
		}
		b.WriteByte(text[i])
	}
	candidates = append(candidates, b.String())
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
