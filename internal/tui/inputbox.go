package tui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// inputBox is a modal for free-form, multi-line text.
type inputBox struct {
	title    string
	area     textarea.Model
	err      string
	onSubmit func(string) tea.Cmd // optional handler outside ticket actions and edits
}

func newInputBox(title, placeholder string) *inputBox {
	a := textarea.New()
	a.ShowLineNumbers = false
	a.Prompt = ""
	a.Placeholder = placeholder
	a.Focus()
	return &inputBox{title: title, area: a}
}

// update returns done with the trimmed text on ctrl+s, or cancelled on esc.
func (b *inputBox) update(msg tea.KeyPressMsg) (res pickResult, cmd tea.Cmd) {
	switch msg.String() {
	case "esc":
		return pickResult{cancelled: true}, nil
	case "ctrl+s":
		return pickResult{done: true, free: strings.TrimSpace(b.area.Value())}, nil
	}
	b.area, cmd = b.area.Update(msg)
	b.err = ""
	return pickResult{}, cmd
}

func (b *inputBox) view(width, height int, accent lipgloss.Style) string {
	errorLine := ""
	available := height - 2 // title and keyboard hints
	if b.err != "" {
		errorLine = errorStyle.Render(b.err) + "\n"
		available -= lipgloss.Height(b.err)
	}
	available = max(available, 1)
	if b.area.DynamicHeight {
		b.area.MaxHeight = min(available, 16)
	}
	// SetWidth also recalculates dynamic height, including soft-wrapped lines.
	b.area.SetWidth(max(width-2, 1))
	if !b.area.DynamicHeight {
		b.area.SetHeight(min(available, 8))
	}
	return accent.Render(b.title) + "\n" + b.area.View() + "\n" + errorLine +
		accent.Render("enter") + dimStyle.Render(" newline · ") + accent.Render("ctrl+s") +
		dimStyle.Render(" submit · ") + accent.Render("esc") + dimStyle.Render(" cancel")
}
