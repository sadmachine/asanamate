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
	Disabled bool // visible but cannot be chosen
	Label    string
	Hint     string
	Help     string // optional description shown below the list when selected
	Search   string // optional text search matches besides Label
	Key      string
	Value    any
}

type pickResult struct {
	done      bool
	cancelled bool
	item      *pickItem
	items     []pickItem // checked items, in list order, for a multi picker
	free      string
	reset     bool // return to defaults, when the picker offers a reset
	copy      bool // copy the item instead of picking it, in a link picker
}

// picker is a modal list. It starts in browse mode, or in search mode with
// picker.type_first; search filters by typed words. With keySelect it
// selects by each item's Key instead. With allowFree, the search text can be
// returned as is. With multi, toggle checks items and choose returns them.
type picker struct {
	onPick      func(pickResult) tea.Cmd // runs on the returned choice
	onCopy      func(*pickItem) tea.Cmd  // copies the highlighted item without choosing it
	title       string
	items       []pickItem
	matches     []int
	cursor      int
	input       textinput.Model
	keySelect   bool
	searching   bool
	allowFree   bool
	multi       bool
	reorder     bool // ctrl+up/down reorders items in an unfiltered multi picker
	allowReset  bool // ctrl+r returns a reset result
	links       bool // choose opens the item's link; copy returns a copy result
	checked     map[int]bool
	err         string
	progress    string      // a running request, drawn with the spinner
	notice      string      // owner's status lines shown under the list; search keeps them
	noticeLevel statusLevel // notice severity; zero is info
	extraHints  [][2]string // controls handled by the picker's owner
	// preview draws the highlighted item, or nil for none, beside the list.
	preview func(it *pickItem, width, height int) string
	// listMaxWidth caps the list beside a preview; zero uses 32 columns.
	listMaxWidth int
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
		if matchesWords(it.Label+" "+it.Search, p.input.Value()) {
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
	if it.Disabled {
		return pickResult{}
	}
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
	if p.multi {
		switch msg.String() {
		case "ctrl+r":
			if p.allowReset {
				return pickResult{done: true, reset: true}, nil
			}
		case "ctrl+up", "ctrl+down":
			if p.reorder {
				p.moveItem(msg.String() == "ctrl+down")
				return pickResult{}, nil
			}
		}
	}
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
	case "copy":
		if p.onCopy != nil {
			if item := p.current(); item != nil {
				return pickResult{}, p.onCopy(item)
			}
			return pickResult{}, nil
		}
		if p.links && p.cursor < len(p.matches) {
			res := p.choose(p.matches[p.cursor])
			res.copy = true
			return res, nil
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

// moveItem keeps checks attached to their items, and the cursor on the moved
// item. Reordering requires an unfiltered list so its order is unambiguous.
func (p *picker) moveItem(down bool) {
	if p.input.Value() != "" {
		p.err = "Clear search before reordering"
		return
	}
	i, j := p.cursor, p.cursor-1
	if down {
		j = i + 1
	}
	if i < 0 || i >= len(p.items) || j < 0 || j >= len(p.items) {
		return
	}
	p.items[i], p.items[j] = p.items[j], p.items[i]
	p.checked[i], p.checked[j] = p.checked[j], p.checked[i]
	p.cursor = j
	p.err = ""
	p.refilter()
}

// current returns the highlighted item, or nil on the Use row, with no
// matches, or for a nil picker.
func (p *picker) current() *pickItem {
	if p == nil || p.cursor >= len(p.matches) {
		return nil
	}
	return &p.items[p.matches[p.cursor]]
}

// minPreviewWidth is the narrowest modal that shows a picker's preview.
const minPreviewWidth = 56

func (p *picker) view(width, height int, look modalLook) string {
	if p.preview == nil || width < minPreviewWidth {
		return p.listView(width, height, look)
	}
	listW := max(min(width*2/5, cmp.Or(p.listMaxWidth, 32)), 24)
	paneW := width - listW - 3 // divider with one space on either side
	list := p.listView(listW, height, look)
	preview := p.preview(p.current(), paneW, height)
	rows := max(lipgloss.Height(list), lipgloss.Height(preview))
	divider := lipgloss.NewStyle().Foreground(borderColor).Padding(0, 1).
		Render(strings.Repeat("│\n", rows-1) + "│")
	return lipgloss.JoinHorizontal(lipgloss.Top, list, divider, preview)
}

// listView draws the title, search, list, and key hints.
func (p *picker) listView(width, height int, look modalLook) string {
	accent := look.accent
	var b strings.Builder
	b.WriteString(accent.Render(ansi.Truncate(p.title, width, "…")) + "\n")
	if !p.keySelect && p.searching {
		styles := p.input.Styles()
		styles.Focused.Prompt = accent
		p.input.SetStyles(styles)
		// Text input width excludes its prompt and trailing cursor cell.
		p.input.SetWidth(max(width-ansi.StringWidth(p.input.Prompt)-1, 1))
		b.WriteString(p.input.View() + "\n")
	}
	help := ""
	for _, item := range p.items {
		if item.Help != "" {
			text := ""
			if p.cursor < len(p.matches) {
				text = p.items[p.matches[p.cursor]].Help
			}
			help = look.message(statusInfo, text, width, min(2, max(height-6, 1)))
			break
		}
	}
	hint := ansi.Wrap(p.controls(accent), width, "")
	// Status lines, most urgent last so they sit next to the key hints.
	var status []string
	for _, msg := range []struct {
		level statusLevel
		text  string
	}{{p.noticeLevel, p.notice}, {statusBusy, p.progress}, {statusError, p.err}} {
		if msg.text != "" {
			status = append(status, look.message(msg.level, msg.text, width, 0))
		}
	}
	notice := strings.Join(status, "\n")
	rows := max(height-3-lipgloss.Height(help)-lipgloss.Height(hint)-lipgloss.Height(notice), 1)
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
			if it.Disabled {
				line = dimStyle.Render(line)
			}
			switch {
			case p.keySelect && it.Key == "":
				line = "    " + line // chosen with the cursor, aligned with keyed rows
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
	if notice != "" {
		b.WriteString(notice + "\n")
	}
	if help != "" {
		b.WriteString(help + "\n")
	}
	b.WriteString(hint)
	return b.String()
}

// controls describes the keys supported by this picker.
func (p *picker) controls(accent lipgloss.Style) string {
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
	} else if p.links {
		copyKey := keyLabel("picker", "copy")
		if p.searching {
			copyKey = typedLabel("picker", "copy")
		}
		hints = append(hints, [2]string{keyLabel("picker", "choose"), "open"}, [2]string{copyKey, "copy"})
	} else {
		hints = append(hints, [2]string{keyLabel("picker", "choose"), "select"})
	}
	if p.onCopy != nil && !p.links {
		copyKey := keyLabel("picker", "copy")
		if p.searching {
			copyKey = typedLabel("picker", "copy")
		}
		hints = append(hints, [2]string{copyKey, "copy"})
	}
	if p.allowFree {
		hints = append(hints, [2]string{keyLabel("picker", "use_typed"), "use typed path"})
	}
	if p.multi && p.reorder {
		hints = append(hints, [2]string{"ctrl+up/down", "reorder"})
	}
	if p.allowReset {
		hints = append(hints, [2]string{"ctrl+r", "defaults"})
	}
	hints = append(hints, p.extraHints...)
	cancel := "cancel"
	if p.searching && !p.keySelect {
		cancel = "clear search"
	}
	return hintLine(accent, append(hints, [2]string{keyLabel("picker", "cancel"), cancel})...)
}

// modalLook is what modals draw with: the accent, the resolved symbols, and
// the spinner frame for progress.
type modalLook struct {
	accent lipgloss.Style
	sym    symbolSet
	frame  int
}

// message draws text led by its level's symbol, wrapped to width. A positive
// rows clips and pads it, so a footer such as item help keeps a stable height.
func (l modalLook) message(level statusLevel, text string, width, rows int) string {
	style, icon := dimStyle, l.sym.info
	switch level {
	case statusWarning:
		style, icon = warnStyle, l.sym.warning
	case statusError:
		style, icon = errorStyle, l.sym.failure
	case statusBusy:
		style = l.accent
		if l.sym.spinner != nil {
			icon = l.sym.spinner[l.frame%len(l.sym.spinner)]
		}
	}
	width = max(width-2, 1)
	if text != "" && icon != "" {
		text = icon + " " + text
	}
	lines := strings.Split(ansi.Wrap(text, width, ""), "\n")
	if rows > 0 && len(lines) > rows {
		lines = lines[:rows]
		lines[rows-1] = ansi.Truncate(lines[rows-1], max(width-1, 0), "") + "…"
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return style.Render(strings.Join(lines, "\n"))
}
