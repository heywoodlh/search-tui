package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/heywoodlh/tui-search/internal/config"
	"github.com/heywoodlh/tui-search/internal/safe"
)

// Client is a small HTTP helper that enforces timeouts and body limits so
// slow links and low-memory devices stay responsive.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	MaxBody   int64
	// SafeSearch and Region are global defaults (engines may override region).
	SafeSearch string
	Region     string
}

// NewClient creates a client from configuration.
func NewClient(cfg *config.Config) *Client {
	return &Client{
		HTTP:       &http.Client{Timeout: cfg.TimeoutDuration(), CheckRedirect: checkRedirect},
		UserAgent:  cfg.UserAgent,
		MaxBody:    int64(cfg.MaxBodyKB) * 1024,
		SafeSearch: strings.ToLower(cfg.SafeSearch),
		Region:     cfg.Region,
	}
}

// Request describes an HTTP request.
type Request struct {
	Method  string
	URL     string
	Body    io.Reader
	Headers map[string]string
}

// HTTPError is a non-2xx response.
type HTTPError struct {
	Status int
	URL    string
	Body   string
}

func (e *HTTPError) Error() string {
	msg := fmt.Sprintf("HTTP %d from %s", e.Status, hostOf(e.URL))
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// Do performs a request and returns the (size-limited) body.
func (c *Client) Do(ctx context.Context, r Request) ([]byte, http.Header, error) {
	if r.Method == "" {
		r.Method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, r.Body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, redactErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.MaxBody))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet := safe.Line(strings.TrimSpace(string(body)))
		if len(snippet) > 200 || strings.Contains(snippet, "<") {
			snippet = ""
		}
		return body, resp.Header, &HTTPError{Status: resp.StatusCode, URL: r.URL, Body: snippet}
	}
	return body, resp.Header, nil
}

// Get fetches a URL.
func (c *Client) Get(ctx context.Context, u string, headers map[string]string) ([]byte, error) {
	b, _, err := c.Do(ctx, Request{URL: u, Headers: headers})
	return b, err
}

// JSON fetches a URL and decodes the JSON response into v.
func (c *Client) JSON(ctx context.Context, r Request, v any) error {
	if r.Headers == nil {
		r.Headers = map[string]string{}
	}
	if _, ok := r.Headers["Accept"]; !ok {
		r.Headers["Accept"] = "application/json"
	}
	b, _, err := c.Do(ctx, r)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("decode response from %s: %w", hostOf(r.URL), err)
	}
	return nil
}

// Doc fetches a URL and parses it as HTML.
func (c *Client) Doc(ctx context.Context, r Request) (*goquery.Document, error) {
	if r.Headers == nil {
		r.Headers = map[string]string{}
	}
	if _, ok := r.Headers["Accept"]; !ok {
		r.Headers["Accept"] = "text/html,application/xhtml+xml"
	}
	b, _, err := c.Do(ctx, r)
	if err != nil {
		return nil, err
	}
	return goquery.NewDocumentFromReader(strings.NewReader(string(b)))
}

var (
	tagRE   = regexp.MustCompile(`<[^>]*>`)
	spaceRE = regexp.MustCompile(`\s+`)
)

// CleanText strips HTML tags, unescapes entities and collapses whitespace.
func CleanText(s string) string {
	s = tagRE.ReplaceAllString(s, "")
	s = strings.ReplaceAll(html.UnescapeString(s), "\u00a0", " ")
	return strings.TrimSpace(spaceRE.ReplaceAllString(s, " "))
}

func hostOf(u string) string {
	s := u
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return s
}

// redactErr hides credentials that net/http includes in errors: *url.Error
// embeds the full request URL, and some APIs take keys as query parameters.
func redactErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		ue.URL = safe.RedactURL(ue.URL)
	}
	return err
}

// redirectSafeHeaders survive a redirect to another host or to plain HTTP.
var redirectSafeHeaders = map[string]bool{
	"User-Agent": true, "Accept": true, "Accept-Language": true, "Content-Type": true, "Referer": true,
}

// checkRedirect limits redirects and strips credentials when one leaves the
// original host or downgrades from https. net/http only drops Authorization
// and Cookie on cross-domain redirects, but engines also authenticate with
// custom headers (PRIVATE-TOKEN, X-Subscription-Token, ...), which it would
// otherwise forward to the new host.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	orig := via[0].URL
	if req.URL.Hostname() != orig.Hostname() || (orig.Scheme == "https" && req.URL.Scheme != "https") {
		for k := range req.Header {
			if !redirectSafeHeaders[http.CanonicalHeaderKey(k)] {
				req.Header.Del(k)
			}
		}
	}
	return nil
}
