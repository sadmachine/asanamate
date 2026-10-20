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

	"github.com/sadmachine/asanamate/internal/addons"
)

type addonDetail struct {
	entry  addons.Entry
	saved  addons.Saved
	report addons.Report
	reader viewport.Model
	busy   bool
	notice string
}

type addonCheckedMsg struct {
	detail *addonDetail
	report addons.Report
}

func (m *Model) addonSaved() addons.Saved {
	path, err := m.configPath()
	if err != nil {
		return addons.Saved{Err: err}
	}
	return addons.Load(path)
}

func (m *Model) openAddons() {
	m.addon = nil
	saved := m.addonSaved()
	var items []pickItem
	for _, e := range addons.Catalog() {
		r := addons.Inspect(e, saved)
		items = append(items, pickItem{Label: e.Name + " [" + r.ListStatus() + "]", Hint: e.Benefit, Help: e.Benefit + ". " + r.Summary() + ". " + r.Check + ". " + e.Availability, Search: e.ID + " " + e.Benefit, Value: e})
	}
	m.modal = newPicker(pickValue(func(e addons.Entry) tea.Cmd {
		m.openAddon(e)
		return nil
	}), "Addons", items)
	m.modal.notice = saved.Status() + "; local inspection only"
}

func (m *Model) openAddon(e addons.Entry) {
	saved := m.addonSaved()
	m.modal = nil
	m.addon = &addonDetail{entry: e, saved: saved, report: addons.Inspect(e, saved), reader: viewport.New()}
}

func (a *addonDetail) view(width, height int) string {
	header := ansi.Truncate(a.entry.Name, width, "…") + "\n" + ansi.Truncate("Next: "+a.report.Next, width, "…")
	footer := ansi.Hardwrap(keyLabel("picker", "down", "up")+" scroll  "+keyLabel("picker", "choose")+" actions  "+keyLabel("picker", "cancel")+" back", width, true)
	if a.busy {
		footer = "Checking locally…\n" + footer
	}
	body := addons.Detail(a.entry, a.saved, a.report)
	if a.notice != "" {
		body = a.notice + "\n\n" + body
	}
	a.reader.SetWidth(width)
	a.reader.SetHeight(max(height-lipgloss.Height(header)-lipgloss.Height(footer)-2, 1))
	a.reader.SetContent(ansi.Hardwrap(body, width, true))
	return header + "\n\n" + a.reader.View() + "\n" + footer
}

func (m *Model) updateAddon(msg tea.KeyPressMsg) tea.Cmd {
	a := m.addon
	switch bound("picker", msg, false) {
	case "cancel":
		m.openAddons()
	case "choose":
		m.addonActions()
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

func (m *Model) addonActions() {
	a := m.addon
	items := []pickItem{
		{Label: "Recheck", Hint: strings.Join(a.entry.Check, " "), Disabled: a.busy, Value: func() tea.Cmd {
			m.modal = nil
			a.saved, a.busy, a.notice = m.addonSaved(), true, ""
			// Capture values before the command runs off the UI goroutine.
			e, saved := a.entry, a.saved
			return func() tea.Msg { return addonCheckedMsg{a, addons.Check(context.Background(), e, saved)} }
		}},
		{Label: "Copy configuration example", Value: func() tea.Cmd {
			m.modal = nil
			a.notice = "Copy requested. If clipboard is unavailable, copy the visible example or CLI guide."
			return tea.SetClipboard(a.entry.Snippet)
		}},
		{Label: "Copy full guide and commands", Value: func() tea.Cmd {
			m.modal = nil
			a.notice = "Copy requested. Text remains visible; CLI: asanamate addons " + a.entry.ID
			return tea.SetClipboard(addons.Detail(a.entry, a.saved, a.report))
		}},
		{Label: "Open guide", Help: a.entry.Availability, Hint: a.entry.Guide, Value: func() tea.Cmd {
			m.modal = nil
			return openURL(a.entry.Guide)
		}},
		{Label: "Edit config", Hint: addons.Clean(a.saved.Path), Value: m.editAddonConfig},
	}
	if len(a.entry.Setup) > 0 {
		items = append(items, pickItem{Label: "Run provider setup…", Hint: strings.Join(a.entry.Setup, " "), Value: func() tea.Cmd { m.confirmAddonSetup(); return nil }})
	}
	if a.entry.Builder {
		items = append(items, pickItem{Label: "Open action builder", Value: func() tea.Cmd { m.modal = nil; m.openActionBuilder(); return nil }})
	}
	items = append(items, pickItem{Label: "Back to details", Value: func() tea.Cmd { m.modal = nil; return nil }})
	m.modal = newPicker(pickValue(func(run func() tea.Cmd) tea.Cmd { return run() }), a.entry.Name+" actions", items)
}

func (m *Model) confirmAddonSetup() {
	a := m.addon
	args := a.entry.Setup
	path, err := exec.LookPath(args[0])
	if err == nil {
		path, err = filepath.Abs(path)
	}
	if err != nil {
		m.modal = nil
		a.notice = "Setup executable missing: " + args[0] + ". Follow installation steps."
		return
	}
	p := newPicker(pickValue(func(run bool) tea.Cmd {
		m.modal = nil
		if !run {
			return nil
		}
		return tea.ExecProcess(exec.Command(path, args[1:]...), func(err error) tea.Msg {
			return editorDoneMsg{err: err, apply: func(_ string, err error) {
				if m.addon != a {
					return
				}
				a.saved = m.addonSaved()
				a.report = addons.Inspect(a.entry, a.saved)
				a.notice = "Provider setup returned. Recheck to verify local readiness. Restart after config edits."
				if err != nil {
					a.notice = "Provider setup cancelled or failed. Details remain available; recheck before retrying."
				}
			}}
		})
	}), "Run provider setup?", []pickItem{{Label: "Run " + strings.Join(args, " "), Value: true}, {Label: "Cancel", Value: false}})
	p.notice = "Executable: " + addons.Clean(path) + ". Provider prompts control their own writes. Core config is not changed by this screen."
	m.modal = p
}

func (m *Model) editAddonConfig() tea.Cmd {
	a := m.addon
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
			if m.addon != a {
				return
			}
			a.saved = m.addonSaved() // Existing config validation, without hot reload.
			a.report = addons.Inspect(a.entry, a.saved)
			a.notice = "Saved config checked. Restart asanamate to activate hand edits."
			if err != nil {
				a.notice = fmt.Sprintf("Config editor failed: %s. Edit %s yourself.", addons.Clean(err.Error()), addons.Clean(a.saved.Path))
			}
		}}
	})
}
