package bang

import (
	"testing"

	"github.com/heywoodlh/tui-search/internal/config"
	"github.com/heywoodlh/tui-search/internal/engine"
)

func TestExtract(t *testing.T) {
	cases := []struct{ in, q, b string }{
		{"!g hello world", "hello world", "g"},
		{"hello !W world", "hello world", "w"},
		{"hello world", "hello world", ""},
		{"! first result", "first result", "!"},
		{"what is 3!", "what is 3!", ""},
		{"wow !!", "wow !!", ""},
	}
	for _, c := range cases {
		q, b := Extract(c.in)
		if q != c.q || b != c.b {
			t.Errorf("Extract(%q) = %q, %q; want %q, %q", c.in, q, b, c.q, c.b)
		}
	}
}

func TestResolve(t *testing.T) {
	cfg := config.Default()
	reg, err := engine.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(reg, map[string]string{"nw": "https://wiki.nixos.org/?search={query}", "work": "gitlab:code", "g": "bing"})
	check := func(b string, want Target) {
		t.Helper()
		got, ok := r.Resolve(b)
		if !ok || got.Engine != want.Engine || got.Kind != want.Kind || got.URL != want.URL || got.Lucky != want.Lucky {
			t.Errorf("Resolve(%q) = %+v, %v; want %+v", b, got, ok, want)
		}
	}
	check("ddg", Target{Engine: "duckduckgo"})
	check("ghc", Target{Engine: "github", Kind: engine.KindCode})
	check("yt", Target{URL: URLBangs["yt"]})
	check("nw", Target{URL: "https://wiki.nixos.org/?search={query}"})
	check("work", Target{Engine: "gitlab", Kind: engine.KindCode})
	check("g", Target{Engine: "bing"}) // user override wins
	check("!", Target{Lucky: true})
	if _, ok := r.Resolve("definitely-not-a-bang"); ok {
		t.Error("unknown bang resolved")
	}
	if c := r.Complete("!gh"); len(c) < 3 {
		t.Errorf("completion = %v", c)
	}
}
