// Package engine implements search backends.
package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/heywoodlh-bot/tui-search/internal/config"
)

// Kind is the type of search to perform.
type Kind string

const (
	KindWeb    Kind = "web"
	KindCode   Kind = "code"
	KindRepos  Kind = "repos"
	KindIssues Kind = "issues"
)

// ParseKind validates a kind string.
func ParseKind(s string) (Kind, error) {
	switch k := Kind(strings.ToLower(strings.TrimSpace(s))); k {
	case KindWeb, KindCode, KindRepos, KindIssues:
		return k, nil
	case "":
		return KindWeb, nil
	case "repo", "repositories":
		return KindRepos, nil
	case "issue", "prs", "pr":
		return KindIssues, nil
	}
	return "", fmt.Errorf("unknown search type %q (want web, code, repos or issues)", s)
}

// Kinds lists all kinds in display order.
var Kinds = []Kind{KindWeb, KindCode, KindRepos, KindIssues}

// Query describes a search request.
type Query struct {
	Text  string
	Kind  Kind
	Page  int // 1-based
	Limit int
	// Lang and Repo are code-search filters; engines translate them to their
	// own qualifier syntax.
	Lang string
	Repo string
}

// Result is a single search hit.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
	Engine  string `json:"engine"`
	// Meta is a short secondary line (stars, score, date...).
	Meta string     `json:"meta,omitempty"`
	Code *CodeMatch `json:"code,omitempty"`
}

// CodeMatch carries code-search details for a result.
type CodeMatch struct {
	Repo      string     `json:"repo"`
	Path      string     `json:"path"`
	Ref       string     `json:"ref,omitempty"`
	Language  string     `json:"language,omitempty"`
	Line      int        `json:"line,omitempty"` // 1-based first matching line
	Fragments []Fragment `json:"fragments,omitempty"`
	// RawURL fetches the full file. RawHeaders are sent with it.
	RawURL     string            `json:"raw_url,omitempty"`
	RawHeaders map[string]string `json:"-"`
}

// Fragment is a snippet of matching code.
type Fragment struct {
	Line int    `json:"line,omitempty"` // 1-based, 0 if unknown
	Text string `json:"text"`
}

// Engine is a search backend.
type Engine interface {
	// Name is the canonical engine identifier.
	Name() string
	// Supports reports whether the engine can return results of kind k.
	Supports(k Kind) bool
	// Kinds lists supported kinds; the first is the primary kind.
	Kinds() []Kind
	// Search returns results. Engines that cannot fetch results (URL-only
	// engines) return ErrBrowserOnly.
	Search(ctx context.Context, q Query) ([]Result, error)
	// SearchURL returns a URL a human can open in a browser.
	SearchURL(q Query) string
}

// ErrBrowserOnly indicates an engine only supports opening a browser.
var ErrBrowserOnly = errors.New("this engine only supports opening results in a browser")

// AuthError indicates missing credentials.
type AuthError struct {
	Engine string
	Hint   string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("%s requires credentials: %s", e.Engine, e.Hint)
}

// Info describes an engine for listings.
type Info struct {
	Name        string
	Description string
	Bangs       []string
	Kinds       []Kind
	Auth        string // "", "optional", "required"
	BrowserOnly bool
}

// Registry holds the configured engines.
type Registry struct {
	cfg     *config.Config
	client  *Client
	engines map[string]Engine
	infos   map[string]Info
	aliases map[string]string
	// unready holds engines that need configuration that is missing; they
	// are hidden from listings, cycling and bang resolution.
	unready map[string]Unconfigured
}

// NewRegistry builds all built-in and configured custom engines.
func NewRegistry(cfg *config.Config) (*Registry, error) {
	r := &Registry{
		cfg:     cfg,
		client:  NewClient(cfg),
		engines: map[string]Engine{},
		infos:   map[string]Info{},
		aliases: map[string]string{},
		unready: map[string]Unconfigured{},
	}
	for _, b := range builtins {
		ec := cfg.EngineConfig(b.info.Name)
		if ec.Disabled {
			continue
		}
		if ec.Type != "" && ec.Type != b.info.Name {
			continue // overridden by a custom definition below
		}
		if b.ready != nil && !b.ready(ec) {
			r.unready[b.info.Name] = Unconfigured{Name: b.info.Name, Missing: b.hint, URL: b.web, Bangs: append(append([]string{}, b.info.Bangs...), ec.Bangs...)}
			continue
		}
		r.add(b.info, b.make(r.client, ec), ec.Bangs)
	}
	names := make([]string, 0, len(cfg.Engines))
	for n := range cfg.Engines {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		ec := cfg.Engines[name]
		if ec == nil || ec.Disabled {
			continue
		}
		if _, ok := r.engines[name]; ok {
			continue
		}
		if _, ok := r.unready[name]; ok {
			continue
		}
		e, info, err := newCustom(r.client, name, ec)
		var nr *notReadyError
		if errors.As(err, &nr) {
			r.unready[name] = Unconfigured{Name: name, Missing: nr.missing, Bangs: ec.Bangs}
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("engine %q: %w", name, err)
		}
		r.add(info, e, ec.Bangs)
	}
	return r, nil
}

func (r *Registry) add(info Info, e Engine, extra []string) {
	info.Bangs = append(append([]string{}, info.Bangs...), extra...)
	r.engines[info.Name] = e
	r.infos[info.Name] = info
	r.aliases[info.Name] = info.Name
	for _, b := range info.Bangs {
		r.aliases[strings.ToLower(b)] = info.Name
	}
}

// Client returns the shared HTTP client.
func (r *Registry) Client() *Client { return r.client }

// Get returns an engine by name, alias or bang.
func (r *Registry) Get(name string) (Engine, bool) {
	name = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "!"))
	if canon, ok := r.aliases[name]; ok {
		return r.engines[canon], true
	}
	return nil, false
}

// Resolve returns the engine named by name (or the default engine for kind
// when name is empty), plus the canonical kind to use.
func (r *Registry) Resolve(name string, kind Kind) (Engine, Kind, error) {
	if name == "" {
		name = r.cfg.DefaultEngine
		if kind == KindCode || kind == KindRepos || kind == KindIssues {
			if e, ok := r.Get(name); !ok || !e.Supports(kind) {
				name = r.DefaultCodeEngine()
			}
		}
	}
	e, ok := r.Get(name)
	if !ok {
		return nil, kind, fmt.Errorf("unknown engine %q (try --list-engines)", name)
	}
	if !e.Supports(kind) {
		if ks := e.Kinds(); len(ks) > 0 {
			return e, ks[0], nil
		}
	}
	return e, kind, nil
}

// DefaultCodeEngine picks the configured or best available code engine.
func (r *Registry) DefaultCodeEngine() string {
	if r.cfg.DefaultCodeEngine != "" {
		return r.cfg.DefaultCodeEngine
	}
	if gh, ok := r.engines["github"].(*GitHub); ok && gh.hasToken(context.Background()) {
		return "github"
	}
	return "sourcegraph"
}

// Names returns canonical engine names, sorted.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.engines))
	for n := range r.engines {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Info returns metadata for an engine.
func (r *Registry) Info(name string) Info { return r.infos[name] }

// Display is a compact label for an engine.
func (r *Registry) Display(name string) string {
	if ec, ok := r.cfg.Engines[name]; ok && ec != nil && ec.Name != "" {
		return ec.Name
	}
	return name
}

// fill replaces {query}, {page}, {limit}, {offset} placeholders.
func fill(tmpl string, q Query) string {
	page := q.Page
	if page < 1 {
		page = 1
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 10
	}
	return strings.NewReplacer(
		"{query}", url.QueryEscape(q.Text),
		"{query_path}", url.PathEscape(q.Text),
		"{query_raw}", q.Text,
		"{page}", fmt.Sprint(page),
		"{page0}", fmt.Sprint(page-1),
		"{limit}", fmt.Sprint(limit),
		"{offset}", fmt.Sprint((page-1)*limit),
		"{offset1}", fmt.Sprint((page-1)*limit+1),
	).Replace(tmpl)
}

func limitOf(q Query, def int) int {
	if q.Limit > 0 {
		return q.Limit
	}
	return def
}

func pageOf(q Query) int {
	if q.Page < 1 {
		return 1
	}
	return q.Page
}

func trimResults(rs []Result, n int) []Result {
	if n > 0 && len(rs) > n {
		return rs[:n]
	}
	return rs
}

// Unconfigured describes an engine hidden because it lacks configuration.
type Unconfigured struct {
	Name    string
	Missing string // what to set, e.g. "api_key and cx"
	URL     string // browser search URL template ({query}), may be empty
	Bangs   []string
}

type notReadyError struct{ missing string }

func (e *notReadyError) Error() string { return "not configured: needs " + e.missing }

// Unconfigured lists hidden engines, sorted by name.
func (r *Registry) Unconfigured() []Unconfigured {
	out := make([]Unconfigured, 0, len(r.unready))
	for _, u := range r.unready {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// UnconfiguredByName finds a hidden engine by name or bang.
func (r *Registry) UnconfiguredByName(name string) (Unconfigured, bool) {
	name = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "!"))
	for _, u := range r.unready {
		if u.Name == name {
			return u, true
		}
		for _, b := range u.Bangs {
			if strings.ToLower(b) == name {
				return u, true
			}
		}
	}
	return Unconfigured{}, false
}
