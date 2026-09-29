package tui

import (
	"cmp"
	"fmt"
	"maps"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/config"
)

// symbolSet holds the ticket markers and agent state symbols for one style.
type symbolSet struct {
	open, done string
	cursor     string // left marker of the selected ticket
	robot      string // leads a list row's agent badge
	barOn      string // a progress bar's done and to-do cells
	barOff     string
	states     map[agents.State]string
	spinner    []string          // frames shown for working agents; nil means static
	border     lipgloss.Border   // reading pane cards and section rules
	icons      map[string]string // glyphs that lead labels; only the nerd set has them
}

// Icon names, for symbolSet.icon.
const (
	iconView   = "view"
	iconFilter = "filter"
	iconGroup  = "group"
	iconFolder = "folder"
	iconDue    = "due"
	iconBranch = "branch"
	iconClip   = "clip"
)

var braille = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Each set only sets what it changes: nerd falls back to unicode, unicode to
// ascii, which defines everything.
var (
	asciiSymbols = symbolSet{open: "[ ]", done: "[x]", cursor: ">", robot: "@", barOn: "#", barOff: "-", border: lipgloss.ASCIIBorder(), states: map[agents.State]string{
		agents.Waiting: "(!)", agents.Working: "(~)", agents.Completed: "(+)", agents.Idle: "(-)", agents.Unknown: "(?)"}}
	unicodeSymbols = asciiSymbols.with(symbolSet{open: "□", done: "✓", cursor: "▌", robot: "🤖", barOn: "▰", barOff: "▱", spinner: braille, border: lipgloss.RoundedBorder(), states: map[agents.State]string{
		agents.Waiting: "⚠", agents.Working: "◐", agents.Completed: "●", agents.Idle: "○", agents.Unknown: "?"}})
	// Nerd Font (Font Awesome) glyphs; needs a Nerd Font.
	nerdSymbols = unicodeSymbols.with(symbolSet{open: "", done: "", robot: "󰚩", states: map[agents.State]string{
		agents.Waiting: "", agents.Working: "", agents.Completed: "", agents.Idle: "", agents.Unknown: ""},
		icons: map[string]string{iconView: "\uf01c", iconFilter: "\uf0b0", iconGroup: "\uf03a", iconFolder: "\uf07b", iconDue: "\uf073", iconBranch: "\ue725", iconClip: "\uf0c6"}})
)

var symbolSets = map[string]symbolSet{
	config.SymbolsASCII:   asciiSymbols,
	config.SymbolsUnicode: unicodeSymbols,
	config.SymbolsNerd:    nerdSymbols,
}

// with returns s with every field o sets replacing its own.
func (s symbolSet) with(o symbolSet) symbolSet {
	s.open, s.done = cmp.Or(o.open, s.open), cmp.Or(o.done, s.done)
	s.cursor, s.robot = cmp.Or(o.cursor, s.cursor), cmp.Or(o.robot, s.robot)
	s.barOn, s.barOff = cmp.Or(o.barOn, s.barOn), cmp.Or(o.barOff, s.barOff)
	s.border = cmp.Or(o.border, s.border)
	if o.spinner != nil {
		s.spinner = o.spinner
	}
	s.states = maps.Clone(s.states)
	maps.Copy(s.states, o.states)
	if o.icons != nil {
		s.icons = o.icons
	}
	return s
}

// icon returns the named icon and a space, or "" when the set has none.
func (s symbolSet) icon(name string) string {
	if g := s.icons[name]; g != "" {
		return g + " "
	}
	return ""
}

var stateStyles = map[agents.State]lipgloss.Style{
	agents.Waiting:   warnStyle,
	agents.Working:   okStyle,
	agents.Completed: okStyle,
	agents.Idle:      dimStyle,
	agents.Unknown:   dimStyle,
}

// newSymbols returns the named set with per-state overrides applied. An
// overridden working symbol, or reduced motion, turns the spinner off.
func newSymbols(set string, overrides map[string]string, reducedMotion bool) symbolSet {
	base, ok := symbolSets[set]
	if !ok {
		base = symbolSets[config.SymbolsUnicode]
	}
	states := make(map[agents.State]string, len(overrides))
	for k, v := range overrides {
		states[agents.State(k)] = v
	}
	s := base.with(symbolSet{states: states})
	if reducedMotion || overrides[string(agents.Working)] != "" {
		s.spinner = nil
	}
	return s
}

// agent returns the symbol for a state; working agents animate by frame.
func (s symbolSet) agent(state agents.State, frame int) string {
	if state == agents.Working && s.spinner != nil {
		return s.spinner[frame%len(s.spinner)]
	}
	return s.states[state]
}

// badge renders agents (already in urgency order) in bold after a "robot :"
// label (the space leaves room for wide icons), as one symbol each, or as
// grouped counts when there are more than four, padded by one trailing cell.
// Reversed badges swap each state's colors, to sit inside a selection
// highlight.
func (s symbolSet) badge(list []agents.Agent, frame int, reversed bool) string {
	if len(list) == 0 {
		return ""
	}
	style := func(st agents.State) lipgloss.Style { return stateStyles[st].Bold(true).Reverse(reversed) }
	parts := []string{lipgloss.NewStyle().Bold(true).Reverse(reversed).Render(s.robot + " :")}
	if len(list) <= 4 {
		for _, a := range list {
			parts = append(parts, style(a.State).Render(s.agent(a.State, frame)))
		}
	} else {
		for _, st := range agents.States {
			if n := countState(list, st); n > 0 {
				parts = append(parts, style(st).Render(fmt.Sprintf("%s%d", s.agent(st, frame), n)))
			}
		}
	}
	sep := " "
	if reversed {
		sep = selectedStyle.Render(sep)
	}
	// Trailing sep pads the badge off the pane's right edge.
	return strings.Join(parts, sep) + sep
}

// summary renders grouped counts, for the header.
func (s symbolSet) summary(list []agents.Agent, frame int) string {
	var parts []string
	for _, st := range agents.States {
		if n := countState(list, st); n > 0 {
			parts = append(parts, stateStyles[st].Render(fmt.Sprintf("%s%d", s.agent(st, frame), n)))
		}
	}
	return strings.Join(parts, " ")
}

func countState(list []agents.Agent, st agents.State) int {
	n := 0
	for _, a := range list {
		if a.State == st {
			n++
		}
	}
	return n
}
