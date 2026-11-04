package login

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// deviceServer fakes GitHub's device flow endpoints.
func deviceServer(t *testing.T, polls []string) *httptest.Server {
	t.Helper()
	var n atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/device", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("device form: %v", err)
		}
		if r.Form.Get("client_id") == "" || r.Form.Get("scope") == "" {
			t.Errorf("device request missing client_id/scope: %v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dev-123",
			"user_code":        "ABCD-1234",
			"verification_uri": "https://github.com/login/device",
			"expires_in":       900,
			"interval":         5,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if got := r.Form.Get("device_code"); got != "dev-123" {
			t.Errorf("device_code = %q", got)
		}
		if got := r.Form.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Errorf("grant_type = %q", got)
		}
		i := int(n.Add(1)) - 1
		if i < len(polls) {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": polls[i]})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "gho_test",
			"token_type":   "bearer",
			"scope":        "repo read:org",
		})
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer gho_test" {
			t.Errorf("Authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"login": "octocat"})
	})
	return httptest.NewServer(mux)
}

func withEndpoints(t *testing.T, srv *httptest.Server) {
	t.Helper()
	oldDevice, oldToken, oldAPI := githubDeviceURL, githubTokenURL, githubAPIURL
	githubDeviceURL = srv.URL + "/device"
	githubTokenURL = srv.URL + "/token"
	githubAPIURL = srv.URL
	t.Cleanup(func() { githubDeviceURL, githubTokenURL, githubAPIURL = oldDevice, oldToken, oldAPI })
}

func TestDeviceFlow(t *testing.T) {
	srv := deviceServer(t, []string{"authorization_pending"})
	defer srv.Close()
	withEndpoints(t, srv)

	p, ok := Lookup("github")
	if !ok {
		t.Fatal("github provider missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dc, err := StartDevice(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if dc.UserCode != "ABCD-1234" || dc.VerificationURI != "https://github.com/login/device" || dc.DeviceCode != "dev-123" {
		t.Fatalf("device code = %+v", dc)
	}
	dc.Interval = time.Millisecond // keep the test fast
	e, err := WaitDevice(ctx, p, dc)
	if err != nil {
		t.Fatal(err)
	}
	if e.Values["token"] != "gho_test" || e.Account != "octocat" || e.Method != string(MethodDevice) {
		t.Fatalf("entry = %+v", e)
	}
	if strings.Join(e.Scopes, " ") != "repo read:org" {
		t.Errorf("scopes = %v", e.Scopes)
	}
}

func TestDeviceFlowDenied(t *testing.T) {
	srv := deviceServer(t, []string{"access_denied"})
	defer srv.Close()
	withEndpoints(t, srv)

	p, _ := Lookup("github")
	dc, err := StartDevice(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	dc.Interval = time.Millisecond
	if _, err := WaitDevice(context.Background(), p, dc); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeviceFlowExpired(t *testing.T) {
	srv := deviceServer(t, []string{"expired_token"})
	defer srv.Close()
	withEndpoints(t, srv)

	p, _ := Lookup("github")
	dc, err := StartDevice(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	dc.Interval = time.Millisecond
	if _, err := WaitDevice(context.Background(), p, dc); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateKagi(t *testing.T) {
	oldAPI, oldHTML := kagiAPIURL, kagiHTMLURL
	defer func() { kagiAPIURL, kagiHTMLURL = oldAPI, oldHTML }()

	var htmlMode string // "results", "challenge", "signin"
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["query"] == nil || body["limit"] != float64(1) {
			t.Errorf("probe body = %v", body)
		}
		if r.Header.Get("Authorization") != "Bearer good-key" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":[{"code":"general.invalid_token","message":"Token signature failed to verify"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"search":[]}}`))
	}))
	defer api.Close()
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Kagi-Authorization"); got == "" {
			t.Errorf("missing X-Kagi-Authorization")
		}
		if got := r.Header.Get("Cookie"); !strings.Contains(got, "kagi_session=") {
			t.Errorf("Cookie = %q", got)
		}
		switch htmlMode {
		case "results":
			_, _ = w.Write([]byte(`<html><body><div class="_0_SRI"><a class="__sri_title_link" href="https://x.example">X</a></div></body></html>`))
		case "challenge":
			w.Header().Set("Location", "/turnstile?r=/html/search")
			w.WriteHeader(http.StatusFound)
		case "signin":
			w.Header().Set("Location", "/signin?p=invalid_token")
			w.WriteHeader(http.StatusFound)
		}
	}))
	defer html.Close()
	kagiAPIURL, kagiHTMLURL = api.URL, html.URL
	p, _ := Lookup("kagi")

	values, account, note, err := Validate(context.Background(), p, map[string]string{"credential": "good-key"})
	if err != nil || values["api_key"] != "good-key" || values["token"] != "" || note != "" || account != "" {
		t.Fatalf("api key: values=%v note=%q err=%v", values, note, err)
	}

	// A Session Link token is not an API key; it is stored as a session.
	htmlMode = "results"
	values, _, note, err = Validate(context.Background(), p, map[string]string{"credential": "https://kagi.com/search?token=session-abc&q=test"})
	if err != nil {
		t.Fatal(err)
	}
	if values["token"] != "session-abc" || values["api_key"] != "" {
		t.Fatalf("session values = %v", values)
	}
	if !strings.Contains(note, "subscription") {
		t.Errorf("note = %q", note)
	}
	if MethodFor(p, values) != "session" {
		t.Errorf("method = %q", MethodFor(p, values))
	}

	// A bot challenge says nothing about the token: save it, but warn.
	htmlMode = "challenge"
	values, _, note, err = Validate(context.Background(), p, map[string]string{"credential": "session-abc"})
	if err != nil {
		t.Fatal(err)
	}
	if values["token"] != "session-abc" || !strings.Contains(note, "bot-challenged") {
		t.Fatalf("challenge: values=%v note=%q", values, note)
	}

	// A redirect to sign-in means the token is dead.
	htmlMode = "signin"
	if _, _, _, err := Validate(context.Background(), p, map[string]string{"credential": "session-abc"}); err == nil {
		t.Fatal("want an error for a rejected session token")
	}
}

func TestLookupAndNames(t *testing.T) {
	if _, ok := Lookup("Kagi"); !ok {
		t.Error("lookup should be case-insensitive")
	}
	if _, ok := Lookup("!github"); !ok {
		t.Error("lookup should accept a bang")
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("unknown provider matched")
	}
	for _, n := range []string{"kagi", "github", "brave", "google", "mojeek", "gitlab"} {
		if _, ok := Lookup(n); !ok {
			t.Errorf("%s missing from the provider table", n)
		}
	}
}
