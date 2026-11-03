package engine

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/heywoodlh/tui-search/internal/config"
)

func redirectReq(t *testing.T, from, to string, hdr map[string]string) *http.Request {
	t.Helper()
	orig, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, from, nil)
	next, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, to, nil)
	for k, v := range hdr {
		next.Header.Set(k, v)
	}
	if err := checkRedirect(next, []*http.Request{orig}); err != nil {
		t.Fatal(err)
	}
	return next
}

func TestRedirectStripsCredentials(t *testing.T) {
	creds := map[string]string{"PRIVATE-TOKEN": "glpat", "X-Subscription-Token": "brave", "Authorization": "Bearer x", "Cookie": "kagi_session=x", "User-Agent": "ua"}
	for _, tc := range []struct {
		name, from, to string
		keep           bool
	}{
		{"same host", "https://gitlab.example.com/a", "https://gitlab.example.com/b", true},
		{"other host", "https://gitlab.example.com/a", "https://evil.example.net/b", false},
		{"subdomain", "https://gitlab.example.com/a", "https://cdn.gitlab.example.com/b", false},
		{"https downgrade", "https://gitlab.example.com/a", "http://gitlab.example.com/b", false},
	} {
		r := redirectReq(t, tc.from, tc.to, creds)
		for _, h := range []string{"PRIVATE-TOKEN", "X-Subscription-Token", "Authorization", "Cookie"} {
			if got := r.Header.Get(h) != ""; got != tc.keep {
				t.Errorf("%s: header %s kept=%v, want %v", tc.name, h, got, tc.keep)
			}
		}
		if r.Header.Get("User-Agent") == "" {
			t.Errorf("%s: harmless headers should survive", tc.name)
		}
	}
	// Redirect loops are bounded.
	orig, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://a.example/", nil)
	via := make([]*http.Request, 10)
	for i := range via {
		via[i] = orig
	}
	if checkRedirect(orig, via) == nil {
		t.Error("expected an error after 10 redirects")
	}
}

func TestErrorsDoNotLeakKeys(t *testing.T) {
	c := NewClient(config.Default())
	_, err := c.Get(context.Background(), "http://127.0.0.1:1/customsearch/v1?key=SUPERSECRET&q=x", nil)
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if strings.Contains(err.Error(), "SUPERSECRET") {
		t.Errorf("API key leaked in error: %v", err)
	}
	var ue *url.Error
	if !strings.Contains(err.Error(), "REDACTED") && !strings.Contains(err.Error(), "q=x") {
		t.Errorf("unexpected error text: %v (%T, %v)", err, err, ue)
	}
}

func TestResultSanitize(t *testing.T) {
	r := Result{
		Title: "repo\x1b]52;c;cm0gLXJmIH4=\x07", URL: "https://x/\x1b[2J", Snippet: "desc\u202eevil\nline", Meta: "m\u009b31m",
		Code: &CodeMatch{Path: "a\x1b.go", Fragments: []Fragment{{Text: "ok\n\tcode\x1b]0;title\x07\r"}}},
	}
	r.Sanitize()
	all := r.Title + r.URL + r.Snippet + r.Meta + r.Code.Path + r.Code.Fragments[0].Text
	if strings.ContainsAny(all, "\x1b\x07\u009b\u202e\r") {
		t.Errorf("control characters survived: %q", all)
	}
	if r.Code.Fragments[0].Text != "ok\n\tcode]0;title" {
		t.Errorf("code layout should keep newlines/tabs: %q", r.Code.Fragments[0].Text)
	}
	if strings.Contains(r.Snippet, "\n") {
		t.Error("snippets are single-line")
	}
}
