// Package browser opens http(s) URLs in the default browser.
package browser

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// Open opens rawURL with open (macOS) or xdg-open (Linux).
func Open(rawURL string) error {
	if err := validate(rawURL); err != nil {
		return err
	}
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, rawURL).Run()
}

func validate(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("refusing to open %q: not an http(s) URL", rawURL)
	}
	return nil
}
