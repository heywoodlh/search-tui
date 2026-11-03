package app

import (
	"strings"
	"testing"

	"github.com/heywoodlh-bot/tui-search/internal/config"
	"github.com/heywoodlh-bot/tui-search/internal/engine"
)

func newApp(t *testing.T, toml string) *App {
	t.Helper()
	cfg, err := config.Parse(toml)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPlan(t *testing.T) {
	a := newApp(t, `default_code_engine = "sourcegraph"`)
	p, err := a.Plan("hello !w", PlanOptions{})
	if err != nil || p.Engine.Name() != "wikipedia" || p.Query.Text != "hello" {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	p, _ = a.Plan("!yt lofi beats", PlanOptions{})
	if p.OpenURL != "https://www.youtube.com/results?search_query=lofi+beats" {
		t.Errorf("url bang = %q", p.OpenURL)
	}
	p, _ = a.Plan("NewProgram", PlanOptions{Kind: engine.KindCode})
	if p.Engine.Name() != "sourcegraph" || p.Query.Kind != engine.KindCode {
		t.Errorf("code plan = %s %s", p.Engine.Name(), p.Query.Kind)
	}
	p, _ = a.Plan("x !ghc", PlanOptions{Engine: "bing"})
	if p.Engine.Name() != "github" || p.Query.Kind != engine.KindCode {
		t.Errorf("bang should override flags: %s %s", p.Engine.Name(), p.Query.Kind)
	}
	p, _ = a.Plan("rust !nosuchbang", PlanOptions{})
	if p.Engine.Name() != "duckduckgo" || p.Query.Text != "rust !nosuchbang" {
		t.Errorf("unknown bang should stay in query: %+v", p.Query)
	}
}

func TestBadDefaultEngine(t *testing.T) {
	cfg, _ := config.Parse(`default_engine = "nope"`)
	if _, err := New(cfg); err == nil {
		t.Error("expected error")
	}
}

func TestFindLineAndMarkdown(t *testing.T) {
	if got := FindLine("a\nb\n  foo := bar()\n", "\n  foo := bar()\n"); got != 3 {
		t.Errorf("FindLine = %d", got)
	}
	rs := []engine.Result{
		{Title: "A [x]", URL: "https://a", Snippet: "snip"},
		{Title: "code", URL: "https://c", Code: &engine.CodeMatch{Language: "go", Fragments: []engine.Fragment{{Line: 4, Text: "x := 1"}}}},
	}
	md := ResultsMarkdown("q", "ddg", engine.KindWeb, 1, rs, Summary{})
	for _, want := range []string{"# Search: q", `[A \[x\]](https://a)`, "```go\nx := 1\n```", "Line 4"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
	if SnippetText(rs[1]) != "x := 1" {
		t.Error("SnippetText should return code")
	}
}

func TestUnconfiguredDefaultAndBang(t *testing.T) {
	a := newApp(t, `default_engine = "kagi"`)
	if a.Cfg.DefaultEngine != "duckduckgo" || len(a.Warnings) != 1 {
		t.Errorf("expected fallback with warning: %q %v", a.Cfg.DefaultEngine, a.Warnings)
	}
	p, _ := a.Plan("!g golang", PlanOptions{})
	if p.OpenURL != "https://www.google.com/search?q=golang" {
		t.Errorf("unconfigured !g should open the browser, got %+v", p)
	}
}
