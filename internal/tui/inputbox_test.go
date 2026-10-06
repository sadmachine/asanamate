package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/config"
)

func TestInputBoxPasteMessages(t *testing.T) {
	const text = "first\nsecond"
	for _, method := range []string{"terminal", "clipboard"} {
		t.Run(method, func(t *testing.T) {
			m := &Model{edit: &pendingEdit{}}
			m.pickedEdit(editComment)
			submitted := false
			m.input.onSubmit = func(value string) tea.Cmd {
				submitted = true
				if value != text {
					t.Fatalf("submitted text = %q, want %q", value, text)
				}
				return nil
			}
			m.input.err = "invalid input"
			m.Update(struct{}{})
			if m.input.err == "" {
				t.Fatal("unrelated message cleared the validation error")
			}
			if method == "clipboard" {
				if runtime.GOOS != "darwin" {
					t.Skip("clipboard stub uses macOS pbpaste")
				}
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "pbpaste"), []byte("#!/bin/sh\nprintf 'first\\nsecond'\n"), 0o700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
				_, cmd := m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
				if cmd == nil {
					t.Fatal("ctrl+v did not request clipboard text")
				}
				m.Update(cmd())
			} else {
				m.Update(tea.PasteMsg{Content: text})
			}
			if got := m.input.area.Value(); got != text {
				t.Fatalf("pasted text = %q, want %q", got, text)
			}
			if submitted {
				t.Fatal("paste submitted the comment")
			}
			if m.input.err != "" {
				t.Fatal("paste did not clear the validation error")
			}
			m.Update(ctrlS)
			if !submitted {
				t.Fatal("ctrl+s did not submit the comment")
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			if m.input != nil {
				t.Fatal("esc did not close the editor")
			}
		})
	}
}

// A missing clipboard tool shows why ctrl+v did nothing and keeps the text.
func TestInputBoxClipboardFailure(t *testing.T) {
	m := &Model{edit: &pendingEdit{}}
	m.pickedEdit(editComment)
	m.input.area.SetValue("kept")
	t.Setenv("PATH", t.TempDir())
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+v did not request clipboard text")
	}
	m.Update(cmd())
	if !strings.Contains(m.input.err, "terminal's paste") || m.input.area.Value() != "kept" {
		t.Fatalf("err = %q, value = %q", m.input.err, m.input.area.Value())
	}
	m.Update(tea.PasteMsg{Content: "!"})
	if m.input.err != "" || m.input.area.Value() != "kept!" {
		t.Fatalf("terminal paste after failure: err = %q, value = %q", m.input.err, m.input.area.Value())
	}
}

func TestCommentBoxGrowsAndScrolls(t *testing.T) {
	m := &Model{edit: &pendingEdit{}}
	m.pickedEdit(editComment)
	b := m.input
	for _, tc := range []struct {
		name          string
		text          string
		width, height int
		want          int
	}{
		{"empty", "", 100, 30, 10},
		{"newlines", strings.Repeat("line\n", 12), 100, 30, 13},
		{"wrapped", strings.Repeat("x", 1100), 100, 30, 12},
		{"scroll", strings.Repeat("line\n", 20), 100, 30, 16},
		{"short terminal", strings.Repeat("line\n", 20), 80, 10, 8},
		{"shrink", "short", 100, 30, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b.area.SetValue(tc.text)
			view := b.view(tc.width, tc.height, lipgloss.NewStyle())
			if got := b.area.Height(); got != tc.want {
				t.Fatalf("height = %d, want %d", got, tc.want)
			}
			if lipgloss.Height(view) > tc.height || b.area.Value() != tc.text {
				t.Fatal("editor exceeds available height or changes text")
			}
		})
	}
	b.area.SetValue(strings.Repeat("line\n", 20))
	b.view(100, 30, lipgloss.NewStyle())
	b.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if b.area.LineCount() != 22 || b.area.Height() != 16 {
		t.Fatal("viewport limit must scroll, not block new lines")
	}
	if view := b.area.View(); !strings.Contains(view, "line") {
		t.Fatal("scrolled viewport lost preceding text")
	}
	// Resize existing content without changing it.
	b.area.SetValue(strings.Repeat("x", 1100))
	b.view(60, 30, lipgloss.NewStyle())
	if b.area.Height() != 16 {
		t.Fatal("narrowing editor did not grow wrapped content")
	}
	b.view(100, 30, lipgloss.NewStyle())
	if b.area.Height() != 12 {
		t.Fatal("widening editor did not shrink wrapped content")
	}
}

func TestCommentBoxUsesAvailableWidth(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	for _, width := range []int{80, 140} {
		m.width, m.height = width, 40
		m.edit = &pendingEdit{}
		m.pickedEdit(editComment)
		m.body()
		if got, want := m.input.area.Width(), min(width-4, 100)-2; got != want {
			t.Fatalf("terminal width %d: editor width = %d, want %d", width, got, want)
		}
		m.input = newInputBox("Branch", "")
		m.body()
		if m.input.area.Width() != min(width-4, 80)-2 || m.input.area.Height() != 8 {
			t.Fatal("non-comment input dimensions changed")
		}
	}
}
