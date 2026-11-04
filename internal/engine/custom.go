package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/heywoodlh/search-tui/internal/config"
)

// URLEngine only knows how to build a browser URL.
type URLEngine struct {
	base
	tmpl string
}

func (u *URLEngine) SearchURL(q Query) string { return fill(u.tmpl, q) }

func (u *URLEngine) Search(context.Context, Query) ([]Result, error) { return nil, ErrBrowserOnly }

// NewURLEngine creates a browser-only engine from a URL template.
func NewURLEngine(name, tmpl string) Engine {
	return &URLEngine{base: base{name: name, kinds: []Kind{KindWeb}, cfg: &config.Engine{}}, tmpl: tmpl}
}

// Custom implements user-defined JSON and HTML engines.
type Custom struct {
	base
	typ string
}

func (c *Custom) SearchURL(q Query) string {
	if c.cfg.BaseURL != "" && !strings.Contains(c.cfg.URL, "{query}") {
		return c.cfg.BaseURL
	}
	return fill(c.cfg.URL, q)
}

func (c *Custom) authHeaders(ctx context.Context) (map[string]string, error) {
	h := map[string]string{}
	key, err := c.cfg.Secret(ctx, "api_key")
	if err != nil {
		return nil, err
	}
	tok, err := c.cfg.Secret(ctx, "token")
	if err != nil {
		return nil, err
	}
	for k, v := range c.cfg.Headers {
		h[k] = strings.NewReplacer("{api_key}", key, "{token}", tok).Replace(v)
	}
	if c.cfg.Username != "" {
		pw, err := c.cfg.Secret(ctx, "password")
		if err != nil {
			return nil, err
		}
		h["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(c.cfg.Username+":"+pw))
	} else if tok != "" && h["Authorization"] == "" {
		h["Authorization"] = "Bearer " + tok
	}
	return h, nil
}

func (c *Custom) request(ctx context.Context, q Query) (Request, error) {
	h, err := c.authHeaders(ctx)
	if err != nil {
		return Request{}, err
	}
	key, _ := c.cfg.Secret(ctx, "api_key")
	u := strings.ReplaceAll(fill(c.cfg.URL, q), "{api_key}", key)
	r := Request{URL: u, Headers: h, Method: strings.ToUpper(c.cfg.Method)}
	if c.cfg.Body != "" {
		r.Body = strings.NewReader(fill(c.cfg.Body, q))
		if r.Method == "" {
			r.Method = http.MethodPost
		}
	}
	return r, nil
}

func (c *Custom) Search(ctx context.Context, q Query) ([]Result, error) {
	req, err := c.request(ctx, q)
	if err != nil {
		return nil, err
	}
	var rs []Result
	switch c.typ {
	case "json":
		var data any
		if err := c.client.JSON(ctx, req, &data); err != nil {
			return nil, err
		}
		rs = extractJSON(data, c.cfg)
	case "html":
		doc, err := c.client.Doc(ctx, req)
		if err != nil {
			return nil, err
		}
		rs = extractHTML(doc, c.cfg)
	}
	for i := range rs {
		rs[i].Engine = c.name
	}
	return trimResults(rs, limitOf(q, 0)), nil
}

func extractJSON(data any, ec *config.Engine) []Result {
	items, _ := jsonPath(data, ec.ResultsPath).([]any)
	tf, uf, sf := orDefault(ec.TitleField, "title"), orDefault(ec.URLField, "url"), orDefault(ec.SnippetField, "snippet")
	var rs []Result
	for _, it := range items {
		title := jsonString(jsonPath(it, tf))
		u := jsonString(jsonPath(it, uf))
		if title == "" && u == "" {
			continue
		}
		if title == "" {
			title = u
		}
		rs = append(rs, Result{Title: CleanText(title), URL: u, Snippet: CleanText(jsonString(jsonPath(it, sf)))})
	}
	return rs
}

func extractHTML(doc *goquery.Document, ec *config.Engine) []Result {
	var rs []Result
	attr := orDefault(ec.LinkAttr, "href")
	doc.Find(orDefault(ec.ResultSelector, "body")).Each(func(_ int, s *goquery.Selection) {
		link := s.Find(orDefault(ec.LinkSelector, "a")).First()
		href, _ := link.Attr(attr)
		title := link.Text()
		if ec.TitleSelector != "" {
			title = s.Find(ec.TitleSelector).First().Text()
		}
		snip := ""
		if ec.SnippetSelector != "" {
			snip = s.Find(ec.SnippetSelector).First().Text()
		}
		if href == "" {
			return
		}
		href = resolveRef(ec.URL, href)
		rs = append(rs, Result{Title: CleanText(title), URL: href, Snippet: CleanText(snip)})
	})
	return rs
}

func resolveRef(baseTmpl, href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	scheme := "https"
	host := hostOf(baseTmpl)
	if strings.HasPrefix(baseTmpl, "http://") {
		scheme = "http"
	}
	if strings.HasPrefix(href, "//") {
		return scheme + ":" + href
	}
	if !strings.HasPrefix(href, "/") {
		href = "/" + href
	}
	return scheme + "://" + host + href
}

// jsonPath walks a dotted path ("data.items", "results.0.url").
func jsonPath(v any, p string) any {
	if p == "" || p == "." {
		return v
	}
	for _, part := range strings.Split(p, ".") {
		switch t := v.(type) {
		case map[string]any:
			v = t[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(t) {
				return nil
			}
			v = t[i]
		default:
			return nil
		}
	}
	return v
}

func jsonString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64, bool:
		return fmt.Sprint(t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// newCustom creates a user-defined engine from its config section.
func newCustom(c *Client, name string, ec *config.Engine) (Engine, Info, error) {
	typ := strings.ToLower(ec.Type)
	if typ == "" {
		switch {
		case ec.URL != "":
			typ = "url"
		case ec.BaseURL != "":
			typ = "searxng"
		default:
			return nil, Info{}, fmt.Errorf("custom engines need a type and url/base_url")
		}
	}
	if typ == "forgejo" {
		typ = "gitea"
	}
	info := Info{Name: name, Description: "custom " + typ + " engine"}
	b := base{name: name, client: c, cfg: ec, kinds: []Kind{KindWeb}}
	// Built-in implementations can be reused for other instances.
	for _, bi := range builtins {
		if bi.info.Name == typ || (bi.typ != "" && bi.typ == typ) {
			if bi.ready != nil && !bi.ready(ec) {
				return nil, info, &notReadyError{missing: bi.hint}
			}
			info.Kinds = bi.info.Kinds
			info.Auth = bi.info.Auth
			info.Description = bi.info.Description + " (" + name + ")"
			e := bi.make(c, ec)
			setName(e, name)
			return e, info, nil
		}
	}
	switch typ {
	case "url":
		if ec.URL == "" {
			return nil, info, fmt.Errorf("type=url requires url with {query}")
		}
		info.BrowserOnly = true
		info.Kinds = []Kind{KindWeb}
		return &URLEngine{base: b, tmpl: ec.URL}, info, nil
	case "json", "html":
		if ec.URL == "" {
			return nil, info, fmt.Errorf("type=%s requires url", typ)
		}
		if typ == "html" && ec.ResultSelector == "" {
			return nil, info, fmt.Errorf("type=html requires result_selector")
		}
		info.Kinds = []Kind{KindWeb}
		return &Custom{base: b, typ: typ}, info, nil
	}
	return nil, info, fmt.Errorf("unknown engine type %q", typ)
}

func setName(e Engine, name string) {
	type named interface{ setName(string) }
	if n, ok := e.(named); ok {
		n.setName(name)
	}
}

func (b *base) setName(n string) { b.name = n }
