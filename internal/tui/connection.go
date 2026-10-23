package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

const (
	connectionGrace      = 30 * time.Second
	maxConnectionOutages = 20
)

type connectionStatus uint8

const (
	connectionUnknown connectionStatus = iota
	connectionConnected
	connectionReconnecting
	connectionUnavailable
)

type connectionOutage struct {
	started, ended time.Time
	attempts       int
	message        string
}

// connectionHealth owns one service's session-only outage history. Failures
// update one episode until recovery, regardless of the error's wording.
type connectionHealth struct {
	lastSuccess time.Time
	outages     []connectionOutage
}

func (c *connectionHealth) active() *connectionOutage {
	if n := len(c.outages); n > 0 && c.outages[n-1].ended.IsZero() {
		return &c.outages[n-1]
	}
	return nil
}

// observe reports whether a new outage needs a grace-period redraw.
func (c *connectionHealth) observe(err error, now time.Time) bool {
	if err == nil {
		c.lastSuccess = now
		if outage := c.active(); outage != nil {
			outage.ended = now
			// Brief network switches need no lasting history entry.
			if now.Sub(outage.started) < connectionGrace {
				c.outages = c.outages[:len(c.outages)-1]
			}
		}
		return false
	}
	started := c.active() == nil
	if started {
		if len(c.outages) == maxConnectionOutages {
			copy(c.outages, c.outages[1:])
			c.outages = c.outages[:maxConnectionOutages-1]
		}
		c.outages = append(c.outages, connectionOutage{started: now})
	}
	outage := c.active()
	outage.attempts++
	outage.message = ticket.OneLine(err.Error())
	return started
}

func (c *connectionHealth) status(now time.Time) connectionStatus {
	if outage := c.active(); outage != nil {
		if now.Sub(outage.started) < connectionGrace {
			return connectionReconnecting
		}
		return connectionUnavailable
	}
	if c.lastSuccess.IsZero() {
		return connectionUnknown
	}
	return connectionConnected
}

func (s connectionStatus) String() string {
	return [...]string{"Unknown", "Connected", "Reconnecting", "Unavailable"}[s]
}

// connectionFailureError recognizes transient reachability failures without
// matching error text. Authentication, permissions, TLS validation and invalid
// responses keep their normal error handling. Cancellation is intentional.
func connectionFailureError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var api *asana.APIError
	if errors.As(err, &api) {
		return api.Status >= http.StatusInternalServerError || api.Status == http.StatusTooManyRequests
	}
	// url.Error itself implements net.Error even for invalid certificates.
	var request *url.Error
	if errors.As(err, &request) {
		return connectionFailureError(request.Err)
	}
	var network net.Error
	return errors.As(err, &network) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func (m *Model) observeAsana(operation string, err error) bool {
	quiet := connectionFailureError(err)
	if err == nil || quiet {
		if err != nil {
			err = fmt.Errorf("%s: %w", operation, err)
		}
		if m.asanaConnection.observe(err, m.now()) {
			m.connectionTickPending = true
		}
	}
	return quiet
}

type connectionTickMsg struct{}

func (m *Model) scheduleConnectionTick() tea.Cmd {
	if !m.connectionTickPending {
		return nil
	}
	m.connectionTickPending = false
	if m.connectionTimer != nil {
		return m.connectionTimer(connectionGrace, connectionTickMsg{})
	}
	return tea.Tick(connectionGrace, func(time.Time) tea.Msg { return connectionTickMsg{} })
}

func (m *Model) connectionIndicator() string {
	status := m.asanaConnection.status(m.now())
	style := [...]lipgloss.Style{dimStyle, okStyle, warnStyle, errorStyle}[status]
	return dimStyle.Render(m.sym.icon(iconNetwork)) + style.Render(m.sym.connection[status])
}

func (c *connectionHealth) detail(now time.Time) string {
	last := "No successful check this session."
	if !c.lastSuccess.IsZero() {
		last = "Last successful check: " + c.lastSuccess.Format("Jan 02 15:04:05") + " (" + shortDuration(max(0, now.Sub(c.lastSuccess)).Truncate(time.Second)) + " ago)."
	}
	if outage := c.active(); outage != nil {
		return last + "\n\n" + outage.detail(now)
	}
	return last
}

func (o connectionOutage) detail(now time.Time) string {
	end, state := o.ended, "Restored"
	if end.IsZero() {
		end, state = now, "Ongoing"
	}
	return fmt.Sprintf("%s: %s\nDuration: %s; %d failed attempts\nLatest error: %s", state,
		o.started.Format("Jan 02 15:04:05"), shortDuration(max(0, end.Sub(o.started)).Truncate(time.Second)), o.attempts, o.message)
}

func (m *Model) openConnectionStatus() {
	p := newPicker(nil, "Connection status", nil)
	p.onPick = func(result pickResult) tea.Cmd {
		if result.item.Key == "r" {
			m.modal = p
			if m.loading || len(m.detailRequests) > 0 {
				return nil
			}
			cmd := m.refresh()
			if m.deps.Config.AgentsEnabled() {
				cmd = tea.Batch(cmd, loadAgents(m.deps.Config.Agents))
			}
			return cmd
		}
		return tea.SetClipboard(result.item.Help)
	}
	p.onCopy = func(item *pickItem) tea.Cmd { return tea.SetClipboard(item.Help) }
	p.keySelect, p.listMaxWidth = true, 42
	p.notice = "Asana reachability, not system Wi-Fi. Agent health is separate.\nSelect a status or outage to copy its details."
	p.preview = func(item *pickItem, width, height int) string {
		if item == nil {
			return ""
		}
		lines := strings.Split(ansi.Hardwrap(ansi.Wrap(item.Label+"\n\n"+item.Help, width, ""), width, true), "\n")
		return strings.Join(lines[:min(len(lines), height)], "\n")
	}
	m.modal, m.connectionMenu = p, p
	m.refreshConnectionStatus()
}

// Update the existing picker so recovery is visible without closing it, while
// retaining its cursor and normal copy/navigation behavior.
func (m *Model) refreshConnectionStatus() {
	if m.connectionMenu == nil || m.modal != m.connectionMenu {
		m.connectionMenu = nil
		return
	}
	now := m.now()
	items := []pickItem{{Label: "Asana: " + m.asanaConnection.status(now).String(), Help: m.asanaConnection.detail(now)}}
	if m.deps.Config.AgentsEnabled() {
		items = append(items, pickItem{Label: "Agents: " + m.agentConnection.status(now).String(), Help: m.agentConnection.detail(now)})
	}
	items = append(items, pickItem{Label: "Retry now", Key: "r", Help: "Refresh Asana and check enabled agents. Automatic refresh also continues while this menu is open."})
	for _, source := range []struct {
		name   string
		health *connectionHealth
	}{{"Asana", &m.asanaConnection}, {"Agents", &m.agentConnection}} {
		for i := len(source.health.outages) - 1; i >= 0; i-- {
			outage := source.health.outages[i]
			state := "Restored"
			if outage.ended.IsZero() {
				state = "Ongoing"
			}
			items = append(items, pickItem{Label: outage.started.Format("Jan 02 15:04:05") + " " + source.name + ": " + state, Help: outage.detail(now)})
		}
	}
	m.connectionMenu.items = items
	m.connectionMenu.refilter()
}
