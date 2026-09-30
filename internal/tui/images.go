package tui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/kitty"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// Inline image ids ride in a 256-color foreground; ids below 16 would be
// sent as basic colors, which placeholders do not accept.
const firstImageID, lastImageID = 16, 255

// inlineImage is an inline image the terminal holds as id, cols x rows cells.
// id is 0 while it loads or after it fails.
type inlineImage struct{ id, cols, rows int }

type inlineImageMsg struct {
	gid            string
	seq            string
	id, cols, rows int
	err            error
}

// inlineImagesOn reports whether inline images draw in place of their links.
func (m *Model) inlineImagesOn() bool {
	return m.deps.Images && m.deps.Config.Images.Inline
}

// loadInlineImages starts loading the inline images of t that are not loaded
// or loading, sized to the reader as it is now.
func (m *Model) loadInlineImages(t ticket.Ticket) tea.Cmd {
	if !m.inlineImagesOn() || m.deps.Client == nil {
		return nil
	}
	gids := ticket.ImageGIDs(t.HTMLNotes)
	for _, c := range t.Comments {
		gids = append(gids, ticket.ImageGIDs(c.HTMLText)...)
	}
	_, readerW, _ := m.paneWidths()
	// Comments sit two columns in, behind their gutter.
	cols, rows := m.textWidth(max(readerW-2, 20))-2, max(m.height/2, 4)
	var cmds []tea.Cmd
	for _, gid := range gids {
		if _, ok := m.images[gid]; ok {
			continue
		}
		m.images[gid] = &inlineImage{}
		cmds = append(cmds, loadInlineImage(m.deps.Client, gid, m.nextImageID(), cols, rows, m.deps.InTmux))
	}
	return tea.Batch(cmds...)
}

// nextImageID cycles through the inline image ids; after that many images
// the oldest id is reused.
func (m *Model) nextImageID() int {
	id := firstImageID + m.imageSeq%(lastImageID-firstImageID+1)
	m.imageSeq++
	return id
}

func loadInlineImage(c *asana.Client, gid string, id, cols, rows int, inTmux bool) tea.Cmd {
	return request(func(ctx context.Context) tea.Msg {
		data, err := fetchImage(ctx, c, gid)
		if err != nil {
			return inlineImageMsg{gid: gid, err: err}
		}
		seq, cols, rows, err := kitty.Inline(data, id, cols, rows, inTmux)
		return inlineImageMsg{gid: gid, seq: seq, id: id, cols: cols, rows: rows, err: err}
	})
}

// fetchImage downloads an attachment's data from a fresh download URL.
func fetchImage(ctx context.Context, c *asana.Client, gid string) ([]byte, error) {
	a, err := c.Attachment(ctx, gid)
	if err != nil {
		return nil, err
	}
	if a.DownloadURL == nil {
		return nil, errors.New("attachment has no download URL")
	}
	return kitty.Download(ctx, *a.DownloadURL)
}

// inlineImageLoaded sends a loaded image to the terminal and redraws the
// reader with it. A failed image stays a link.
func (m *Model) inlineImageLoaded(msg inlineImageMsg) tea.Cmd {
	img := m.images[msg.gid]
	if img == nil {
		return nil
	}
	if msg.err != nil {
		m.status = "inline image: " + msg.err.Error()
		return nil
	}
	*img = inlineImage{id: msg.id, cols: msg.cols, rows: msg.rows}
	if m.shownGID != "" {
		m.renderDetail(true)
	}
	return tea.Raw(msg.seq)
}

// renderRich renders Asana rich text like renderBody. With inline images on,
// each loaded image that fits draws in place of its link.
func (m *Model) renderRich(html string, width int) string {
	if !m.inlineImagesOn() {
		return m.renderBody(ticket.HTMLToMarkdown(html), width)
	}
	var out []string
	for _, p := range ticket.SplitImages(html) {
		if img := m.images[p.ImageGID]; p.ImageGID != "" && img != nil && img.id != 0 && img.cols <= m.textWidth(width) {
			out = append(out, kitty.Placeholder(img.id, img.cols, img.rows))
		} else if s := m.renderBody(p.Markdown, width); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n\n")
}
