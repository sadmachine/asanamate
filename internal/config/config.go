// Package config loads the user-edited asanamate configuration file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// TokenEnv names the environment variable that holds the Asana personal access token.
const TokenEnv = "ASANA_ACCESS_TOKEN"

// Action run modes.
const (
	ModeForeground = "foreground"
	ModeBackground = "background"
	ModeExit       = "exit"
)

// List layouts.
const (
	LayoutSingle = "single"
	LayoutMulti  = "multi"
)

// Reading pane views.
const (
	ViewCards    = "cards"
	ViewMarkdown = "markdown"
)

// ErrNotConfigured means the config file does not exist yet.
var ErrNotConfigured = errors.New("asanamate is not configured; run `asanamate setup`")

// Config is the contents of config.toml.
type Config struct {
	Workspace     string     `toml:"workspace"`
	Theme         string     `toml:"theme"`
	Images        string     `toml:"images"`
	DefaultFilter string     `toml:"default_filter"`
	ConfirmWrites bool       `toml:"confirm_writes"`
	BranchField   string     `toml:"branch_field"`
	Symbols       string     `toml:"symbols"`
	ReducedMotion *bool      `toml:"reduced_motion"`
	RepoSource    RepoSource `toml:"repo_source"`
	Agents        Agents     `toml:"agents"`
	List          List       `toml:"list"`
	Reader        Reader     `toml:"reader"`
	Actions       []Action   `toml:"actions"`
}

// Agents links tickets to running coding agents. It is off unless Preset or
// Command is set. Command prints "<path>\t<status>[\t<target>]" per agent.
// States maps extra raw statuses onto asanamate's states; Symbols overrides
// the symbol shown per state.
type Agents struct {
	Preset  string              `toml:"preset"`
	Command string              `toml:"command"`
	States  map[string][]string `toml:"states"`
	Symbols map[string]string   `toml:"symbols"`
}

// AgentsEnabled reports whether agent tracking is turned on.
func (c Config) AgentsEnabled() bool {
	return c.Agents.Preset != "" || strings.TrimSpace(c.Agents.Command) != ""
}

// Symbol sets.
const (
	SymbolsUnicode = "unicode"
	SymbolsNerd    = "nerd"
	SymbolsASCII   = "ascii"
)

// SymbolSet returns the symbol set to use. When none is configured it is
// unicode if the locale is UTF-8, else ascii.
func (c Config) SymbolSet(getenv func(string) string) string {
	if c.Symbols != "" {
		return c.Symbols
	}
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := getenv(k); v != "" {
			v = strings.ToLower(v)
			if strings.Contains(v, "utf-8") || strings.Contains(v, "utf8") {
				return SymbolsUnicode
			}
			return SymbolsASCII
		}
	}
	return SymbolsASCII
}

// List configures how tickets appear in the list. The title is always shown;
// Fields are extra values: section, due, assignee, project, tags, completed,
// or any custom field name. Separator frames each ticket with lines; neighbours share one.
type List struct {
	Layout    string   `toml:"layout"`
	Fields    []string `toml:"fields"`
	Separator bool     `toml:"separator"`
}

// Reader configures the reading pane. View is its starting view: cards
// (sections and boxed comments) or markdown (the rendered ticket Markdown).
type Reader struct {
	View string `toml:"view"`
}

// RepoSource configures where repo picker candidates come from.
type RepoSource struct {
	Command string `toml:"command"`
}

// Action is a user-defined command run against the selected ticket.
type Action struct {
	Name          string `toml:"name"`
	Key           string `toml:"key"`
	Mode          string `toml:"mode"`
	Repo          bool   `toml:"repo"`
	Agent         bool   `toml:"agent"`
	Command       string `toml:"command"`
	ConfirmWrites *bool  `toml:"confirm_writes"`
}

// Default returns the values used for keys the config file omits.
func Default() Config {
	return Config{
		Theme: "dark", Images: "auto", DefaultFilter: "is:open", ConfirmWrites: true,
		List:   List{Layout: LayoutSingle, Fields: []string{"section"}},
		Reader: Reader{View: ViewCards},
	}
}

// Load reads and validates the config file at path.
func Load(path string) (Config, error) {
	cfg := Default()
	md, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, ErrNotConfigured
	}
	if err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return cfg, fmt.Errorf("%s: unknown keys: %v", path, undecoded)
	}
	for i := range cfg.Actions {
		if cfg.Actions[i].Mode == "" {
			cfg.Actions[i].Mode = ModeForeground
		}
	}
	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.Workspace == "" {
		return errors.New("workspace is required; run `asanamate setup`")
	}
	if c.Theme != "dark" && c.Theme != "light" {
		return fmt.Errorf("theme must be \"dark\" or \"light\", got %q", c.Theme)
	}
	switch c.Images {
	case "auto", "kitty", "off":
	default:
		return fmt.Errorf("images must be \"auto\", \"kitty\", or \"off\", got %q", c.Images)
	}
	if c.List.Layout != LayoutSingle && c.List.Layout != LayoutMulti {
		return fmt.Errorf("list.layout must be %q or %q, got %q", LayoutSingle, LayoutMulti, c.List.Layout)
	}
	if c.Reader.View != ViewCards && c.Reader.View != ViewMarkdown {
		return fmt.Errorf("reader.view must be %q or %q, got %q", ViewCards, ViewMarkdown, c.Reader.View)
	}
	for _, f := range c.List.Fields {
		switch name := strings.TrimSpace(f); {
		case name == "":
			return errors.New("list.fields must not contain empty names")
		case strings.EqualFold(name, "title"):
			return errors.New("the title is always shown; remove it from list.fields")
		}
	}
	switch c.Symbols {
	case "", SymbolsUnicode, SymbolsNerd, SymbolsASCII:
	default:
		return fmt.Errorf("symbols must be %q, %q, or %q, got %q", SymbolsUnicode, SymbolsNerd, SymbolsASCII, c.Symbols)
	}
	if err := c.Agents.validate(); err != nil {
		return err
	}
	keys := map[string]string{}
	for _, a := range c.Actions {
		if a.Name == "" || a.Command == "" {
			return errors.New("every action needs a name and a command")
		}
		switch a.Mode {
		case ModeForeground, ModeBackground, ModeExit:
		default:
			return fmt.Errorf("action %q: mode must be foreground, background, or exit", a.Name)
		}
		if len([]rune(a.Key)) != 1 {
			return fmt.Errorf("action %q: key must be a single character", a.Name)
		}
		if other, ok := keys[a.Key]; ok {
			return fmt.Errorf("actions %q and %q share key %q", other, a.Name, a.Key)
		}
		keys[a.Key] = a.Name
	}
	return nil
}

var agentStates = map[string]bool{"working": true, "waiting": true, "completed": true, "idle": true}

func (a Agents) validate() error {
	switch {
	case a.Preset != "" && a.Preset != "ccmux":
		return fmt.Errorf("agents.preset must be \"ccmux\", got %q", a.Preset)
	case a.Preset != "" && strings.TrimSpace(a.Command) != "":
		return errors.New("set agents.preset or agents.command, not both")
	}
	for state := range a.States {
		if !agentStates[state] {
			return fmt.Errorf("agents.states: unknown state %q (use working, waiting, completed, idle)", state)
		}
	}
	for state := range a.Symbols {
		if !agentStates[state] && state != "unknown" {
			return fmt.Errorf("agents.symbols: unknown state %q (use working, waiting, completed, idle, unknown)", state)
		}
	}
	return nil
}

// ConfirmWritesFor reports whether Asana writes made by action a need confirmation.
func (c Config) ConfirmWritesFor(a Action) bool {
	if a.ConfirmWrites != nil {
		return *a.ConfirmWrites
	}
	return c.ConfirmWrites
}

// Dir returns the asanamate config directory.
func Dir() (string, error) { return xdgDir("XDG_CONFIG_HOME", ".config") }

// Path returns the config file path.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// StateDir returns the directory for files asanamate writes itself.
func StateDir() (string, error) { return xdgDir("XDG_STATE_HOME", filepath.Join(".local", "state")) }

func xdgDir(envKey, homeFallback string) (string, error) {
	base := os.Getenv(envKey)
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, homeFallback)
	}
	return filepath.Join(base, "asanamate"), nil
}

// Token returns the Asana personal access token from the environment.
func Token() (string, error) {
	token := strings.TrimSpace(os.Getenv(TokenEnv))
	if token == "" {
		return "", fmt.Errorf("%s is not set; create a personal access token at https://app.asana.com/0/my-apps and export it", TokenEnv)
	}
	return token, nil
}
