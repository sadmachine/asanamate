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
	"github.com/sadmachine/asanamate/internal/keymap"
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

// Config combines config.toml and settings.toml.
type Config struct {
	Workspace string `toml:"workspace"`
	// Theme selects the color theme; Colors overrides its roles.
	Theme         Theme      `toml:"theme"`
	Colors        Colors     `toml:"colors"`
	ConfirmWrites bool       `toml:"confirm_writes"`
	BranchField   string     `toml:"branch_field"`
	Symbols       string     `toml:"symbols"`
	ReducedMotion *bool      `toml:"reduced_motion"`
	RepoSource    RepoSource `toml:"repo_source"`
	Agents        Agents     `toml:"agents"`
	// Conversations is disabled until both source and foreground opener are set.
	Conversations Conversations `toml:"conversations"`
	// TimeTracking is disabled until both its provider ID and command are set.
	TimeTracking TimeTracking `toml:"time_tracking"`
	// ProjectLinks is disabled until its command is set.
	ProjectLinks ProjectLinks `toml:"project_links"`
	Layout       Layout       `toml:"layout"`
	List         List         `toml:"list"`
	Reader       Reader       `toml:"reader"`
	Images       Images       `toml:"images"`
	Picker       Picker       `toml:"picker"`
	Activity     Activity     `toml:"activity"`
	Feed         Feed         `toml:"feed"`
	// Keys overrides key bindings: scope, then binding name, then keys. See
	// internal/keymap for the scopes and names, including Activity controls.
	Keys map[string]map[string][]string `toml:"keys"`
	// Keymap is Keys applied to the default bindings, set by Load.
	Keymap keymap.Keymap `toml:"-"`
	// Actions come from the *.toml files in ActionsDir, not from config.toml.
	Actions []Action `toml:"-"`
	// Themes are the theme files Theme names, read from ThemesDir by Load.
	Themes map[string]ThemeFile `toml:"-"`
}

// TimeTracking configures an optional JSON command provider.
type TimeTracking struct {
	// ID separates saved project choices for different providers.
	ID string `toml:"id"`
	// Command reads a JSON request from stdin and returns a form spec as JSON.
	Command string `toml:"command"`
	// PromptAfter lists the TUI edits that open the log time form once they
	// succeed: PromptAssign (to someone else) and PromptComplete.
	PromptAfter []string `toml:"prompt_after"`
}

// Edits time_tracking.prompt_after accepts.
const (
	PromptAssign   = "assign"
	PromptComplete = "complete"
)

// PromptsAfter reports whether a successful edit opens the log time form.
func (t TimeTracking) PromptsAfter(edit string) bool { return slices.Contains(t.PromptAfter, edit) }

func (c Config) TimeTrackingEnabled() bool {
	return c.TimeTracking.ID != "" && c.TimeTracking.Command != ""
}

// ProjectLinks configures an optional JSON command provider that returns an
// Asana project's links in other systems. They preselect pickers: a linked
// git repository in the repo picker, a tracker project in the time form.
type ProjectLinks struct {
	// Command reads a JSON request from stdin and returns the links as JSON.
	Command string `toml:"command"`
}

// ProjectLinksEnabled reports whether a project links provider is configured.
func (c Config) ProjectLinksEnabled() bool {
	return c.ProjectLinks.Command != ""
}

// Agents links tickets to running coding agents. It is off unless Command is
// set. Command prints one line per agent (docs/extensions.md).
// States maps extra raw statuses onto asanamate's states; Symbols overrides
// the symbol shown per state.
type Agents struct {
	Command string `toml:"command"`
	// Focus is an optional noninteractive command that focuses the selected agent's target
	// without resuming a conversation or closing asanamate.
	Focus   string              `toml:"focus"`
	States  map[string][]string `toml:"states"`
	Symbols map[string]string   `toml:"symbols"`
}

// AgentsEnabled reports whether agent tracking is turned on.
func (c Config) AgentsEnabled() bool {
	return strings.TrimSpace(c.Agents.Command) != ""
}

// Conversations configures saved conversation discovery independently of agents.
type Conversations struct {
	// Command reads a versioned JSON task request and returns saved metadata.
	Command string `toml:"command"`
	// Open is foreground shell text that reads a private selection snapshot.
	Open string `toml:"open"`
}

// ConversationsEnabled reports whether both conversation commands are configured.
func (c Config) ConversationsEnabled() bool {
	return strings.TrimSpace(c.Conversations.Command) != "" && strings.TrimSpace(c.Conversations.Open) != ""
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

// Layout configures the panes. Reader starts with the reading pane shown;
// --hide-preview starts with it hidden and the toggle_reader key flips it for the
// session. ListWidth fixes the list pane's width in columns, borders
// included, and gives the reader the rest; 0 sizes the list automatically.
// Focus styles the focused pane against the rest.
type Layout struct {
	Reader    bool  `toml:"reader"`
	ListWidth int   `toml:"list_width"`
	Focus     Focus `toml:"focus"`
}

// Focus configures how the focused pane stands out. The focused pane's title
// is always a filled tab; Dim also draws every other pane faint. Resize gives
// the focused pane the room: the list fits its titles while it has focus, and
// shrinks to short titles while the reader has it.
type Focus struct {
	Dim    bool `toml:"dim"`
	Resize bool `toml:"resize"`
}

// List configures how tickets appear in the list. The title is always shown;
// Fields are extra values: section, due, assignee, initials, project, tags,
// completed, or any custom field name. Separator frames each ticket with lines; neighbours share one.
// Projects whose columns were set in the TUI keep their own.
type List struct {
	// RefreshInterval is the automatic list reload interval, at least one second.
	RefreshInterval string   `toml:"refresh_interval"`
	Layout          string   `toml:"layout"`
	Fields          []string `toml:"fields"`
	Separator       bool     `toml:"separator"`
	// DefaultView applies until a project has its own saved view.
	DefaultView DefaultView `toml:"default_view"`
	Header      Header      `toml:"header"`
	Selection   Selection   `toml:"selection"`
	New         NewTickets  `toml:"new"`
}

// DefaultView is the starting filter, grouping, and sort for a project.
type DefaultView struct {
	Filter  string `toml:"filter"`
	GroupBy string `toml:"group_by"`
	Sort    Sort   `toml:"sort"`
}

// Sort orders tickets within each group, or the whole ungrouped list.
// By is a list field or title; empty keeps Asana order. Direction is asc or desc.
type Sort struct {
	By        string `toml:"by"`
	Direction string `toml:"direction"`
}

// Header configures group headers. Style draws them as a reversed bar or a
// rule; Spacing adds a blank line above and below each.
type Header struct {
	Style   string `toml:"style"`
	Spacing bool   `toml:"spacing"`
}

// NewTickets configures My Tasks tickets changed since last viewed, which
// carry a NEW badge. Section lists them first, under a New header.
type NewTickets struct {
	Section bool `toml:"section"`
}

// Selection configures the selected ticket. Style marks it with a bold title
// and a left marker, or a reversed bar.
type Selection struct {
	Style string `toml:"style"`
}

// Reader configures the reading pane. View is its starting view: cards
// (sections and boxed comments) or markdown (the rendered ticket Markdown).
// MaxTextWidth caps the wrap width of the cards view's description and
// comment text, and of the comment editor; 0 wraps at the pane width. Comments shapes its comments.
type Reader struct {
	View         string   `toml:"view"`
	MaxTextWidth int      `toml:"max_text_width"`
	Comments     Comments `toml:"comments"`
}

// Comments configures the cards view's comments. Latest is how many of the
// newest comments, or activity entries in the All tab, it lists until Show
// all is opened; 0 lists them all.
// PreviewLines is how many lines of text each listed comment but the newest
// keeps until opened; 0 shows them in full.
type Comments struct {
	Latest       int `toml:"latest"`
	PreviewLines int `toml:"preview_lines"`
}

// Images configures kitty graphics. Mode is "auto" (detect kitty-protocol
// terminals), "kitty" (force on), or "off". Inline draws images in ticket
// descriptions and comments in place of their links, in the cards view.
type Images struct {
	Mode   string `toml:"mode"`
	Inline bool   `toml:"inline"`
}

// Picker configures list pickers. TypeFirst opens them in search mode
// instead of browse mode.
type Picker struct {
	TypeFirst bool `toml:"type_first"`
}

// Activity configures the Activity view. RefreshInterval is how often
// Activity scans run in the background: a duration of at least one second,
// or "0" for no background scans.
type Activity struct {
	RefreshInterval string `toml:"refresh_interval"`
}

// Interval is the background scan interval; 0 turns background scans off.
func (a Activity) Interval() (time.Duration, error) { return optionalInterval(a.RefreshInterval) }

// Feed configures the Feed panel under the ticket list. Expand is "auto"
// (expanded when the list leaves Height empty rows), "collapsed", or
// "expanded". Height is its rows when expanded, borders included.
// RefreshInterval is how often Feeds run: a duration of at least one second,
// or "0" to run them only at start and on reload. Feeds are commands that
// answer the feed protocol (docs/extensions.md).
type Feed struct {
	Expand          string   `toml:"expand"`
	Height          int      `toml:"height"`
	RefreshInterval string   `toml:"refresh_interval"`
	Commands        []string `toml:"commands"`
}

// Feed.Expand values.
const (
	ExpandAuto      = "auto"
	ExpandCollapsed = "collapsed"
	ExpandExpanded  = "expanded"
)

// Interval is the feed refresh interval; 0 runs feeds only at start and on
// reload.
func (f Feed) Interval() (time.Duration, error) { return optionalInterval(f.RefreshInterval) }

// optionalInterval parses an interval of at least one second, or "0" for off.
func optionalInterval(value string) (time.Duration, error) {
	if d, err := time.ParseDuration(value); err == nil && d == 0 {
		return 0, nil
	}
	d, err := ParseRefreshInterval(value)
	if err != nil {
		return 0, errors.New("must be 0 (off) or a duration of at least 1s, such as 2m")
	}
	return d, nil
}

// RepoSource configures where repo picker candidates come from.
type RepoSource struct {
	// Root is a directory whose direct children with a .git entry are listed,
	// without a shell. Absolute, or starting with "~". Empty lists nothing.
	Root string `toml:"root"`
	// Command prints one repo path per line; when set, it replaces Root.
	Command string `toml:"command"`
}

// ActionForm configures either static controls or a command that returns them.
// The zero value skips the form, as in Default.
type ActionForm struct {
	form.Spec
	// Command reads a ticket request as JSON and returns a fresh form.Spec.
	// Empty uses Fields; setting both command and fields is invalid.
	Command string `toml:"command,omitempty"`
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
	// Form optionally loads controls and asks for values before running.
	Form ActionForm `toml:"form"`
	// Input, when set, is the title of a text box shown before the action
	// runs; the typed text reaches the command as $ASANAMATE_INPUT_FILE.
	Input         string `toml:"input"`
	ConfirmWrites *bool  `toml:"confirm_writes"`
}

// Default returns the values used for keys the config file omits.
func Default() Config {
	return Config{
		Theme:         Theme{Name: ThemeAuto, Auto: AutoThemes{Dark: "ansi-dark", Light: "ansi-light"}},
		ConfirmWrites: true,
		Agents:        Agents{Focus: ""},
		Conversations: Conversations{},
		TimeTracking:  TimeTracking{},
		ProjectLinks:  ProjectLinks{},
		Actions:       nil,
		Layout:        Layout{Reader: true, Focus: Focus{Dim: true, Resize: true}},
		List: List{
			RefreshInterval: "30s",
			DefaultView:     DefaultView{Filter: "is:open", Sort: Sort{Direction: "asc"}},
			Layout:          LayoutSingle, Fields: []string{"section", "due"},
			Header: Header{Style: StyleRule}, Selection: Selection{Style: StyleMarker},
		},
		Reader:   Reader{View: ViewCards, Comments: Comments{Latest: 3, PreviewLines: 2}},
		Images:   Images{Mode: "auto"},
		Activity: Activity{RefreshInterval: "2m"},
		Feed:     Feed{Expand: ExpandAuto, Height: 12, RefreshInterval: "1m", Commands: []string{}},
		Keymap:   keymap.Default(),
	}
}

// Load reads and validates the config file at path.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, ErrNotConfigured
	}
	if err != nil {
		return cfg, err
	}
	settings, err := os.ReadFile(SettingsPath(path))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	return LoadData(path, data, settings)
}

// LoadData validates candidate file contents before setup changes either file.
// Themes and actions resolve relative to path, as in Load.
func LoadData(path string, data, settings []byte) (Config, error) {
	cfg := Default()
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if changes := PendingUpdates(raw); len(changes) > 0 {
		return cfg, &UpdateNeeded{Path: path, Changes: changes}
	}
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return cfg, fmt.Errorf("%s: unknown keys: %v", path, keys)
	}
	values, err := decodeSettings(SettingsPath(path), settings)
	if err != nil {
		return cfg, err
	}
	if _, err := toml.Decode(string(settings), &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", SettingsPath(path), err)
	}
	if err := cfg.validate(); err != nil {
		source := path
		for _, key := range SettingKeys {
			if _, ok := getPath(values, key); ok && strings.HasPrefix(err.Error(), key) {
				source = SettingsPath(path)
				break
			}
		}
		return cfg, fmt.Errorf("%s: %w", source, err)
	}
	cfg.Keys = compactKeys(cfg.Keys)
	if cfg.Keymap, err = keymap.Resolve(cfg.Keys); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.Themes, err = loadThemes(ThemesDir(path), cfg.Theme); err != nil {
		return cfg, fmt.Errorf("%s: %w", SettingsPath(path), err)
	}
	cfg.Actions, err = loadActions(ActionsDir(path))
	return cfg, err
}

// compactKeys drops scopes that override nothing, such as the template's
// empty [keys.main] tables, and returns nil when none are left.
func compactKeys(keys map[string]map[string][]string) map[string]map[string][]string {
	for scope, names := range keys {
		if len(names) == 0 {
			delete(keys, scope)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
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
	if a.Form.Command != "" {
		if strings.TrimSpace(a.Form.Command) == "" {
			return errors.New("form.command must not be blank")
		}
		if len(a.Form.Fields) > 0 {
			return errors.New("form.command and form.fields cannot both be set")
		}
	} else if len(a.Form.Fields) > 0 {
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
	for _, edit := range c.TimeTracking.PromptAfter {
		if edit != PromptAssign && edit != PromptComplete {
			return fmt.Errorf("time_tracking.prompt_after: %q must be %q or %q", edit, PromptAssign, PromptComplete)
		}
	}
	if c.ProjectLinks.Command != "" && strings.TrimSpace(c.ProjectLinks.Command) == "" {
		return errors.New("project_links.command must not be blank; remove it to turn project links off")
	}
	for _, e := range []error{
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
	if r := c.RepoSource.Root; r != "" && r != "~" && !strings.HasPrefix(r, "~/") && !filepath.IsAbs(r) {
		return fmt.Errorf("repo_source.root must be an absolute path or start with ~, got %q", r)
	}
	if c.Layout.ListWidth < 0 {
		return fmt.Errorf("layout.list_width must be 0 (automatic) or more, got %d", c.Layout.ListWidth)
	}
	if c.Reader.MaxTextWidth < 0 {
		return fmt.Errorf("reader.max_text_width must be 0 (no limit) or more, got %d", c.Reader.MaxTextWidth)
	}
	if c.Reader.Comments.Latest < 0 {
		return fmt.Errorf("reader.comments.latest must be 0 (all) or more, got %d", c.Reader.Comments.Latest)
	}
	if c.Reader.Comments.PreviewLines < 0 {
		return fmt.Errorf("reader.comments.preview_lines must be 0 (full comments) or more, got %d", c.Reader.Comments.PreviewLines)
	}
	if _, err := ParseRefreshInterval(c.List.RefreshInterval); err != nil {
		return fmt.Errorf("list.refresh_interval: %w", err)
	}
	if _, err := c.Activity.Interval(); err != nil {
		return fmt.Errorf("activity.refresh_interval: %w", err)
	}
	if err := oneOf("feed.expand", c.Feed.Expand, ExpandAuto, ExpandCollapsed, ExpandExpanded); err != nil {
		return err
	}
	if c.Feed.Height < 3 {
		return fmt.Errorf("feed.height must be at least 3, got %d", c.Feed.Height)
	}
	if _, err := c.Feed.Interval(); err != nil {
		return fmt.Errorf("feed.refresh_interval: %w", err)
	}
	for _, f := range c.Feed.Commands {
		if strings.TrimSpace(f) == "" {
			return errors.New("feed.commands must not hold a blank command")
		}
	}
	for _, f := range c.List.Fields {
		switch name := strings.TrimSpace(f); {
		case name == "":
			return errors.New("list.fields must not contain empty names")
		case strings.EqualFold(name, "title"):
			return errors.New("the title is always shown; remove it from list.fields")
		}
	}
	if strings.EqualFold(strings.TrimSpace(c.List.DefaultView.GroupBy), "title") {
		return errors.New("list.default_view.group_by can't be the title; use a field such as section or due")
	}
	if strings.ContainsFunc(c.List.DefaultView.Sort.By, unicode.IsControl) {
		return errors.New("list.default_view.sort.by must not contain control characters")
	}
	if err := oneOf("list.default_view.sort.direction", c.List.DefaultView.Sort.Direction, "asc", "desc"); err != nil {
		return err
	}
	if err := c.Colors.validate("colors"); err != nil {
		return err
	}
	if strings.TrimSpace(c.Conversations.Command) != "" && strings.TrimSpace(c.Conversations.Open) == "" {
		return errors.New("conversations.open is required when conversations.command is set")
	}
	if strings.TrimSpace(c.Conversations.Open) != "" && strings.TrimSpace(c.Conversations.Command) == "" {
		return errors.New("conversations.command is required when conversations.open is set")
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
	if strings.TrimSpace(a.Focus) != "" && strings.TrimSpace(a.Command) == "" {
		return errors.New("agents.command is required when agents.focus is set")
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

// CacheDir returns the directory for data asanamate can fetch again.
func CacheDir() (string, error) { return xdgDir("XDG_CACHE_HOME", ".cache") }

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
