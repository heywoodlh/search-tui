package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/heywoodlh/tui-search/internal/app"
	"github.com/heywoodlh/tui-search/internal/config"
	"github.com/heywoodlh/tui-search/internal/engine"
)

func testRoot(t *testing.T, w, h int) *Root {
	t.Helper()
	a, err := app.New(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	r := NewRoot(a, "", app.PlanOptions{})
	r.focus.flash = ""
	plan, _ := a.Plan("kubernetes", app.PlanOptions{})
	r.focus.plan = plan
	r.focus.mode = modeNormal
	r.focus.input.SetValue("kubernetes")
	r.focus.setResults([]engine.Result{{Title: "A", URL: "https://a"}, {Title: "B", URL: "https://b"}})
	r.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return r
}

func rkey(r *Root, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "ctrl+w":
			msg = tea.KeyMsg{Type: tea.KeyCtrlW}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		r.Update(msg)
	}
}

func TestSplitAndFocus(t *testing.T) {
	r := testRoot(t, 160, 40)
	first := r.focus
	rkey(r, "ctrl+w", "v")
	if len(r.panes()) != 2 || r.focus == first {
		t.Fatalf("vsplit: %d panes, focus moved=%v", len(r.panes()), r.focus != first)
	}
	right := r.focus
	if len(right.results) != 2 || right.input.Value() != "kubernetes" {
		t.Error("new pane should start as a copy of the current search")
	}
	a, b := r.rects[first], r.rects[right]
	if a.y != b.y || b.x <= a.x || a.w+b.w+1 != 160 {
		t.Errorf("side-by-side rects wrong: %+v %+v", a, b)
	}
	rkey(r, "ctrl+w", "s") // stack inside the right column
	if len(r.panes()) != 3 {
		t.Fatal("split failed")
	}
	bottomRight := r.focus
	rkey(r, "ctrl+w", "k")
	if r.focus != right {
		t.Error("ctrl+w k should focus the pane above")
	}
	rkey(r, "ctrl+w", "h")
	if r.focus != first {
		t.Error("ctrl+w h should focus the left pane")
	}
	rkey(r, "ctrl+w", "l")
	if r.focus != right && r.focus != bottomRight {
		t.Error("ctrl+w l should focus a right pane")
	}
	rkey(r, "ctrl+w", "w")
	rkey(r, "ctrl+w", "w")
	rkey(r, "ctrl+w", "w")
	if len(r.panes()) != 3 {
		t.Error("cycling changed pane count")
	}
	// Only the focused pane draws a cursor / active status.
	n := 0
	for _, p := range r.panes() {
		if p.focused {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d focused panes", n)
	}
	rkey(r, "ctrl+w", "o")
	if len(r.panes()) != 1 || r.rects[r.focus].w != 160 {
		t.Error("ctrl+w o should keep only the focused pane at full size")
	}
}

func TestClosePanes(t *testing.T) {
	r := testRoot(t, 160, 40)
	rkey(r, "ctrl+w", "v", "ctrl+w", "s")
	if len(r.panes()) != 3 {
		t.Fatal("setup")
	}
	rkey(r, "ctrl+w", "c")
	if len(r.panes()) != 2 {
		t.Fatal("ctrl+w c should close")
	}
	// Remaining two should again fill the screen side by side.
	total := 0
	for _, rc := range r.rects {
		total += rc.w
		if rc.h != 40 {
			t.Errorf("pane height %d after collapse", rc.h)
		}
	}
	if total != 159 {
		t.Errorf("widths sum to %d", total)
	}
	r.focus.mode = modeNormal
	_, cmd := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if len(r.panes()) != 1 || cmd != nil {
		t.Error("q with several panes should close the pane, not quit")
	}
	rkey(r, "ctrl+w", "c")
	if len(r.panes()) != 1 || !strings.Contains(r.focus.flash, "last pane") {
		t.Error("ctrl+w c must not close the last pane")
	}
	r.focus.mode = modeNormal
	_, cmd = r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Error("q on the last pane should quit")
	}
}

func TestSplitRefusedWhenTooSmall(t *testing.T) {
	r := testRoot(t, 40, 10)
	rkey(r, "ctrl+w", "v")
	if len(r.panes()) != 1 || !strings.Contains(r.focus.flash, "room") {
		t.Error("vsplit at 40 columns should be refused")
	}
}

func TestCtrlWInSearchBarDeletesWord(t *testing.T) {
	r := testRoot(t, 160, 40)
	p := r.focus
	p.mode = modeInsert
	p.input.Focus()
	p.input.SetValue("hello world")
	p.input.CursorEnd()
	rkey(r, "ctrl+w")
	if len(r.panes()) != 1 || r.pending || p.input.Value() != "hello " {
		t.Errorf("ctrl+w in insert mode: panes=%d pending=%v value=%q", len(r.panes()), r.pending, p.input.Value())
	}
}

func TestMessagesRouteToTheirPane(t *testing.T) {
	r := testRoot(t, 160, 40)
	left := r.focus
	rkey(r, "ctrl+w", "v")
	right := r.focus
	left.searchID, left.loading = 7, true
	r.Update(resultsMsg{pid: left.pid, id: 7, plan: left.plan, results: []engine.Result{{Title: "only-left", URL: "https://l"}}})
	if len(left.results) != 1 || left.results[0].Title != "only-left" || left.loading {
		t.Error("results not delivered to the left pane")
	}
	if len(right.results) != 2 {
		t.Error("right pane should be untouched")
	}
	// Messages for closed panes are dropped (and editor temp files cleaned).
	cleaned := false
	r.Update(editorDoneMsg{pid: 999, cleanup: func() { cleaned = true }})
	if !cleaned {
		t.Error("cleanup should run for a closed pane")
	}
}

func TestMouseFocusesPane(t *testing.T) {
	r := testRoot(t, 160, 40)
	left := r.focus
	rkey(r, "ctrl+w", "v")
	rc := r.rects[left]
	r.Update(tea.MouseMsg{X: rc.x + 5, Y: rc.y + 10, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if r.focus != left {
		t.Error("clicking a pane should focus it")
	}
}

func TestPaneLayoutsRender(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {100, 30}, {200, 60}, {300, 80}} {
		for _, seq := range [][]string{{"ctrl+w", "v"}, {"ctrl+w", "s"}, {"ctrl+w", "v", "ctrl+w", "s"}, {"ctrl+w", "v", "ctrl+w", "v", "ctrl+w", "s"}} {
			r := testRoot(t, size[0], size[1])
			rkey(r, seq...)
			out := r.View()
			lines := strings.Split(out, "\n")
			if len(lines) != size[1] {
				t.Errorf("%v %v: %d lines, want %d", size, seq, len(lines), size[1])
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w != size[0] {
					t.Errorf("%v %v: line %d width %d, want %d", size, seq, i, w, size[0])
					break
				}
			}
		}
	}
}

func TestSeparatorSpansNestedColumn(t *testing.T) {
	r := testRoot(t, 160, 40)
	rkey(r, "ctrl+w", "v", "ctrl+w", "s")
	left := r.panes()[0]
	col := r.rects[left].w
	for i, l := range strings.Split(r.View(), "\n") {
		if []rune(stripANSI(l))[col] != '│' {
			t.Fatalf("separator missing on line %d", i)
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			esc = true
		case esc && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestDirectPaneBindings(t *testing.T) {
	cfg := config.Default()
	cfg.Keys = map[string][]string{"split_vertical": {"|"}, "close_pane": {"X"}}
	a, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRoot(a, "", app.PlanOptions{})
	r.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	r.focus.mode = modeNormal
	rkey(r, "|")
	if len(r.panes()) != 2 {
		t.Fatal("split_vertical binding did not split")
	}
	r.focus.mode = modeNormal
	rkey(r, "X")
	if len(r.panes()) != 1 {
		t.Error("close_pane binding did not close")
	}
}
