// Package login implements the interactive credential flows behind the TUI's
// :login command. Credentials are stored in the central auth file
// ($XDG_CONFIG_HOME/search-tui/auth.json) by the config package.
package login

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/heywoodlh/search-tui/internal/config"
)

// Method is how a provider hands out credentials.
type Method string

const (
	// MethodKey is a value the user pastes from the provider's settings page.
	MethodKey Method = "api_key"
	// MethodDevice is an OAuth device flow: the user visits a URL and enters
	// a short code, and the client polls for the token.
	MethodDevice Method = "device"
)

// Field is one credential a provider needs.
type Field struct {
	Credential string // api_key, token or cx (see config.Engine.Secret)
	Label      string // prompt shown by :login
	Secret     bool   // mask the typed value
}

// Provider describes how to log in to one engine.
type Provider struct {
	Engine string
	Title  string
	Method Method
	Fields []Field
	// Docs is where the user creates the credential.
	Docs string
	// Notes are provider-specific oddities shown before the prompt.
	Notes []string
	// Scopes are requested by the device flow.
	Scopes []string
}

var providers = []Provider{
	{
		Engine: "kagi",
		Title:  "Kagi",
		Method: MethodKey,
		Fields: []Field{{Credential: "credential", Label: "Kagi Search API key or Session Link", Secret: true}},
		Docs:   "https://kagi.com/settings/api",
		Notes: []string{
			"A Session Link (kagi.com/settings → Account) uses your Kagi plan and costs nothing extra; it works through the no-JS HTML interface.",
			"An API key (kagi.com/settings/api) uses the official Search API, which is billed separately at ~$12/1000 queries.",
			"Paste either one; the difference is detected. Session Links are unofficial (Kagi documents them for browsers) and may hit a bot challenge on some networks.",
		},
	},
	{
		Engine: "github",
		Title:  "GitHub",
		Method: MethodDevice,
		Docs:   "https://github.com/login/device",
		Scopes: []string{"repo", "read:org", "gist"},
		Notes: []string{
			"Device flow of GitHub's public CLI OAuth app: no client secret is stored, and the token can be revoked under Settings → Applications.",
			"Set SEARCH_TUI_GITHUB_CLIENT_ID to use your own OAuth app instead (device flow must be enabled on it).",
		},
	},
	{
		Engine: "brave",
		Title:  "Brave Search",
		Method: MethodKey,
		Fields: []Field{{Credential: "api_key", Label: "Brave Search API key", Secret: true}},
		Docs:   "https://api.search.brave.com/app/keys",
		Notes:  []string{"The free plan requires a card on file and is rate limited."},
	},
	{
		Engine: "mojeek",
		Title:  "Mojeek",
		Method: MethodKey,
		Fields: []Field{{Credential: "api_key", Label: "Mojeek Search API key", Secret: true}},
		Docs:   "https://www.mojeek.com/services/search/web-search-api/",
		Notes:  []string{"The API is a paid service; Mojeek's HTML endpoint is captcha'd and cannot be scraped."},
	},
	{
		Engine: "google",
		Title:  "Google Programmable Search",
		Method: MethodKey,
		Fields: []Field{
			{Credential: "api_key", Label: "Google API key", Secret: true},
			{Credential: "cx", Label: "Programmable Search engine ID (cx)", Secret: false},
		},
		Docs:  "https://programmablesearchengine.google.com/controlpanel/all",
		Notes: []string{"Google blocks scraping, so both values are required: the API key and the engine ID of a search engine that searches the whole web."},
	},
	{
		Engine: "gitlab",
		Title:  "GitLab",
		Method: MethodKey,
		Fields: []Field{{Credential: "token", Label: "GitLab personal access token", Secret: true}},
		Docs:   "https://gitlab.com/-/user_settings/personal_access_tokens",
		Notes: []string{
			"Needs the read_api scope. For a self-hosted instance set [engines.gitlab] base_url in config.toml first.",
		},
	},
}

// Providers returns the supported providers in display order.
func Providers() []Provider { return providers }

// Names returns the provider names, for completion and error messages.
func Names() []string {
	out := make([]string, len(providers))
	for i, p := range providers {
		out[i] = p.Engine
	}
	return out
}

// Lookup finds a provider by engine name or title, case-insensitively.
func Lookup(name string) (Provider, bool) {
	name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, "!")))
	for _, p := range providers {
		if p.Engine == name || strings.ToLower(p.Title) == name {
			return p, true
		}
	}
	return Provider{}, false
}

// Save writes the provider's credentials to the auth file. The recorded method
// is derived from the values, because one prompt can produce different kinds of
// credential (Kagi: an API key or a Session Link token).
func Save(p Provider, values map[string]string, account string, scopes []string) error {
	a, err := config.LoadAuth()
	if err != nil {
		return err
	}
	return a.Set(p.Engine, config.AuthEntry{
		Method:  MethodFor(p, values),
		Values:  values,
		Account: account,
		Scopes:  scopes,
	})
}

// MethodFor names the credential kind stored for a provider.
func MethodFor(p Provider, values map[string]string) string {
	switch {
	case p.Engine == "kagi" && values["token"] != "":
		return "session"
	case p.Method == MethodDevice:
		return string(MethodDevice)
	}
	return string(p.Method)
}

// Endpoints are variables so tests can point them at httptest servers.
var (
	httpClient      = &http.Client{Timeout: 30 * time.Second}
	githubDeviceURL = "https://github.com/login/device/code"
	githubGrantURL  = "https://github.com/login/oauth/access_token"
	githubAPIURL    = "https://api.github.com"
	kagiAPIURL      = "https://kagi.com/api/v1/search"
	kagiHTMLURL     = "https://kagi.com/html/search"
	// kagiProbe is the query used to check a pasted key.
	kagiProbe = "kagi"
)

// GitHubClientID is the OAuth app used by the device flow. It is the GitHub
// CLI's public client id, which allows device flow without registering an app;
// override it with SEARCH_TUI_GITHUB_CLIENT_ID.
func GitHubClientID() string {
	if id := strings.TrimSpace(os.Getenv("SEARCH_TUI_GITHUB_CLIENT_ID")); id != "" {
		return id
	}
	return "178c6fc778ccc68e1d6a"
}

// DeviceCode is a started OAuth device flow.
type DeviceCode struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	ExpiresIn       time.Duration
}

// StartDevice begins a device flow for p.
func StartDevice(ctx context.Context, p Provider) (DeviceCode, error) {
	if p.Method != MethodDevice {
		return DeviceCode{}, fmt.Errorf("%s does not use a device flow", p.Engine)
	}
	form := url.Values{
		"client_id": {GitHubClientID()},
		"scope":     {strings.Join(p.Scopes, " ")},
	}
	var resp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := postForm(ctx, githubDeviceURL, form, &resp); err != nil {
		return DeviceCode{}, err
	}
	if resp.DeviceCode == "" || resp.UserCode == "" {
		return DeviceCode{}, errors.New("github did not return a device code")
	}
	dc := DeviceCode{
		DeviceCode:      resp.DeviceCode,
		UserCode:        resp.UserCode,
		VerificationURI: resp.VerificationURI,
		Interval:        time.Duration(max(resp.Interval, 1)) * time.Second,
		ExpiresIn:       time.Duration(max(resp.ExpiresIn, 60)) * time.Second,
	}
	return dc, nil
}

// WaitDevice polls until the user finishes the device flow, and returns the
// entry to store.
func WaitDevice(ctx context.Context, p Provider, dc DeviceCode) (config.AuthEntry, error) {
	interval := dc.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(dc.ExpiresIn)
	if dc.ExpiresIn <= 0 {
		deadline = time.Now().Add(15 * time.Minute)
	}
	for {
		if time.Now().After(deadline) {
			return config.AuthEntry{}, errors.New("the device code expired; run :login github again")
		}
		select {
		case <-ctx.Done():
			return config.AuthEntry{}, ctx.Err()
		case <-time.After(interval):
		}
		form := url.Values{
			"client_id":   {GitHubClientID()},
			"device_code": {dc.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}
		var resp struct {
			AccessToken string `json:"access_token"`
			Scope       string `json:"scope"`
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if err := postForm(ctx, githubGrantURL, form, &resp); err != nil {
			return config.AuthEntry{}, err
		}
		switch resp.Error {
		case "":
			if resp.AccessToken == "" {
				return config.AuthEntry{}, errors.New("github returned no token")
			}
			e := config.AuthEntry{
				Method: string(MethodDevice),
				Values: map[string]string{"token": resp.AccessToken},
				Scopes: strings.Fields(resp.Scope),
			}
			e.Account = githubAccount(ctx, resp.AccessToken)
			return e, nil
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
		case "expired_token":
			return config.AuthEntry{}, errors.New("the device code expired; run :login github again")
		case "access_denied":
			return config.AuthEntry{}, errors.New("authorization was denied")
		default:
			msg := resp.Description
			if msg == "" {
				msg = resp.Error
			}
			return config.AuthEntry{}, errors.New("github: " + msg)
		}
	}
}

// Validate checks a pasted credential and returns the values to store, the
// account name when the provider reports one, and a note explaining anything
// the user should know (e.g. a credential that could not be verified). A
// provider may rename the credential: Kagi accepts either an API key or a
// Session Link token in the same prompt.
func Validate(ctx context.Context, p Provider, values map[string]string) (map[string]string, string, string, error) {
	if p.Engine == "kagi" {
		return validateKagi(ctx, values["credential"])
	}
	return values, "", "", nil
}

// validateKagi decides which kind of Kagi credential was pasted: an API key
// (billed Search API) or a Session Link token (subscription, HTML interface).
func validateKagi(ctx context.Context, value string) (map[string]string, string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, "", "", errors.New("empty credential")
	}
	err := kagiKeyProbe(ctx, value)
	if err == nil {
		return map[string]string{"api_key": value}, "", "", nil
	}
	var rejected *authError
	if !errors.As(err, &rejected) {
		return nil, "", "", err
	}
	// Not an API key. A Session Link may be pasted as a URL or as the bare
	// token, so normalise before probing the HTML interface.
	tok := sessionToken(value)
	verdict, err := kagiSessionProbe(ctx, tok)
	if err != nil {
		return nil, "", "", err
	}
	switch verdict {
	case sessionValid:
		return map[string]string{"token": tok}, "", "using your Kagi subscription through a Session Link (no API billing)", nil
	case sessionChallenged:
		return map[string]string{"token": tok}, "", "saved, but Kagi bot-challenged the check from this network; if searches fail, use an API key instead", nil
	default:
		return nil, "", "", errors.New("kagi rejected the credential: neither a Search API key nor a valid Session Link")
	}
}

// authError marks a credential the provider refused (as opposed to a network
// failure), so the login flow can try the other credential type.
type authError struct{ msg string }

func (e *authError) Error() string { return e.msg }

type sessionVerdict int

const (
	sessionInvalid sessionVerdict = iota
	sessionValid
	sessionChallenged
)

// kagiKeyProbe checks the value against the v1 Search API.
func kagiKeyProbe(ctx context.Context, key string) error {
	body, _ := json.Marshal(map[string]any{"query": kagiProbe, "limit": 1})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, kagiAPIURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	var e struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(raw, &e)
	msg := fmt.Sprintf("kagi rejected the key (HTTP %d)", resp.StatusCode)
	if len(e.Errors) > 0 && e.Errors[0].Message != "" {
		msg = e.Errors[0].Message
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden ||
		(len(e.Errors) > 0 && strings.Contains(e.Errors[0].Code, "invalid_token")) {
		return &authError{msg: msg}
	}
	return errors.New(msg)
}

// kagiSessionProbe asks the HTML interface with a Session Link token. A bot
// challenge is reported separately: it says nothing about the token itself.
func kagiSessionProbe(ctx context.Context, tok string) (sessionVerdict, error) {
	noRedirect := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kagiHTMLURL+"?q="+url.QueryEscape(kagiProbe), nil)
	if err != nil {
		return sessionInvalid, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Safari/605.1.15")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Cookie", "kagi_session="+tok)
	req.Header.Set("X-Kagi-Authorization", tok)
	resp, err := noRedirect.Do(req)
	if err != nil {
		return sessionInvalid, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	body := strings.ToLower(string(raw))
	if loc := strings.ToLower(resp.Header.Get("Location")); loc != "" {
		switch {
		case strings.Contains(loc, "turnstile"), strings.Contains(loc, "challenge"):
			return sessionChallenged, nil
		case strings.Contains(loc, "signin"), strings.Contains(loc, "welcome"), strings.Contains(loc, "login"):
			return sessionInvalid, nil
		}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return sessionInvalid, nil
	}
	switch {
	case strings.Contains(body, "turnstile"), strings.Contains(body, "challenge-platform"):
		return sessionChallenged, nil
	case strings.Contains(body, "_0_sri"), strings.Contains(body, "search-result"):
		return sessionValid, nil
	}
	return sessionInvalid, nil
}

// sessionToken accepts a bare token or a full Session Link URL.
func sessionToken(v string) string {
	if u, err := url.Parse(v); err == nil && u.Query().Get("token") != "" {
		return u.Query().Get("token")
	}
	return v
}

// githubAccount returns the login owning token, or "" when it cannot be read.
func githubAccount(ctx context.Context, token string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIURL+"/user", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var u struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&u); err != nil {
		return ""
	}
	return u.Login
}

func postForm(ctx context.Context, endpoint string, form url.Values, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
	}
	return json.Unmarshal(raw, v)
}
