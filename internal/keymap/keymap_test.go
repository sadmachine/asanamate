package keymap

import (
	"slices"
	"strings"
	"testing"
)

func TestDefaultResolves(t *testing.T) {
	k := Default()
	if got := k.Keys("main", "quit"); !slices.Equal(got, []string{"q"}) {
		t.Fatalf("main.quit = %q", got)
	}
	if k.Name("main", "j") != "down" || k.Name("picker", "ctrl+n") != "down" || k.Name("main", "nope") != "" {
		t.Fatal("Name lookups")
	}
}

func TestCatalogIsComplete(t *testing.T) {
	for _, s := range Catalog() {
		if s.Doc == "" {
			t.Errorf("scope %s has no doc", s.Name)
		}
		for _, b := range s.Bindings {
			if b.Desc == "" {
				t.Errorf("%s.%s has no desc", s.Name, b.Name)
			}
			if s.Name == "main" && b.Pair == "" && !slices.Contains([]string{"Move", "Ticket", "View"}, b.Group) {
				t.Errorf("main.%s has group %q", b.Name, b.Group)
			}
		}
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"j": "j", "G": "G", "?": "?", "+": "+", "ctrl++": "ctrl++",
		"shift+ctrl+x": "ctrl+shift+x", "shift+g": "G", "ctrl+_": "ctrl+/",
		"enter": "enter", "f12": "f12", "alt+pgdown": "alt+pgdown",
		"shift+Q": "Q", "ctrl+shift+q": "ctrl+shift+q",
	} {
		if got, err := Normalize(in); err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for in, want := range map[string]string{
		"":            "empty",
		" ":           `use "space"`,
		"g g":         `key sequences such as "g g" are not supported yet`,
		"ctrl+foo":    `unknown key "foo"`,
		"hyperx+a":    `unknown modifier "hyperx"`,
		"ctrl+ctrl+a": `repeats "ctrl"`,
		"ctrl+Q":      `use "ctrl+shift+q"`,
		"alt+shift+A": `use "alt+shift+a"`,
	} {
		if _, err := Normalize(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Normalize(%q) err = %v, want %q", in, err, want)
		}
	}
}

func TestPrintable(t *testing.T) {
	for key, want := range map[string]bool{"j": true, "?": true, "space": true, "esc": false, "ctrl+s": false, "tab": false} {
		if Printable(key) != want {
			t.Errorf("Printable(%q) = %v", key, !want)
		}
	}
}

func TestResolveOverridesAndUnbinds(t *testing.T) {
	k, err := Resolve(map[string]map[string][]string{"main": {"quit": {"x", "ctrl+q"}, "help": {}}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(k.Keys("main", "quit"), []string{"x", "ctrl+q"}) || len(k.Keys("main", "help")) != 0 || k.Name("main", "q") != "" {
		t.Fatalf("k.main = %v", k["main"])
	}
}

func TestResolveErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		o    map[string]map[string][]string
		want string
	}{
		"unknown scope":   {map[string]map[string][]string{"lsit": {"down": {"j"}}}, "keys.lsit: unknown scope"},
		"unknown binding": {map[string]map[string][]string{"main": {"dwon": {"j"}}}, "keys.main.dwon: unknown binding"},
		"bad key":         {map[string]map[string][]string{"main": {"quit": {"ctrl+qq"}}}, "keys.main.quit"},
		"conflict":        {map[string]map[string][]string{"main": {"quit": {"j"}}}, `keys.main: "j" is bound to both down and quit`},
		"chord":           {map[string]map[string][]string{"main": {"top": {"g g"}}}, "not supported yet"},
	} {
		if _, err := Resolve(tc.o); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestResolveRejectsPrintableOnlyTypingBindings(t *testing.T) {
	_, err := Resolve(map[string]map[string][]string{"picker": {"toggle": {"space"}}})
	if err == nil || !strings.Contains(err.Error(), "keys.picker.toggle") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Resolve(map[string]map[string][]string{"picker": {"toggle": {}}}); err != nil {
		t.Fatalf("unbinding a typing binding must be allowed: %v", err)
	}
}

func TestCtrlSlashAlias(t *testing.T) {
	k := Default()
	if k.Name("filter", "ctrl+_") != "help" || k.Name("filter", "ctrl+/") != "help" {
		t.Fatal("ctrl+_ and ctrl+/ must both open filter help")
	}
}

func TestLabel(t *testing.T) {
	if got := Label("down", "up"); got != "↓/↑" {
		t.Fatalf("Label = %q", got)
	}
	if got := Label("space", "a"); got != "space/a" {
		t.Fatalf("Label = %q", got)
	}
}

func TestResolveRejectsCtrlC(t *testing.T) {
	_, err := Resolve(map[string]map[string][]string{"picker": {"cancel": {"esc", "ctrl+c"}}})
	if err == nil || !strings.Contains(err.Error(), "keys.picker.cancel: ctrl+c always quits") {
		t.Fatalf("err = %v", err)
	}
}
