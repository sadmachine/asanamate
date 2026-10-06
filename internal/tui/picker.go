package tui

import (
	"cmp"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/keymap"
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

// picker is a modal list. It starts in browse mode, or in search mode with
// picker.type_first; search filters by typed words. With keySelect it
// selects by each item's Key instead. With allowFree, the search text can be
// returned as is. With multi, toggle checks items and choose returns them.
type picker struct {
	onPick    func(pickResult) tea.Cmd // runs on the returned choice
	title     string
	items     []pickItem
	matches   []int
	cursor    int
	input     textinput.Model
	keySelect bool
	searching bool
	helpIcon  string // resolved symbol set's info icon; empty uses (i)
	allowFree bool
	multi     bool
	checked   map[int]bool
	err       string
}

// newPicker returns a picker that calls onPick with the choice.
func newPicker(onPick func(pickResult) tea.Cmd, title string, items []pickItem) *picker {
	in := textinput.New()
	in.Prompt = "/ "
	p := &picker{onPick: onPick, title: title, items: items, input: in}
	if typeFirst {
		p.searching = true
		p.input.Focus()
	}
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
	p.cursor = max(min(p.cursor, p.rowCount()-1), 0)
}

// useRow reports whether the list ends with a row that picks the search text.
func (p *picker) useRow() bool {
	return p.allowFree && p.searching && strings.TrimSpace(p.input.Value()) != ""
}

// rowCount is the number of rows the cursor moves over.
func (p *picker) rowCount() int {
	if p.useRow() {
		return len(p.matches) + 1
	}
	return len(p.matches)
}

// stopSearch clears the search and returns to browse mode, keeping the
// highlighted item highlighted, or going to the top from the Use row.
func (p *picker) stopSearch() {
	selected := 0
	if p.cursor < len(p.matches) {
		selected = p.matches[p.cursor]
	}
	p.searching = false
	p.input.Blur()
	p.input.SetValue("")
	p.refilter()
	p.cursor = selected
}

// checkedResult returns the checked items of a multi picker, in list order.
func (p *picker) checkedResult() pickResult {
	res := pickResult{done: true, items: []pickItem{}}
	for i, it := range p.items {
		if p.checked[i] {
			res.items = append(res.items, it)
		}
	}
	return res
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
	if p.keySelect {
		for i := range p.items {
			if p.items[i].Key == msg.String() {
				return p.choose(i), nil
			}
		}
	}
	// Key-select pickers keep printable keys for their items.
	var name string
	if !p.keySelect || !keymap.Printable(msg.String()) {
		name = bound("picker", msg, p.searching && !p.keySelect)
	}
	switch name {
	case "cancel":
		// Key-select pickers have no search, even with picker.type_first.
		if p.searching && !p.keySelect {
			p.stopSearch()
			return pickResult{}, nil
		}
		return pickResult{cancelled: true}, nil
	case "down":
		p.cursor = max(min(p.cursor+1, p.rowCount()-1), 0)
		return pickResult{}, nil
	case "up":
		p.cursor = max(p.cursor-1, 0)
		return pickResult{}, nil
	case "top":
		p.cursor = 0
		return pickResult{}, nil
	case "bottom":
		p.cursor = max(p.rowCount()-1, 0)
		return pickResult{}, nil
	case "choose":
		switch {
		case p.multi:
			return p.checkedResult(), nil
		case p.cursor < len(p.matches):
			return p.choose(p.matches[p.cursor]), nil
		}
		res, _ := p.freeText()
		return res, nil
	case "toggle":
		if p.multi && p.cursor < len(p.matches) {
			i := p.matches[p.cursor]
			p.checked[i] = !p.checked[i]
		}
		return pickResult{}, nil
	case "use_typed":
		res, _ := p.freeText()
		return res, nil
	case "search":
		if !p.keySelect {
			p.searching = true
			return pickResult{}, p.input.Focus()
		}
	}
	if !p.searching || p.keySelect {
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
	if !p.keySelect && p.searching {
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
			if p.cursor < len(p.matches) {
				text = p.items[p.matches[p.cursor]].Help
			}
			help = modalHelp(text, p.helpIcon, width, min(2, max(height-6, 1)))
			break
		}
	}
	rows := max(height-4-lipgloss.Height(help), 1)
	start := max(p.cursor-rows+1, 0)
	if p.rowCount() == 0 {
		b.WriteString(dimStyle.Render("no matches") + "\n")
	}
	for i := start; i < p.rowCount() && i < start+rows; i++ {
		var line string
		if i == len(p.matches) {
			line = fmt.Sprintf("Use %q", strings.TrimSpace(p.input.Value()))
		} else {
			it := p.items[p.matches[i]]
			line = it.Label
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
	var hints [][2]string
	if p.searching && !p.keySelect {
		hints = append(hints, [2]string{typedLabel("picker", "down", "up"), "move"})
	} else if !p.keySelect {
		hints = append(hints, [2]string{keyLabel("picker", "down", "up"), "move"}, [2]string{keyLabel("picker", "search"), "search"})
	}
	if p.multi {
		toggle := keyLabel("picker", "toggle")
		if p.searching {
			toggle = typedLabel("picker", "toggle")
		}
		hints = append(hints, [2]string{toggle, "toggle"}, [2]string{keyLabel("picker", "choose"), "done"})
	} else {
		hints = append(hints, [2]string{keyLabel("picker", "choose"), "select"})
	}
	if p.allowFree {
		hints = append(hints, [2]string{keyLabel("picker", "use_typed"), "use typed path"})
	}
	cancel := "cancel"
	if p.searching && !p.keySelect {
		cancel = "clear search"
	}
	b.WriteString(hintLine(accent, append(hints, [2]string{keyLabel("picker", "cancel"), cancel})...))
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
