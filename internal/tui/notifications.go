package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/ticket"
)

type statusLevel uint8

const (
	statusInfo statusLevel = iota
	statusWarning
	statusError
)

const (
	maxStatusHistory = 100
	toastHighlight   = 250 * time.Millisecond
)

type statusMessage struct {
	text  string
	level statusLevel
	at    time.Time
	count int
}

type statusToast struct {
	message     statusMessage
	seq         uint64
	highlighted bool
}

type toastTickMsg struct {
	seq    uint64
	finish bool
}

// setStatus publishes a completed operation or a condition requiring attention.
// Progress has its own path so periodic work never creates toasts or history.
func (m *Model) setStatus(level statusLevel, text string) {
	m.publishStatus(level, text, level == statusError)
}

// setInlineError records an editor's own error without opening a second modal.
func (m *Model) setInlineError(text string) {
	m.publishStatus(statusError, text, false)
}

// setBackgroundError asks for acknowledgment once per failure, until recovery.
func (m *Model) setBackgroundError(source, text string) {
	if m.backgroundErrors == nil {
		m.backgroundErrors = map[string]string{}
	}
	changed := m.backgroundErrors[source] != text
	m.backgroundErrors[source] = text
	m.publishStatus(statusError, text, changed)
}

func (m *Model) publishStatus(level statusLevel, text string, modal bool) {
	text = ticket.OneLine(text)
	if text == "" {
		return
	}
	at := time.Now()
	if m.now != nil {
		at = m.now()
	}
	message := statusMessage{text: text, level: level, at: at, count: 1}
	m.status, m.statusLevel, m.statusProgress = text, level, false
	history := m.statusHistory
	if n := len(history); n > 0 && history[n-1].text == text && history[n-1].level == level {
		message.count = history[n-1].count + 1
		history[n-1] = message
	} else {
		if len(history) == maxStatusHistory {
			copy(history, history[1:])
			history = history[:maxStatusHistory-1]
		}
		history = append(history, message)
	}
	m.statusHistory = history
	m.toastSeq++
	m.toast, m.toastPending = nil, false
	if level == statusError {
		if modal {
			for _, notice := range m.notices {
				if notice.text == text && notice.level == level {
					return
				}
			}
			m.notices = append(m.notices, message)
		}
		return
	}
	m.toast = &statusToast{message: message, seq: m.toastSeq, highlighted: !m.deps.ReducedMotion}
	m.toastPending = true
}

func (m *Model) setProgress(text string) {
	m.status, m.statusLevel, m.statusProgress = ticket.OneLine(text), statusInfo, true
}

// clearProgress restores the last meaningful status, including after navigation.
func (m *Model) clearProgress() {
	if !m.statusProgress {
		return
	}
	m.status, m.statusLevel, m.statusProgress = "", statusInfo, false
	if n := len(m.statusHistory); n > 0 {
		last := m.statusHistory[n-1]
		m.status, m.statusLevel = last.text, last.level
	}
}

func toastDuration(level statusLevel) time.Duration {
	if level == statusWarning {
		return 6 * time.Second
	}
	return 4 * time.Second
}

func (m *Model) scheduleToast() tea.Cmd {
	if !m.toastPending || m.toast == nil {
		return nil
	}
	m.toastPending = false
	t := *m.toast
	after := toastDuration(t.message.level)
	if t.highlighted {
		after = toastHighlight
	}
	return m.afterToast(after, toastTickMsg{seq: t.seq, finish: !t.highlighted})
}

func (m *Model) afterToast(after time.Duration, msg toastTickMsg) tea.Cmd {
	if m.toastTimer != nil {
		return m.toastTimer(after, msg)
	}
	return tea.Tick(after, func(time.Time) tea.Msg { return msg })
}

func (m *Model) toastTick(msg toastTickMsg) tea.Cmd {
	if m.toast == nil || msg.seq != m.toast.seq {
		return nil
	}
	if msg.finish {
		m.toast = nil
		return nil
	}
	m.toast.highlighted = false
	return m.afterToast(toastDuration(m.toast.message.level)-toastHighlight, toastTickMsg{seq: msg.seq, finish: true})
}

// Defer unrelated errors until the current interaction ends. Both rendering and
// key routing use this condition, so an unseen notice never consumes input.
func (m *Model) noticeVisible() bool {
	return len(m.notices) > 0 && !m.statusObscured()
}

func (m *Model) statusObscured() bool {
	return m.input != nil || m.form != nil || m.builder != nil || m.composer != nil || m.modal != nil || m.extension != nil ||
		m.filtering || m.help || (m.activityOpen && m.activity.filtering)
}

func (m *Model) statusAppearance(level statusLevel) (lipgloss.Style, string) {
	switch level {
	case statusWarning:
		return warnStyle, m.sym.warning
	case statusError:
		return errorStyle, m.sym.failure
	default:
		return m.accentStyle, m.sym.statusInfo
	}
}

func (m *Model) renderStatus(level statusLevel, text string) string {
	style, icon := m.statusAppearance(level)
	return style.Render(icon + " " + ticket.OneLine(text))
}

func (m *Model) toastVisible() bool {
	return m.toast != nil && len(m.notices) == 0 && !m.statusObscured() && m.width >= 12 && m.bodyHeight() >= 4 &&
		m.waiting == "" && !(m.loading && m.tasks != nil && !m.background)
}

// overlayToast covers the statusline without changing pane geometry or input.
func (m *Model) overlayToast(frame string) string {
	if !m.toastVisible() {
		return frame
	}
	h := m.bodyHeight() + 1 // include the statusline beneath the toast
	width := min(52, m.width-2)
	inner := width - 4 // border and horizontal padding
	t := m.toast
	text := m.renderStatus(t.message.level, t.message.text)
	lines := strings.Split(ansi.Hardwrap(ansi.Wrap(text, inner, ""), inner, true), "\n")
	rows := min(4, h-2)
	if len(lines) > rows {
		lines = lines[:rows]
		lines[rows-1] = ansi.Truncate(lines[rows-1], inner-1, "") + "…"
	}
	border := borderColor
	if t.highlighted {
		style, _ := m.statusAppearance(t.message.level)
		border = style.GetForeground()
	}
	box := lipgloss.NewStyle().Border(m.sym.border).BorderForeground(border).Padding(0, 1).Width(width).
		Render(strings.Join(lines, "\n"))
	x, y := max(m.width-lipgloss.Width(box)-1, 0), max(h-lipgloss.Height(box), 0)
	canvas := lipgloss.NewCanvas(m.width, h).Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(frame), lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	))
	return lipgloss.NewStyle().Height(h).Render(canvas.Render())
}

func statusName(level statusLevel) string {
	switch level {
	case statusWarning:
		return "Warning"
	case statusError:
		return "Error"
	default:
		return "Info"
	}
}

// openStatusHistory reuses the searchable picker, newest first. Selection copies
// the full message, including text too long to fit its preview.
func (m *Model) openStatusHistory() {
	items := make([]pickItem, 0, len(m.statusHistory))
	for i := len(m.statusHistory) - 1; i >= 0; i-- {
		message := m.statusHistory[i]
		label := message.at.Format("15:04:05") + " " + statusName(message.level)
		if message.count > 1 {
			label += fmt.Sprintf(" (x%d)", message.count)
		}
		items = append(items, pickItem{Label: label + ": " + message.text, Help: message.text, Value: message})
	}
	p := newPicker(pickValue(func(message statusMessage) tea.Cmd {
		return tea.SetClipboard(message.text)
	}), "Status history (select to copy)", items)
	p.onCopy = func(item *pickItem) tea.Cmd {
		return tea.SetClipboard(item.Value.(statusMessage).text)
	}
	p.listMaxWidth = 56
	p.preview = func(item *pickItem, width, height int) string {
		if item == nil {
			return dimStyle.Render("No status messages this session.")
		}
		message := item.Value.(statusMessage)
		text := message.at.Format("15:04:05") + " " + statusName(message.level) + "\n\n" + m.renderStatus(message.level, message.text)
		lines := strings.Split(ansi.Hardwrap(ansi.Wrap(text, width, ""), width, true), "\n")
		if len(lines) > height {
			lines = lines[:height]
		}
		return strings.Join(lines, "\n")
	}
	m.modal = p
}
