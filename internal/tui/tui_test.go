package tui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/heywoodlh-bot/tui-search/internal/app"
	"github.com/heywoodlh-bot/tui-search/internal/config"
	"github.com/heywoodlh-bot/tui-search/internal/engine"
	"github.com/heywoodlh-bot/tui-search/internal/theme"
	"github.com/muesli/termenv"
)

func testModel(t *testing.T) *Model {
	t.Helper()
	a, err := app.New(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	m := New(a, "", app.PlanOptions{})
	plan, _ := a.Plan("kubernetes", app.PlanOptions{})
	m.plan = plan
	m.mode = modeNormal
	var rs []engine.Result
	for i := 0; i < 15; i++ {
		rs = append(rs, engine.Result{Title: "Result title that is quite long indeed", URL: "https://example.com/some/long/path", Snippet: strings.Repeat("Kubernetes automates deployment of containerized applications. ", 3), Meta: "meta"})
	}
	rs = append(rs, engine.Result{Title: "repo/file.go", URL: "https://x", Code: &engine.CodeMatch{Repo: "repo", Path: "file.go", Fragments: []engine.Fragment{{Line: 3, Text: "func main() {\n\tkubernetes()\n}"}}}})
	m.setResults(rs)
	return m
}

func send(m *Model, msgs ...tea.Msg) {
	for _, msg := range msgs {
		m.Update(msg)
	}
}

func kp(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+l":
		return tea.KeyMsg{Type: tea.KeyCtrlL}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestRenderAllSizes(t *testing.T) {
	sizes := [][2]int{{1, 1}, {10, 3}, {20, 5}, {40, 20}, {60, 15}, {80, 24}, {120, 40}, {200, 60}}
	for _, mode := range []mode{modeNormal, modeDetail, modeHelp, modeInsert} {
		for _, s := range sizes {
			m := testModel(t)
			m.mode = mode
			send(m, tea.WindowSizeMsg{Width: s[0], Height: s[1]})
			out := m.View()
			lines := strings.Split(out, "\n")
			if len(lines) > s[1] {
				t.Errorf("mode %d size %v: %d lines exceeds height", mode, s, len(lines))
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > s[0] {
					t.Errorf("mode %d size %v: line %d width %d exceeds %d: %q", mode, s, i, w, s[0], l)
					break
				}
			}
		}
	}
}

func TestNavigation(t *testing.T) {
	m := testModel(t)
	send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	send(m, kp("j"), kp("j"))
	if m.sel != 2 {
		t.Fatalf("sel = %d", m.sel)
	}
	send(m, kp("G"))
	if m.sel != len(m.results)-1 {
		t.Errorf("G: sel = %d", m.sel)
	}
	if m.offset == 0 {
		t.Error("list did not scroll")
	}
	send(m, kp("g"))
	if m.sel != 0 || m.offset != 0 {
		t.Errorf("g: sel=%d offset=%d", m.sel, m.offset)
	}
	send(m, kp("enter"))
	if m.mode != modeDetail {
		t.Error("enter should open details")
	}
	send(m, kp("esc"))
	if m.mode != modeNormal {
		t.Error("esc should return")
	}
	send(m, kp("?"))
	if m.mode != modeHelp {
		t.Error("? should open help")
	}
	send(m, kp("?"), kp("/"))
	if m.mode != modeInsert {
		t.Error("/ should enter insert mode")
	}
}

func TestMouseSelect(t *testing.T) {
	m := testModel(t)
	send(m, tea.WindowSizeMsg{Width: 80, Height: 30})
	l := m.layout()
	starts := m.itemStarts()
	y := l.listTop + starts[2] + 1
	send(m, tea.MouseMsg{X: 5, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.sel != 2 {
		t.Fatalf("click selected %d", m.sel)
	}
	send(m, tea.MouseMsg{X: 5, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.mode != modeDetail {
		t.Error("second click should open details")
	}
	m.mode = modeNormal
	send(m, tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.sel != 3 {
		t.Errorf("wheel: sel = %d", m.sel)
	}
}

func TestCustomKeys(t *testing.T) {
	a, _ := app.New(config.Default())
	a.Cfg.Keys = map[string][]string{"down": {"x"}, "bogus": {"z"}}
	m := New(a, "", app.PlanOptions{})
	if !strings.Contains(m.flash, "bogus") {
		t.Error("unknown action not reported")
	}
	if !m.keys.is(kp("x"), ActDown) || m.keys.is(kp("j"), ActDown) {
		t.Error("override not applied")
	}
}

func TestEscBackChain(t *testing.T) {
	m := testModel(t)
	m.input.SetValue("kubernetes")
	send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	send(m, kp("enter")) // details
	if m.mode != modeDetail {
		t.Fatal("expected details")
	}
	send(m, kp("esc"))
	if m.mode != modeNormal || len(m.results) == 0 {
		t.Fatal("esc from details should return to results")
	}
	send(m, kp("esc"))
	if len(m.results) != 0 || m.plan.Engine != nil || m.input.Value() != "" || m.mode != modeInsert {
		t.Fatalf("esc from results should reach the start page: mode=%d results=%d input=%q", m.mode, len(m.results), m.input.Value())
	}
	send(m, kp("esc"))
	if m.mode != modeNormal {
		t.Error("esc on start page should leave the search bar")
	}
	send(m, kp("esc"))
	if m.mode != modeInsert {
		t.Error("esc again should refocus the search bar")
	}
}

func TestCtrlLFocusesSearch(t *testing.T) {
	for _, start := range []mode{modeNormal, modeDetail} {
		m := testModel(t)
		m.mode = start
		send(m, tea.WindowSizeMsg{Width: 80, Height: 24}, kp("ctrl+l"))
		if m.mode != modeInsert || !m.input.Focused() {
			t.Errorf("ctrl+l from mode %d did not focus the search bar", start)
		}
	}
}

func TestEnginePicker(t *testing.T) {
	m := testModel(t)
	send(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	send(m, kp("tab"))
	if !m.picker.open {
		t.Fatal("tab should open the picker")
	}
	for _, n := range m.picker.items {
		if n == "google" || n == "kagi" || n == "brave" {
			t.Errorf("unconfigured engine %s listed", n)
		}
	}
	start := m.picker.idx
	if m.picker.items[start] != "duckduckgo" {
		t.Errorf("picker should start on the current engine, got %s", m.picker.items[start])
	}
	send(m, kp("tab"), kp("j"), kp("down"), kp("k"))
	if m.picker.idx != (start+2)%len(m.picker.items) {
		t.Errorf("picker idx = %d, want %d", m.picker.idx, start+2)
	}
	out := m.View()
	if !strings.Contains(out, "wikipedia") || !strings.Contains(out, "sourcegraph") {
		t.Error("picker not rendered")
	}
	for i, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w > 100 {
			t.Errorf("line %d too wide (%d)", i, w)
		}
	}
	send(m, kp("w")) // jump
	if m.picker.items[m.picker.idx] != "wikipedia" {
		t.Errorf("letter jump landed on %s", m.picker.items[m.picker.idx])
	}
	m.input.SetValue("")
	send(m, kp("enter"))
	if m.picker.open || m.opts.Engine != "wikipedia" {
		t.Errorf("enter should select: open=%v engine=%q", m.picker.open, m.opts.Engine)
	}
	send(m, kp("tab"), kp("esc"))
	if m.picker.open || m.opts.Engine != "wikipedia" {
		t.Error("esc should close the picker without changing engine")
	}
	// From the search bar too.
	m.mode = modeInsert
	m.input.Focus()
	m.input.SetValue("abc")
	send(m, kp("tab"))
	if !m.picker.open {
		t.Error("tab in the search bar should open the picker")
	}
}

func TestThemesNeverSetBackground(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	for _, name := range theme.Names() {
		cfg := config.Default()
		cfg.UI.Theme = name
		a, err := app.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		m := New(a, "", app.PlanOptions{})
		plan, _ := a.Plan("kubernetes", app.PlanOptions{})
		m.plan = plan
		m.setResults([]engine.Result{{Title: "T", URL: "https://x", Snippet: "Kubernetes automates deployment of containerized applications today.", Meta: "m"}})
		send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
		for _, mode := range []mode{modeInsert, modeNormal, modeDetail, modeHelp} {
			m.mode = mode
			views := []string{m.View()}
			m.mode = modeNormal
			m.openPicker(0)
			views = append(views, m.View())
			m.closePicker()
			for _, out := range views {
				if setsBackground(out) {
					t.Fatalf("theme %s mode %d sets a background:\n%q", name, mode, out)
				}
			}
		}
		if name == "dracula" && !strings.Contains(m.View(), "38;2;") {
			t.Error("expected truecolor foreground output for dracula")
		}
	}
}

// setsBackground parses SGR sequences and reports background colours
// (40-49, 100-107, 48;…) or reverse video (7).
func setsBackground(s string) bool {
	for _, seq := range regexp.MustCompile(`\x1b\[([0-9;]*)m`).FindAllStringSubmatch(s, -1) {
		ps := strings.Split(seq[1], ";")
		for i := 0; i < len(ps); i++ {
			n, _ := strconv.Atoi(ps[i])
			switch {
			case n == 38: // foreground: skip its arguments
				if i+1 < len(ps) && ps[i+1] == "5" {
					i += 2
				} else if i+1 < len(ps) && ps[i+1] == "2" {
					i += 4
				}
			case n == 7, n >= 40 && n <= 48, n >= 100 && n <= 107:
				return true
			}
		}
	}
	return false
}

func TestSetsBackgroundDetector(t *testing.T) {
	for s, want := range map[string]bool{
		"\x1b[48;2;1;2;3mx": true, "\x1b[1;44mx": true, "\x1b[7mx": true, "\x1b[101mx": true,
		"\x1b[38;2;48;54;60mx": false, "\x1b[38;5;48mx": false, "\x1b[1;4;35mx": false,
	} {
		if setsBackground(s) != want {
			t.Errorf("setsBackground(%q) != %v", s, want)
		}
	}
}
