package config

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func authFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "auth.json")
	t.Setenv("SEARCH_TUI_AUTH", p)
	return p
}

func TestAuthRoundTrip(t *testing.T) {
	path := authFile(t)
	a, err := LoadAuth()
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Providers) != 0 {
		t.Fatalf("fresh store should be empty: %+v", a.Providers)
	}
	if err := a.Set("kagi", AuthEntry{Method: "api_key", Values: map[string]string{"api_key": "secret-key"}}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no POSIX permission bits: Chmod(0600) leaves 0666 there.
	if runtime.GOOS != "windows" {
		if perm := st.Mode().Perm(); perm != 0o600 {
			t.Errorf("auth file mode = %o, want 600", perm)
		}
	}
	b, err := LoadAuth()
	if err != nil {
		t.Fatal(err)
	}
	e, ok := b.Get("kagi")
	if !ok || e.Values["api_key"] != "secret-key" || e.Method != "api_key" || e.Updated.IsZero() {
		t.Fatalf("entry = %+v", e)
	}
	if got := Credential("kagi", "api_key"); got != "secret-key" {
		t.Errorf("Credential = %q", got)
	}
	if got := Credential("kagi", "token"); got != "" {
		t.Errorf("token should not be set, got %q", got)
	}
	if got := Credential("brave", "api_key"); got != "" {
		t.Errorf("unknown provider = %q", got)
	}
}

func TestAuthFileCorrupt(t *testing.T) {
	path := authFile(t)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAuth(); err == nil {
		t.Fatal("want a decode error")
	}
	// A corrupt store must not stop a search: lookups just miss.
	if got := Credential("kagi", "api_key"); got != "" {
		t.Errorf("Credential on corrupt file = %q", got)
	}
}

func TestSecretFallsBackToAuthFile(t *testing.T) {
	authFile(t)
	a, _ := LoadAuth()
	if err := a.Set("kagi", AuthEntry{Method: "api_key", Values: map[string]string{"api_key": "from-login"}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse("")
	if err != nil {
		t.Fatal(err)
	}
	ec := cfg.EngineConfig("kagi")
	if !ec.HasSecret("api_key") {
		t.Error("HasSecret should see the login credential")
	}
	got, err := ec.Secret(context.Background(), "api_key")
	if err != nil || got != "from-login" {
		t.Fatalf("Secret = %q, %v", got, err)
	}

	// An explicit config value always wins over the auth file.
	cfg2, err := Parse("[engines.kagi]\napi_key = \"from-config\"\n")
	if err != nil {
		t.Fatal(err)
	}
	got, _ = cfg2.EngineConfig("kagi").Secret(context.Background(), "api_key")
	if got != "from-config" {
		t.Errorf("explicit config should win, got %q", got)
	}
}

func TestClearSecrets(t *testing.T) {
	authFile(t)
	a, _ := LoadAuth()
	if err := a.Set("kagi", AuthEntry{Method: "api_key", Values: map[string]string{"api_key": "old"}}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := Parse("")
	ec := cfg.EngineConfig("kagi")
	if v, _ := ec.Secret(context.Background(), "api_key"); v != "old" {
		t.Fatalf("Secret = %q", v)
	}
	// :login overwrites the entry; the cached value must be dropped.
	a, _ = LoadAuth()
	if err := a.Set("kagi", AuthEntry{Method: "api_key", Values: map[string]string{"api_key": "new"}}); err != nil {
		t.Fatal(err)
	}
	if v, _ := ec.Secret(context.Background(), "api_key"); v != "old" {
		t.Fatalf("cached value should still be old, got %q", v)
	}
	ec.ClearSecrets()
	if v, _ := ec.Secret(context.Background(), "api_key"); v != "new" {
		t.Errorf("after ClearSecrets Secret = %q", v)
	}
}

func TestAuthPathEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "custom.json")
	t.Setenv("SEARCH_TUI_AUTH", p)
	if got := AuthPath(); got != p {
		t.Errorf("AuthPath = %q, want %q", got, p)
	}
	t.Setenv("SEARCH_TUI_AUTH", "")
	if got := AuthPath(); !strings.HasSuffix(got, filepath.Join("search-tui", "auth.json")) {
		t.Errorf("default AuthPath = %q", got)
	}
}
