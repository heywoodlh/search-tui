package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/heywoodlh/search-tui/internal/app"
	"github.com/heywoodlh/search-tui/internal/engine"
)

func TestLinkCommandsAndCompletion(t *testing.T) {
	r := testRoot(t, 100, 30)
	r.focus.setResults([]engine.Result{{Title: "First", URL: "https://example.com/a"}, {Title: "Second", URL: "https://example.com/b"}})
	r.focus.pages["https://example.com/a"] = app.Page{Markdown: "Read [the docs](https://example.com/docs) and [again](https://example.com/docs)."}
	if got, err := r.focus.resolveLink("3"); err != nil || got != "https://example.com/docs" {
		t.Fatalf("page link: %q, %v", got, err)
	}
	for _, arg := range []string{"0", "4", "javascript:alert(1)", "file:///etc/passwd"} {
		if _, err := r.focus.resolveLink(arg); err == nil {
			t.Errorf("unsafe or nonexistent link %q accepted", arg)
		}
	}
	typeKeys(r, ":open-link ")
	special(r, tea.KeyTab)
	if got := r.cmd.input.Value(); got != "open-link 1" {
		t.Errorf("first completion: %q", got)
	}
	if !strings.Contains(stripANSI(r.View()), "[1]") || !strings.Contains(stripANSI(r.View()), "[3]") {
		t.Error("completion should show numbered available links")
	}
	special(r, tea.KeyTab)
	if got := r.cmd.input.Value(); got != "open-link 2" {
		t.Errorf("second completion: %q", got)
	}
	special(r, tea.KeyEsc)
	p := filepath.Join(t.TempDir(), "clipboard")
	r.app.Cfg.Clipboard = "command"
	r.app.Cfg.ClipboardCommand = "cat > " + p
	ex(r, "copy-link 3")
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "https://example.com/docs" {
		t.Errorf("copy-link wrote %q: %v", b, err)
	}
}

func TestCompactPeekAndFullDetail(t *testing.T) {
	r := testRoot(t, 120, 30)
	if strings.Contains(stripANSI(r.View()), "No result selected") {
		t.Fatal("empty preview takes space")
	}
	r.focus.setResults([]engine.Result{{Title: "Selected page", URL: "https://example.com/a", Snippet: "A compact snippet for the page"}})
	r.focus.refreshDetail()
	out := stripANSI(r.View())
	if !strings.Contains(out, "A compact snippet for the page") || strings.Contains(out, "No result selected") {
		t.Fatalf("compact preview missing or empty preview shown: %q", out)
	}
	typeKeys(r, "l")
	if r.focus.mode != modeDetail {
		t.Fatal("l should open detailed view")
	}
	if got := r.focus.detail.Width; got < 90 {
		t.Errorf("detailed view not full width: %d", got)
	}
}

func TestPeekSeparatedAndPinnedWhenScrolling(t *testing.T) {
	r := testRoot(t, 40, 12)
	r.focus.setResults(manyResults(20))
	r.layout()
	initial := stripANSI(r.View())
	if !strings.Contains(initial, "────────") {
		t.Fatalf("peek needs a clear divider: %q (layout %+v)", initial, r.focus.layout())
	}
	for i := 0; i < 8; i++ {
		rkey(r, "j")
	}
	out := stripANSI(r.View())
	if !strings.Contains(out, "[9] example.com/") {
		t.Errorf("selected result peek disappeared while scrolling: %q", out)
	}
	if !strings.Contains(out, "────────") {
		t.Error("divider should stay visible while scrolling")
	}
}

func TestNoAggregateResultsSummary(t *testing.T) {
	r := testRoot(t, 100, 30)
	rs := []engine.Result{
		{Title: "Kubernetes documentation", URL: "https://example.com/a", Snippet: "Kubernetes is an open source container orchestration system for automating software deployment, scaling and management."},
		{Title: "Deployments", URL: "https://example.com/b", Snippet: "A Kubernetes deployment provides declarative updates for pods and replica sets in a container orchestration system."},
		{Title: "Services", URL: "https://example.com/c", Snippet: "A service exposes an application running on pods inside the container orchestration system, Kubernetes."},
	}
	r.focus.setResults(rs)
	r.focus.refreshDetail()
	out := stripANSI(r.View())
	for _, unwanted := range []string{"Summary", "related:"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("results page still shows an aggregate summary (%q): %q", unwanted, out)
		}
	}
	if !strings.Contains(out, "Kubernetes is an open source container") {
		t.Errorf("selected-result peek missing: %q", out)
	}
	var l = r.focus.layout()
	if l.peekH != 4 {
		t.Errorf("peek strip should be 3 lines plus a divider: %+v", l)
	}
	// 30 rows minus the app-wide mode line and the pane's own status line,
	// the input plus rule, and the peek strip.
	if l.listH != 30-1-1-2-4 {
		t.Errorf("result list should use every row left by the header, peek and status bar: %+v", l)
	}
	if strings.Contains(stripANSI(r.focus.detailContent(rs[0], 80)), "Summary") {
		t.Error("result details should not show the results summary")
	}
	if err := r.set("summary"); err == nil {
		t.Error(":set summary should report an unknown option")
	}
}

func TestRootTinySizes(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {10, 3}, {20, 5}, {40, 8}} {
		r := testRoot(t, size[0], size[1])
		for _, cmd := range []bool{false, true} {
			if cmd {
				r.openCmd("")
			}
			lines := strings.Split(r.View(), "\n")
			if len(lines) > size[1] {
				t.Errorf("size %v command=%v: %d lines", size, cmd, len(lines))
			}
			for _, line := range lines {
				if w := lipgloss.Width(line); w > size[0] {
					t.Errorf("size %v command=%v: width %d", size, cmd, w)
				}
			}
		}
	}
}

func TestCommandLineFitsWhenStatusDisabled(t *testing.T) {
	r := testRoot(t, 40, 8)
	f := false
	r.app.Cfg.UI.StatusBar = &f
	r.layout()
	typeKeys(r, ":")
	if got := len(strings.Split(r.View(), "\n")); got != 8 {
		t.Errorf("command line overflow: %d lines", got)
	}
}
