package kitty

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
