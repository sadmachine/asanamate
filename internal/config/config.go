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

// ErrNotConfigured means the config file does not exist yet.
var ErrNotConfigured = errors.New("asanamate is not configured; run `asanamate setup`")

// Config is the contents of config.toml.
type Config struct {
	Workspace     string     `toml:"workspace"`
	Theme         string     `toml:"theme"`
	Images        string     `toml:"images"`
	DefaultFilter string     `toml:"default_filter"`
	ConfirmWrites bool       `toml:"confirm_writes"`
	RepoSource    RepoSource `toml:"repo_source"`
	List          List       `toml:"list"`
	Actions       []Action   `toml:"actions"`
}

// List configures how tickets appear in the list. The title is always shown;
// Fields are extra values: section, due, assignee, project, tags, completed,
// or any custom field name.
type List struct {
	Layout string   `toml:"layout"`
	Fields []string `toml:"fields"`
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
	Command       string `toml:"command"`
	ConfirmWrites *bool  `toml:"confirm_writes"`
}

// Default returns the values used for keys the config file omits.
func Default() Config {
	return Config{
		Theme: "dark", Images: "auto", DefaultFilter: "is:open", ConfirmWrites: true,
		List: List{Layout: LayoutSingle, Fields: []string{"section"}},
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
	for _, f := range c.List.Fields {
		switch name := strings.TrimSpace(f); {
		case name == "":
			return errors.New("list.fields must not contain empty names")
		case strings.EqualFold(name, "title"):
			return errors.New("the title is always shown; remove it from list.fields")
		}
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
