package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTheme writes body to themes/<name>.toml next to the config at path.
func writeTheme(t *testing.T, path, name, body string) {
	t.Helper()
	dir := ThemesDir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBuiltinThemesLoad(t *testing.T) {
	for _, name := range []string{"dark", "light"} {
		th, ok := builtinThemes[name]
		if !ok || th.Base != name || th.Colors.Accent.FG != "4" || len(th.Colors.Authors) != 6 {
			t.Errorf("%s = %+v", name, th)
		}
	}
	if builtinThemes["light"].Colors.Working.FG != "6" || builtinThemes["dark"].Colors.Working.FG != "14" {
		t.Error("light theme must use a darker working color")
	}
}

func TestPaletteLayersAndFallbacks(t *testing.T) {
	path := writeFile(t, "workspace = \"1\"\n[theme]\nname = \"night\"\n[colors]\naccent = \"#7aa2f7\"\n[colors.mode]\nread = { bg = \"0\" }\n")
	writeTheme(t, path, "night", "base = \"dark\"\n[colors]\nborder = \"#3b4261\"\npinned = { fg = \"#ff9e64\" }\n[colors.markdown]\nlink = { underline = true }\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Palette(true)
	c := p.Colors
	switch {
	case p.Base != "dark":
		t.Errorf("base = %q", p.Base)
	case c.Accent.FG != "#7aa2f7" || c.Accent.Bold == nil || !*c.Accent.Bold:
		t.Errorf("accent = %+v, want config fg over the built-in bold", c.Accent)
	case c.Border.FG != "#3b4261":
		t.Errorf("border = %+v, want the theme file's", c.Border)
	case c.Pinned.FG != "#ff9e64" || !*c.Pinned.Bold:
		t.Errorf("pinned = %+v, want theme fg over built-in bold", c.Pinned)
	case c.Header.FG != "#7aa2f7" || c.Selection.FG != "#7aa2f7" || c.Mode.Normal.FG != "#7aa2f7":
		t.Errorf("header, selection, and mode.normal must fall back to accent: %+v %+v %+v", c.Header, c.Selection, c.Mode.Normal)
	case c.Mode.Filter.FG != "3":
		t.Errorf("mode.filter = %+v, want warn", c.Mode.Filter)
	case c.Mode.Read.FG != "6" || c.Mode.Read.BG != "0":
		t.Errorf("mode.read = %+v", c.Mode.Read)
	case c.Markdown.Link.Underline == nil || !*c.Markdown.Link.Underline:
		t.Errorf("markdown.link = %+v", c.Markdown.Link)
	}
}

func TestPaletteAutoPicksByBackground(t *testing.T) {
	cfg := Config{Theme: Theme{Name: ThemeAuto, Dark: "dark", Light: "light"}}
	if cfg.Palette(true).Base != "dark" || cfg.Palette(false).Base != "light" {
		t.Fatal("auto must follow the background")
	}
	if (Config{}).Palette(false).Base != "light" {
		t.Fatal("a zero config must behave like the defaults")
	}
}

func TestThemeFileErrors(t *testing.T) {
	for name, tc := range map[string]struct{ config, theme, want string }{
		"missing file": {"[theme]\nname = \"nope\"\n", "", `theme.name: no theme "nope"`},
		"bad name":     {"[theme]\nname = \"Night Owl\"\n", "", "theme.name must be"},
		"auto as dark": {"[theme]\ndark = \"auto\"\n", "", `theme.dark can't be "auto"`},
		"bad base":     {"[theme]\nname = \"x\"\n", "base = \"blue\"\n", "base must be"},
		"missing base": {"[theme]\nname = \"x\"\n", "[colors]\naccent = \"1\"\n", "base must be"},
		"unknown key":  {"[theme]\nname = \"x\"\n", "base = \"dark\"\n[colors]\naccnt = \"1\"\n", "unknown keys"},
		"bad color":    {"[theme]\nname = \"x\"\n", "base = \"dark\"\n[colors.mode]\nread = \"blue\"\n", "colors.mode.read"},
		"bad author":   {"[theme]\nname = \"x\"\n", "base = \"dark\"\n[colors]\nauthors = [\"1\", \"x\"]\n", "colors.authors[1]"},
	} {
		t.Run(name, func(t *testing.T) {
			path := writeFile(t, "workspace = \"1\"\n"+tc.config)
			if tc.theme != "" {
				writeTheme(t, path, "x", tc.theme)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestThemeFileCannotShadowBuiltin(t *testing.T) {
	path := writeFile(t, "workspace = \"1\"\n")
	writeTheme(t, path, "dark", "base = \"dark\"\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), `"dark" is a built-in theme; rename the file`) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadValidatesBothAutoThemes(t *testing.T) {
	path := writeFile(t, "workspace = \"1\"\n[theme]\nname = \"auto\"\nlight = \"day\"\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), `theme.light: no theme "day"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigColorsAreValidated(t *testing.T) {
	_, err := Load(writeFile(t, "workspace = \"1\"\n[colors]\nheader = { fg = \"256\" }\n"))
	if err == nil || !strings.Contains(err.Error(), "colors.header") {
		t.Fatalf("err = %v", err)
	}
}
