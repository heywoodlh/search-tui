package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestLiveLoginSession drives :login kagi with a real Session Link.
func TestLiveLoginSession(t *testing.T) {
	if os.Getenv("SEARCH_TUI_LIVE") == "" {
		t.Skip("set SEARCH_TUI_LIVE=1 to hit the network")
	}
	link := strings.TrimSpace(os.Getenv("SEARCH_TUI_KAGI_SESSION"))
	if link == "" {
		t.Skip("set SEARCH_TUI_KAGI_SESSION to the Session Link")
	}
	authPath := filepath.Join(t.TempDir(), "auth.json")
	t.Setenv("SEARCH_TUI_AUTH", authPath)
	r := loginRoot(t)
	r.Update(tea.WindowSizeMsg{Width: 110, Height: 30})

	r.execute("login kagi")
	if !r.cmd.active || r.cmd.prompt == "" {
		t.Fatalf("prompt did not open: %+v", r.focus.err)
	}
	r.cmd.input.SetValue(strings.TrimSpace(link))
	cmd := r.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("no command returned")
	}
	msg, ok := cmd().(loginDoneMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	r.Update(msg)
	if r.focus.err != nil {
		t.Fatalf("login failed: %v", r.focus.err)
	}
	b, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"session"`) || !strings.Contains(string(b), `"token"`) {
		t.Fatalf("auth file = %s", b)
	}
	t.Logf("flash: %s", r.focus.flash)
}
