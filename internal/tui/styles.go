package tui

import (
	"hash/fnv"
	"image/color"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// Styles from the active palette. applyPalette sets them; New calls it once,
// since a process runs one Model. init applies the defaults so code that runs
// before New, and tests, see the built-in dark theme.
var (
	borderColor   color.Color
	okStyle       lipgloss.Style
	warnStyle     lipgloss.Style
	errorStyle    lipgloss.Style
	workingStyle  lipgloss.Style
	dimStyle      lipgloss.Style
	selectedStyle lipgloss.Style
	loadingStyle  lipgloss.Style
	authorStyles  []lipgloss.Style
	reactedStyle  lipgloss.Style // pill of a comment reaction the user made
	moreStyle     lipgloss.Style // +N more badge on an Activity run
	newStyle      lipgloss.Style // NEW badge on changed My Tasks tickets
	stateStyles   map[agents.State]lipgloss.Style
	asanaStyles   map[string]lipgloss.Style // by Asana project or enum option color
	branchStyle   lipgloss.Style
	tagStyles     []lipgloss.Style // leading [TAG] of ticket titles
	// Mode pills: normal (also PLACES and loading), filter, read, and edit.
	normalPill, filterPill, readPill, editPill lipgloss.Style
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	modalStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

func init() { applyPalette(config.Config{}.Palette(true)) }

// applyPalette sets the package styles from p.
func applyPalette(p config.Palette) {
	c := p.Colors
	borderColor = lipStyle(c.Border).GetForeground()
	okStyle, warnStyle, errorStyle = lipStyle(c.OK), lipStyle(c.Warn), lipStyle(c.Error)
	workingStyle, dimStyle, selectedStyle = lipStyle(c.Working), lipStyle(c.Muted), lipStyle(c.Highlight)
	loadingStyle = modalStyle.BorderForeground(warnStyle.GetForeground()).Padding(2, 4)
	reactedStyle = lipStyle(c.Reacted)
	newStyle = lipStyle(c.New)
	moreStyle = lipStyle(c.More)
	stateStyles = map[agents.State]lipgloss.Style{
		agents.Waiting:   warnStyle,
		agents.Working:   workingStyle,
		agents.Completed: okStyle,
		agents.Idle:      dimStyle,
		agents.Unknown:   dimStyle,
	}
	// Asana names enum option colors, and project colors in a dark and a
	// light shade; each takes its hue family's colors.asana role.
	asanaStyles = map[string]lipgloss.Style{}
	for _, family := range []struct {
		style config.Style
		names []string
	}{
		{c.Asana.Red, []string{"red", "dark-red", "light-red"}},
		{c.Asana.Orange, []string{"orange", "dark-orange", "light-orange", "dark-brown", "light-brown"}},
		{c.Asana.Yellow, []string{"yellow", "yellow-orange"}},
		{c.Asana.Green, []string{"green", "yellow-green", "dark-green", "light-green"}},
		{c.Asana.Teal, []string{"blue-green", "aqua", "dark-teal", "light-teal"}},
		{c.Asana.Blue, []string{"blue", "indigo", "dark-blue", "light-blue"}},
		{c.Asana.Purple, []string{"purple", "dark-purple", "light-purple"}},
		{c.Asana.Pink, []string{"pink", "magenta", "hot-pink", "dark-pink", "light-pink"}},
		{c.Asana.Gray, []string{"cool-gray", "dark-warm-gray", "light-warm-gray"}},
	} {
		for _, name := range family.names {
			asanaStyles[name] = lipStyle(family.style)
		}
	}
	branchStyle = lipStyle(c.Branch)
	authorStyles, tagStyles = cycleStyles(authorStyles, c.Authors), cycleStyles(tagStyles, c.TitleTags)
	normalPill, filterPill = pillStyle(c.Mode.Normal), pillStyle(c.Mode.Filter)
	readPill, editPill = pillStyle(c.Mode.Read), pillStyle(c.Mode.Edit)
}

// lipStyle draws a themed role.
func lipStyle(s config.Style) lipgloss.Style {
	st := lipgloss.NewStyle()
	if s.FG != "" {
		st = st.Foreground(lipgloss.Color(s.FG))
	}
	if s.BG != "" {
		st = st.Background(lipgloss.Color(s.BG))
	}
	for _, attr := range []struct {
		on  *bool
		set func(lipgloss.Style, bool) lipgloss.Style
	}{
		{s.Bold, lipgloss.Style.Bold}, {s.Italic, lipgloss.Style.Italic}, {s.Underline, lipgloss.Style.Underline},
		{s.Faint, lipgloss.Style.Faint}, {s.Reverse, lipgloss.Style.Reverse},
	} {
		if attr.on != nil {
			st = attr.set(st, *attr.on)
		}
	}
	return st
}

// cycleStyles refills dst with a style per themed role, keeping one plain
// style when there are none, so hashStyle always has a pick.
func cycleStyles(dst []lipgloss.Style, roles []config.Style) []lipgloss.Style {
	dst = dst[:0]
	for _, s := range roles {
		dst = append(dst, lipStyle(s))
	}
	if len(dst) == 0 {
		dst = append(dst, lipgloss.NewStyle())
	}
	return dst
}

// hashStyle picks key's style from styles, the same one every time.
func hashStyle(styles []lipgloss.Style, key string) lipgloss.Style {
	h := fnv.New32a()
	h.Write([]byte(key))
	return styles[h.Sum32()%uint32(len(styles))]
}

// asanaStyle is the style of an Asana color name, and whether it has one.
func asanaStyle(color string) (lipgloss.Style, bool) {
	s, ok := asanaStyles[color]
	return s, ok
}

// enumStyle is the style of f's value from its enum option's Asana color, and
// whether it has one.
func enumStyle(f asana.CustomField) (lipgloss.Style, bool) { return asanaStyle(f.Color()) }

// titleTag matches a ticket title's leading tag, such as "[MOCPA]".
var titleTag = regexp.MustCompile(`^\[[^\[\]]+\]`)

// tagTitle renders title in style, with its leading tag, if any, in the tag's
// colors.title_tags color, the same for every title with that tag.
func tagTitle(title string, style lipgloss.Style) string {
	tag := titleTag.FindString(title)
	if tag == "" {
		return style.Render(title)
	}
	tagStyle := hashStyle(tagStyles, strings.ToLower(tag)).Strikethrough(style.GetStrikethrough())
	return tagStyle.Render(tag) + style.Render(title[len(tag):])
}

// pillStyle draws a mode pill: bold unless the style says otherwise, and
// reversed when it has no background, so its foreground fills it.
func pillStyle(s config.Style) lipgloss.Style {
	bold := true
	st := lipStyle(s.Over(config.Style{Bold: &bold}))
	if s.BG == "" {
		st = st.Reverse(true)
	}
	return st
}

// projectLabel prefixes a project name with its themed Asana color marker.
func (m *Model) projectLabel(project asana.Ref, style lipgloss.Style) string {
	color := project.Color
	if color == "" {
		for _, p := range m.projects {
			if p.GID == project.GID {
				color = p.Color
				break
			}
		}
	}
	label := style.Render(ticket.OneLine(project.Name))
	if st, ok := asanaStyle(color); ok {
		return st.Render(m.sym.icon(iconProjectColor)) + label
	}
	return label
}
