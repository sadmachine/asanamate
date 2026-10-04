package tui

import (
	"cmp"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type pickItem struct {
	Label string
	Hint  string
	Help  string // optional description shown below the list when selected
	Key   string
	Value any
}

type pickResult struct {
	done      bool
	cancelled bool
	item      *pickItem
	items     []pickItem // checked items, in list order, for a multi picker
	free      string
}

// picker is a modal list. It filters by typed words, or with keySelect it
// selects by each item's Key. With allowFree, typed text can be returned as is.
// With multi, enter toggles items and ctrl+s returns the checked ones.
type picker struct {
	onPick      func(pickResult) tea.Cmd // runs on the returned choice
	title       string
	items       []pickItem
	matches     []int
	cursor      int
	input       textinput.Model
	keySelect   bool
	browseFirst bool // optional j/k navigation; / activates filtering
	searching   bool
	helpIcon    string // resolved symbol set's info icon; empty uses (i)
	allowFree   bool
	multi       bool
	checked     map[int]bool
	err         string
}

// newPicker returns a picker that calls onPick with the choice.
func newPicker(onPick func(pickResult) tea.Cmd, title string, items []pickItem) *picker {
	in := textinput.New()
	in.Prompt = "> "
	in.Focus()
	p := &picker{onPick: onPick, title: title, items: items, input: in}
	p.refilter()
	return p
}

// newMultiPicker is a picker for checking several items; checked holds the
// indexes of items checked at the start.
func newMultiPicker(onPick func(pickResult) tea.Cmd, title string, items []pickItem, checked map[int]bool) *picker {
	p := newPicker(onPick, title, items)
	p.multi, p.checked = true, checked
	return p
}

// pickValue adapts fn, which takes the chosen item's Value, to an onPick.
func pickValue[T any](fn func(T) tea.Cmd) func(pickResult) tea.Cmd {
	return func(res pickResult) tea.Cmd { return fn(res.item.Value.(T)) }
}

func matchesWords(label, query string) bool {
	l := strings.ToLower(label)
	for _, w := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(l, w) {
			return false
		}
	}
	return true
}

func (p *picker) refilter() {
	p.matches = p.matches[:0]
	for i, it := range p.items {
		if matchesWords(it.Label, p.input.Value()) {
			p.matches = append(p.matches, i)
		}
	}
	p.cursor = max(min(p.cursor, len(p.matches)-1), 0)
}

func (p *picker) choose(i int) pickResult {
	it := p.items[i]
	return pickResult{done: true, item: &it}
}

func (p *picker) freeText() (pickResult, bool) {
	text := strings.TrimSpace(p.input.Value())
	if !p.allowFree || text == "" {
		return pickResult{}, false
	}
	return pickResult{done: true, free: text}, true
}

func (p *picker) update(msg tea.KeyPressMsg) (pickResult, tea.Cmd) {
	if p.browseFirst {
		if p.searching && msg.String() == "esc" {
			selected := -1
			if len(p.matches) > 0 {
				selected = p.matches[p.cursor]
			}
			p.searching = false
			p.input.Blur()
			p.input.SetValue("")
			p.refilter()
			if selected >= 0 {
				p.cursor = selected
			}
			return pickResult{}, nil
		}
		if !p.searching {
			switch msg.String() {
			case "/":
				p.searching = true
				p.input.Prompt = "/ "
				return pickResult{}, p.input.Focus()
			case "j":
				msg = tea.KeyPressMsg{Code: tea.KeyDown}
			case "k":
				msg = tea.KeyPressMsg{Code: tea.KeyUp}
			}
		}
	}
	switch msg.String() {
	case "esc":
		return pickResult{cancelled: true}, nil
	case "up", "ctrl+p":
		p.cursor = max(p.cursor-1, 0)
		return pickResult{}, nil
	case "down", "ctrl+n":
		p.cursor = max(min(p.cursor+1, len(p.matches)-1), 0)
		return pickResult{}, nil
	case "enter":
		if p.multi {
			if len(p.matches) > 0 {
				i := p.matches[p.cursor]
				p.checked[i] = !p.checked[i]
			}
			return pickResult{}, nil
		}
		if len(p.matches) > 0 {
			return p.choose(p.matches[p.cursor]), nil
		}
		res, _ := p.freeText()
		return res, nil
	case "tab":
		res, _ := p.freeText()
		return res, nil
	case "ctrl+s":
		if !p.multi {
			return pickResult{}, nil
		}
		res := pickResult{done: true, items: []pickItem{}}
		for i, it := range p.items {
			if p.checked[i] {
				res.items = append(res.items, it)
			}
		}
		return res, nil
	}
	if p.keySelect {
		for i := range p.items {
			if p.items[i].Key == msg.String() {
				return p.choose(i), nil
			}
		}
		return pickResult{}, nil
	}
	if p.browseFirst && !p.searching {
		return pickResult{}, nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.err = ""
	p.refilter()
	return pickResult{}, cmd
}

func (p *picker) view(width, height int, accent lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(accent.Render(p.title) + "\n")
	if !p.keySelect && (!p.browseFirst || p.searching) {
		styles := p.input.Styles()
		styles.Focused.Prompt = accent
		p.input.SetStyles(styles)
		p.input.SetWidth(max(width-2, 1))
		b.WriteString(p.input.View() + "\n")
	}
	help := ""
	for _, item := range p.items {
		if item.Help != "" {
			text := ""
			if len(p.matches) > 0 {
				text = p.items[p.matches[p.cursor]].Help
			}
			help = modalHelp(text, p.helpIcon, width, min(2, max(height-6, 1)))
			break
		}
	}
	rows := max(height-4-lipgloss.Height(help), 1)
	start := max(p.cursor-rows+1, 0)
	if len(p.matches) == 0 {
		b.WriteString(dimStyle.Render("no matches") + "\n")
	}
	for i := start; i < len(p.matches) && i < start+rows; i++ {
		it := p.items[p.matches[i]]
		line := it.Label
		switch {
		case p.keySelect:
			line = accent.Render("["+it.Key+"]") + " " + line
		case p.multi && p.checked[p.matches[i]]:
			line = okStyle.Render("[x]") + " " + line
		case p.multi:
			line = "[ ] " + line
		}
		if it.Hint != "" {
			line += "  " + dimStyle.Render(it.Hint)
		}
		line = ansi.Truncate(line, max(width-4, 1), "…")
		if i == p.cursor {
			selected := "▸ " + ansi.Strip(line)
			line = accent.Reverse(true).Render(selected + strings.Repeat(" ", max(width-2-ansi.StringWidth(selected), 0)))
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}
	if p.err != "" {
		b.WriteString(errorStyle.Render(p.err) + "\n")
	}
	if help != "" {
		b.WriteString(help + "\n")
	}
	hint := accent.Render("enter") + dimStyle.Render(" select · ") + accent.Render("esc") + dimStyle.Render(" cancel")
	if p.browseFirst {
		if p.searching {
			hint = accent.Render("enter") + dimStyle.Render(" select · ") + accent.Render("esc") + dimStyle.Render(" clear search")
		} else {
			hint = accent.Render("j/k") + dimStyle.Render(" move · ") + accent.Render("/") + dimStyle.Render(" search · ") + hint
		}
	}
	if p.multi {
		hint = accent.Render("enter") + dimStyle.Render(" toggle · ") + accent.Render("ctrl+s") +
			dimStyle.Render(" save · ") + accent.Render("esc") + dimStyle.Render(" cancel")
	}
	if p.allowFree {
		hint += dimStyle.Render(" · ") + accent.Render("tab") + dimStyle.Render(" use typed path")
	}
	b.WriteString(hint)
	return b.String()
}

// modalHelp reserves a stable footer height while wrapping the selected
// description. Long descriptions are clipped to keep controls visible.
func modalHelp(text, icon string, width, rows int) string {
	width = max(width-2, 1)
	if text != "" {
		text = cmp.Or(icon, asciiSymbols.info) + " " + text
	}
	lines := strings.Split(ansi.Wrap(text, width, ""), "\n")
	if len(lines) > rows {
		lines = lines[:rows]
		lines[rows-1] = ansi.Truncate(lines[rows-1], max(width-1, 0), "") + "…"
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return dimStyle.Render(strings.Join(lines, "\n"))
}
