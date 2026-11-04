package engine

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/heywoodlh/search-tui/internal/config"
)

// base holds what every engine needs.
type base struct {
	name   string
	kinds  []Kind
	client *Client
	cfg    *config.Engine
}

func (b *base) Name() string { return b.name }

func (b *base) Kinds() []Kind { return b.kinds }

func (b *base) Supports(k Kind) bool {
	for _, x := range b.kinds {
		if x == k {
			return true
		}
	}
	return false
}

func (b *base) headers(extra map[string]string) map[string]string {
	h := map[string]string{}
	for k, v := range b.cfg.Headers {
		h[k] = v
	}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

func (b *base) region(def string) string {
	if b.cfg.Region != "" {
		return b.cfg.Region
	}
	if b.client != nil && b.client.Region != "" {
		return b.client.Region
	}
	return def
}

// ---------------------------------------------------------------- DuckDuckGo

// DuckDuckGo scrapes the JavaScript-free HTML endpoints.
type DuckDuckGo struct {
	base
	safe string
}

func (d *DuckDuckGo) SearchURL(q Query) string {
	return "https://duckduckgo.com/?q=" + url.QueryEscape(q.Text)
}

func (d *DuckDuckGo) Search(ctx context.Context, q Query) ([]Result, error) {
	form := url.Values{"q": {q.Text}}
	if r := d.region(""); r != "" {
		form.Set("kl", r)
	}
	switch d.safe {
	case "strict":
		form.Set("kp", "1")
	case "off":
		form.Set("kp", "-2")
	}
	if p := pageOf(q); p > 1 {
		off := (p - 1) * 10
		form.Set("s", fmt.Sprint(off))
		form.Set("dc", fmt.Sprint(off+1))
		form.Set("v", "l")
		form.Set("o", "json")
		form.Set("api", "d.js")
	}
	headers := d.headers(map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
		"Referer":      "https://html.duckduckgo.com/",
	})
	doc, err := d.client.Doc(ctx, Request{
		Method:  http.MethodPost,
		URL:     "https://html.duckduckgo.com/html/",
		Body:    strings.NewReader(form.Encode()),
		Headers: headers,
	})
	var rs []Result
	if err == nil {
		rs = parseDDGHTML(doc)
	}
	if len(rs) == 0 {
		// Fall back to the lite endpoint, which is rate limited separately.
		doc2, err2 := d.client.Doc(ctx, Request{
			Method:  http.MethodPost,
			URL:     "https://lite.duckduckgo.com/lite/",
			Body:    strings.NewReader(form.Encode()),
			Headers: headers,
		})
		if err2 == nil {
			rs = parseDDGLite(doc2)
		}
		if len(rs) == 0 {
			if err == nil {
				err = err2
			}
			if err != nil {
				return nil, fmt.Errorf("duckduckgo: %w", err)
			}
			if isDDGAnomaly(doc) || isDDGAnomaly(doc2) {
				return nil, errors.New("duckduckgo is rate limiting this IP (bot challenge); retry shortly or use another engine")
			}
		}
	}
	for i := range rs {
		rs[i].Engine = d.name
	}
	return trimResults(rs, limitOf(q, 10)), nil
}

func isDDGAnomaly(doc *goquery.Document) bool {
	return doc != nil && doc.Find(".anomaly-modal, #challenge-form").Length() > 0
}

func parseDDGHTML(doc *goquery.Document) []Result {
	var rs []Result
	doc.Find(".result").Each(func(_ int, s *goquery.Selection) {
		if s.HasClass("result--ad") || s.Find(".badge--ad").Length() > 0 {
			return
		}
		a := s.Find("a.result__a").First()
		href, _ := a.Attr("href")
		u := ddgUnwrap(href)
		title := CleanText(a.Text())
		if u == "" || title == "" {
			return
		}
		rs = append(rs, Result{
			Title:   title,
			URL:     u,
			Snippet: CleanText(s.Find(".result__snippet").Text()),
		})
	})
	return rs
}

func parseDDGLite(doc *goquery.Document) []Result {
	var rs []Result
	doc.Find("a.result-link").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		u := ddgUnwrap(href)
		if u == "" {
			return
		}
		row := a.Closest("tr")
		snip := ""
		for n := row.Next(); n.Length() > 0; n = n.Next() {
			if t := n.Find("td.result-snippet"); t.Length() > 0 {
				snip = CleanText(t.Text())
				break
			}
			if n.Find("a.result-link").Length() > 0 {
				break
			}
		}
		rs = append(rs, Result{Title: CleanText(a.Text()), URL: u, Snippet: snip})
	})
	return rs
}

func ddgUnwrap(href string) string {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if strings.HasSuffix(u.Host, "duckduckgo.com") {
		if strings.HasPrefix(u.Path, "/l/") {
			return u.Query().Get("uddg")
		}
		if strings.HasPrefix(u.Path, "/y.js") {
			return "" // ad
		}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

// ---------------------------------------------------------------------- Bing

// Bing scrapes bing.com (the Bing Web Search API was retired in 2025).
type Bing struct {
	base
	safe string
}

func (b *Bing) SearchURL(q Query) string {
	return "https://www.bing.com/search?q=" + url.QueryEscape(q.Text)
}

func (b *Bing) Search(ctx context.Context, q Query) ([]Result, error) {
	v := url.Values{"q": {q.Text}, "setlang": {"en-US"}, "form": {"QBLH"}}
	if r := b.region(""); r != "" {
		v.Set("cc", r)
	}
	if p := pageOf(q); p > 1 {
		v.Set("first", fmt.Sprint((p-1)*10+1))
	}
	switch b.safe {
	case "strict":
		v.Set("adlt", "strict")
	case "off":
		v.Set("adlt", "off")
	}
	doc, err := b.client.Doc(ctx, Request{URL: "https://www.bing.com/search?" + v.Encode(), Headers: b.headers(nil)})
	if err != nil {
		return nil, err
	}
	rs := parseBing(doc)
	for i := range rs {
		rs[i].Engine = b.name
	}
	if len(rs) == 0 {
		return nil, errors.New("bing returned no parseable results (it may be blocking automated requests)")
	}
	return trimResults(rs, limitOf(q, 10)), nil
}

func parseBing(doc *goquery.Document) []Result {
	var rs []Result
	doc.Find("li.b_algo").Each(func(_ int, s *goquery.Selection) {
		a := s.Find("h2 a").First()
		href, _ := a.Attr("href")
		u := bingUnwrap(href)
		title := CleanText(a.Text())
		if u == "" || title == "" {
			return
		}
		snip := s.Find(".b_caption p").First().Text()
		if strings.TrimSpace(snip) == "" {
			snip = s.Find("p").First().Text()
		}
		rs = append(rs, Result{Title: title, URL: u, Snippet: CleanText(snip)})
	})
	return rs
}

func bingUnwrap(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if strings.HasSuffix(u.Host, "bing.com") && strings.HasPrefix(u.Path, "/ck/") {
		enc := u.Query().Get("u")
		if strings.HasPrefix(enc, "a1") {
			enc = enc[2:]
			if b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(enc, "=")); err == nil {
				return string(b)
			}
		}
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return href
}

// -------------------------------------------------------------------- Google

// Google uses the Programmable Search (Custom Search JSON) API.
type Google struct {
	base
	safe string
}

func (g *Google) SearchURL(q Query) string {
	return "https://www.google.com/search?q=" + url.QueryEscape(q.Text)
}

func (g *Google) Search(ctx context.Context, q Query) ([]Result, error) {
	key, err := g.cfg.Secret(ctx, "api_key")
	if err != nil {
		return nil, err
	}
	cx, err := g.cfg.Secret(ctx, "cx")
	if err != nil {
		return nil, err
	}
	if key == "" || cx == "" {
		return nil, &AuthError{Engine: "google", Hint: "set [engines.google] api_key and cx (Programmable Search Engine); Google blocks scraping. Press O / use --open to search in a browser"}
	}
	n := limitOf(q, 10)
	if n > 10 {
		n = 10
	}
	v := url.Values{"key": {key}, "cx": {cx}, "q": {q.Text}, "num": {fmt.Sprint(n)}}
	if p := pageOf(q); p > 1 {
		v.Set("start", fmt.Sprint((p-1)*n+1))
	}
	if g.safe == "strict" {
		v.Set("safe", "active")
	}
	if r := g.region(""); r != "" {
		v.Set("gl", r)
	}
	var resp struct {
		Items []struct {
			Title       string `json:"title"`
			Link        string `json:"link"`
			Snippet     string `json:"snippet"`
			DisplayLink string `json:"displayLink"`
		} `json:"items"`
	}
	if err := g.client.JSON(ctx, Request{URL: "https://www.googleapis.com/customsearch/v1?" + v.Encode(), Headers: g.headers(nil)}, &resp); err != nil {
		return nil, err
	}
	rs := make([]Result, 0, len(resp.Items))
	for _, it := range resp.Items {
		rs = append(rs, Result{Title: it.Title, URL: it.Link, Snippet: CleanText(it.Snippet), Engine: g.name})
	}
	return rs, nil
}

// ---------------------------------------------------------------------- Kagi

// Kagi uses the Search API with an API key, or the no-JS HTML interface with
// a session token (from the "Session Link" in Kagi's account settings).
type Kagi struct{ base }

func (k *Kagi) SearchURL(q Query) string {
	return "https://kagi.com/search?q=" + url.QueryEscape(q.Text)
}

func (k *Kagi) Search(ctx context.Context, q Query) ([]Result, error) {
	key, err := k.cfg.Secret(ctx, "api_key")
	if err != nil {
		return nil, err
	}
	if key != "" {
		return k.api(ctx, q, key)
	}
	tok, err := k.cfg.Secret(ctx, "token")
	if err != nil {
		return nil, err
	}
	if tok == "" {
		return nil, &AuthError{Engine: "kagi", Hint: "set [engines.kagi] api_key (Search API) or token (session link token)"}
	}
	tok = kagiToken(tok)
	v := url.Values{"q": {q.Text}, "token": {tok}}
	if p := pageOf(q); p > 1 {
		v.Set("batch", fmt.Sprint(p))
	}
	doc, err := k.client.Doc(ctx, Request{
		URL:     "https://kagi.com/html/search?" + v.Encode(),
		Headers: k.headers(map[string]string{"Cookie": "kagi_session=" + tok}),
	})
	if err != nil {
		return nil, err
	}
	var rs []Result
	doc.Find(".search-result, ._0_SRI").Each(func(_ int, s *goquery.Selection) {
		a := s.Find("a.__sri_title_link, .__sri-title a, h3 a").First()
		href, _ := a.Attr("href")
		title := CleanText(a.Text())
		if href == "" || title == "" || !strings.HasPrefix(href, "http") {
			return
		}
		snip := CleanText(s.Find(".__sri-desc, .__sri-body").First().Text())
		rs = append(rs, Result{Title: title, URL: href, Snippet: snip, Engine: k.name})
	})
	if len(rs) == 0 {
		return nil, errors.New("kagi returned no parseable results (is the session token valid?)")
	}
	return trimResults(rs, limitOf(q, 20)), nil
}

// kagiToken accepts either a bare token or a full session link.
func kagiToken(t string) string {
	if u, err := url.Parse(t); err == nil && u.Query().Get("token") != "" {
		return u.Query().Get("token")
	}
	return t
}

func (k *Kagi) api(ctx context.Context, q Query, key string) ([]Result, error) {
	v := url.Values{"q": {q.Text}, "limit": {fmt.Sprint(limitOf(q, 20))}}
	var resp struct {
		Data []struct {
			T       int    `json:"t"`
			URL     string `json:"url"`
			Title   string `json:"title"`
			Snippet string `json:"snippet"`
		} `json:"data"`
		Error []struct {
			Msg string `json:"msg"`
		} `json:"error"`
	}
	err := k.client.JSON(ctx, Request{
		URL:     "https://kagi.com/api/v0/search?" + v.Encode(),
		Headers: k.headers(map[string]string{"Authorization": "Bot " + key}),
	}, &resp)
	if err != nil {
		return nil, err
	}
	if len(resp.Error) > 0 {
		return nil, errors.New("kagi: " + resp.Error[0].Msg)
	}
	var rs []Result
	for _, d := range resp.Data {
		if d.T != 0 {
			continue
		}
		rs = append(rs, Result{Title: d.Title, URL: d.URL, Snippet: CleanText(d.Snippet), Engine: k.name})
	}
	return rs, nil
}

// --------------------------------------------------------------------- Brave

// Brave uses the Brave Search API.
type Brave struct {
	base
	safe string
}

func (b *Brave) SearchURL(q Query) string {
	return "https://search.brave.com/search?q=" + url.QueryEscape(q.Text)
}

func (b *Brave) Search(ctx context.Context, q Query) ([]Result, error) {
	key, err := b.cfg.Secret(ctx, "api_key")
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, &AuthError{Engine: "brave", Hint: "set [engines.brave] api_key (https://brave.com/search/api/)"}
	}
	n := limitOf(q, 20)
	if n > 20 {
		n = 20
	}
	v := url.Values{"q": {q.Text}, "count": {fmt.Sprint(n)}, "offset": {fmt.Sprint(pageOf(q) - 1)}}
	if b.safe != "" {
		v.Set("safesearch", b.safe)
	}
	if r := b.region(""); r != "" {
		v.Set("country", r)
	}
	var resp struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
				Age         string `json:"age"`
			} `json:"results"`
		} `json:"web"`
	}
	err = b.client.JSON(ctx, Request{
		URL:     "https://api.search.brave.com/res/v1/web/search?" + v.Encode(),
		Headers: b.headers(map[string]string{"X-Subscription-Token": key}),
	}, &resp)
	if err != nil {
		return nil, err
	}
	var rs []Result
	for _, r := range resp.Web.Results {
		rs = append(rs, Result{Title: CleanText(r.Title), URL: r.URL, Snippet: CleanText(r.Description), Meta: r.Age, Engine: b.name})
	}
	return rs, nil
}

// -------------------------------------------------------------------- Mojeek

// Mojeek uses its API when a key is configured, otherwise scrapes HTML.
type Mojeek struct {
	base
	safe string
}

func (m *Mojeek) SearchURL(q Query) string {
	return "https://www.mojeek.com/search?q=" + url.QueryEscape(q.Text)
}

func (m *Mojeek) Search(ctx context.Context, q Query) ([]Result, error) {
	n := limitOf(q, 10)
	key, err := m.cfg.Secret(ctx, "api_key")
	if err != nil {
		return nil, err
	}
	if key != "" {
		v := url.Values{"q": {q.Text}, "api_key": {key}, "fmt": {"json"}, "t": {fmt.Sprint(n)}, "s": {fmt.Sprint((pageOf(q)-1)*n + 1)}}
		if m.safe == "strict" {
			v.Set("safe", "1")
		}
		var resp struct {
			Response struct {
				Results []struct {
					Title string `json:"title"`
					URL   string `json:"url"`
					Desc  string `json:"desc"`
				} `json:"results"`
			} `json:"response"`
		}
		if err := m.client.JSON(ctx, Request{URL: "https://api.mojeek.com/search?" + v.Encode(), Headers: m.headers(nil)}, &resp); err != nil {
			return nil, err
		}
		var rs []Result
		for _, r := range resp.Response.Results {
			rs = append(rs, Result{Title: CleanText(r.Title), URL: r.URL, Snippet: CleanText(r.Desc), Engine: m.name})
		}
		return rs, nil
	}
	v := url.Values{"q": {q.Text}}
	if p := pageOf(q); p > 1 {
		v.Set("s", fmt.Sprint((p-1)*10+1))
	}
	doc, err := m.client.Doc(ctx, Request{URL: "https://www.mojeek.com/search?" + v.Encode(), Headers: m.headers(nil)})
	if err != nil {
		return nil, err
	}
	var rs []Result
	doc.Find("ul.results-standard li").Each(func(_ int, s *goquery.Selection) {
		a := s.Find("a.title").First()
		href, _ := a.Attr("href")
		if href == "" {
			return
		}
		rs = append(rs, Result{Title: CleanText(a.Text()), URL: href, Snippet: CleanText(s.Find("p.s").Text()), Engine: m.name})
	})
	if len(rs) == 0 {
		return nil, &AuthError{Engine: "mojeek", Hint: "scraping was blocked (captcha); set [engines.mojeek] api_key"}
	}
	return trimResults(rs, n), nil
}

// ------------------------------------------------------------------- SearXNG

// SearXNG queries a SearXNG instance's JSON API.
type SearXNG struct{ base }

func (s *SearXNG) baseURL() string { return strings.TrimRight(s.cfg.BaseURL, "/") }

func (s *SearXNG) SearchURL(q Query) string {
	if s.cfg.BaseURL == "" {
		return ""
	}
	return s.baseURL() + "/search?q=" + url.QueryEscape(q.Text)
}

func (s *SearXNG) Search(ctx context.Context, q Query) ([]Result, error) {
	if s.cfg.BaseURL == "" {
		return nil, &AuthError{Engine: s.name, Hint: "set base_url to a SearXNG instance with the json format enabled"}
	}
	v := url.Values{"q": {q.Text}, "format": {"json"}, "pageno": {fmt.Sprint(pageOf(q))}}
	if s.cfg.Language != "" {
		v.Set("language", s.cfg.Language)
	}
	h := s.headers(nil)
	if s.cfg.Username != "" {
		pw, err := s.cfg.Secret(ctx, "password")
		if err != nil {
			return nil, err
		}
		h["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(s.cfg.Username+":"+pw))
	}
	if tok, _ := s.cfg.Secret(ctx, "token"); tok != "" {
		h["Authorization"] = "Bearer " + tok
	}
	var resp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
			Engine  string `json:"engine"`
		} `json:"results"`
	}
	if err := s.client.JSON(ctx, Request{URL: s.baseURL() + "/search?" + v.Encode(), Headers: h}, &resp); err != nil {
		var he *HTTPError
		if errors.As(err, &he) && he.Status == http.StatusForbidden {
			return nil, fmt.Errorf("%s: JSON output is disabled on this instance (enable 'json' in search.formats)", s.name)
		}
		return nil, err
	}
	var rs []Result
	for _, r := range resp.Results {
		rs = append(rs, Result{Title: CleanText(r.Title), URL: r.URL, Snippet: CleanText(r.Content), Meta: r.Engine, Engine: s.name})
	}
	return trimResults(rs, limitOf(q, 0)), nil
}

// ----------------------------------------------------------------- Wikipedia

// Wikipedia uses the MediaWiki search API.
type Wikipedia struct{ base }

func (w *Wikipedia) lang() string {
	if w.cfg.Language != "" {
		return w.cfg.Language
	}
	return "en"
}

func (w *Wikipedia) SearchURL(q Query) string {
	return fmt.Sprintf("https://%s.wikipedia.org/w/index.php?search=%s", w.lang(), url.QueryEscape(q.Text))
}

func (w *Wikipedia) Search(ctx context.Context, q Query) ([]Result, error) {
	n := limitOf(q, 10)
	v := url.Values{
		"action": {"query"}, "list": {"search"}, "format": {"json"},
		"srsearch": {q.Text}, "srlimit": {fmt.Sprint(n)}, "sroffset": {fmt.Sprint((pageOf(q) - 1) * n)},
	}
	var resp struct {
		Query struct {
			Search []struct {
				Title     string `json:"title"`
				Snippet   string `json:"snippet"`
				WordCount int    `json:"wordcount"`
				Timestamp string `json:"timestamp"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := w.client.JSON(ctx, Request{URL: fmt.Sprintf("https://%s.wikipedia.org/w/api.php?%s", w.lang(), v.Encode()), Headers: w.headers(nil)}, &resp); err != nil {
		return nil, err
	}
	var rs []Result
	for _, s := range resp.Query.Search {
		rs = append(rs, Result{
			Title:   s.Title,
			URL:     fmt.Sprintf("https://%s.wikipedia.org/wiki/%s", w.lang(), url.PathEscape(strings.ReplaceAll(s.Title, " ", "_"))),
			Snippet: CleanText(s.Snippet),
			Meta:    fmt.Sprintf("%d words", s.WordCount),
			Engine:  w.name,
		})
	}
	return rs, nil
}

// ------------------------------------------------------------- StackOverflow

// StackExchange uses the Stack Exchange API (works without a key).
type StackExchange struct {
	base
	site string
}

func (s *StackExchange) SearchURL(q Query) string {
	return fmt.Sprintf("https://%s.com/search?q=%s", s.site, url.QueryEscape(q.Text))
}

func (s *StackExchange) Search(ctx context.Context, q Query) ([]Result, error) {
	v := url.Values{
		"order": {"desc"}, "sort": {"relevance"}, "q": {q.Text}, "site": {s.site},
		"pagesize": {fmt.Sprint(limitOf(q, 15))}, "page": {fmt.Sprint(pageOf(q))},
	}
	if key, _ := s.cfg.Secret(ctx, "api_key"); key != "" {
		v.Set("key", key)
	}
	var resp struct {
		Items []struct {
			Title       string   `json:"title"`
			Excerpt     string   `json:"excerpt"`
			Body        string   `json:"body"`
			QuestionID  int      `json:"question_id"`
			AnswerID    int      `json:"answer_id"`
			ItemType    string   `json:"item_type"`
			Score       int      `json:"score"`
			AnswerCount int      `json:"answer_count"`
			Accepted    bool     `json:"has_accepted_answer"`
			Tags        []string `json:"tags"`
		} `json:"items"`
		ErrorMessage string `json:"error_message"`
	}
	if err := s.client.JSON(ctx, Request{URL: "https://api.stackexchange.com/2.3/search/excerpts?" + v.Encode(), Headers: s.headers(nil)}, &resp); err != nil {
		return nil, err
	}
	if resp.ErrorMessage != "" {
		return nil, errors.New(resp.ErrorMessage)
	}
	var rs []Result
	for _, it := range resp.Items {
		u := fmt.Sprintf("https://%s.com/q/%d", s.site, it.QuestionID)
		if it.ItemType == "answer" {
			u = fmt.Sprintf("https://%s.com/a/%d", s.site, it.AnswerID)
		}
		snip := it.Excerpt
		if snip == "" {
			snip = it.Body
		}
		meta := fmt.Sprintf("score %d · %d answers", it.Score, it.AnswerCount)
		if it.Accepted {
			meta += " · accepted"
		}
		if len(it.Tags) > 0 {
			meta += " · " + strings.Join(it.Tags, ", ")
		}
		rs = append(rs, Result{Title: CleanText(it.Title), URL: u, Snippet: CleanText(snip), Meta: meta, Engine: s.name})
	}
	return rs, nil
}

// ---------------------------------------------------------------- HackerNews

// HackerNews uses the Algolia HN search API.
type HackerNews struct{ base }

func (h *HackerNews) SearchURL(q Query) string {
	return "https://hn.algolia.com/?q=" + url.QueryEscape(q.Text)
}

func (h *HackerNews) Search(ctx context.Context, q Query) ([]Result, error) {
	v := url.Values{"query": {q.Text}, "hitsPerPage": {fmt.Sprint(limitOf(q, 20))}, "page": {fmt.Sprint(pageOf(q) - 1)}, "tags": {"story"}}
	var resp struct {
		Hits []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			ObjectID    string `json:"objectID"`
			Points      int    `json:"points"`
			NumComments int    `json:"num_comments"`
			Author      string `json:"author"`
			CreatedAt   string `json:"created_at"`
			StoryText   string `json:"story_text"`
		} `json:"hits"`
	}
	if err := h.client.JSON(ctx, Request{URL: "https://hn.algolia.com/api/v1/search?" + v.Encode(), Headers: h.headers(nil)}, &resp); err != nil {
		return nil, err
	}
	var rs []Result
	for _, it := range resp.Hits {
		disc := "https://news.ycombinator.com/item?id=" + it.ObjectID
		u := it.URL
		if u == "" {
			u = disc
		}
		date := it.CreatedAt
		if len(date) >= 10 {
			date = date[:10]
		}
		snip := CleanText(it.StoryText)
		if snip == "" {
			snip = "Discussion: " + disc
		}
		rs = append(rs, Result{
			Title:   it.Title,
			URL:     u,
			Snippet: snip,
			Meta:    fmt.Sprintf("%d points · %d comments · %s · %s", it.Points, it.NumComments, it.Author, date),
			Engine:  h.name,
		})
	}
	return rs, nil
}
