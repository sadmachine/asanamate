package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestStyleDecodesStringOrTable(t *testing.T) {
	var got struct {
		A Style   `toml:"a"`
		B Style   `toml:"b"`
		L []Style `toml:"l"`
	}
	md, err := toml.Decode("a = \"4\"\nb = { fg = \"#fff\", bg = \"1\", bold = true, faint = false }\nl = [\"2\", { reverse = true }]\n", &got)
	if err != nil || len(md.Undecoded()) > 0 {
		t.Fatalf("err = %v, undecoded = %v", err, md.Undecoded())
	}
	if got.A != (Style{FG: "4"}) {
		t.Errorf("a = %+v", got.A)
	}
	if got.B.FG != "#fff" || got.B.BG != "1" || !*got.B.Bold || *got.B.Faint || got.B.Italic != nil {
		t.Errorf("b = %+v", got.B)
	}
	if len(got.L) != 2 || got.L[0].FG != "2" || !*got.L[1].Reverse {
		t.Errorf("l = %+v", got.L)
	}
}

func TestStyleRejectsBadTables(t *testing.T) {
	for body, want := range map[string]string{
		"a = { fgg = \"1\" }":    `unknown style field "fgg"`,
		"a = { bold = \"yes\" }": "bold must be true or false",
		"a = { fg = 4 }":         "fg must be a color string",
		"a = 4":                  "must be a color or a style table",
	} {
		var got struct {
			A Style `toml:"a"`
		}
		if _, err := toml.Decode(body, &got); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", body, err, want)
		}
	}
}

func TestStyleOverInheritsUnsetFields(t *testing.T) {
	yes, no := true, false
	base := Style{FG: "4", BG: "0", Bold: &yes, Italic: &yes}
	got := Style{FG: "#fff", Italic: &no}.Over(base)
	if got.FG != "#fff" || got.BG != "0" || got.Bold != &yes || *got.Italic {
		t.Fatalf("got %+v", got)
	}
}

func TestStyleValidateNamesKey(t *testing.T) {
	if err := (Style{FG: "4", BG: "#abc"}).validate("colors.accent"); err != nil {
		t.Fatal(err)
	}
	err := Style{BG: "blue"}.validate("colors.accent")
	if err == nil || !strings.Contains(err.Error(), "colors.accent") {
		t.Fatalf("err = %v", err)
	}
}
