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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	ansikitty "github.com/charmbracelet/x/ansi/kitty"
	"golang.org/x/sys/unix"
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

// Encode converts an image to kitty graphics escape sequences sized to fit
// cols x rows cells, assuming cells are about twice as tall as wide.
func Encode(data []byte, cols, rows int, inTmux bool) (string, error) {
	data, w, h, err := toPNG(data)
	if err != nil {
		return "", err
	}
	size := fmt.Sprintf("r=%d", max(rows, 1))
	if w*2*rows > cols*h {
		size = fmt.Sprintf("c=%d", max(cols, 1))
	}
	return transmit(data, "a=T,f=100,q=2,"+size, inTmux), nil
}

// terminalCellSize returns the terminal's cell dimensions in pixels. Older
// terminals that omit pixel dimensions use the previous 8x16 estimate.
func terminalCellSize() (width, height int) {
	for _, f := range []*os.File{os.Stdout, os.Stdin} {
		ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
		if err == nil && ws.Col > 0 && ws.Row > 0 {
			width, height = int(ws.Xpixel)/int(ws.Col), int(ws.Ypixel)/int(ws.Row)
			if width > 0 && height > 0 {
				return width, height
			}
		}
	}
	return 8, 16
}

// inlineSize fits an image to the available cells without enlarging it.
// Round height up so the placement leaves less than one row of padding.
func inlineSize(w, h, maxCols, maxRows, cellWidth, cellHeight int) (cols, rows int) {
	cols = min(max(maxCols, 1), (w+cellWidth-1)/cellWidth, maxDiacritic)
	lim := min(max(maxRows, 1), maxDiacritic)
	cols = min(cols, max(lim*w*cellHeight/(h*cellWidth), 1))
	rows = min(max((cols*h*cellWidth+w*cellHeight-1)/(w*cellHeight), 1), lim)
	return cols, rows
}

// Inline transmits an image as id with a virtual placement no larger than
// maxCols x maxRows cells, drawn wherever Placeholder text for id is shown.
// id must be in 16..255, since the placeholder's 256-color foreground carries
// it. It returns the escape sequences and the placement's size in cells.
func Inline(data []byte, id, maxCols, maxRows int, inTmux bool) (seq string, cols, rows int, err error) {
	data, w, h, err := toPNG(data)
	if err != nil {
		return "", 0, 0, err
	}
	cellWidth, cellHeight := terminalCellSize()
	cols, rows = inlineSize(w, h, maxCols, maxRows, cellWidth, cellHeight)
	control := fmt.Sprintf("a=T,f=100,q=2,U=1,i=%d,c=%d,r=%d", id, cols, rows)
	return transmit(data, control, inTmux), cols, rows, nil
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
		fmt.Fprintf(&b, "\x1b[38;5;%dm", id)
		// Every cell names its row and column so a partial redraw still
		// places it.
		for c := range cols {
			b.WriteRune(ansikitty.Placeholder)
			b.WriteRune(ansikitty.Diacritic(r))
			b.WriteRune(ansikitty.Diacritic(c))
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

// Viewer shows an encoded image full screen until Enter is pressed.
// It implements tea.ExecCommand so Bubble Tea releases the terminal first.
type Viewer struct {
	payload, clear string
	stdin          io.Reader
	stdout         io.Writer
}

// NewViewer returns a viewer for a payload produced by Encode.
func NewViewer(payload string, inTmux bool) *Viewer {
	return &Viewer{payload: payload, clear: Clear(inTmux)}
}

func (v *Viewer) SetStdin(r io.Reader)  { v.stdin = r }
func (v *Viewer) SetStdout(w io.Writer) { v.stdout = w }
func (v *Viewer) SetStderr(io.Writer)   {}

// Run draws the image, waits for Enter, then removes the image.
func (v *Viewer) Run() error {
	fmt.Fprint(v.stdout, "\x1b[2J\x1b[H", v.payload, "\r\n\r\nPress Enter to return.")
	_, err := bufio.NewReader(v.stdin).ReadString('\n')
	fmt.Fprint(v.stdout, v.clear)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
