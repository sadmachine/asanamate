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
	codeWrap   string    // marks a wrapped code-block continuation
	info       string    // leads descriptive modal help
	statusInfo string    // leads informational status
	warning    string    // marks warning status
	failure    string    // marks error status
	connection [4]string // unknown, connected, reconnecting, unavailable
	emoji      bool      // the terminal draws emoji, such as comment reactions
	states     map[agents.State]string
	spinner    []string          // frames shown for working agents; nil means static
	border     lipgloss.Border   // reading pane cards and section rules
	icons      map[string]string // glyphs that lead labels
}

// Icon names, for symbolSet.icon.
const (
	iconView         = "view"
	iconFilter       = "filter"
	iconGroup        = "group"
	iconFolder       = "folder"
	iconProjectColor = "project_color"
	iconDue          = "due"
	iconClock        = "clock"
	iconBranch       = "branch"
	iconClip         = "clip"
	iconRepo         = "repo"
	iconRefresh      = "refresh"
	iconBell         = "bell"
	iconWarning      = "warning"
	iconNetwork      = "network"
)

// Spinners use dense glyphs, so a working agent stands out at a glance.
var braille = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// Each set only sets what it changes: nerd falls back to unicode, unicode to
// ascii, which defines everything.
var (
	asciiSymbols = symbolSet{connection: [4]string{"?", "+", "~", "x"}, open: "[ ]", done: "[x]", cursor: ">", robot: "@", barOn: "#", barOff: "-", codeWrap: ">", info: "(i)", statusInfo: "(i)", warning: "(!)", failure: "(x)", icons: map[string]string{iconNetwork: "(net)", iconClock: "(time)", iconProjectColor: "*"}, border: lipgloss.ASCIIBorder(), states: map[agents.State]string{
		agents.Waiting: "(!)", agents.Working: "(~)", agents.Completed: "(+)", agents.Idle: "(-)", agents.Unknown: "(?)"}}
	unicodeSymbols = asciiSymbols.with(symbolSet{connection: [4]string{"?", "●", "◐", "○"}, open: "□", done: "✓", cursor: "▌", robot: "🤖", barOn: "▰", barOff: "▱", codeWrap: "↪", warning: "⚠", failure: "✖", emoji: true, spinner: braille, icons: map[string]string{iconNetwork: "⇄", iconClock: "◷", iconProjectColor: "●"}, border: lipgloss.RoundedBorder(), states: map[agents.State]string{
		agents.Waiting: "⚠", agents.Working: "◐", agents.Completed: "●", agents.Idle: "○", agents.Unknown: "?"}})
	// Nerd Font (Font Awesome) glyphs; needs a Nerd Font.
	nerdSymbols = unicodeSymbols.with(symbolSet{open: "", done: "", robot: "󰚩", info: "\uf05a", statusInfo: "\U000f02fd", warning: "\uf071", failure: "\U000f05d6", states: map[agents.State]string{
		agents.Waiting: "", agents.Working: "", agents.Completed: "", agents.Idle: "", agents.Unknown: ""},
		icons: map[string]string{iconNetwork: "\uf1eb", iconView: "\uf01c", iconFilter: "\uf0b0", iconGroup: "\uf03a", iconFolder: "\uf07b", iconDue: "\uf073", iconClock: "\uf017", iconBranch: "\ue725", iconClip: "\uf0c6", iconRepo: "\ue702", iconRefresh: "\uf021", iconBell: "\uf0f3", iconWarning: "\uf071"}})
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
	s.codeWrap = cmp.Or(o.codeWrap, s.codeWrap)
	s.info = cmp.Or(o.info, s.info)
	s.statusInfo = cmp.Or(o.statusInfo, s.statusInfo)
	s.warning, s.failure = cmp.Or(o.warning, s.warning), cmp.Or(o.failure, s.failure)
	if o.connection != [4]string{} {
		s.connection = o.connection
	}
	s.emoji = s.emoji || o.emoji
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

// badge renders agents (already in urgency order) in bold after the robot,
// which gets a two-cell slot plus a space, as one symbol each, or as
// grouped counts when there are more than four, padded by one trailing cell.
// Highlighted badges follow the selection theme while keeping state colors.
func (s symbolSet) badge(list []agents.Agent, frame int, highlighted bool) string {
	if len(list) == 0 {
		return ""
	}
	sep := " "
	if highlighted {
		sep = selectedStyle.Render(sep)
	}
	style := func(st agents.State) lipgloss.Style {
		if highlighted && !selectedStyle.GetReverse() {
			return selectedStyle.Foreground(stateStyles[st].GetForeground()).Bold(true)
		}
		return stateStyles[st].Bold(true).Reverse(highlighted)
	}
	// Pad narrow robots outside Render, which trims trailing spaces; terminals
	// often draw Nerd Font glyphs wider than the one cell they count.
	robotStyle := lipgloss.NewStyle().Bold(true)
	if highlighted {
		robotStyle = selectedStyle.Bold(true)
	}
	robot := robotStyle.Render(s.robot)
	if lipgloss.Width(s.robot) < 2 {
		robot += sep
	}
	parts := []string{robot}
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
