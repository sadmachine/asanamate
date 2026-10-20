package tui

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/extensions"
)

type extensionDetail struct {
	entry  extensions.Entry
	saved  extensions.Saved
	report extensions.Report
	reader viewport.Model
	busy   bool
	notice string
}

type extensionCheckedMsg struct {
	detail *extensionDetail
	report extensions.Report
}

func (m *Model) extensionSaved() extensions.Saved {
	path, err := m.configPath()
	if err != nil {
		return extensions.Saved{Err: err}
	}
	saved := extensions.Load(path)
	saved.Version = m.deps.Version
	return saved
}

func (m *Model) openExtensions() {
	m.extension = nil
	saved := m.extensionSaved()
	var items []pickItem
	for _, e := range extensions.Catalog() {
		r := extensions.Inspect(e, saved)
		items = append(items, pickItem{Label: e.Name + " [" + r.ListStatus() + "]", Hint: e.Benefit, Help: e.Benefit + ". " + r.Summary() + ". " + r.Check + ". " + e.Availability, Search: e.ID + " " + e.Benefit, Value: e})
	}
	m.modal = newPicker(pickValue(func(e extensions.Entry) tea.Cmd {
		m.openExtension(e)
		return nil
	}), "Extensions", items)
	m.modal.notice = saved.Status() + "; local inspection only"
}

func (m *Model) openExtension(e extensions.Entry) {
	saved := m.extensionSaved()
	m.modal = nil
	m.extension = &extensionDetail{entry: e, saved: saved, report: extensions.Inspect(e, saved), reader: viewport.New()}
}

func (a *extensionDetail) view(width, height int) string {
	header := ansi.Truncate(a.entry.Name, width, "…") + "\n" + ansi.Truncate("Next: "+a.report.Next, width, "…")
	footer := ansi.Hardwrap(keyLabel("picker", "down", "up")+" scroll  "+keyLabel("picker", "choose")+" actions  "+keyLabel("picker", "cancel")+" back", width, true)
	if a.busy {
		footer = "Checking locally…\n" + footer
	}
	body := extensions.Detail(a.entry, a.saved, a.report)
	if a.notice != "" {
		body = a.notice + "\n\n" + body
	}
	a.reader.SetWidth(width)
	a.reader.SetHeight(max(height-lipgloss.Height(header)-lipgloss.Height(footer)-2, 1))
	a.reader.SetContent(ansi.Hardwrap(body, width, true))
	return header + "\n\n" + a.reader.View() + "\n" + footer
}

func (m *Model) updateExtension(msg tea.KeyPressMsg) tea.Cmd {
	a := m.extension
	switch bound("picker", msg, false) {
	case "cancel":
		m.openExtensions()
	case "choose":
		m.extensionActions()
	case "down":
		a.reader.ScrollDown(1)
	case "up":
		a.reader.ScrollUp(1)
	case "top":
		a.reader.GotoTop()
	case "bottom":
		a.reader.GotoBottom()
	default:
		a.reader, _ = a.reader.Update(msg)
	}
	return nil
}

func (m *Model) extensionActions() {
	a := m.extension
	items := []pickItem{
		{Label: "Recheck", Hint: strings.Join(a.entry.Check, " "), Disabled: a.busy, Value: func() tea.Cmd {
			m.modal = nil
			a.saved, a.busy, a.notice = m.extensionSaved(), true, ""
			// Capture values before the command runs off the UI goroutine.
			e, saved := a.entry, a.saved
			return func() tea.Msg { return extensionCheckedMsg{a, extensions.Check(context.Background(), e, saved)} }
		}},
		{Label: "Copy configuration example", Value: func() tea.Cmd {
			m.modal = nil
			a.notice = "Copy requested. If clipboard is unavailable, copy the visible example or CLI guide."
			return tea.SetClipboard(a.entry.Snippet)
		}},
		{Label: "Copy full guide and commands", Value: func() tea.Cmd {
			m.modal = nil
			a.notice = "Copy requested. Text remains visible; CLI: asanamate extensions " + a.entry.ID
			return tea.SetClipboard(extensions.Detail(a.entry, a.saved, a.report))
		}},
		{Label: "Open guide", Help: a.entry.Availability, Hint: a.entry.Guide, Value: func() tea.Cmd {
			m.modal = nil
			return openURL(a.entry.Guide)
		}},
		{Label: "Build your own extension", Hint: extensions.DocumentURL(a.saved.Version, "docs/extensions.md", "implementing-an-extension"), Value: func() tea.Cmd {
			m.modal = nil
			return openURL(extensions.DocumentURL(a.saved.Version, "docs/extensions.md", "implementing-an-extension"))
		}},
		{Label: "Edit config", Hint: extensions.Clean(a.saved.Path), Value: m.editExtensionConfig},
	}
	for _, contract := range a.entry.Contracts() {
		url := extensions.DocumentURL(a.saved.Version, "docs/extensions.md", contract.Anchor)
		items = append(items, pickItem{Label: "Read " + contract.Label() + " contract", Hint: url, Value: func() tea.Cmd {
			m.modal = nil
			return openURL(url)
		}})
	}
	if len(a.entry.Setup) > 0 {
		items = append(items, pickItem{Label: cmp.Or(a.entry.SetupLabel, "Run provider setup…"), Hint: strings.Join(a.entry.Setup, " "), Value: func() tea.Cmd { m.confirmExtensionSetup(); return nil }})
	}
	if len(a.entry.Diagnostics) > 0 {
		items = append(items, pickItem{Label: "Run detailed diagnostics…", Hint: strings.Join(a.entry.Diagnostics, " "), Value: func() tea.Cmd {
			m.confirmExtensionCommand("Run detailed diagnostics?", a.entry.Diagnostics)
			return nil
		}})
	}
	if a.entry.Builder {
		items = append(items, pickItem{Label: "Open action builder", Value: func() tea.Cmd { m.modal = nil; m.openActionBuilder(); return nil }})
	}
	items = append(items, pickItem{Label: "Back to details", Value: func() tea.Cmd { m.modal = nil; return nil }})
	m.modal = newPicker(pickValue(func(run func() tea.Cmd) tea.Cmd { return run() }), a.entry.Name+" actions", items)
}

func (m *Model) confirmExtensionSetup() {
	m.confirmExtensionCommand(cmp.Or(m.extension.entry.SetupLabel, "Run provider setup?"), m.extension.entry.Setup)
}

func (m *Model) confirmExtensionCommand(title string, args []string) {
	a := m.extension
	path, err := exec.LookPath(args[0])
	if err == nil {
		path, err = filepath.Abs(path)
	}
	if err != nil {
		m.modal = nil
		a.notice = "Executable missing: " + args[0] + ". Follow installation steps."
		return
	}
	p := newPicker(pickValue(func(run bool) tea.Cmd {
		m.modal = nil
		if !run {
			return nil
		}
		return tea.ExecProcess(exec.Command(path, args[1:]...), func(err error) tea.Msg {
			return editorDoneMsg{err: err, apply: func(_ string, err error) {
				if m.extension != a {
					return
				}
				a.saved = m.extensionSaved()
				a.report = extensions.Inspect(a.entry, a.saved)
				a.notice = "Command returned. Recheck to verify local readiness. Restart after config edits."
				if err != nil {
					a.notice = "Command cancelled or failed. Details remain available; follow the guide to diagnose."
				}
			}}
		})
	}), title, []pickItem{{Label: "Run " + strings.Join(args, " "), Value: true}, {Label: "Cancel", Value: false}})
	p.notice = "Executable: " + extensions.Clean(path) + ". Provider prompts control their own writes. Core config is not changed by this screen."
	m.modal = p
}

func (m *Model) editExtensionConfig() tea.Cmd {
	a := m.extension
	m.modal = nil
	if a.saved.Path == "" {
		a.notice = "Config path unavailable: " + a.saved.Status()
		return nil
	}
	if _, err := os.Stat(a.saved.Path); err != nil {
		a.notice = "Core setup required: run asanamate setup first. Guide and example remain available."
		return nil
	}
	editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	cmd := exec.Command("/bin/sh", "-c", editor+` "$1"`, "asanamate-config", a.saved.Path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{err: err, apply: func(_ string, err error) {
			if m.extension != a {
				return
			}
			a.saved = m.extensionSaved() // Existing config validation, without hot reload.
			a.report = extensions.Inspect(a.entry, a.saved)
			a.notice = "Saved config checked. Restart asanamate to activate hand edits."
			if err != nil {
				a.notice = fmt.Sprintf("Config editor failed: %s. Edit %s yourself.", extensions.Clean(err.Error()), extensions.Clean(a.saved.Path))
			}
		}}
	})
}
