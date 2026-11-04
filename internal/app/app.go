// Package app ties configuration, engines, bangs and summaries together for
// both the inline CLI and the TUI.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/heywoodlh/search-tui/internal/bang"
	"github.com/heywoodlh/search-tui/internal/config"
	"github.com/heywoodlh/search-tui/internal/engine"
	"github.com/heywoodlh/search-tui/internal/safe"
	"github.com/heywoodlh/search-tui/internal/summary"
	"github.com/heywoodlh/search-tui/internal/theme"
)

// App is the shared application core.
type App struct {
	Cfg   *config.Config
	Reg   *engine.Registry
	Bangs *bang.Resolver
	// Warnings are non-fatal configuration problems to show the user.
	Warnings []string
	// Theme is the resolved colour theme.
	Theme theme.Theme

	mu    sync.Mutex
	pages map[string]Page
}

// New builds the application from configuration.
func New(cfg *config.Config) (*App, error) {
	reg, err := engine.NewRegistry(cfg)
	if err != nil {
		return nil, err
	}
	var warnings []string
	if _, ok := reg.Get(cfg.DefaultEngine); !ok {
		u, hidden := reg.UnconfiguredByName(cfg.DefaultEngine)
		if !hidden {
			return nil, fmt.Errorf("default_engine %q is not a known engine", cfg.DefaultEngine)
		}
		warnings = append(warnings, fmt.Sprintf("default_engine %s is not configured (set %s); using duckduckgo", u.Name, u.Missing))
		cfg.DefaultEngine = "duckduckgo"
	}
	th, err := theme.Resolve(cfg.UI.Theme, ThemeOverrides(cfg.UI))
	if err != nil {
		warnings = append(warnings, err.Error())
	}
	return &App{Cfg: cfg, Reg: reg, Bangs: bang.NewResolver(reg, cfg.Bangs), Warnings: warnings, Theme: th, pages: map[string]Page{}}, nil
}

// Reload rebuilds the engine registry and bang resolver after credentials
// changed (the TUI's :login command), so newly configured engines appear
// without restarting.
func (a *App) Reload() error {
	for _, e := range a.Cfg.Engines {
		e.ClearSecrets()
	}
	reg, err := engine.NewRegistry(a.Cfg)
	if err != nil {
		return err
	}
	a.Reg = reg
	a.Bangs = bang.NewResolver(reg, a.Cfg.Bangs)
	return nil
}

// Plan is a resolved search request.
type Plan struct {
	Query  engine.Query
	Engine engine.Engine
	// OpenURL is set when the request should open a browser (URL bangs and
	// browser-only engines).
	OpenURL string
	Lucky   bool
	Bang    string
	// Notice explains a fallback to another engine, if one happened.
	Notice string
}

// PlanOptions are explicit user selections that bangs may override.
type PlanOptions struct {
	Engine string
	Kind   engine.Kind
	Page   int
	Limit  int
	Lang   string
	Repo   string
}

// Plan resolves bangs, engine and kind for text.
func (a *App) Plan(text string, o PlanOptions) (Plan, error) {
	q, b := bang.Extract(text)
	p := Plan{Bang: b}
	engineName, kind := o.Engine, o.Kind
	if kind == "" {
		k, err := engine.ParseKind(a.Cfg.DefaultType)
		if err != nil {
			return p, err
		}
		kind = k
	}
	if b != "" {
		t, ok := a.Bangs.Resolve(b)
		if !ok {
			// Unknown bang: keep it in the query like DuckDuckGo does.
			q = strings.TrimSpace(text)
		} else {
			switch {
			case t.Lucky:
				p.Lucky = true
			case t.URL != "":
				p.OpenURL = fillURL(t.URL, q)
			default:
				if t.Engine != "" {
					engineName = t.Engine
				}
				if t.Kind != "" {
					kind = t.Kind
				}
			}
		}
	}
	limit := o.Limit
	if limit <= 0 {
		limit = a.Cfg.Results
	}
	p.Query = engine.Query{Text: q, Kind: kind, Page: max(o.Page, 1), Limit: limit, Lang: o.Lang, Repo: o.Repo}
	if p.OpenURL != "" {
		return p, nil
	}
	e, k, err := a.Reg.Resolve(engineName, kind)
	if err != nil {
		return p, err
	}
	p.Engine, p.Query.Kind = e, k
	if a.Reg.Info(e.Name()).BrowserOnly {
		p.OpenURL = e.SearchURL(p.Query)
	}
	return p, nil
}

func fillURL(tmpl, q string) string {
	return strings.NewReplacer("{query}", url.QueryEscape(q), "{query_path}", url.PathEscape(q), "{query_raw}", q).Replace(tmpl)
}

// Search runs a plan. If a web search fails for a reason other than missing
// credentials, the configured fallback_engine is tried and p is updated.
func (a *App) Search(ctx context.Context, p *Plan) ([]engine.Result, error) {
	if p.Engine == nil {
		return nil, engine.ErrBrowserOnly
	}
	if strings.TrimSpace(p.Query.Text) == "" {
		return nil, fmt.Errorf("empty query")
	}
	rs, err := p.Engine.Search(ctx, p.Query)
	if err == nil || p.Query.Kind != engine.KindWeb || ctx.Err() != nil {
		return sanitized(rs), err
	}
	var ae *engine.AuthError
	if errors.As(err, &ae) {
		return rs, err
	}
	fb, ok := a.Reg.Get(a.Cfg.FallbackEngine)
	if !ok || fb.Name() == p.Engine.Name() || !fb.Supports(engine.KindWeb) {
		return rs, err
	}
	rs2, err2 := fb.Search(ctx, p.Query)
	if err2 != nil {
		return rs, err
	}
	p.Notice = fmt.Sprintf("%s failed (%v); showing %s results", p.Engine.Name(), err, fb.Name())
	p.Engine = fb
	return sanitized(rs2), nil
}

// sanitized cleans attacker-controlled text in results (see package safe).
func sanitized(rs []engine.Result) []engine.Result {
	for i := range rs {
		rs[i].Sanitize()
	}
	return rs
}

// Summary is an aggregate summary of a result set.
type Summary struct {
	Sentences []summary.Sentence
	Terms     []string
	// Text is set when an external summariser produced free text.
	Text string
}

// Empty reports whether there is nothing to show.
func (s Summary) Empty() bool { return len(s.Sentences) == 0 && s.Text == "" }

// Summarize computes the on-device summary of results.
func (a *App) Summarize(query string, rs []engine.Result) Summary {
	docs := ResultDocs(rs)
	return Summary{
		Sentences: summary.Summarize(docs, query, a.Cfg.Summary.Sentences),
		Terms:     summary.KeyTerms(docs, query, 6),
	}
}

// SummarizeExternal runs the configured external summariser, if any.
func (a *App) SummarizeExternal(ctx context.Context, query string, rs []engine.Result) (string, error) {
	if a.Cfg.Summary.Command == "" {
		return "", nil
	}
	d, err := time.ParseDuration(a.Cfg.Summary.CommandTimeout)
	if err != nil {
		d = time.Minute
	}
	out, err := summary.External(ctx, a.Cfg.Summary.Command, summary.Prompt(query, ResultDocs(rs)), d)
	return safe.Text(out), err
}

// ResultDocs converts results to summariser documents.
func ResultDocs(rs []engine.Result) []summary.Doc {
	docs := make([]summary.Doc, 0, len(rs))
	for i, r := range rs {
		t := r.Snippet
		if r.Code != nil {
			continue // code fragments make poor prose summaries
		}
		if t == "" {
			continue
		}
		docs = append(docs, summary.Doc{Text: t, Source: i + 1})
	}
	return docs
}

// Page is a fetched and extracted web page.
type Page struct {
	URL      string
	Title    string
	Markdown string
	Summary  []summary.Sentence
}

// FetchPage downloads url and extracts readable Markdown plus a summary.
func (a *App) FetchPage(ctx context.Context, u, query string) (Page, error) {
	a.mu.Lock()
	if p, ok := a.pages[u]; ok {
		a.mu.Unlock()
		return p, nil
	}
	a.mu.Unlock()
	body, hdr, err := a.Reg.Client().Do(ctx, engine.Request{URL: u, Headers: map[string]string{"Accept": "text/html,text/plain;q=0.9,*/*;q=0.5"}})
	if err != nil {
		return Page{}, err
	}
	p := Page{URL: u}
	ct := hdr.Get("Content-Type")
	if strings.Contains(ct, "html") || ct == "" {
		p.Title, p.Markdown = summary.ExtractPage(string(body), u)
	} else {
		p.Markdown = string(body)
	}
	p.Title, p.Markdown = safe.Line(p.Title), safe.Text(p.Markdown)
	p.Summary = summary.SummarizeText(summary.StripLinks(p.Markdown), query, max(a.Cfg.Summary.Sentences, 3))
	a.mu.Lock()
	if len(a.pages) > 32 { // keep memory bounded on small devices
		a.pages = map[string]Page{}
	}
	a.pages[u] = p
	a.mu.Unlock()
	return p, nil
}

// Code is a fetched source file for a code result.
type Code struct {
	Name    string // file name (for syntax highlighting)
	Content string
	Line    int // 1-based line of the first match (0 if unknown)
}

// FetchCode downloads the full file for a code result and locates the match.
func (a *App) FetchCode(ctx context.Context, r engine.Result) (Code, error) {
	if r.Code == nil {
		return Code{}, fmt.Errorf("not a code result")
	}
	content, err := a.Reg.Client().FetchRaw(ctx, r.Code)
	if err != nil {
		return Code{}, err
	}
	c := Code{Name: path.Base(r.Code.Path), Content: safe.Text(content), Line: r.Code.Line}
	if c.Line == 0 && len(r.Code.Fragments) > 0 {
		c.Line = FindLine(content, r.Code.Fragments[0].Text)
	}
	return c, nil
}

// FindLine returns the 1-based line where fragment's first non-empty line
// occurs in content, or 0.
func FindLine(content, fragment string) int {
	needle := ""
	for _, l := range strings.Split(fragment, "\n") {
		if t := strings.TrimSpace(l); len(t) > 3 {
			needle = t
			break
		}
	}
	if needle == "" {
		return 0
	}
	for i, l := range strings.Split(content, "\n") {
		if strings.Contains(l, needle) {
			return i + 1
		}
	}
	return 0
}

// SnippetText returns the most useful text to copy for a result: code
// fragments for code results, otherwise the snippet.
func SnippetText(r engine.Result) string {
	if r.Code != nil && len(r.Code.Fragments) > 0 {
		parts := make([]string, len(r.Code.Fragments))
		for i, f := range r.Code.Fragments {
			parts[i] = f.Text
		}
		return strings.Join(parts, "\n\n")
	}
	return r.Snippet
}

// ThemeOverrides merges [ui.colors] with the accent_color/muted_color
// shorthands.
func ThemeOverrides(ui config.UI) map[string]string {
	o := map[string]string{}
	if ui.AccentColor != "" {
		o["accent"] = ui.AccentColor
	}
	if ui.MutedColor != "" {
		o["muted"] = ui.MutedColor
		o["border"] = ui.MutedColor
	}
	for k, v := range ui.Colors {
		o[k] = v
	}
	return o
}
