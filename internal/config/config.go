// Package config loads the user-edited asanamate configuration file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"

	"github.com/sadmachine/asanamate/internal/agents"
	"github.com/sadmachine/asanamate/internal/form"
)

// TokenEnv names the environment variable that holds the Asana personal access token.
const TokenEnv = "ASANA_ACCESS_TOKEN"

// Action run modes.
const (
	ModeForeground = "foreground"
	ModeBackground = "background"
	ModeExit       = "exit"
)

// ContextComment limits an action to a highlighted comment.
const ContextComment = "comment"

// List layouts.
const (
	LayoutSingle = "single"
	LayoutMulti  = "multi"
)

// List header and selection styles.
const (
	StyleBar    = "bar"
	StyleRule   = "rule"
	StyleMarker = "marker"
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
	AccentColor   string     `toml:"accent_color"`
	DefaultFilter string     `toml:"default_filter"`
	ConfirmWrites bool       `toml:"confirm_writes"`
	BranchField   string     `toml:"branch_field"`
	Symbols       string     `toml:"symbols"`
	ReducedMotion *bool      `toml:"reduced_motion"`
	RepoSource    RepoSource `toml:"repo_source"`
	Agents        Agents     `toml:"agents"`
	// TimeTracking is disabled until both its provider ID and command are set.
	TimeTracking TimeTracking `toml:"time_tracking"`
	List         List         `toml:"list"`
	Reader       Reader       `toml:"reader"`
	Images       Images       `toml:"images"`
	// Actions come from the *.toml files in ActionsDir, not from config.toml.
	Actions []Action `toml:"-"`
}

// TimeTracking configures an optional JSON command provider.
type TimeTracking struct {
	// ID separates saved project choices for different providers.
	ID string `toml:"id"`
	// Command reads a JSON request from stdin and returns a form spec as JSON.
	Command string `toml:"command"`
}

func (c Config) TimeTrackingEnabled() bool {
	return c.TimeTracking.ID != "" && c.TimeTracking.Command != ""
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
// Fields are extra values: section, due, assignee, initials, project, tags,
// completed, or any custom field name. Separator frames each ticket with lines; neighbours share one.
// GroupBy groups tickets under a header per value of one such field; "" is ungrouped.
type List struct {
	// RefreshInterval is the automatic list reload interval, at least one second.
	RefreshInterval string    `toml:"refresh_interval"`
	Layout          string    `toml:"layout"`
	Fields          []string  `toml:"fields"`
	Separator       bool      `toml:"separator"`
	GroupBy         string    `toml:"group_by"`
	Sort            Sort      `toml:"sort"`
	Header          Header    `toml:"header"`
	Pinned          Pinned    `toml:"pinned"`
	Selection       Selection `toml:"selection"`
}

// Sort orders tickets within each group, or the whole ungrouped list.
// By is a list field or title; empty keeps Asana order. Direction is asc or desc.
type Sort struct {
	By        string `toml:"by"`
	Direction string `toml:"direction"`
}

// Header configures group headers. Style draws them as a reversed bar or a
// rule; Spacing adds a blank line above and below each. Color overrides the
// accent color when set.
type Header struct {
	Style   string `toml:"style"`
	Spacing bool   `toml:"spacing"`
	Color   string `toml:"color"`
}

// Pinned configures the section of explicitly pinned tickets at the top of
// the list. Color overrides the accent color for its header when set.
type Pinned struct {
	Color string `toml:"color"`
}

// Selection configures the selected ticket. Style marks it with a bold title
// and a left marker, or a reversed bar. Color overrides the accent color for
// the marker when set.
type Selection struct {
	Style string `toml:"style"`
	Color string `toml:"color"`
}

// Reader configures the reading pane. View is its starting view: cards
// (sections and boxed comments) or markdown (the rendered ticket Markdown).
// MaxTextWidth caps the wrap width of the cards view's description and
// comment text; 0 wraps at the pane width.
type Reader struct {
	View         string `toml:"view"`
	MaxTextWidth int    `toml:"max_text_width"`
}

// Images configures kitty graphics. Mode is "auto" (detect kitty-protocol
// terminals), "kitty" (force on), or "off". Inline draws images in ticket
// descriptions and comments in place of their links, in the cards view.
type Images struct {
	Mode   string `toml:"mode"`
	Inline bool   `toml:"inline"`
}

// RepoSource configures where repo picker candidates come from.
type RepoSource struct {
	Command string `toml:"command"`
}

// Action is a user-defined command run against the selected ticket. Each
// action is its own file in ActionsDir, with these keys at the top level.
type Action struct {
	Name  string `toml:"name"`
	Key   string `toml:"key"`
	Mode  string `toml:"mode"`
	Repo  bool   `toml:"repo"`
	Agent bool   `toml:"agent"`
	// Context limits the action to a highlighted item, such as ContextComment,
	// where it is listed first; "" shows it everywhere.
	Context string `toml:"context"`
	Command string `toml:"command"`
	// Form optionally asks for select or hours values before running the command.
	Form form.Spec `toml:"form"`
	// Input, when set, is the title of a text box shown before the action
	// runs; the typed text reaches the command as $ASANAMATE_INPUT_FILE.
	Input         string `toml:"input"`
	ConfirmWrites *bool  `toml:"confirm_writes"`
}

// Default returns the values used for keys the config file omits.
func Default() Config {
	return Config{
		Theme: "dark", AccentColor: "4", DefaultFilter: "is:open", ConfirmWrites: true,
		TimeTracking: TimeTracking{},
		Actions:      nil,
		List: List{
			RefreshInterval: "30s",
			Sort:            Sort{By: "", Direction: "asc"},
			Layout:          LayoutSingle, Fields: []string{"section", "due"},
			Header: Header{Style: StyleRule}, Pinned: Pinned{Color: "208"}, Selection: Selection{Style: StyleMarker},
		},
		Reader: Reader{View: ViewCards},
		Images: Images{Mode: "auto"},
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
	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	cfg.Actions, err = loadActions(ActionsDir(path))
	return cfg, err
}

// loadActions reads every *.toml file in dir, in file name order. A missing
// dir means no actions.
func loadActions(dir string) ([]Action, error) {
	documents, err := LoadActionFiles(dir)
	if err != nil {
		return nil, err
	}
	var actions []Action
	for _, document := range documents {
		actions = append(actions, document.Action)
	}
	return actions, nil
}

func (a Action) validate() error {
	if a.Name == "" || a.Command == "" {
		return errors.New("name and command are required")
	}
	if err := oneOf("mode", a.Mode, ModeForeground, ModeBackground, ModeExit); err != nil {
		return err
	}
	if err := oneOf("context", a.Context, "", ContextComment); err != nil {
		return err
	}
	if len([]rune(a.Key)) != 1 {
		return errors.New("key must be a single character")
	}
	if len(a.Form.Fields) > 0 {
		if err := a.Form.Validate(); err != nil {
			return fmt.Errorf("action %q form: %w", a.Name, err)
		}
	}
	return nil
}

func (c Config) validate() error {
	if c.Workspace == "" {
		return errors.New("workspace is required; run `asanamate setup`")
	}
	if (c.TimeTracking.ID == "") != (c.TimeTracking.Command == "") || (c.TimeTracking.ID != "" && strings.TrimSpace(c.TimeTracking.Command) == "") {
		return errors.New("time_tracking.id and time_tracking.command must both be set")
	}
	if strings.TrimSpace(c.TimeTracking.ID) != c.TimeTracking.ID {
		return errors.New("time_tracking.id must not have surrounding whitespace")
	}
	for _, e := range []error{
		oneOf("theme", c.Theme, "dark", "light"),
		oneOf("images.mode", c.Images.Mode, "auto", "kitty", "off"),
		oneOf("list.layout", c.List.Layout, LayoutSingle, LayoutMulti),
		oneOf("reader.view", c.Reader.View, ViewCards, ViewMarkdown),
		oneOf("list.header.style", c.List.Header.Style, StyleBar, StyleRule),
		oneOf("list.selection.style", c.List.Selection.Style, StyleMarker, StyleBar),
	} {
		if e != nil {
			return e
		}
	}
	if c.Symbols != "" {
		if err := oneOf("symbols", c.Symbols, SymbolsUnicode, SymbolsNerd, SymbolsASCII); err != nil {
			return err
		}
	}
	if c.Reader.MaxTextWidth < 0 {
		return fmt.Errorf("reader.max_text_width must be 0 (no limit) or more, got %d", c.Reader.MaxTextWidth)
	}
	if _, err := ParseRefreshInterval(c.List.RefreshInterval); err != nil {
		return fmt.Errorf("list.refresh_interval: %w", err)
	}
	for _, f := range c.List.Fields {
		switch name := strings.TrimSpace(f); {
		case name == "":
			return errors.New("list.fields must not contain empty names")
		case strings.EqualFold(name, "title"):
			return errors.New("the title is always shown; remove it from list.fields")
		}
	}
	for key, color := range map[string]string{"accent_color": c.AccentColor, "list.header.color": c.List.Header.Color, "list.pinned.color": c.List.Pinned.Color, "list.selection.color": c.List.Selection.Color} {
		if (key == "accent_color" || color != "") && !validColor(color) {
			return fmt.Errorf("%s must be an ANSI color number (0-255) or #rrggbb, got %q", key, color)
		}
	}
	if strings.EqualFold(strings.TrimSpace(c.List.GroupBy), "title") {
		return errors.New("list.group_by can't be the title; use a field such as section or due")
	}
	if strings.ContainsFunc(c.List.Sort.By, unicode.IsControl) {
		return errors.New("list.sort.by must not contain control characters")
	}
	if err := oneOf("list.sort.direction", c.List.Sort.Direction, "asc", "desc"); err != nil {
		return err
	}
	return c.Agents.validate()
}

// ParseRefreshInterval validates a duration used by config and session overrides.
func ParseRefreshInterval(value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil || d < time.Second {
		return 0, errors.New("must be a duration of at least 1s, such as 15s or 1m")
	}
	return d, nil
}

// validColor reports whether s is an ANSI color number or a #rgb/#rrggbb hex color.
func validColor(s string) bool {
	if n, err := strconv.Atoi(s); err == nil {
		return n >= 0 && n <= 255
	}
	hex, ok := strings.CutPrefix(s, "#")
	if !ok || (len(hex) != 3 && len(hex) != 6) {
		return false
	}
	_, err := strconv.ParseUint(hex, 16, 32)
	return err == nil
}

// oneOf errors unless got is one of allowed, naming the dotted key.
func oneOf(key, got string, allowed ...string) error {
	if slices.Contains(allowed, got) {
		return nil
	}
	quoted := make([]string, len(allowed))
	for i, a := range allowed {
		quoted[i] = strconv.Quote(a)
	}
	list := quoted[0]
	switch n := len(quoted); {
	case n == 2:
		list = quoted[0] + " or " + quoted[1]
	case n > 2:
		list = strings.Join(quoted[:n-1], ", ") + ", or " + quoted[n-1]
	}
	return fmt.Errorf("%s must be %s, got %q", key, list, got)
}

func (a Agents) validate() error {
	if a.Preset != "" {
		if err := oneOf("agents.preset", a.Preset, agents.Presets()...); err != nil {
			return err
		}
		if strings.TrimSpace(a.Command) != "" {
			return errors.New("set agents.preset or agents.command, not both")
		}
	}
	// agents.states maps onto the known states; agents.symbols also covers unknown.
	var known []string
	for _, s := range agents.States {
		known = append(known, string(s))
	}
	mappable := slices.DeleteFunc(slices.Clone(known), func(s string) bool { return s == string(agents.Unknown) })
	for state := range a.States {
		if !slices.Contains(mappable, state) {
			return fmt.Errorf("agents.states: unknown state %q (use %s)", state, strings.Join(mappable, ", "))
		}
	}
	for state := range a.Symbols {
		if !slices.Contains(known, state) {
			return fmt.Errorf("agents.symbols: unknown state %q (use %s)", state, strings.Join(known, ", "))
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

// ActionsDir returns the directory of action files next to the config file.
func ActionsDir(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "actions")
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
