package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/heywoodlh-bot/tui-search/internal/app"
	"github.com/heywoodlh-bot/tui-search/internal/config"
	"github.com/heywoodlh-bot/tui-search/internal/engine"
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
