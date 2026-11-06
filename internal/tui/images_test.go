package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/kitty"
)

func TestInlineImageConnectionFailureAndRecovery(t *testing.T) {
	m := splitModel(t)
	m.images["9"] = &inlineImage{}
	now := testToday
	m.now = func() time.Time { return now }
	for range 3 {
		m.Update(inlineImageMsg{gid: "9", generation: m.imageEpoch, cell: m.imageCell, err: connectionFailure()})
	}
	if len(m.notices) != 0 || len(m.statusHistory) != 0 || m.status != "" || m.toast != nil {
		t.Fatal("inline image connection failures leaked into ordinary notifications")
	}
	if m.images["9"].id != 0 {
		t.Fatal("failed image must remain a link")
	}
	openConnectionMenu(t, m)
	if text := connectionMenuText(m); !strings.Contains(text, "Asana: Reconnecting") ||
		!strings.Contains(text, "3 failed attempts") || !strings.Contains(text, "loading inline image") ||
		!strings.Contains(text, "no such host") {
		t.Fatalf("image connection details = %s", text)
	}
	now = now.Add(time.Minute)
	m.Update(connectionTickMsg{})
	if text := connectionMenuText(m); !strings.Contains(text, "Asana: Unavailable") {
		t.Fatalf("persistent image connection failure = %s", text)
	}
	_, cmd := m.Update(inlineImageMsg{gid: "9", generation: m.imageEpoch, cell: m.imageCell,
		id: 20, cols: 4, rows: 2, seq: "image"})
	if cmd == nil || m.images["9"].id != 20 {
		t.Fatal("successful image must load and transmit")
	}
	if text := connectionMenuText(m); !strings.Contains(text, "Asana: Connected") || !strings.Contains(text, "Restored") {
		t.Fatalf("image connection recovery = %s", text)
	}
	if len(m.notices) != 0 || len(m.statusHistory) != 0 || m.status != "" || m.toast != nil {
		t.Fatal("image connection recovery created an ordinary notification")
	}
}

func TestInlineImageNonConnectionErrorsRemainVisible(t *testing.T) {
	// A complete download can still contain a truncated image. Its decode
	// error must not be treated as a truncated network response.
	_, _, _, decodeErr := kitty.Inline([]byte{137, 80, 78, 71, 13, 10, 26, 10}, 20, 4, 2, kitty.CellSize{}, false)
	if decodeErr == nil {
		t.Fatal("truncated PNG must fail decoding")
	}
	for _, tc := range []struct {
		name           string
		err, renderErr error
	}{
		{name: "missing download URL", err: errors.New("attachment has no download URL")},
		{name: "permission", err: &asana.APIError{Status: 403, Message: "permission denied"}},
		{name: "decoding", renderErr: decodeErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := splitModel(t)
			m.images["9"] = &inlineImage{}
			m.Update(inlineImageMsg{gid: "9", generation: m.imageEpoch, cell: m.imageCell, err: tc.err, renderErr: tc.renderErr})
			failure := tc.err
			if failure == nil {
				failure = tc.renderErr
			}
			if len(m.notices) != 1 || len(m.statusHistory) != 1 || m.status != "inline image: "+failure.Error() {
				t.Fatalf("image error disappeared: status = %q", m.status)
			}
			if m.asanaConnection.active() != nil || m.images["9"].id != 0 {
				t.Fatal("invalid image must remain a link without starting a connection outage")
			}
		})
	}
}

func TestInlineImageStaleResultsDoNotChangeConnection(t *testing.T) {
	for _, stale := range []string{"generation", "cell size", "missing image"} {
		for _, failed := range []bool{false, true} {
			name := "success"
			if failed {
				name = "failure"
			}
			t.Run(stale+"/"+name, func(t *testing.T) {
				m := splitModel(t)
				m.images["9"] = &inlineImage{}
				msg := inlineImageMsg{gid: "9", generation: m.imageEpoch, cell: m.imageCell, id: 20}
				if failed {
					msg.err = connectionFailure()
				} else {
					m.Update(tasksMsg{err: connectionFailure()})
				}
				before := m.asanaConnection.status(m.now())
				switch stale {
				case "generation":
					msg.generation++
				case "cell size":
					msg.cell.Width++
				case "missing image":
					delete(m.images, "9")
				}
				_, cmd := m.Update(msg)
				if cmd != nil || m.asanaConnection.status(m.now()) != before || len(m.notices) != 0 || len(m.statusHistory) != 0 {
					t.Fatal("stale image result transmitted an image or changed connection health or notifications")
				}
			})
		}
	}
}
