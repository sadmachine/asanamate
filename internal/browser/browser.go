// Package browser opens http(s) URLs in the default browser.
package browser

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// Open opens rawURL with Opener. Its error names the URL, so it stays
// visible to copy when no browser can open it.
func Open(rawURL string) error {
	if err := validate(rawURL); err != nil {
		return err
	}
	if err := exec.Command(Opener(), rawURL).Run(); err != nil {
		hint := ""
		if runtime.GOOS != "darwin" {
			hint = "; install xdg-utils and a desktop browser"
		}
		return fmt.Errorf("could not open %s with %s: %v%s", rawURL, Opener(), err, hint)
	}
	return nil
}

// Opener is the program that opens URLs: open on macOS, else xdg-open.
func Opener() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	return "xdg-open"
}

func validate(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("refusing to open %q: not an http(s) URL", rawURL)
	}
	return nil
}
