package tui

import (
	"math"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/clipboard"
)

// inputBox is a modal for free-form, multi-line text.
type inputBox struct {
	title          string
	help           string // optional persistent description above keyboard hints
	notice         string // optional warning, such as uncertain delivery
	progress       string // a running request, drawn with the spinner
	area           textarea.Model
	err            string
	onSubmit       func(string) tea.Cmd // optional handler outside ticket actions and edits
	onSaveDraft    func() tea.Cmd       // only inputs with persistent drafts
	sanitizedPaste bool                 // tracks content changed by the textarea during paste
	images         bool                 // paste_image inserts clipboard images as Markdown
	oneLine        bool                 // enter adds no newline, as for a title
	maxTextWidth   int                  // caps the text's wrap width, as reader.max_text_width; 0 for none
}

// imagePasteMsg is a clipboard image saved to path for an input box, or why not.
type imagePasteMsg struct {
	box  *inputBox
	path string
	err  error
}

func newInputBox(title, placeholder string) *inputBox {
	a := textarea.New()
	a.ShowLineNumbers = false
	a.Prompt = ""
	a.Placeholder = placeholder
	a.Focus()
	return &inputBox{title: title, area: a}
}

// update returns done with the trimmed text on submit, or cancelled on cancel.
func (b *inputBox) update(msg tea.Msg) (res pickResult, cmd tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch bound("input", key, true) {
		case "cancel":
			return pickResult{cancelled: true}, nil
		case "submit":
			return pickResult{done: true, free: strings.TrimSpace(b.area.Value())}, nil
		case "save_draft":
			if b.onSaveDraft != nil {
				return pickResult{}, b.onSaveDraft()
			}
		case "paste_image":
			if b.images {
				b.err = ""
				return pickResult{}, func() tea.Msg {
					path, err := clipboard.SaveImage()
					return imagePasteMsg{box: b, path: path, err: err}
				}
			}
		case "external_editor":
			return pickResult{}, externalEditor(b.area.Value(), func(text string, err error) {
				if err != nil {
					b.err = err.Error()
					return
				}
				b.area.SetValue(text)
				b.err = ""
				if b.area.Value() != text {
					b.sanitizedPaste = true
					b.err = "Editor changes tabs/control characters or exceeds the input limit. Review before submitting."
				}
			})
		}
		b.err = ""
	} else if pasted, ok := msg.(imagePasteMsg); ok {
		if pasted.box != nil && pasted.box != b {
			return pickResult{}, nil
		}
		if pasted.err != nil {
			b.err = "image paste failed: " + pasted.err.Error()
			return pickResult{}, nil
		}
		dest := pasted.path
		if strings.ContainsAny(dest, " ()<>") {
			dest = "<" + dest + ">"
		}
		b.area.InsertString("![](" + dest + ")")
		return pickResult{}, nil
	} else if paste, ok := msg.(tea.PasteMsg); ok {
		b.err = ""
		// Use the widget's sanitizer rather than duplicating its conversion rules.
		probe := textarea.New()
		probe.MaxContentHeight = math.MaxInt
		probe.SetValue(paste.Content)
		b.sanitizedPaste = b.sanitizedPaste || probe.Value() != paste.Content
	}
	b.area, cmd = b.area.Update(msg)
	if b.area.Err != nil {
		// ctrl+v reads the OS clipboard; terminal paste works without it.
		b.err = "ctrl+v paste failed (" + b.area.Err.Error() + "); use your terminal's paste instead"
		b.area.Err = nil
	}
	return pickResult{}, cmd
}

func (b *inputBox) view(width, height int, look modalLook) string {
	return b.layout(width, height, look, "", b.hints("cancel")...)
}

// hints are the box's keyboard hints, with cancel labelling the cancel key.
func (b *inputBox) hints(cancel string) [][2]string {
	newlineHint, pasteHint := [2]string{"enter", "newline"}, [2]string{}
	if b.oneLine {
		newlineHint = [2]string{}
	}
	if b.images {
		pasteHint = [2]string{keyLabel("input", "paste_image"), "paste image"}
	}
	draftHint := [2]string{}
	if b.onSaveDraft != nil {
		draftHint = [2]string{keyLabel("input", "save_draft"), "save draft"}
	}
	return [][2]string{newlineHint, pasteHint, {keyLabel("input", "submit"), "submit"}, draftHint, {keyLabel("input", "external_editor"), "editor"}, {keyLabel("input", "cancel"), cancel}}
}

// layout renders the box with footer, such as a composer's task controls,
// between the text and the keyboard hints.
func (b *inputBox) layout(width, height int, look modalLook, footer string, keyHints ...[2]string) string {
	return look.accent.Render(b.title) + "\n" + b.body(width, height-1, look, footer, keyHints...)
}

// body is layout without the title, for a pane whose edge shows it.
func (b *inputBox) body(width, height int, look modalLook, footer string, keyHints ...[2]string) string {
	hints := wrapHints(look.accent, max(width-2, 1), keyHints...)
	available := height - lipgloss.Height(hints)
	if footer != "" {
		available -= lipgloss.Height(footer)
		footer += "\n"
	}
	status := ""
	if b.help != "" {
		status += look.message(statusInfo, b.help, width, min(2, max(height-3, 1))) + "\n"
	}
	for _, msg := range []struct {
		level statusLevel
		text  string
	}{{statusWarning, b.notice}, {statusBusy, b.progress}, {statusError, b.err}} {
		if msg.text != "" {
			status += look.message(msg.level, msg.text, width, 0) + "\n"
		}
	}
	available -= lipgloss.Height(status) - 1
	available = max(available, 1)
	if b.area.DynamicHeight {
		b.area.MaxHeight = min(available, 16)
	}
	// SetWidth also recalculates dynamic height, including soft-wrapped lines.
	textWidth := max(width-2, 1)
	if b.maxTextWidth > 0 {
		textWidth = min(textWidth, b.maxTextWidth)
	}
	b.area.SetWidth(textWidth)
	if !b.area.DynamicHeight {
		b.area.SetHeight(min(available, 8))
	}
	return b.area.View() + "\n" + status + footer + hints
}
