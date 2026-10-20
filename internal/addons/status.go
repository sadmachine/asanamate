package addons

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/jsoncmd"
)

// Saved is a validated snapshot, never the active TUI configuration.
type Saved struct {
	Path   string
	Config config.Config
	Err    error
}

func Load(path string) Saved {
	cfg, err := config.Load(path)
	if err != nil {
		cfg = config.Default() // Do not interpret partially decoded invalid data.
	}
	return Saved{Path: path, Config: cfg, Err: err}
}

// Clean removes terminal controls from local diagnostics and file paths.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

func (s Saved) Status() string {
	if errors.Is(s.Err, config.ErrNotConfigured) {
		return "core setup required: run asanamate setup"
	}
	if s.Err != nil {
		return "invalid saved config: " + Clean(s.Err.Error())
	}
	return "saved config valid"
}

type CapabilityStatus struct {
	Name, Key, Status           string
	Configured, Custom, Unknown bool
}

type ToolStatus struct{ Name, Path, Status string }

// Report separates installation, configuration and explicit check results.
type Report struct {
	Capabilities []CapabilityStatus
	Tools        []ToolStatus
	Check, Next  string
	Failed       bool
}

func Inspect(e Entry, saved Saved) Report {
	r := Report{Check: "connection unverified; no provider check performed"}
	for _, group := range []struct {
		names []string
		kind  string
	}{{e.Adapters, "adapter"}, {e.Dependencies, "dependency"}} {
		for _, name := range group.names {
			path, err := exec.LookPath(name)
			status := group.kind + " available"
			if err != nil {
				status = group.kind + " missing"
				if r.Next == "" {
					r.Next = "Install " + name + "; see installation steps."
				}
			}
			r.Tools = append(r.Tools, ToolStatus{Name: name, Path: path, Status: status})
		}
	}
	for _, c := range e.Capabilities {
		cs := CapabilityStatus{Name: c.Name, Key: c.Key, Status: "not configured"}
		if saved.Err != nil {
			cs.Unknown = true
			cs.Status = "configuration unknown: " + saved.Status()
		} else {
			for _, command := range c.Commands(saved.Config) {
				if c.recognizes(command) {
					cs.Configured, cs.Status = true, "configured"
				} else if strings.TrimSpace(command) != "" {
					cs.Custom = true
				}
			}
			if cs.Custom {
				cs.Status += "; custom command present (unverified; see extension guide)"
			}
		}
		if !cs.Configured && r.Next == "" {
			r.Next = "Configure " + c.Key + " using the example; preserve custom commands."
		}
		r.Capabilities = append(r.Capabilities, cs)
	}
	if saved.Err != nil {
		r.Next = saved.Status() + ". Guides remain available; edit " + Clean(saved.Path) + "."
	}
	if r.Next == "" {
		r.Next = "Run asanamate addons check " + e.ID + ", then follow the first-use example."
	}
	return r
}

// Configured reports whether any documented capability uses this addon.
func (r Report) Configured() bool {
	for _, c := range r.Capabilities {
		if c.Configured {
			return true
		}
	}
	return false
}

func (r Report) Summary() string {
	var parts []string
	for _, t := range r.Tools {
		if t.Path == "" {
			parts = append(parts, t.Name+": "+t.Status)
		}
	}
	for _, c := range r.Capabilities {
		parts = append(parts, c.Name+": "+c.Status)
	}
	if len(parts) == 0 {
		parts = append(parts, "custom commands unverified")
	}
	return strings.Join(parts, "; ")
}

// ListStatus keeps the installation and configuration states visible in a
// picker row; detail and help retain the independent capability statuses.
func (r Report) ListStatus() string {
	var parts []string
	for _, t := range r.Tools {
		if t.Path == "" {
			parts = append(parts, t.Status)
			break
		}
	}
	configured, custom, unknown := 0, false, false
	for _, c := range r.Capabilities {
		if c.Configured {
			configured++
		}
		custom = custom || c.Custom
		unknown = unknown || c.Unknown
	}
	switch {
	case unknown:
		parts = append(parts, "config unavailable")
	case configured > 0:
		parts = append(parts, "configured")
	case custom:
		parts = append(parts, "custom command")
	case len(r.Capabilities) > 0:
		parts = append(parts, "not configured")
	default:
		parts = append(parts, "custom commands")
	}
	return strings.Join(parts, "; ")
}

// Check runs only the shipped read-only vector, bounded to five seconds and
// 64 KiB per stream. Provider output is discarded, even on failure: it may
// contain credentials, account data or terminal controls. No shell is used.
func Check(ctx context.Context, e Entry, saved Saved) Report {
	r := Inspect(e, saved)
	fail := func(message string) Report {
		r.Check, r.Failed = "check failed: "+message+"; connection unverified", true
		return r
	}
	if saved.Err != nil {
		return fail(saved.Status())
	}
	for _, t := range r.Tools {
		if t.Path == "" {
			return fail(t.Name + ": " + t.Status)
		}
	}
	if len(e.Check) == 0 {
		r.Check = "local check passed: executable/config inspection only; connection unverified; " + e.Verify
		r.Next = "Follow the first-use example; verify custom commands using their extension protocols."
		return r
	}
	next := r.Next
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	r.Next = e.CheckFailure
	defer cancel()
	out, err := jsoncmd.RunArgsBounded(ctx, "addon check", e.Check, jsoncmd.Limits{Stdout: 64 << 10, Stderr: 64 << 10})
	if err != nil {
		// Never expose provider diagnostics, including tokens printed on stderr.
		reason := "command failed; follow verification steps to diagnose locally"
		if ctx.Err() != nil {
			reason = "command timed out or cancelled"
		} else if errors.Is(err, jsoncmd.ErrOutputLimit) {
			reason = "command exceeded output limit"
		}
		return fail(strings.Join(e.Check, " ") + ": " + reason)
	}
	if e.CheckArray {
		var sessions []json.RawMessage
		if json.Unmarshal(out, &sessions) != nil || !strings.HasPrefix(strings.TrimSpace(string(out)), "[") {
			return fail(strings.Join(e.Check, " ") + " did not return a session array")
		}
	}
	r.Check = "local check passed: " + strings.Join(e.Check, " ") + "; connection unverified"
	r.Next = next
	if r.Configured() {
		r.Next = "Follow the first-use example to verify access; hand edits require restarting asanamate. Other capabilities remain independently configurable."
	}
	return r
}

// Detail renders identical plain-text guidance for both entry points.
func Detail(e Entry, saved Saved, r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n%s\n", e.Name, e.ID, e.Benefit)
	for _, c := range e.Capabilities {
		fmt.Fprintf(&b, "- %s (%s), independently configurable\n", c.Name, c.Key)
	}
	fmt.Fprintf(&b, "\nRequirements and current local status\n%s\nConfig: %s\n%s\n", e.Availability, Clean(saved.Path), saved.Status())
	for _, t := range r.Tools {
		fmt.Fprintf(&b, "%s: %s %s\n", t.Name, t.Status, Clean(t.Path))
	}
	for _, c := range r.Capabilities {
		fmt.Fprintf(&b, "%s: %s\n", c.Name, c.Status)
	}
	fmt.Fprintf(&b, "%s\nNext: %s\n\nInstallation, authentication and configuration\n", r.Check, r.Next)
	for i, step := range e.Steps {
		fmt.Fprintf(&b, "%d. %s\n\n", i+1, step)
	}
	if len(e.Adapters) > 0 {
		b.WriteString("Bundled adapter installation (macOS/Linux amd64/arm64): normal installation includes adapters. If missing, re-extract your SAME release archive, without downloading another release:\n")
		fmt.Fprintf(&b, "tar -xzf asanamate_<os>_<arch>.tar.gz %s\nmkdir -p ~/.local/bin\nmv %s ~/.local/bin/\nexport PATH=\"$HOME/.local/bin:$PATH\"\n", strings.Join(e.Adapters, " "), strings.Join(e.Adapters, " "))
		b.WriteString("From the same source checkout (Go 1.26+): sh ./install.sh\nInstalls all commands to $(go env GOBIN), else $(go env GOPATH)/bin, plus themes. Add that bin directory to PATH. Installation does not enable or authenticate addons.\nOn macOS, if browser download quarantine blocks an adapter:\n")
		for _, name := range e.Adapters {
			fmt.Fprintf(&b, "xattr -d com.apple.quarantine ~/.local/bin/%s\n", name)
		}
		b.WriteString("Recheck after each step: asanamate addons check " + e.ID + "\n\n")
	}
	fmt.Fprintf(&b, "%s\n\nEdit %s with asanamate config, or your editor. Hand edits require restarting asanamate. Saved configuration shown here may differ from the current session's active configuration. No snippets are applied automatically.\n\nChecks and first use\n%s\n%s\n\nDisable, uninstall and help\n%s\nGuide: %s\nExtension protocols: %s\n", e.Snippet, Clean(saved.Path), e.Verify, e.Example, e.Remove, e.Guide, Extensions)
	return b.String()
}
