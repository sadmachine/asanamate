package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type pickKind int

const (
	pickProject pickKind = iota
	pickAction
	pickTicketProject
	pickRepo
	pickAttachment
	pickAgent
	pickGroup
)

type pickItem struct {
	Label string
	Hint  string
	Key   string
	Value any
}

type pickResult struct {
	done      bool
	cancelled bool
	item      *pickItem
	free      string
}

// picker is a modal list. It filters by typed words, or with keySelect it
// selects by each item's Key. With allowFree, typed text can be returned as is.
type picker struct {
	kind      pickKind
	title     string
	items     []pickItem
	matches   []int
	cursor    int
	input     textinput.Model
	keySelect bool
	allowFree bool
	err       string
}

func newPicker(kind pickKind, title string, items []pickItem) *picker {
	in := textinput.New()
	in.Prompt = "> "
	in.Focus()
	p := &picker{kind: kind, title: title, items: items, input: in}
	p.refilter()
	return p
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
		if len(p.matches) > 0 {
			return p.choose(p.matches[p.cursor]), nil
		}
		res, _ := p.freeText()
		return res, nil
	case "tab":
		res, _ := p.freeText()
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
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.err = ""
	p.refilter()
	return pickResult{}, cmd
}

func (p *picker) view(width, height int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(p.title) + "\n")
	if !p.keySelect {
		b.WriteString(p.input.View() + "\n")
	}
	rows := max(height-4, 1)
	start := max(p.cursor-rows+1, 0)
	if len(p.matches) == 0 {
		b.WriteString(dimStyle.Render("no matches") + "\n")
	}
	for i := start; i < len(p.matches) && i < start+rows; i++ {
		it := p.items[p.matches[i]]
		line := it.Label
		if p.keySelect {
			line = "[" + it.Key + "] " + line
		}
		if it.Hint != "" {
			line += "  " + dimStyle.Render(it.Hint)
		}
		line = ansi.Truncate(line, max(width-2, 1), "…")
		if i == p.cursor {
			line = selectedStyle.Render("▸ " + ansi.Strip(line))
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}
	if p.err != "" {
		b.WriteString(errorStyle.Render(p.err) + "\n")
	}
	hint := "enter select · esc cancel"
	if p.allowFree {
		hint += " · tab use typed path"
	}
	b.WriteString(dimStyle.Render(hint))
	return b.String()
}
