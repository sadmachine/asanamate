package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/config"
)

// setPalette draws with p from now on.
func (m *Model) setPalette(p config.Palette) {
	applyPalette(p)
	c := p.Colors
	m.palette = p
	m.accentStyle, m.headerStyle = lipStyle(c.Accent), lipStyle(c.Header)
	m.pinnedStyle, m.viewingStyle = lipStyle(c.Pinned), lipStyle(c.Viewing)
	m.markerStyle = lipStyle(c.Selection)
	m.renderers = map[rendererKey]*markdownRenderer{}
}

// configPath is the config file's path, from Deps or the default location.
func (m *Model) configPath() (string, error) {
	if m.deps.ConfigPath != "" {
		return m.deps.ConfigPath, nil
	}
	return config.Path()
}

// themeChoice is a theme's config, or why its file did not load.
type themeChoice struct {
	cfg config.Config
	err error
}

// openThemes shows every theme with a preview of the highlighted one. It
// asks the terminal for its background, so the auto theme previews right.
func (m *Model) openThemes() tea.Cmd {
	path, err := m.configPath()
	if err != nil {
		m.setStatus(statusError, err.Error())
		return nil
	}
	dir := config.ThemesDir(path)
	names, err := config.ThemeNames(dir)
	if err != nil {
		m.setStatus(statusError, err.Error())
		return nil
	}
	current := cmp.Or(m.deps.Config.Theme.Name, config.ThemeAuto)
	items := make([]pickItem, len(names))
	for i, name := range names {
		items[i] = pickItem{Label: name, Value: name}
		if name == current {
			items[i].Hint = "current"
		}
	}
	// Theme files load once, when first highlighted.
	choices := map[string]themeChoice{}
	p := newPicker(pickValue(func(name string) tea.Cmd {
		m.modal = nil
		m.setTheme(dir, name)
		return nil
	}), "Theme", items)
	p.cursor = max(slices.Index(names, current), 0)
	p.preview = func(it *pickItem, width, height int) string {
		if it == nil {
			return ""
		}
		name := it.Value.(string)
		choice, ok := choices[name]
		if !ok {
			choice.cfg, choice.err = m.deps.Config.WithTheme(dir, name)
			choices[name] = choice
		}
		if choice.err != nil {
			return errorStyle.Render(ansi.Wrap(choice.err.Error(), width, ""))
		}
		return themePreview(choice.cfg.Palette(!m.lightBackground), width, height)
	}
	m.modal = p
	return tea.RequestBackgroundColor
}

// setTheme switches to the theme name from dir and saves it for later
// sessions.
func (m *Model) setTheme(dir, name string) {
	cfg, err := m.deps.Config.WithTheme(dir, name)
	if err != nil {
		m.setStatus(statusError, err.Error())
		return
	}
	m.deps.Config = cfg
	m.setPalette(cfg.Palette(!m.lightBackground))
	m.renderDetail(true)
	m.saveSetting("theme.name", name)
}

// themePreview draws a mock of the TUI in p's colors, without touching the
// active palette: the full layout when there is room, else a ticket list.
func themePreview(p config.Palette, width, height int) string {
	s := newPreviewStyles(p.Colors)
	if width >= 80 && height >= 22 {
		return s.full(p.Base, width, min(height, 30))
	}
	lines := []string{
		previewPane(s.accent.Render(" Tickets "), s.accent, width, 10, s.ticketLines(width-4)),
		s.pills(true),
		s.names(),
		s.swatches(p.Base),
	}
	return lipgloss.NewStyle().MaxHeight(height).MaxWidth(width).Render(strings.Join(lines, "\n"))
}

// previewStyles are a palette's styles for drawing a preview.
type previewStyles struct {
	c                      config.Colors
	accent, muted, border  lipgloss.Style
	tags, authors          []lipgloss.Style
	mdText, mdHead, mdLink lipgloss.Style
}

func newPreviewStyles(c config.Colors) previewStyles {
	return previewStyles{
		c:      c,
		accent: lipStyle(c.Accent), muted: lipStyle(c.Muted), border: lipStyle(c.Border),
		tags: cycleStyles(nil, c.TitleTags), authors: cycleStyles(nil, c.Authors),
		mdText: lipStyle(c.Markdown.Text), mdHead: lipStyle(c.Markdown.Heading.Over(c.Header)),
		mdLink: lipStyle(c.Markdown.Link),
	}
}

// previewRow puts left and right at the two ends of a line w wide.
func previewRow(left, right string, w int) string {
	gap := max(w-ansi.StringWidth(left)-ansi.StringWidth(right), 1)
	return ansi.Truncate(left+strings.Repeat(" ", gap)+right, w, "…")
}

// header draws a group or section header with a rule to w.
func (s previewStyles) header(name string, w int) string {
	label := lipStyle(s.c.Header).Render(name)
	return label + " " + s.border.Render(strings.Repeat("─", max(w-ansi.StringWidth(label)-1, 0)))
}

// ticketLines are the ticket list's rows, the most telling colors first, so a
// short pane still shows them.
func (s previewStyles) ticketLines(w int) []string {
	c := s.c
	tag := s.tags[min(1, len(s.tags)-1)]
	return []string{
		s.header("Overdue", w),
		lipStyle(c.Selection).Render("▌") + lipStyle(c.Highlight).Render(previewRow(" Fix login loop", "2d ago", w)[1:]),
		previewRow("  Billing retries", lipStyle(c.Error).Render("1d ago"), w),
		s.header("Today", w),
		previewRow("  "+tag.Render("[API]")+" Limits "+lipStyle(c.New).Render("NEW"), lipStyle(c.Warn).Render("today"), w),
		previewRow("  "+lipStyle(c.Pinned).Render("Pinned")+" notes", lipStyle(c.Warn).Render("today"), w),
		s.header("Done", w),
		previewRow("  "+s.muted.Render("Ship release"), lipStyle(c.OK).Render("done"), w),
		s.header("Next 7 days", w),
		previewRow("  Dark mode tokens", "Thu", w),
		previewRow("  "+s.tags[0].Render("[DOCS]")+" Onboarding", "Fri", w),
		previewRow("  Audit dependencies", "Sat", w),
		s.header("Later", w),
		previewRow("  Migrate cron jobs", "Oct 14", w),
		previewRow("  Design system audit", "Oct 20", w),
		previewRow("  Retire /v1 API", "Nov 1", w),
	}
}

// ticketDetail is the reading pane: details, agents, description, comments.
func (s previewStyles) ticketDetail(w int) []string {
	c, a := s.c, s.c.Asana
	field := func(label, value string) string { return s.muted.Render(fmt.Sprintf("%-10s", label)) + value }
	comment := func(author lipgloss.Style, name, date string) string {
		return author.Bold(true).Render(name) + s.muted.Render(" · "+date)
	}
	return []string{
		lipgloss.NewStyle().Bold(true).Render("Fix login loop"),
		lipStyle(a.Blue).Render("■") + s.muted.Render(" Website › ") + s.accent.Render("In Progress") +
			"  " + lipStyle(c.Error).Render("2d overdue") + "  " + lipStyle(c.Branch).Render("fix-login"),
		"",
		s.header("Details", w),
		field("Status", lipStyle(c.OK).Render("open")),
		field("Assignee", s.authors[0].Render("Austin F.")),
		field("Priority", lipStyle(a.Red).Render("● High")),
		field("Type", lipStyle(a.Purple).Render("● Design")),
		"",
		s.header("Agents", w),
		lipStyle(c.Working).Render("/ claude working") + s.muted.Render(" · 4m"),
		lipStyle(c.Warn).Render("▲ codex waiting for input"),
		"",
		s.header("Description", w),
		s.mdText.Render("Users hit ") + s.mdLink.Render("/login") + s.mdText.Render(" in a loop."),
		s.mdHead.Render("Acceptance"),
		s.mdText.Render("• Passes ") + lipStyle(c.Markdown.Code).Render("make test"),
		lipStyle(c.Markdown.Quote).Render("│ Only on Safari."),
		"",
		s.header("Comments", w),
		comment(s.authors[1%len(s.authors)], "Jane D.", "Sep 25"),
		"  Reproduced on Safari. " + lipStyle(c.Reacted).Render(" +1 "),
		comment(s.authors[2%len(s.authors)], "asanamate", "Sep 27"),
		"  claude opened " + lipStyle(c.Branch).Render("fix-login-loop"),
	}
}

// full draws the ticket list and reading pane side by side over a
// statusline and the remaining swatches, h lines tall.
func (s previewStyles) full(base string, w, h int) string {
	paneH := h - 2
	listW := w * 2 / 5
	readW := w - listW - 1
	list := previewPane(s.accent.Render(" Tickets ")+s.muted.Render("· My Tasks "), s.accent, listW, paneH, s.ticketLines(listW-4))
	reader := previewPane(" Ticket ", s.border, readW, paneH, s.ticketDetail(readW-4))
	hint := func(key, label string) string { return s.accent.Render(key) + " " + s.muted.Render(label) }
	status := previewRow(
		pillStyle(s.c.Mode.Normal).Render(" NORMAL ")+" My Tasks "+lipStyle(s.c.Warn).Render("is:open")+s.muted.Render(" 12/40"),
		hint("enter", "act")+"  "+hint("/", "filter")+"  "+hint("?", "keys"), w)
	return strings.Join([]string{
		lipgloss.JoinHorizontal(lipgloss.Top, list, " ", reader),
		status,
		ansi.Truncate(s.pills(false)+"  "+s.names()+"  "+s.swatches(base), w, ""),
	}, "\n")
}

// pills draws the mode pills; withNormal adds the normal one, which the full
// preview's statusline already shows.
func (s previewStyles) pills(withNormal bool) string {
	m := s.c.Mode
	var pills []string
	if withNormal {
		pills = append(pills, pillStyle(m.Normal).Render(" NORMAL "))
	}
	pills = append(pills, pillStyle(m.Filter).Render(" FILTER "), pillStyle(m.Read).Render(" READ "), pillStyle(m.Edit).Render(" EDIT "))
	return strings.Join(pills, " ")
}

// names draws sample authors, the working color, and a branch.
func (s previewStyles) names() string {
	var names []string
	for i, name := range []string{"alice", "bob", "carol", "dave"} {
		names = append(names, s.authors[i%len(s.authors)].Render(name))
	}
	return strings.Join(names, " ") + "  " + lipStyle(s.c.Working).Render("working") + " " + lipStyle(s.c.Branch).Render("main")
}

// swatches draws a dot per Asana color family and the theme's base.
func (s previewStyles) swatches(base string) string {
	a := s.c.Asana
	var dots []string
	for _, role := range []config.Style{a.Red, a.Orange, a.Yellow, a.Green, a.Teal, a.Blue, a.Purple, a.Pink, a.Gray} {
		dots = append(dots, lipStyle(role).Render("●"))
	}
	return strings.Join(dots, " ") + s.muted.Render("  base: "+base)
}

// previewPane draws lines in a rounded box w wide and h tall, with title in
// its top border and a column of padding on each side. Lines fit w-4.
func previewPane(title string, border lipgloss.Style, w, h int, lines []string) string {
	inner := max(w-2, 1)
	title = ansi.Truncate(title, max(inner-1, 0), "")
	out := []string{border.Render("╭─") + title + border.Render(strings.Repeat("─", max(inner-1-ansi.StringWidth(title), 0))+"╮")}
	for i := range max(h-2, 0) {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], max(inner-2, 0), "…")
		}
		out = append(out, border.Render("│")+" "+line+strings.Repeat(" ", max(inner-1-ansi.StringWidth(line), 0))+border.Render("│"))
	}
	out = append(out, border.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return strings.Join(out, "\n")
}
