// Package bang implements DuckDuckGo-style !bang shortcuts.
package bang

import (
	"sort"
	"strings"

	"github.com/heywoodlh/tui-search/internal/engine"
)

// Target is what a bang resolves to.
type Target struct {
	Bang   string
	Engine string      // engine name (results shown in the app)
	Kind   engine.Kind // optional kind override
	URL    string      // URL template with {query} (opens a browser)
	Lucky  bool        // bare "!": open the first result
}

// Entry is a bang listing row.
type Entry struct {
	Bang   string
	Target string
}

// kindBangs are built-in bangs that select an engine *and* a search kind.
var kindBangs = map[string]string{
	"ghc": "github:code", "ghr": "github:repos", "ghi": "github:issues",
	"glc": "gitlab:code", "glr": "gitlab:repos", "gli": "gitlab:issues",
	"sgc": "sourcegraph:code", "sgr": "sourcegraph:repos",
	"code": ":code",
}

// URLBangs are browser-only shortcuts for popular sites.
var URLBangs = map[string]string{
	"yt":         "https://www.youtube.com/results?search_query={query}",
	"aw":         "https://wiki.archlinux.org/index.php?search={query}",
	"nix":        "https://search.nixos.org/packages?channel=unstable&query={query}",
	"nixpkgs":    "https://search.nixos.org/packages?channel=unstable&query={query}",
	"nixopt":     "https://search.nixos.org/options?channel=unstable&query={query}",
	"hm":         "https://home-manager-options.extranix.com/?query={query}",
	"noogle":     "https://noogle.dev/q?term={query}",
	"mdn":        "https://developer.mozilla.org/en-US/search?q={query}",
	"pypi":       "https://pypi.org/search/?q={query}",
	"npm":        "https://www.npmjs.com/search?q={query}",
	"crates":     "https://crates.io/search?q={query}",
	"go":         "https://pkg.go.dev/search?q={query}",
	"godoc":      "https://pkg.go.dev/search?q={query}",
	"docker":     "https://hub.docker.com/search?q={query}",
	"r":          "https://www.reddit.com/search/?q={query}",
	"reddit":     "https://www.reddit.com/search/?q={query}",
	"a":          "https://www.amazon.com/s?k={query}",
	"maps":       "https://www.openstreetmap.org/search?query={query}",
	"osm":        "https://www.openstreetmap.org/search?query={query}",
	"gmaps":      "https://www.google.com/maps/search/{query}",
	"imdb":       "https://www.imdb.com/find/?q={query}",
	"wa":         "https://www.wolframalpha.com/input?i={query}",
	"translate":  "https://translate.google.com/?text={query}",
	"archive":    "https://web.archive.org/web/*/{query}",
	"man":        "https://man.archlinux.org/search?q={query}",
	"tldr":       "https://tldr.inbrowser.app/pages/common/{query_raw}",
	"rfc":        "https://www.rfc-editor.org/search/rfc_search_detail.php?title={query}",
	"cve":        "https://www.cve.org/CVERecord/SearchResults?query={query}",
	"aur":        "https://aur.archlinux.org/packages?K={query}",
	"brew":       "https://formulae.brew.sh/formula/{query_raw}",
	"ddgi":       "https://duckduckgo.com/?ia=images&iax=images&q={query}",
	"gi":         "https://www.google.com/search?tbm=isch&q={query}",
	"gn":         "https://news.google.com/search?q={query}",
	"tw":         "https://x.com/search?q={query}",
	"x":          "https://x.com/search?q={query}",
	"mastodon":   "https://mastodon.social/search?q={query}",
	"perplexity": "https://www.perplexity.ai/search?q={query}",
	"startpage":  "https://www.startpage.com/do/search?q={query}",
	"yandex":     "https://yandex.com/search/?text={query}",
	"ecosia":     "https://www.ecosia.org/search?q={query}",
	"qwant":      "https://www.qwant.com/?q={query}",
}

// Resolver resolves bangs against the engine registry and configuration.
type Resolver struct {
	reg    *engine.Registry
	custom map[string]string
}

// NewResolver creates a resolver. custom are user bangs from the config.
func NewResolver(reg *engine.Registry, custom map[string]string) *Resolver {
	c := map[string]string{}
	for k, v := range custom {
		c[strings.ToLower(strings.TrimPrefix(k, "!"))] = v
	}
	return &Resolver{reg: reg, custom: c}
}

// Extract removes the first !bang token from text. It returns the remaining
// query and the bang (without "!"), or "" when there is none. A bare "!"
// yields bang "!" (I'm feeling lucky).
func Extract(text string) (string, string) {
	fields := strings.Fields(text)
	for i, f := range fields {
		if strings.HasPrefix(f, "!") && (len(f) == 1 || isBangName(f[1:])) {
			rest := append(append([]string{}, fields[:i]...), fields[i+1:]...)
			b := strings.ToLower(f[1:])
			if b == "" {
				b = "!"
			}
			return strings.Join(rest, " "), b
		}
	}
	return strings.TrimSpace(text), ""
}

func isBangName(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return s != ""
}

// Resolve looks up a bang. User config wins over built-ins.
func (r *Resolver) Resolve(b string) (Target, bool) {
	b = strings.ToLower(strings.TrimPrefix(b, "!"))
	if b == "" || b == "!" {
		return Target{Bang: "!", Lucky: true}, true
	}
	if v, ok := r.custom[b]; ok {
		return r.parseValue(b, v)
	}
	if e, ok := r.reg.Get(b); ok {
		return Target{Bang: b, Engine: e.Name()}, true
	}
	if v, ok := kindBangs[b]; ok {
		return r.parseValue(b, v)
	}
	// Engines hidden for lack of configuration still open in the browser.
	if u, ok := r.reg.UnconfiguredByName(b); ok && u.URL != "" {
		return Target{Bang: b, URL: u.URL}, true
	}
	if v, ok := URLBangs[b]; ok {
		return Target{Bang: b, URL: v}, true
	}
	return Target{}, false
}

func (r *Resolver) parseValue(b, v string) (Target, bool) {
	if strings.Contains(v, "{query") || strings.Contains(v, "://") {
		return Target{Bang: b, URL: v}, true
	}
	name, kind, _ := strings.Cut(v, ":")
	t := Target{Bang: b}
	if name != "" {
		e, ok := r.reg.Get(name)
		if !ok {
			return Target{}, false
		}
		t.Engine = e.Name()
	}
	if kind != "" {
		k, err := engine.ParseKind(kind)
		if err != nil {
			return Target{}, false
		}
		t.Kind = k
	}
	return t, true
}

// List returns every known bang, sorted.
func (r *Resolver) List() []Entry {
	m := map[string]string{}
	for k, v := range URLBangs {
		m[k] = v
	}
	for k, v := range kindBangs {
		m[k] = v
	}
	for _, n := range r.reg.Names() {
		for _, b := range r.reg.Info(n).Bangs {
			m[strings.ToLower(b)] = n
		}
	}
	for _, u := range r.reg.Unconfigured() {
		if u.URL == "" {
			continue
		}
		for _, b := range append([]string{u.Name}, u.Bangs...) {
			if _, taken := m[strings.ToLower(b)]; !taken {
				m[strings.ToLower(b)] = u.URL
			}
		}
	}
	for k, v := range r.custom {
		m[k] = v
	}
	out := make([]Entry, 0, len(m))
	for k, v := range m {
		out = append(out, Entry{Bang: "!" + k, Target: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bang < out[j].Bang })
	return out
}

// Complete returns bang names starting with prefix (for TUI completion).
func (r *Resolver) Complete(prefix string) []string {
	prefix = strings.ToLower(strings.TrimPrefix(prefix, "!"))
	var out []string
	for _, e := range r.List() {
		if strings.HasPrefix(e.Bang[1:], prefix) {
			out = append(out, e.Bang)
		}
	}
	return out
}
