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

// Inline image ids carry their low byte in a 256-color foreground, where
// values below 16 would be sent as basic colors that placeholders do not
// accept, and their high byte in a diacritic. Other programs in the same
// terminal, such as another asanamate in a tmux pane, share the id space.
const (
	firstImageLow, lastImageLow = 16, 255
	imageLows                   = lastImageLow - firstImageLow + 1
	imageIDs                    = imageLows * 256
)

// inlineImage is an inline image the terminal holds as id, cols x rows cells.
// id is 0 while it loads or after it fails.
type inlineImage struct{ id, cols, rows int }

type inlineImageMsg struct {
	generation     uint64
	gid            string
	seq            string
	id, cols, rows int
	cell           kitty.CellSize
	err            error
}

func (m *Model) requestImageCellSize() tea.Cmd {
	if !m.inlineImagesOn() {
		return nil
	}
	return tea.Raw(kitty.RequestCellSize(m.deps.InTmux))
}

func (m *Model) imageCellSizeChanged(cell kitty.CellSize) tea.Cmd {
	if !m.inlineImagesOn() || cell.Width <= 0 || cell.Height <= 0 || cell == m.imageCell {
		return nil
	}
	m.imageCell = cell
	// Reload placements sized with the old estimate. Late results from those
	// requests are ignored, so all images use the same cell dimensions.
	m.images = map[string]*inlineImage{}
	if t, ok := m.selectedDetail(); ok {
		cmd := m.loadInlineImages(t)
		m.renderDetail(true)
		return cmd
	}
	return nil
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
		cmds = append(cmds, loadInlineImage(m.deps.Client, gid, m.nextImageID(), cols, rows, m.imageCell, m.deps.InTmux, m.imageEpoch))
	}
	return tea.Batch(cmds...)
}

// nextImageID cycles through the inline image ids from a random start, so
// programs sharing the terminal rarely pick the same ids; after that many
// images the oldest id is reused.
func (m *Model) nextImageID() int {
	n := m.imageSeq % imageIDs
	m.imageSeq++
	return (n/imageLows)<<24 | (firstImageLow + n%imageLows)
}

func loadInlineImage(c *asana.Client, gid string, id, cols, rows int, cell kitty.CellSize, inTmux bool, generation uint64) tea.Cmd {
	return request(func(ctx context.Context) tea.Msg {
		data, err := fetchImage(ctx, c, gid)
		if err != nil {
			return inlineImageMsg{gid: gid, generation: generation, cell: cell, err: err}
		}
		seq, cols, rows, err := kitty.Inline(data, id, cols, rows, cell, inTmux)
		return inlineImageMsg{gid: gid, generation: generation, seq: seq, id: id, cols: cols, rows: rows, cell: cell, err: err}
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
	if img == nil || msg.generation != m.imageEpoch || msg.cell != m.imageCell {
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
	var firstImage, lastImage bool
	for _, p := range ticket.SplitImages(html) {
		if img := m.images[p.ImageGID]; p.ImageGID != "" && img != nil && img.id != 0 && img.cols <= m.textWidth(width) {
			if len(out) == 0 {
				firstImage = true
			}
			lastImage = true
			out = append(out, kitty.Placeholder(img.id, img.cols, img.rows))
		} else if s := m.renderBody(p.Markdown, width); s != "" {
			lastImage = false
			out = append(out, s)
		}
	}
	body := strings.Join(out, "\n\n")
	if firstImage {
		body = "\n" + body
	}
	if lastImage {
		body += "\n"
	}
	return body
}
