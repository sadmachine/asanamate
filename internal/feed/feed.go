// Package feed runs feed commands: providers of time-stamped lines for the
// Feed panel, such as time entries, CI runs, or meetings (docs/extensions.md).
package feed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sadmachine/asanamate/internal/display"
	"github.com/sadmachine/asanamate/internal/jsoncmd"
)

// ProtocolVersion is the feed protocol version every request carries. It
// changes only when existing providers would break; new optional fields keep
// it, so providers must ignore request fields they do not know.
const ProtocolVersion = 1

// Timeout bounds one run of a feed command.
const Timeout = time.Minute

// Request is sent as JSON on provider stdin.
type Request struct {
	Version   int    `json:"version"`
	Operation string `json:"operation"`
	Date      string `json:"date"`
}

// Tones color an item through the theme's roles.
const (
	ToneOK    = "ok"
	ToneWarn  = "warn"
	ToneError = "error"
	ToneMuted = "muted"
)

// Item is one line of a feed. Time after now marks an upcoming item. The
// links are optional: TicketGID, then URL when it is an Asana task URL, then
// Branch (narrowed by Repo) name the ticket it belongs to.
type Item struct {
	Time      time.Time `json:"time"`
	Text      string    `json:"text"`
	TicketGID string    `json:"ticket_gid,omitempty"`
	URL       string    `json:"url,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	Repo      string    `json:"repo,omitempty"`
	Tone      string    `json:"tone,omitempty"`
}

// Reply is one feed command's answer. Icon names a glyph that leads the
// summary, such as "clock"; a name the panel does not know shows none.
type Reply struct {
	Source  string `json:"source"`
	Summary string `json:"summary,omitempty"`
	Icon    string `json:"icon,omitempty"`
	Items   []Item `json:"items"`
}

// Limits on a reply's text.
const (
	MaxSource  = 8
	MaxSummary = 24
	maxText    = 200
)

// Fetch runs command for the local date of day and returns its validated
// reply, with text made single-line and cut to the limits.
func Fetch(ctx context.Context, command string, day time.Time) (Reply, error) {
	out, err := jsoncmd.Run(ctx, "feed", command, Request{Version: ProtocolVersion, Operation: "feed", Date: day.Format(time.DateOnly)})
	if err != nil {
		return Reply{}, err
	}
	var r struct {
		Source  string  `json:"source"`
		Summary string  `json:"summary"`
		Icon    string  `json:"icon"`
		Items   *[]Item `json:"items"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return Reply{}, fmt.Errorf("feed: %w", err)
	}
	source := display.Line(r.Source)
	if source == "" || len([]rune(source)) > MaxSource {
		return Reply{}, fmt.Errorf("feed: source must be 1 to %d characters, got %q", MaxSource, r.Source)
	}
	if r.Items == nil {
		return Reply{}, errors.New("feed: missing items")
	}
	reply := Reply{Source: source, Summary: cut(display.Line(r.Summary), MaxSummary), Icon: display.Line(r.Icon), Items: *r.Items}
	for i, it := range reply.Items {
		if it.Time.IsZero() {
			return Reply{}, fmt.Errorf("feed %s: item %d needs a time", source, i+1)
		}
		switch it.Tone {
		case "", ToneOK, ToneWarn, ToneError, ToneMuted:
		default:
			return Reply{}, fmt.Errorf("feed %s: item %d has unknown tone %q", source, i+1, it.Tone)
		}
		reply.Items[i].Text = cut(display.Line(it.Text), maxText)
		if reply.Items[i].Text == "" {
			return Reply{}, fmt.Errorf("feed %s: item %d needs text", source, i+1)
		}
	}
	return reply, nil
}

// cut shortens s to n characters, ending in "…" when it cuts.
func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
