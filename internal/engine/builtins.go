package engine

import (
	"github.com/heywoodlh-bot/tui-search/internal/config"
)

type builtin struct {
	info Info
	// ready reports whether required configuration is present (nil: always);
	// hint names what is missing.
	ready func(ec *config.Engine) bool
	hint  string
	// web is a browser URL template used by the bangs of unconfigured engines.
	web string
	// typ is an extra type alias custom engines may use (e.g. "forgejo").
	typ  string
	make func(c *Client, ec *config.Engine) Engine
}

func mk(name string, c *Client, ec *config.Engine, kinds ...Kind) base {
	return base{name: name, client: c, cfg: ec, kinds: kinds}
}

var builtins = []builtin{
	{
		info: Info{Name: "duckduckgo", Description: "DuckDuckGo (HTML endpoint, no key)", Bangs: []string{"ddg", "d", "duck"}, Kinds: []Kind{KindWeb}},
		make: func(c *Client, ec *config.Engine) Engine {
			return &DuckDuckGo{base: mk("duckduckgo", c, ec, KindWeb), safe: c.SafeSearch}
		},
	},
	{
		info:  Info{Name: "google", Description: "Google Programmable Search API", Bangs: []string{"g", "google"}, Kinds: []Kind{KindWeb}, Auth: "required"},
		ready: func(ec *config.Engine) bool { return ec.HasSecret("api_key") && ec.HasSecret("cx") },
		hint:  "api_key and cx",
		web:   "https://www.google.com/search?q={query}",
		make: func(c *Client, ec *config.Engine) Engine {
			return &Google{base: mk("google", c, ec, KindWeb), safe: c.SafeSearch}
		},
	},
	{
		info: Info{Name: "bing", Description: "Bing (HTML, best effort)", Bangs: []string{"b", "bing"}, Kinds: []Kind{KindWeb}},
		make: func(c *Client, ec *config.Engine) Engine {
			return &Bing{base: mk("bing", c, ec, KindWeb), safe: c.SafeSearch}
		},
	},
	{
		info:  Info{Name: "kagi", Description: "Kagi (API key or session token)", Bangs: []string{"k", "kagi"}, Kinds: []Kind{KindWeb}, Auth: "required"},
		ready: func(ec *config.Engine) bool { return ec.HasSecret("api_key") || ec.HasSecret("token") },
		hint:  "api_key or token",
		web:   "https://kagi.com/search?q={query}",
		make: func(c *Client, ec *config.Engine) Engine {
			return &Kagi{base: mk("kagi", c, ec, KindWeb)}
		},
	},
	{
		info:  Info{Name: "brave", Description: "Brave Search API", Bangs: []string{"br", "brave"}, Kinds: []Kind{KindWeb}, Auth: "required"},
		ready: func(ec *config.Engine) bool { return ec.HasSecret("api_key") },
		hint:  "api_key",
		web:   "https://search.brave.com/search?q={query}",
		make: func(c *Client, ec *config.Engine) Engine {
			return &Brave{base: mk("brave", c, ec, KindWeb), safe: c.SafeSearch}
		},
	},
	{
		info:  Info{Name: "mojeek", Description: "Mojeek Search API", Bangs: []string{"m", "mojeek"}, Kinds: []Kind{KindWeb}, Auth: "required"},
		ready: func(ec *config.Engine) bool { return ec.HasSecret("api_key") },
		hint:  "api_key",
		web:   "https://www.mojeek.com/search?q={query}",
		make: func(c *Client, ec *config.Engine) Engine {
			return &Mojeek{base: mk("mojeek", c, ec, KindWeb), safe: c.SafeSearch}
		},
	},
	{
		info:  Info{Name: "searxng", Description: "SearXNG instance (set base_url)", Bangs: []string{"sx", "searx", "searxng"}, Kinds: []Kind{KindWeb}, Auth: "optional"},
		ready: func(ec *config.Engine) bool { return ec.BaseURL != "" },
		hint:  "base_url",
		make: func(c *Client, ec *config.Engine) Engine {
			return &SearXNG{base: mk("searxng", c, ec, KindWeb)}
		},
	},
	{
		info: Info{Name: "wikipedia", Description: "Wikipedia", Bangs: []string{"w", "wiki", "wikipedia"}, Kinds: []Kind{KindWeb}},
		make: func(c *Client, ec *config.Engine) Engine {
			return &Wikipedia{base: mk("wikipedia", c, ec, KindWeb)}
		},
	},
	{
		info: Info{Name: "stackoverflow", Description: "Stack Overflow (Stack Exchange API)", Bangs: []string{"so", "stackoverflow"}, Kinds: []Kind{KindWeb}},
		make: func(c *Client, ec *config.Engine) Engine {
			site := "stackoverflow"
			if ec.BaseURL != "" {
				site = hostOf(ec.BaseURL)
				site = trimSuffix(site, ".com")
			}
			return &StackExchange{base: mk("stackoverflow", c, ec, KindWeb), site: site}
		},
	},
	{
		info: Info{Name: "hackernews", Description: "Hacker News (Algolia)", Bangs: []string{"hn", "hackernews"}, Kinds: []Kind{KindWeb}},
		make: func(c *Client, ec *config.Engine) Engine {
			return &HackerNews{base: mk("hackernews", c, ec, KindWeb)}
		},
	},
	{
		info: Info{Name: "github", Description: "GitHub code/repos/issues (code needs a token)", Bangs: []string{"gh", "github"}, Kinds: []Kind{KindRepos, KindCode, KindIssues}, Auth: "optional"},
		make: func(c *Client, ec *config.Engine) Engine {
			return &GitHub{base: mk("github", c, ec, KindRepos, KindCode, KindIssues)}
		},
	},
	{
		info: Info{Name: "gitlab", Description: "GitLab projects/code/issues (code needs a token)", Bangs: []string{"gl", "gitlab"}, Kinds: []Kind{KindRepos, KindCode, KindIssues}, Auth: "optional"},
		make: func(c *Client, ec *config.Engine) Engine {
			return &GitLab{base: mk("gitlab", c, ec, KindRepos, KindCode, KindIssues)}
		},
	},
	{
		info: Info{Name: "sourcegraph", Description: "Sourcegraph public code search (no key)", Bangs: []string{"sg", "sourcegraph"}, Kinds: []Kind{KindCode, KindRepos}, Auth: "optional"},
		make: func(c *Client, ec *config.Engine) Engine {
			return &Sourcegraph{base: mk("sourcegraph", c, ec, KindCode, KindRepos)}
		},
	},
	{
		info: Info{Name: "codeberg", Description: "Codeberg / Gitea / Forgejo repositories", Bangs: []string{"cb", "codeberg"}, Kinds: []Kind{KindRepos}, Auth: "optional"},
		typ:  "gitea",
		make: func(c *Client, ec *config.Engine) Engine {
			return &Gitea{base: mk("codeberg", c, ec, KindRepos)}
		},
	},
}

func trimSuffix(s, suf string) string {
	if len(s) > len(suf) && s[len(s)-len(suf):] == suf {
		return s[:len(s)-len(suf)]
	}
	return s
}

// BuiltinNames lists built-in engine names.
func BuiltinNames() []string {
	out := make([]string, 0, len(builtins))
	for _, b := range builtins {
		out = append(out, b.info.Name)
	}
	return out
}
