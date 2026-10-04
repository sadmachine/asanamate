package tui

import (
	"math"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// inputBox is a modal for free-form, multi-line text.
type inputBox struct {
	title          string
	help           string // optional persistent description above keyboard hints
	helpIcon       string // resolved symbol set's info icon
	area           textarea.Model
	err            string
	onSubmit       func(string) tea.Cmd // optional handler outside ticket actions and edits
	sanitizedPaste bool                 // tracks content changed by the textarea during paste
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
func (b *inputBox) update(msg tea.Msg) (res pickResult, cmd tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			return pickResult{cancelled: true}, nil
		case "ctrl+s":
			return pickResult{done: true, free: strings.TrimSpace(b.area.Value())}, nil
		}
		b.err = ""
	} else if paste, ok := msg.(tea.PasteMsg); ok {
		b.err = ""
		// Use the widget's sanitizer rather than duplicating its conversion rules.
		probe := textarea.New()
		probe.MaxContentHeight = math.MaxInt
		probe.SetValue(paste.Content)
		b.sanitizedPaste = b.sanitizedPaste || probe.Value() != paste.Content
	}
	b.area, cmd = b.area.Update(msg)
	return pickResult{}, cmd
}

func (b *inputBox) view(width, height int, accent lipgloss.Style) string {
	errorLine := ""
	available := height - 2 // title and keyboard hints
	help := ""
	if b.help != "" {
		help = modalHelp(b.help, b.helpIcon, width, min(2, max(height-4, 1))) + "\n"
		available -= lipgloss.Height(strings.TrimSuffix(help, "\n"))
	}
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
	return accent.Render(b.title) + "\n" + b.area.View() + "\n" + errorLine + help +
		accent.Render("enter") + dimStyle.Render(" newline · ") + accent.Render("ctrl+s") +
		dimStyle.Render(" submit · ") + accent.Render("esc") + dimStyle.Render(" cancel")
}
