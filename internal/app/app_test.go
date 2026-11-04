package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/heywoodlh/search-tui/internal/config"
	"github.com/heywoodlh/search-tui/internal/engine"
	"github.com/heywoodlh/search-tui/internal/sys"
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

func TestMaliciousEngineIsSanitized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"title":"pwn\u001b]52;c;ZWNobyBwd25lZA==\u0007ed","url":"javascript:alert(1)","desc":"clear\u001b[2J\u001b[Hscreen \u202eevil"}]}`))
	}))
	defer srv.Close()
	a := newApp(t, `
[engines.evil]
type = "json"
url = "`+srv.URL+`/?q={query}"
results_path = "items"
snippet_field = "desc"
`)
	p, err := a.Plan("x", PlanOptions{Engine: "evil"})
	if err != nil {
		t.Fatal(err)
	}
	rs, err := a.Search(context.Background(), &p)
	if err != nil || len(rs) != 1 {
		t.Fatalf("search: %v %d", err, len(rs))
	}
	r := rs[0]
	for _, s := range []string{r.Title, r.URL, r.Snippet, ResultsMarkdown("x", "evil", engine.KindWeb, 1, rs, a.Summarize("x", rs))} {
		if strings.ContainsAny(s, "\x1b\x07\u202e") {
			t.Errorf("escape sequence reached output: %q", s)
		}
	}
	if r.Title != "pwn]52;c;ZWNobyBwd25lZA==ed" {
		t.Errorf("title = %q", r.Title)
	}
	if sys.OpenURL("true", r.URL) == nil {
		t.Error("javascript: URL from a result must not be opened")
	}
}
