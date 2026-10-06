// Package browser opens http(s) URLs in the default browser.
package browser

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// Open opens rawURL with Opener.
func Open(rawURL string) error {
	if err := validate(rawURL); err != nil {
		return err
	}
	return exec.Command(Opener(), rawURL).Run()
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
