package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/heywoodlh-bot/tui-search/internal/config"
)

func doc(t *testing.T, path string) *goquery.Document {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestParseDDG(t *testing.T) {
	rs := parseDDGHTML(doc(t, "testdata/ddg.html"))
	if len(rs) != 2 {
		t.Fatalf("want 2 results (ad skipped), got %d: %+v", len(rs), rs)
	}
	if rs[0].Snippet != "A powerful little TUI framework." {
		t.Errorf("snippet = %q", rs[0].Snippet)
	}
	if rs[1].URL != "https://pkg.go.dev/github.com/charmbracelet/bubbletea" {
		t.Errorf("redirect not unwrapped: %q", rs[1].URL)
	}
}

func TestParseBing(t *testing.T) {
	rs := parseBing(doc(t, "testdata/bing.html"))
	if len(rs) != 2 {
		t.Fatalf("want 2, got %d", len(rs))
	}
	if rs[0].URL != "https://go.dev/" {
		t.Errorf("ck/a link not decoded: %q", rs[0].URL)
	}
	if strings.Contains(rs[0].Snippet, "\u00a0") {
		t.Errorf("nbsp not normalised: %q", rs[0].Snippet)
	}
}

func TestRegistryAndCustomJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("q") != "hello world" {
			t.Errorf("query = %q", r.URL.Query().Get("q"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"items":[{"name":"One","link":"https://one.example","desc":"<b>first</b>"},{"name":"Two","link":"https://two.example"}]}}`))
	}))
	defer srv.Close()
	t.Setenv("MYAPI_TOKEN", "secret")
	cfg, err := config.Parse(`
[engines.myapi]
type = "json"
url = "` + srv.URL + `/search?q={query}"
token_env = "MYAPI_TOKEN"
results_path = "data.items"
title_field = "name"
url_field = "link"
snippet_field = "desc"
bangs = ["my"]

[engines.arch]
type = "url"
url = "https://wiki.archlinux.org/index.php?search={query}"

[engines.work]
type = "gitlab"
base_url = "https://gitlab.example.com"

[engines.bing]
disabled = true
`)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("bing"); ok {
		t.Error("disabled engine registered")
	}
	e, ok := reg.Get("!my")
	if !ok || e.Name() != "myapi" {
		t.Fatalf("bang lookup failed: %v %v", e, ok)
	}
	rs, err := e.Search(context.Background(), Query{Text: "hello world"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].Title != "One" || rs[0].Snippet != "first" || rs[1].URL != "https://two.example" {
		t.Errorf("unexpected results: %+v", rs)
	}
	arch, _ := reg.Get("arch")
	if got := arch.SearchURL(Query{Text: "a b"}); got != "https://wiki.archlinux.org/index.php?search=a+b" {
		t.Errorf("url engine = %q", got)
	}
	if !reg.Info("arch").BrowserOnly {
		t.Error("url engine should be browser-only")
	}
	work, ok := reg.Get("work")
	if !ok || work.Name() != "work" || !work.Supports(KindCode) {
		t.Errorf("custom gitlab instance: %v", work)
	}
	if !strings.HasPrefix(work.SearchURL(Query{Text: "x", Kind: KindCode}), "https://gitlab.example.com/search") {
		t.Error("custom gitlab base_url ignored")
	}
}

func TestResolveKindFallback(t *testing.T) {
	cfg := config.Default()
	cfg.DefaultCodeEngine = "sourcegraph"
	reg, err := NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e, k, err := reg.Resolve("", KindCode)
	if err != nil || e.Name() != "sourcegraph" || k != KindCode {
		t.Errorf("default code engine: %v %v %v", e, k, err)
	}
	e, k, _ = reg.Resolve("github", KindWeb)
	if e.Name() != "github" || k != KindRepos {
		t.Errorf("github web should fall back to repos, got %v", k)
	}
	if _, _, err := reg.Resolve("nope", KindWeb); err == nil {
		t.Error("unknown engine should error")
	}
}

func TestJSONPath(t *testing.T) {
	data := map[string]any{"a": []any{map[string]any{"b": "x"}}}
	if got := jsonPath(data, "a.0.b"); got != "x" {
		t.Errorf("got %v", got)
	}
	if jsonPath(data, "a.5.b") != nil {
		t.Error("out of range should be nil")
	}
}

func TestFocusAndMerge(t *testing.T) {
	f := Fragment{Line: 10, Text: "\t}\n\n\t// make it\n\tp := tea.NewProgram(m)\n\tp.Run()"}
	got := Focus(f, "tea.NewProgram lang:go", 0)
	if !strings.HasPrefix(got.Text, "// make it\np := tea.NewProgram") || got.Line != 12 {
		t.Errorf("focus = %q line %d", got.Text, got.Line)
	}
	m := mergeFragments([]Fragment{{Line: 1, Text: "a"}, {Line: 2, Text: "b"}, {Line: 5, Text: "c"}})
	if len(m) != 2 || m[0].Text != "a\nb" {
		t.Errorf("merge = %+v", m)
	}
}

func TestGitHubAuthError(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("PATH", "")
	cfg := config.Default()
	reg, _ := NewRegistry(cfg)
	gh, _ := reg.Get("github")
	_, err := gh.Search(context.Background(), Query{Text: "x", Kind: KindCode})
	if _, ok := err.(*AuthError); !ok {
		t.Errorf("want AuthError, got %v", err)
	}
}
