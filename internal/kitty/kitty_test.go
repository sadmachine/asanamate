package kitty

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestSupported(t *testing.T) {
	yes := func() bool { return true }
	no := func() bool { return false }
	cases := []struct {
		name string
		mode string
		vars map[string]string
		pass func() bool
		want bool
	}{
		{"off", "off", map[string]string{"KITTY_WINDOW_ID": "1"}, yes, false},
		{"forced", "kitty", nil, no, true},
		{"kitty direct", "auto", map[string]string{"KITTY_WINDOW_ID": "1"}, no, true},
		{"ghostty in tmux with passthrough", "auto", map[string]string{"TERM_PROGRAM": "ghostty", "TMUX": "x"}, yes, true},
		{"ghostty in tmux without passthrough", "auto", map[string]string{"GHOSTTY_RESOURCES_DIR": "/x", "TMUX": "x"}, no, false},
		{"plain xterm", "auto", map[string]string{"TERM": "xterm-256color"}, yes, false},
	}
	for _, c := range cases {
		if got := Supported(c.mode, env(c.vars), c.pass); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsImage(t *testing.T) {
	if !IsImage("Shot.PNG") || !IsImage("a.jpeg") || IsImage("doc.pdf") {
		t.Fatal("IsImage misclassified")
	}
}

func pngBytes(t *testing.T, w, h int, noisy bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			c := color.RGBA{A: 255}
			if noisy {
				c.R, c.G, c.B = uint8(rand.Intn(256)), uint8(rand.Intn(256)), uint8(rand.Intn(256))
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestEncodeSmall(t *testing.T) {
	out, err := Encode(pngBytes(t, 10, 400, false), 80, 24, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "\x1b_Ga=T,f=100,q=2,r=24,m=0;") || !strings.HasSuffix(out, "\x1b\\") {
		t.Fatalf("unexpected payload prefix/suffix: %q", out[:40])
	}
}

func TestEncodeChunksWideImagesAndWrapsForTmux(t *testing.T) {
	out, err := Encode(pngBytes(t, 400, 100, true), 80, 24, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "\x1bPtmux;\x1b\x1b_Ga=T,f=100,q=2,c=80,m=1;") {
		t.Fatalf("prefix = %q", out[:48])
	}
	if strings.Count(out, "\x1bPtmux;") < 2 || !strings.Contains(out, "m=0;") {
		t.Fatal("want several chunks, the last with m=0")
	}
}

func TestEncodeRejectsNonImage(t *testing.T) {
	if _, err := Encode([]byte("not an image"), 80, 24, false); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestDownload(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("Authorization header must not be sent to download hosts")
		}
		w.Write([]byte("0123456789"))
	}))
	defer srv.Close()
	oldClient, oldMax := httpClient, maxImageBytes
	httpClient, maxImageBytes = srv.Client(), 10
	defer func() { httpClient, maxImageBytes = oldClient, oldMax }()

	if data, err := Download(context.Background(), srv.URL); err != nil || string(data) != "0123456789" {
		t.Fatalf("data = %q, err = %v", data, err)
	}
	maxImageBytes = 5
	if _, err := Download(context.Background(), srv.URL); err == nil {
		t.Fatal("expected a size-limit error")
	}
	if _, err := Download(context.Background(), "http://example.com/a.png"); err == nil {
		t.Fatal("expected rejection of a non-https URL")
	}
}

func TestViewerWritesPayloadAndClears(t *testing.T) {
	v := NewViewer("PAYLOAD", false)
	var out bytes.Buffer
	v.SetStdin(strings.NewReader("\n"))
	v.SetStdout(&out)
	if err := v.Run(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "PAYLOAD") || !strings.HasSuffix(out.String(), Clear(false)) {
		t.Fatalf("out = %q", out.String())
	}
}

func TestEncodeRejectsHugeDimensions(t *testing.T) {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 40000)
	binary.BigEndian.PutUint32(ihdr[4:], 40000)
	ihdr[8], ihdr[9] = 8, 6
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	_, err := Encode(b.Bytes(), 80, 24, false)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v, want a too-large error", err)
	}
}

func TestInlineFitsAndPlaces(t *testing.T) {
	// 160x80 px is 20x5 cells at the assumed scale.
	seq, cols, rows, err := Inline(pngBytes(t, 160, 80, false), 42, 80, 24, CellSize{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if cols != 20 || rows != 5 {
		t.Fatalf("size = %dx%d, want 20x5", cols, rows)
	}
	if !strings.HasPrefix(seq, "\x1b_Ga=T,f=100,q=2,U=1,i=42,c=20,r=5,m=0;") {
		t.Fatalf("prefix = %q", seq[:48])
	}
	// Width and height limits shrink it, keeping its shape.
	if _, cols, rows, _ := Inline(pngBytes(t, 160, 80, false), 42, 10, 24, CellSize{}, false); cols != 10 || rows != 3 {
		t.Fatalf("width-capped size = %dx%d, want 10x3", cols, rows)
	}
	if _, cols, rows, _ := Inline(pngBytes(t, 160, 80, false), 42, 80, 2, CellSize{}, false); cols != 8 || rows != 2 {
		t.Fatalf("height-capped size = %dx%d, want 8x2", cols, rows)
	}
}

func TestInlineSize(t *testing.T) {
	cases := []struct {
		name                              string
		w, h, maxCols, maxRows            int
		cellWidth, cellHeight, cols, rows int
	}{
		{"fallback", 160, 80, 80, 24, 8, 16, 20, 5},
		{"taller cells", 160, 80, 80, 24, 8, 24, 20, 4},
		{"retina cells", 320, 160, 80, 24, 16, 36, 40, 9},
		{"task first image", 872, 684, 77, 36, 8, 18, 77, 27},
		{"task first image retina", 872, 684, 77, 36, 16, 36, 77, 27},
		{"task second image", 654, 336, 77, 36, 8, 18, 77, 18},
		{"task second image retina", 654, 336, 77, 36, 16, 36, 77, 18},
		{"width limit", 160, 80, 10, 24, 8, 24, 10, 2},
		{"height limit", 160, 80, 80, 2, 8, 24, 12, 2},
		{"fractional height limit", 160, 80, 10, 2, 8, 16, 8, 2},
		{"tiny image", 1, 1, 80, 24, 8, 24, 1, 1},
		{"invalid limits", 160, 80, 0, 0, 8, 24, 1, 1},
		{"diacritic limit", 10000, 10000, 1000, 1000, 8, 16, 297, 149},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cols, rows := inlineSize(tc.w, tc.h, tc.maxCols, tc.maxRows, tc.cellWidth, tc.cellHeight)
			if cols != tc.cols || rows != tc.rows {
				t.Fatalf("size = %dx%d, want %dx%d", cols, rows, tc.cols, tc.rows)
			}
		})
	}
}

func TestRequestCellSize(t *testing.T) {
	if got := RequestCellSize(false); got != "\x1b[16t" {
		t.Fatalf("query = %q", got)
	}
	if got := RequestCellSize(true); got != "\x1bPtmux;\x1b\x1b[16t\x1b\\" {
		t.Fatalf("tmux query = %q", got)
	}
}

func TestPlaceholderCells(t *testing.T) {
	lines := strings.Split(Placeholder(42, 3, 2), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w != 3 {
			t.Fatalf("width = %d, want 3: %q", w, l)
		}
		if !strings.HasPrefix(l, "\x1b[38;5;42m") {
			t.Fatalf("line lacks the id color: %q", l)
		}
	}
}
