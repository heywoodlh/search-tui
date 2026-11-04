package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heywoodlh/search-tui/internal/config"
)

func newKagiEngine(t *testing.T, ec *config.Engine, apiURL, htmlURL string) *Kagi {
	t.Helper()
	cfg, err := config.Parse("")
	if err != nil {
		t.Fatal(err)
	}
	return &Kagi{base: mk("kagi", NewClient(cfg), ec, KindWeb), apiURL: apiURL, htmlURL: htmlURL}
}

// TestKagiAPIV1 checks the v1 Search API request and response shapes.
func TestKagiAPIV1(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &body); err != nil {
			t.Fatalf("body is not JSON: %v (%s)", err, b)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"meta":{"ms":12},"data":{"search":[
			{"url":"https://one.example","title":"One &amp; only","snippet":"<strong>first</strong> result"},
			{"url":"https://two.example","title":"Two","snippet":null},
			{"title":"no url"}],"related_search":{"list":["a"]}}}`))
	}))
	defer srv.Close()

	k := newKagiEngine(t, &config.Engine{APIKey: "secret-key"}, srv.URL, "")
	rs, err := k.Search(context.Background(), Query{Text: "nixos flakes", Limit: 5, Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	if body["query"] != "nixos flakes" || body["limit"] != float64(5) || body["page"] != float64(2) {
		t.Errorf("request body = %v", body)
	}
	if len(rs) != 2 {
		t.Fatalf("want 2 results (one without a URL skipped), got %d: %+v", len(rs), rs)
	}
	if rs[0].Title != "One & only" || rs[0].Snippet != "first result" {
		t.Errorf("entities/tags not cleaned: %+v", rs[0])
	}
	if rs[0].Engine != "kagi" {
		t.Errorf("engine = %q", rs[0].Engine)
	}
}

// TestKagiTokenAsAPIKey covers configs that keep the Search API key in
// token_file: the token is tried against the API before the HTML fallback.
func TestKagiTokenAsAPIKey(t *testing.T) {
	htmlHits := 0
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlHits++
	}))
	defer html.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer session-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"search":[{"url":"https://one.example","title":"One"}]}}`))
	}))
	defer api.Close()

	k := newKagiEngine(t, &config.Engine{Token: "session-token"}, api.URL, html.URL)
	rs, err := k.Search(context.Background(), Query{Text: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Title != "One" {
		t.Errorf("results = %+v", rs)
	}
	if htmlHits != 0 {
		t.Errorf("HTML fallback used although the API accepted the token")
	}
}

// TestKagiTokenFallsBackToHTML covers real Session Link tokens, which the API
// rejects but the no-JS interface accepts.
func TestKagiTokenFallsBackToHTML(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":null,"errors":[{"code":"general.invalid_token","message":"Token signature failed to verify"}]}`))
	}))
	defer api.Close()
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Cookie"); got != "kagi_session=session-token" {
			t.Errorf("Cookie = %q", got)
		}
		if got := r.URL.Query().Get("q"); got != "nixos" {
			t.Errorf("q = %q", got)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>
			<div class="search-result"><a class="__sri_title_link" href="https://one.example">One</a><div class="__sri-desc">first</div></div>
			<div class="_0_SRI"><a class="__sri_title_link" href="https://two.example">Two</a></div>
		</body></html>`))
	}))
	defer html.Close()

	k := newKagiEngine(t, &config.Engine{Token: "session-token"}, api.URL, html.URL)
	rs, err := k.Search(context.Background(), Query{Text: "nixos"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].URL != "https://one.example" || rs[1].Title != "Two" {
		t.Errorf("results = %+v", rs)
	}
}

// TestKagiRejectedKeyIsAuthError checks the message users see for a bad key.
func TestKagiRejectedKeyIsAuthError(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"code":"general.unauthorized","message":"Unauthorized"}]}`))
	}))
	defer api.Close()

	k := newKagiEngine(t, &config.Engine{APIKey: "bad"}, api.URL, "")
	_, err := k.Search(context.Background(), Query{Text: "q"})
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("want AuthError, got %v", err)
	}
	if !strings.Contains(ae.Hint, "api_key") {
		t.Errorf("hint = %q", ae.Hint)
	}
}

// TestKagiSessionSkipsAPIProbe checks that a credential :login recorded as a
// Session Link goes straight to the HTML interface, and that the request uses
// the extension's auth header as well as the cookie.
func TestKagiSessionSkipsAPIProbe(t *testing.T) {
	authPath := filepath.Join(t.TempDir(), "auth.json")
	t.Setenv("SEARCH_TUI_AUTH", authPath)
	if err := os.WriteFile(authPath, []byte(`{"version":1,"providers":{"kagi":{"method":"session","values":{"token":"sess-token"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	apiHits := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiHits++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer api.Close()
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Kagi-Authorization"); got != "sess-token" {
			t.Errorf("X-Kagi-Authorization = %q", got)
		}
		if got := r.Header.Get("Cookie"); got != "kagi_session=sess-token" {
			t.Errorf("Cookie = %q", got)
		}
		_, _ = w.Write([]byte(`<html><body><div class="search-result"><a class="__sri_title_link" href="https://one.example">One</a></div></body></html>`))
	}))
	defer html.Close()

	cfg, err := config.Parse("")
	if err != nil {
		t.Fatal(err)
	}
	k := &Kagi{base: mk("kagi", NewClient(cfg), cfg.EngineConfig("kagi"), KindWeb), apiURL: api.URL, htmlURL: html.URL}
	rs, err := k.Search(context.Background(), Query{Text: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Title != "One" {
		t.Errorf("results = %+v", rs)
	}
	if apiHits != 0 {
		t.Errorf("the paid API was probed %d times for a session credential", apiHits)
	}
}

// TestKagiChallengeIsAuthError checks that a bot-challenge page is reported as
// an authentication problem, not as an empty result set.
func TestKagiChallengeIsAuthError(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"code":"general.invalid_token","message":"bad"}]}`))
	}))
	defer api.Close()
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Kagi Search</title></head><body>
			<div>Please complete the turnstile challenge to continue.</div></body></html>`))
	}))
	defer html.Close()

	k := newKagiEngine(t, &config.Engine{Token: "session-token"}, api.URL, html.URL)
	_, err := k.Search(context.Background(), Query{Text: "q"})
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("want AuthError, got %v", err)
	}
}
