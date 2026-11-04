package config

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	c := Default()
	if c.DefaultEngine != "duckduckgo" || c.Results != 20 || !c.SummaryEnabled() || !*c.UI.Mouse {
		t.Errorf("bad defaults: %+v", c)
	}
	lp, _ := Parse("low_power = true")
	if lp.Results != 10 || *lp.UI.ShowSummary {
		t.Errorf("low_power defaults not applied: results=%d", lp.Results)
	}
}

func TestExampleParses(t *testing.T) {
	if _, err := Parse(Example); err != nil {
		t.Fatalf("example config invalid: %v", err)
	}
}

func TestSecrets(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "tok")
	if err := os.WriteFile(f, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TS_TEST_KEY", "from-env")
	c, err := Parse(`
[engines.a]
api_key = "literal"
[engines.b]
api_key_env = "TS_TEST_KEY"
[engines.c]
token_file = "` + filepath.ToSlash(f) + `"
[engines.d]
token_command = "echo from-command"
`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	check := func(eng, name, want string) {
		t.Helper()
		got, err := c.EngineConfig(eng).Secret(ctx, name)
		if err != nil || got != want {
			t.Errorf("%s.%s = %q, %v; want %q", eng, name, got, err, want)
		}
	}
	check("a", "api_key", "literal")
	check("b", "api_key", "from-env")
	check("c", "token", "from-file")
	if runtime.GOOS != "windows" {
		check("d", "token", "from-command")
	}
	check("missing", "token", "")
}

func TestLoadMissingDefault(t *testing.T) {
	t.Setenv("TUI_SEARCH_CONFIG", filepath.Join(t.TempDir(), "nope.toml"))
	c, err := Load("")
	if err != nil || c.DefaultEngine != "duckduckgo" {
		t.Errorf("missing default config should yield defaults: %v", err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Error("missing explicit config should error")
	}
}

func TestSecretPermissionWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[engines.brave]\napi_key = \"literal\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// WriteFile honours the process umask; make the intended exposure explicit.
	if err := os.Chmod(p, 0o644); err != nil { //nolint:gosec // G302: deliberately world-readable fixture to test the warning
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if w := c.secretPermissionWarning(); !strings.Contains(w, "brave") || !strings.Contains(w, "chmod 600") {
		t.Errorf("expected a warning, got %q", w)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if w := c.secretPermissionWarning(); w != "" {
		t.Errorf("0600 should not warn: %q", w)
	}
	// Indirect credentials are fine at any mode.
	if err := os.WriteFile(p, []byte("[engines.brave]\napi_key_command = \"pass show brave\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(p)
	if w := c.secretPermissionWarning(); w != "" {
		t.Errorf("*_command should not warn: %q", w)
	}
}
