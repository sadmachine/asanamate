package config

import (
	"cmp"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"

	"github.com/BurntSushi/toml"
)

// ThemeAuto picks Theme.Dark or Theme.Light from the terminal background.
const ThemeAuto = "auto"

// Theme selects the color theme: a built-in ("dark", "light"), a file in
// ThemesDir, or ThemeAuto. Dark and Light are the themes ThemeAuto uses.
type Theme struct {
	Name  string `toml:"name"`
	Dark  string `toml:"dark"`
	Light string `toml:"light"`
}

// Colors holds a style per themed role. Theme files and config.toml share it;
// in config.toml every role overrides the active theme.
type Colors struct {
	Accent    Style          `toml:"accent"`
	Border    Style          `toml:"border"`
	Muted     Style          `toml:"muted"`
	Highlight Style          `toml:"highlight"`
	OK        Style          `toml:"ok"`
	Warn      Style          `toml:"warn"`
	Error     Style          `toml:"error"`
	Working   Style          `toml:"working"`
	Header    Style          `toml:"header"`
	Selection Style          `toml:"selection"`
	Pinned    Style          `toml:"pinned"`
	Viewing   Style          `toml:"viewing"`
	Authors   []Style        `toml:"authors"`
	Mode      ModeColors     `toml:"mode"`
	Markdown  MarkdownColors `toml:"markdown"`
}

// ModeColors styles the statusline mode pills. A pill without a background
// is drawn reversed, so its foreground fills it.
type ModeColors struct {
	Normal Style `toml:"normal"`
	Filter Style `toml:"filter"`
	Read   Style `toml:"read"`
	Edit   Style `toml:"edit"`
}

// MarkdownColors override parts of the reading pane's glamour style.
type MarkdownColors struct {
	Text      Style `toml:"text"`
	Heading   Style `toml:"heading"`
	H1        Style `toml:"h1"`
	Link      Style `toml:"link"`
	Code      Style `toml:"code"`
	CodeBlock Style `toml:"code_block"`
	Quote     Style `toml:"quote"`
	Rule      Style `toml:"rule"`
}

// ThemeFile is a theme: the built-in it starts from and its colors. Built-in
// themes have no Base in their file; their name is their base.
type ThemeFile struct {
	Base   string `toml:"base"`
	Colors Colors `toml:"colors"`
}

// Palette is the active theme with config.toml's overrides and the fallback
// roles applied.
type Palette struct {
	// Base is "dark" or "light": the glamour style Markdown starts from.
	Base   string
	Colors Colors
}

//go:embed themes/*.toml
var builtinThemeFiles embed.FS

// builtinThemes are the embedded themes, by name.
var builtinThemes = mustBuiltinThemes()

var themeName = regexp.MustCompile(`^[a-z0-9_-]+$`)

func mustBuiltinThemes() map[string]ThemeFile {
	themes := map[string]ThemeFile{}
	for _, name := range []string{"dark", "light"} {
		path := "themes/" + name + ".toml"
		data, err := builtinThemeFiles.ReadFile(path)
		if err != nil {
			panic(err)
		}
		t, err := parseTheme(path, data)
		if err != nil {
			panic(err)
		}
		t.Base = name
		themes[name] = t
	}
	return themes
}

// parseTheme decodes and validates a theme file's contents.
func parseTheme(path string, data []byte) (ThemeFile, error) {
	var t ThemeFile
	md, err := toml.Decode(string(data), &t)
	if err != nil {
		return t, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return t, fmt.Errorf("%s: unknown keys: %v", path, undecoded)
	}
	if err := t.Colors.validate("colors"); err != nil {
		return t, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

// ThemesDir returns the directory of theme files next to the config file.
func ThemesDir(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "themes")
}

// loadThemes reads every theme file t names from dir, layered over its base.
// It also refuses files that would shadow a built-in theme.
func loadThemes(dir string, t Theme) (map[string]ThemeFile, error) {
	for _, reserved := range []string{"dark", "light", ThemeAuto} {
		path := filepath.Join(dir, reserved+".toml")
		if _, err := os.Stat(path); err == nil {
			return nil, fmt.Errorf("%s: %q is a built-in theme; rename the file", path, reserved)
		}
	}
	// nil when only built-ins are used, so Load's result equals Default's.
	var themes map[string]ThemeFile
	for _, ref := range []struct{ key, name string }{{"theme.name", t.Name}, {"theme.dark", t.Dark}, {"theme.light", t.Light}} {
		switch _, builtin := builtinThemes[ref.name]; {
		case builtin, ref.name == ThemeAuto && ref.key == "theme.name":
			continue
		case ref.name == ThemeAuto:
			return nil, fmt.Errorf("%s can't be %q", ref.key, ThemeAuto)
		case !themeName.MatchString(ref.name):
			return nil, fmt.Errorf("%s must be a theme name of lowercase letters, digits, - and _, got %q", ref.key, ref.name)
		}
		if _, ok := themes[ref.name]; ok {
			continue
		}
		path := filepath.Join(dir, ref.name+".toml")
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: no theme %q in %s", ref.key, ref.name, dir)
		}
		if err != nil {
			return nil, err
		}
		file, err := parseTheme(path, data)
		if err != nil {
			return nil, err
		}
		if err := oneOf("base", file.Base, "dark", "light"); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		file.Colors = file.Colors.Over(builtinThemes[file.Base].Colors)
		if themes == nil {
			themes = map[string]ThemeFile{}
		}
		themes[ref.name] = file
	}
	return themes, nil
}

// Palette resolves the active theme. darkBackground picks Theme.Dark or
// Theme.Light when the theme is ThemeAuto. Names Load has not read fall back
// to the built-in dark theme, so a zero Config still has colors.
func (c Config) Palette(darkBackground bool) Palette {
	name := cmp.Or(c.Theme.Name, ThemeAuto)
	if name == ThemeAuto {
		name = cmp.Or(c.Theme.Light, "light")
		if darkBackground {
			name = cmp.Or(c.Theme.Dark, "dark")
		}
	}
	t, ok := c.Themes[name]
	if !ok {
		t, ok = builtinThemes[name]
	}
	if !ok {
		t = builtinThemes["dark"]
	}
	colors := c.Colors.Over(t.Colors)
	colors.Header = colors.Header.Over(colors.Accent)
	colors.Selection = colors.Selection.Over(colors.Accent)
	colors.Mode.Normal = colors.Mode.Normal.Over(colors.Accent)
	colors.Mode.Filter = colors.Mode.Filter.Over(colors.Warn)
	return Palette{Base: t.Base, Colors: colors}
}

// Over returns c with every unset role, or unset field of a role, taken from
// base. A set Authors list replaces base's whole list.
func (c Colors) Over(base Colors) Colors {
	overFields(reflect.ValueOf(&c).Elem(), reflect.ValueOf(base))
	return c
}

func overFields(dst, base reflect.Value) {
	for i := range dst.NumField() {
		switch field := dst.Field(i).Addr().Interface().(type) {
		case *Style:
			*field = field.Over(base.Field(i).Interface().(Style))
		case *[]Style:
			if *field == nil {
				*field = base.Field(i).Interface().([]Style)
			}
		default:
			overFields(dst.Field(i), base.Field(i))
		}
	}
}

// validate checks every role's colors, naming the dotted key under prefix.
func (c Colors) validate(prefix string) error {
	return validateFields(reflect.ValueOf(c), prefix)
}

func validateFields(v reflect.Value, prefix string) error {
	for i := range v.NumField() {
		key := prefix + "." + v.Type().Field(i).Tag.Get("toml")
		switch field := v.Field(i).Interface().(type) {
		case Style:
			if err := field.validate(key); err != nil {
				return err
			}
		case []Style:
			for j, s := range field {
				if err := s.validate(fmt.Sprintf("%s[%d]", key, j)); err != nil {
					return err
				}
			}
		default:
			if err := validateFields(v.Field(i), key); err != nil {
				return err
			}
		}
	}
	return nil
}
