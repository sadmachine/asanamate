package tui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// inputBox is a modal for free-form, multi-line text.
type inputBox struct {
	title string
	area  textarea.Model
}

func newInputBox(title string) *inputBox {
	a := textarea.New()
	a.ShowLineNumbers = false
	a.Prompt = ""
	a.Placeholder = "optional; leave empty to skip"
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
	return pickResult{}, cmd
}

func (b *inputBox) view(width, height int) string {
	b.area.SetWidth(max(width-2, 1))
	b.area.SetHeight(max(min(height-2, 8), 1))
	return titleStyle.Render(b.title) + "\n" + b.area.View() + "\n" +
		dimStyle.Render("enter newline · ctrl+s run · esc cancel")
}
