package theme

import (
	"strings"
	"testing"
)

func TestThemes(t *testing.T) {
	names := Names()
	for _, want := range []string{"default", "ansi", "catppuccin-mocha", "catppuccin-latte", "dracula", "nord", "gruvbox-dark", "solarized-light", "tokyo-night", "one-dark", "rose-pine", "kanagawa"} {
		found := false
		for _, n := range names {
			found = found || n == want
		}
		if !found {
			t.Errorf("missing theme %s", want)
		}
	}
	for _, n := range names {
		th, _ := Get(n)
		if n != "mono" && th.Accent == nil {
			t.Errorf("%s has no accent", n)
		}
	}
	if th, ok := Get("Catppuccin_Mocha"); !ok || th.Name != "catppuccin-mocha" {
		t.Error("lookup should be case/separator-insensitive")
	}
	if th, _ := Get("gruvbox"); th.Name != "gruvbox-dark" {
		t.Error("alias gruvbox")
	}
}

func TestResolve(t *testing.T) {
	th, err := Resolve("nord", map[string]string{"accent": "#ff0000", "title": ""})
	if err != nil || th.Accent != c("#ff0000") || th.Title != nil || th.URL == nil {
		t.Errorf("overrides: %+v %v", th, err)
	}
	th, err = Resolve("nope", nil)
	if err == nil || th.Name != "default" || !strings.Contains(err.Error(), "dracula") {
		t.Errorf("unknown theme should fall back with a helpful error: %v", err)
	}
	if _, err := Resolve("nord", map[string]string{"bogus": "1"}); err == nil {
		t.Error("unknown colour key should error")
	}
}
