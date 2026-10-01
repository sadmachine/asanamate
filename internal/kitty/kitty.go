// Package kitty shows images with the kitty terminal graphics protocol.
package kitty

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	ansikitty "github.com/charmbracelet/x/ansi/kitty"
	"github.com/charmbracelet/x/term"
)

const chunkSize = 4096

// maxPixels caps decoded image size; a small file can declare huge dimensions.
const maxPixels = 50_000_000

var (
	httpClient    = &http.Client{Timeout: 30 * time.Second}
	maxImageBytes = 20 << 20
)

// Supported reports whether images can be shown. mode is "auto", "kitty", or "off".
func Supported(mode string, getenv func(string) string, passthrough func() bool) bool {
	switch mode {
	case "off":
		return false
	case "kitty":
		return true
	}
	if !kittyTerminal(getenv) {
		return false
	}
	return getenv("TMUX") == "" || passthrough()
}

func kittyTerminal(getenv func(string) string) bool {
	if getenv("KITTY_WINDOW_ID") != "" || getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return true
	}
	switch getenv("TERM_PROGRAM") {
	case "ghostty", "WezTerm":
		return true
	}
	term := getenv("TERM")
	return strings.Contains(term, "kitty") || strings.Contains(term, "ghostty")
}

// TmuxPassthrough reports whether tmux forwards escape sequences to the outer terminal.
func TmuxPassthrough() bool {
	out, err := exec.Command("tmux", "show", "-gv", "allow-passthrough").Output()
	v := strings.TrimSpace(string(out))
	return err == nil && (v == "on" || v == "all")
}

// IsImage reports whether a file name has an image extension we can decode.
func IsImage(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	}
	return false
}

// Download fetches an image over https without Asana credentials.
func Download(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return nil, errors.New("attachment download URL is not https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxImageBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImageBytes {
		return nil, fmt.Errorf("image is larger than %d bytes", maxImageBytes)
	}
	return data, nil
}

// Encode converts an image to kitty graphics escape sequences that draw it
// near its own size, no larger than maxCols x maxRows cells, centered in that
// area from the top-left of the screen.
func Encode(data []byte, maxCols, maxRows int, cell CellSize, inTmux bool) (string, error) {
	data, w, h, err := toPNG(data)
	if err != nil {
		return "", err
	}
	cols, rows := fit(w, h, maxCols, maxRows, cell)
	moveTo := fmt.Sprintf("\x1b[%d;%dH", max(maxRows-rows, 0)/2+1, max(maxCols-cols, 0)/2+1)
	return moveTo + transmit(data, fmt.Sprintf("a=T,f=100,q=2,c=%d,r=%d", cols, rows), inTmux), nil
}

// CellSize is the terminal's directly reported cell size in pixels.
// A zero value uses the previous 8x16 estimate.
type CellSize struct{ Width, Height int }

// RequestCellSize asks the outer terminal for its cell dimensions, bypassing
// tmux's pane/window pixel geometry.
func RequestCellSize(inTmux bool) string { return wrap("\x1b[16t", inTmux) }

// inlineSize fits an image to the available cells at the original eight-pixel
// column scale. Only the cell aspect ratio affects its shape, so Retina pixel
// dimensions do not shrink images.
// Round height up so the placement leaves less than one row of padding.
func inlineSize(w, h, maxCols, maxRows, cellWidth, cellHeight int) (cols, rows int) {
	cols = min(max(maxCols, 1), (w+7)/8, maxDiacritic)
	lim := min(max(maxRows, 1), maxDiacritic)
	cols = min(cols, max(lim*w*cellHeight/(h*cellWidth), 1))
	rows = min(max((cols*h*cellWidth+w*cellHeight-1)/(w*cellHeight), 1), lim)
	return cols, rows
}

// fit sizes a w x h pixel image in cells, using the 8x16 estimate when the
// cell size is unknown.
func fit(w, h, maxCols, maxRows int, cell CellSize) (cols, rows int) {
	if cell.Width <= 0 || cell.Height <= 0 {
		cell = CellSize{Width: 8, Height: 16}
	}
	return inlineSize(w, h, maxCols, maxRows, cell.Width, cell.Height)
}

// Inline transmits an image as id with a virtual placement no larger than
// maxCols x maxRows cells, drawn wherever Placeholder text for id is shown.
// id's low byte must be in 16..255, since the placeholder's 256-color
// foreground carries it; a third diacritic carries its high byte. It returns
// the escape sequences and the placement's size in cells.
func Inline(data []byte, id, maxCols, maxRows int, cell CellSize, inTmux bool) (seq string, cols, rows int, err error) {
	data, w, h, err := toPNG(data)
	if err != nil {
		return "", 0, 0, err
	}
	cols, rows = fit(w, h, maxCols, maxRows, cell)
	control := fmt.Sprintf("a=T,f=100,q=2,U=1,i=%d,c=%d,r=%d", id, cols, rows)
	// Delete any earlier image with this id first: some terminals keep its old
	// placements when it is retransmitted and size the new image to them.
	del := wrap(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id), inTmux)
	return del + transmit(data, control, inTmux), cols, rows, nil
}

// maxDiacritic is the number of row and column diacritics placeholders can use.
const maxDiacritic = 297

// Placeholder returns rows lines of cols cells that draw image id, placed by
// Inline, one cell of the image each.
func Placeholder(id, cols, rows int) string {
	var b strings.Builder
	for r := range rows {
		if r > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "\x1b[38;5;%dm", id&0xff)
		// Every cell names its row and column so a partial redraw still
		// places it.
		for c := range cols {
			b.WriteRune(ansikitty.Placeholder)
			b.WriteRune(ansikitty.Diacritic(r))
			b.WriteRune(ansikitty.Diacritic(c))
			b.WriteRune(ansikitty.Diacritic(id >> 24))
		}
		b.WriteString("\x1b[39m")
	}
	return b.String()
}

// toPNG decodes an image, returning it as PNG with its size. Decoding catches
// corrupt files the terminal would silently drop.
func toPNG(data []byte) ([]byte, int, int, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decode image: %w", err)
	}
	if cfg.Width*cfg.Height > maxPixels {
		return nil, 0, 0, fmt.Errorf("image is too large to display (%dx%d)", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decode image: %w", err)
	}
	if format != "png" {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, 0, 0, err
		}
		data = buf.Bytes()
	}
	return data, cfg.Width, cfg.Height, nil
}

// transmit sends PNG data in chunks, control leading the first.
func transmit(data []byte, control string, inTmux bool) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	var out strings.Builder
	for i := 0; i < len(encoded); i += chunkSize {
		end := min(i+chunkSize, len(encoded))
		more := 1
		if end == len(encoded) {
			more = 0
		}
		c := fmt.Sprintf("m=%d", more)
		if i == 0 {
			c = control + "," + c
		}
		out.WriteString(wrap("\x1b_G"+c+";"+encoded[i:end]+"\x1b\\", inTmux))
	}
	return out.String()
}

// Clear deletes every image placed on screen.
func Clear(inTmux bool) string { return wrap("\x1b_Ga=d,q=2\x1b\\", inTmux) }

func wrap(seq string, inTmux bool) string {
	if !inTmux {
		return seq
	}
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// Viewer shows an encoded image until Enter, q, or Esc is pressed. When it
// is one of several images, j and k also close it, asking for the next or
// previous image.
// It implements tea.ExecCommand so Bubble Tea releases the terminal first.
type Viewer struct {
	payload, clear string
	index, total   int
	stdin          io.Reader
	stdout         io.Writer
	// Step is 1 after j, -1 after k, and 0 after a close key.
	Step int
}

// NewViewer returns a viewer for a payload produced by Encode, the index'th
// (from 0) of total images.
func NewViewer(payload string, index, total int, inTmux bool) *Viewer {
	return &Viewer{payload: payload, clear: Clear(inTmux), index: index, total: total}
}

func (v *Viewer) SetStdin(r io.Reader)  { v.stdin = r }
func (v *Viewer) SetStdout(w io.Writer) { v.stdout = w }
func (v *Viewer) SetStderr(io.Writer)   {}

// Run draws the image, waits for a close or step key, then removes the image.
// A terminal stdin is put in raw mode so single keys arrive unbuffered.
func (v *Viewer) Run() error {
	if f, ok := v.stdin.(interface{ Fd() uintptr }); ok && term.IsTerminal(f.Fd()) {
		state, err := term.MakeRaw(f.Fd())
		if err != nil {
			return err
		}
		defer term.Restore(f.Fd(), state)
	}
	footer := fmt.Sprintf("Image %d/%d · ", v.index+1, max(v.total, 1))
	if v.total > 1 {
		footer += "j next · k previous · "
	}
	// Row 999 clamps to the bottom line.
	fmt.Fprint(v.stdout, "\x1b[2J", v.payload, "\x1b[999;1H", footer, "Enter, q, or Esc to return.")
	step, err := waitForKey(bufio.NewReader(v.stdin), v.total > 1)
	v.Step = step
	fmt.Fprint(v.stdout, v.clear)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// waitForKey reads until Enter, q, Esc, or Ctrl+C, returning 0, or, when
// stepping is on, until j or k, returning 1 or -1.
func waitForKey(r io.ByteReader, stepping bool) (int, error) {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		switch {
		case b == '\r', b == '\n', b == 'q', b == 0x1b, b == 0x03:
			return 0, nil
		case stepping && b == 'j':
			return 1, nil
		case stepping && b == 'k':
			return -1, nil
		}
	}
}
