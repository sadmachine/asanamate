package config

import (
	"cmp"
	"fmt"
	"strings"
)

// Style is how a themed role draws: colors plus text attributes. Unset fields
// (an empty color, a nil attribute) inherit from the layer below.
type Style struct {
	FG, BG    string
	Bold      *bool
	Italic    *bool
	Underline *bool
	Faint     *bool
	Reverse   *bool
}

// styleFields are the keys a style table accepts.
var styleFields = []string{"fg", "bg", "bold", "italic", "underline", "faint", "reverse"}

// UnmarshalTOML accepts a color string, which sets FG only, or a style table.
func (s *Style) UnmarshalTOML(v any) error {
	switch v := v.(type) {
	case string:
		*s = Style{FG: v}
		return nil
	case map[string]any:
		var out Style
		for field, value := range v {
			if err := out.set(field, value); err != nil {
				return err
			}
		}
		*s = out
		return nil
	}
	return fmt.Errorf("must be a color or a style table, got %v", v)
}

func (s *Style) set(field string, v any) error {
	switch field {
	case "fg", "bg":
		c, ok := v.(string)
		if !ok {
			return fmt.Errorf("style %s must be a color string, got %v", field, v)
		}
		if field == "fg" {
			s.FG = c
		} else {
			s.BG = c
		}
		return nil
	}
	flag := map[string]**bool{"bold": &s.Bold, "italic": &s.Italic, "underline": &s.Underline, "faint": &s.Faint, "reverse": &s.Reverse}[field]
	if flag == nil {
		return fmt.Errorf("unknown style field %q (use %s)", field, strings.Join(styleFields, ", "))
	}
	b, ok := v.(bool)
	if !ok {
		return fmt.Errorf("style %s must be true or false, got %v", field, v)
	}
	*flag = &b
	return nil
}

// Over returns s with every unset field taken from base.
func (s Style) Over(base Style) Style {
	s.FG, s.BG = cmp.Or(s.FG, base.FG), cmp.Or(s.BG, base.BG)
	s.Bold, s.Italic = cmp.Or(s.Bold, base.Bold), cmp.Or(s.Italic, base.Italic)
	s.Underline, s.Faint = cmp.Or(s.Underline, base.Underline), cmp.Or(s.Faint, base.Faint)
	s.Reverse = cmp.Or(s.Reverse, base.Reverse)
	return s
}

// validate errors on a color that is not an ANSI number or hex, naming key.
func (s Style) validate(key string) error {
	for _, c := range []string{s.FG, s.BG} {
		if c != "" && !validColor(c) {
			return fmt.Errorf("%s must be an ANSI color number (0-255) or #rrggbb, got %q", key, c)
		}
	}
	return nil
}
