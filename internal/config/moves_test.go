package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func decode(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if _, err := toml.Decode(body, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestApplyMovesMovesEveryReleasedKey(t *testing.T) {
	m := decode(t, `workspace = "1"
theme = "light"
accent_color = "2"
[list]
layout = "multi"
[list.header]
style = "bar"
color = "3"
[list.pinned]
color = ""
[list.viewing]
color = "5"
[list.selection]
color = "6"
`)
	if err := ApplyMoves(m); err != nil {
		t.Fatal(err)
	}
	want := decode(t, `workspace = "1"
[theme]
name = "light"
[colors]
accent = "2"
header = "3"
pinned = "2"
viewing = "5"
selection = "6"
[list]
layout = "multi"
[list.header]
style = "bar"
`)
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got  %v\nwant %v", m, want)
	}
}

func TestApplyMovesLeavesNewShapeAlone(t *testing.T) {
	m := decode(t, "[theme]\nname = \"dark\"\n[colors]\naccent = \"4\"\n")
	want := decode(t, "[theme]\nname = \"dark\"\n[colors]\naccent = \"4\"\n")
	if err := ApplyMoves(m); err != nil || !reflect.DeepEqual(m, want) {
		t.Fatalf("m = %v, err = %v", m, err)
	}
}

func TestApplyMovesRejectsOldAndNew(t *testing.T) {
	err := ApplyMoves(decode(t, "accent_color = \"2\"\n[colors]\naccent = \"3\"\n"))
	if err == nil || !strings.Contains(err.Error(), "accent_color and colors.accent are both set") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadNamesMovedKeys(t *testing.T) {
	for body, key := range map[string]string{
		"accent_color = \"2\"\n":         "accent_color moved to colors.accent",
		"theme = \"dark\"\n":             "theme moved to theme.name",
		"[list.pinned]\ncolor = \"1\"\n": "list.pinned.color moved to colors.pinned",
	} {
		_, err := Load(writeFile(t, "workspace = \"1\"\n"+body))
		if err == nil || !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "asanamate config update") {
			t.Errorf("%q: err = %v", body, err)
		}
	}
}
