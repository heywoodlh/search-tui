package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/heywoodlh/search-tui/internal/app"
	"github.com/heywoodlh/search-tui/internal/config"
	"github.com/heywoodlh/search-tui/internal/login"
)

func loginRoot(t *testing.T) *Root {
	t.Helper()
	a, err := app.New(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	return NewRoot(a, "", app.PlanOptions{})
}

func TestLoginRequiresProvider(t *testing.T) {
	r := loginRoot(t)
	r.execute("login")
	if r.focus.err == nil || !strings.Contains(r.focus.err.Error(), "Argument required") {
		t.Fatalf("err = %v", r.focus.err)
	}
	r.execute("login nosuch")
	if r.focus.err == nil || !strings.Contains(r.focus.err.Error(), "Unknown provider") {
		t.Fatalf("err = %v", r.focus.err)
	}
	if r.cmd.active {
		t.Error("a rejected :login must not open a prompt")
	}
}

func TestLoginPromptSavesCredential(t *testing.T) {
	authPath := filepath.Join(t.TempDir(), "auth.json")
	t.Setenv("SEARCH_TUI_AUTH", authPath)
	r := loginRoot(t)

	r.execute("login brave")
	if !r.cmd.active || r.cmd.prompt == "" {
		t.Fatal("prompt did not open")
	}
	if !strings.Contains(r.cmd.prompt, "Brave") {
		t.Errorf("prompt = %q", r.cmd.prompt)
	}
	for _, ch := range "brv-secret" {
		r.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	if strings.Contains(r.cmdView(), "brv-secret") {
		t.Error("secret is echoed on screen")
	}
	cmd := r.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("no command returned on submit")
	}
	msg, ok := cmd().(loginDoneMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	r.Update(msg)
	if r.focus.err != nil {
		t.Fatalf("login failed: %v", r.focus.err)
	}

	b, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "brv-secret") || !strings.Contains(string(b), `"brave"`) {
		t.Errorf("auth file = %s", b)
	}
	if _, ok := r.app.Reg.Get("brave"); !ok {
		t.Error("brave should be registered after :login")
	}
	got, err := r.app.Cfg.EngineConfig("brave").Secret(context.Background(), "api_key")
	if err != nil || got != "brv-secret" {
		t.Errorf("engine secret = %q, %v", got, err)
	}
}

func TestLoginCancelWritesNothing(t *testing.T) {
	authPath := filepath.Join(t.TempDir(), "auth.json")
	t.Setenv("SEARCH_TUI_AUTH", authPath)
	r := loginRoot(t)

	r.execute("login kagi")
	if !r.cmd.active {
		t.Fatal("prompt did not open")
	}
	if cmd := r.handleKey(tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		// The flash tick is a tea.Tick; don't wait on it.
		_ = cmd
	}
	if _, err := os.Stat(authPath); !os.IsNotExist(err) {
		t.Errorf("cancelled login created %s", authPath)
	}
	if r.focus.flash != "login cancelled" {
		t.Errorf("flash = %q", r.focus.flash)
	}
}

// TestLoginKeys drives the real key path: esc leaves the search bar, ":" opens
// the command line, and the provider name runs :login.
func TestLoginKeys(t *testing.T) {
	t.Setenv("SEARCH_TUI_AUTH", filepath.Join(t.TempDir(), "auth.json"))
	r := loginRoot(t)
	r.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	r.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if r.focus.mode != modeNormal {
		t.Fatalf("esc did not leave insert mode (mode %d)", r.focus.mode)
	}
	r.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	if !r.cmd.active {
		t.Fatal(": did not open the command line")
	}
	for _, ch := range "login brave" {
		r.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	r.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !r.cmd.active || r.cmd.prompt == "" {
		t.Fatalf(":login brave did not open the key prompt (active=%v prompt=%q err=%v)", r.cmd.active, r.cmd.prompt, r.focus.err)
	}
	if view := r.cmdView(); !strings.Contains(view, "Brave Search API key") {
		t.Errorf("prompt not rendered: %q", view)
	}
}

// TestDevicePromptShowsCode checks the GitHub device flow instructions.
func TestDevicePromptShowsCode(t *testing.T) {
	r := loginRoot(t)
	r.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	p, ok := login.Lookup("github")
	if !ok {
		t.Fatal("github provider missing")
	}
	cmd := r.showDevicePrompt(loginDeviceMsg{provider: p, code: login.DeviceCode{
		UserCode:        "ABCD-1234",
		VerificationURI: "https://github.com/login/device",
		ExpiresIn:       time.Minute,
	}})
	if cmd == nil {
		t.Error("no poll command returned")
	}
	out := r.focus.help.View()
	if !strings.Contains(out, "ABCD-1234") || !strings.Contains(out, "github.com/login/device") {
		t.Errorf("device instructions missing the code or URL:\n%s", out)
	}
}

func TestLoginPromptRenders(t *testing.T) {
	r := loginRoot(t)
	r.execute("login google")
	for _, s := range [][2]int{{40, 12}, {80, 24}, {120, 40}} {
		r.Update(tea.WindowSizeMsg{Width: s[0], Height: s[1]})
		for i, line := range strings.Split(r.View(), "\n") {
			if w := lipgloss.Width(line); w > s[0] {
				t.Fatalf("size %v line %d width %d: %q", s, i, w, line)
			}
		}
	}
	r.cmd.input.SetValue("AIza-very-long-key-value-that-should-truncate")
	r.cmd.secret = false
	if out := r.cmdView(); !strings.Contains(out, "AIza") {
		t.Errorf("prompt should show a non-secret value: %q", out)
	}
	r.cmd.secret = true
	if out := r.cmdView(); strings.Contains(out, "AIza") {
		t.Errorf("prompt echoed a secret: %q", out)
	}

	// :help login lists the providers and their oddities.
	r.focus.showHelp("login")
	if out := r.focus.help.View(); !strings.Contains(out, "github") || !strings.Contains(out, "device flow") {
		t.Errorf("login help missing providers:\n%s", out)
	}
}
