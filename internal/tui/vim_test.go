package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/heywoodlh/tui-search/internal/engine"
	"github.com/muesli/termenv"
)

func typeKeys(r *Root, s string) {
	for _, c := range s {
		r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{c}})
	}
}

func special(r *Root, t tea.KeyType) { r.Update(tea.KeyMsg{Type: t}) }

func ex(r *Root, line string) {
	typeKeys(r, ":")
	typeKeys(r, line)
	special(r, tea.KeyEnter)
}

func manyResults(n int) []engine.Result {
	var rs []engine.Result
	for i := 0; i < n; i++ {
		rs = append(rs, engine.Result{Title: "Result", URL: "https://example.com/" + string(rune('a'+i%26)), Snippet: "snippet text"})
	}
	return rs
}

func TestCommandLineQuitAndSplits(t *testing.T) {
	r := testRoot(t, 160, 40)
	ex(r, "vsp")
	ex(r, "sp")
	if len(r.panes()) != 3 {
		t.Fatalf("splits via :vsp and :sp: %d panes", len(r.panes()))
	}
	ex(r, "q")
	if len(r.panes()) != 2 {
		t.Error(":q should close one pane")
	}
	ex(r, "only")
	if len(r.panes()) != 1 {
		t.Error(":only should leave one pane")
	}
	ex(r, "close")
	if r.focus.err == nil || !strings.Contains(r.focus.err.Error(), "E444") {
		t.Error(":close on the last pane should fail with E444")
	}
	ex(r, "frobnicate")
	if r.focus.err == nil || !strings.Contains(r.focus.err.Error(), "E492") {
		t.Error("unknown commands should report E492")
	}
	typeKeys(r, ":qa")
	_, cmd := r.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error(":qa should quit")
	}
}

func TestCommandModifiersAndNew(t *testing.T) {
	r := testRoot(t, 160, 40)
	first := r.focus
	ex(r, "vert new")
	if len(r.panes()) != 2 || r.rects[r.focus].x <= r.rects[first].x {
		t.Error(":vert new should open an empty pane to the right")
	}
	if len(r.focus.results) != 0 {
		t.Error(":new panes start empty")
	}
	ex(r, "aboveleft split")
	if r.rects[r.focus].y != r.rects[r.panes()[1]].y && r.rects[r.focus].y > 0 {
		t.Error(":aboveleft split should open above")
	}
}

func TestTabs(t *testing.T) {
	r := testRoot(t, 120, 30)
	ex(r, "tabnew")
	if len(r.tabs) != 2 || r.cur != 1 {
		t.Fatalf(":tabnew: %d tabs, cur %d", len(r.tabs), r.cur)
	}
	if r.focus.mode != modeInsert {
		t.Error("a new tab starts on the start page, ready to type")
	}
	special(r, tea.KeyEsc)
	out := r.View()
	if !strings.Contains(out, " 1 ") || !strings.Contains(out, " 2 ") {
		t.Error("tab bar should show both tabs")
	}
	for _, l := range strings.Split(out, "\n") {
		if lipgloss.Width(l) > 120 {
			t.Fatal("line too wide with tab bar")
		}
	}
	if n := len(strings.Split(out, "\n")); n != 30 {
		t.Errorf("view has %d lines, want 30", n)
	}
	r.focus.mode = modeNormal
	typeKeys(r, "gt")
	if r.cur != 0 {
		t.Error("gt should wrap to the first tab")
	}
	typeKeys(r, "gT")
	if r.cur != 1 {
		t.Error("gT should go back")
	}
	r.focus.mode = modeNormal
	typeKeys(r, "1gt")
	if r.cur != 0 {
		t.Error("1gt should go to tab 1")
	}
	ex(r, "tabn 2")
	ex(r, "q") // last pane of tab 2 closes the tab, not the app
	if len(r.tabs) != 1 {
		t.Errorf(":q on a tab's last pane should close the tab (%d tabs)", len(r.tabs))
	}
	ex(r, "tabclose")
	if r.focus.err == nil {
		t.Error(":tabclose on the last tab should fail")
	}
}

func TestResize(t *testing.T) {
	r := testRoot(t, 160, 40)
	ex(r, "sp")
	ex(r, "resize 10")
	if h := r.rects[r.focus].h; h != 10 {
		t.Errorf(":resize 10 -> height %d", h)
	}
	ex(r, "res +4")
	if h := r.rects[r.focus].h; h != 14 {
		t.Errorf(":res +4 -> height %d", h)
	}
	total := 0
	for _, rc := range r.rects {
		total += rc.h
	}
	if total != 40 {
		t.Errorf("heights sum to %d", total)
	}
	rkey(r, "ctrl+w", "=")
	for _, rc := range r.rects {
		if rc.h != 20 {
			t.Errorf("ctrl+w = should equalise, got %d", rc.h)
		}
	}
	ex(r, "vsp")
	ex(r, "vert resize 30")
	if w := r.rects[r.focus].w; w != 30 {
		t.Errorf(":vert resize 30 -> width %d", w)
	}
}

func TestCompletionAndHistory(t *testing.T) {
	r := testRoot(t, 160, 40)
	typeKeys(r, ":tabn")
	special(r, tea.KeyTab)
	if v := r.cmd.input.Value(); v != "tabnew" && v != "tabnext" {
		t.Errorf("completion = %q", v)
	}
	special(r, tea.KeyEsc)
	typeKeys(r, ":engine wik")
	special(r, tea.KeyTab)
	if v := r.cmd.input.Value(); v != "engine wikipedia" {
		t.Errorf("argument completion = %q", v)
	}
	special(r, tea.KeyEnter)
	if r.focus.opts.Engine != "wikipedia" {
		t.Errorf(":engine wikipedia -> %q", r.focus.opts.Engine)
	}
	typeKeys(r, ":")
	special(r, tea.KeyUp)
	if v := r.cmd.input.Value(); v != "engine wikipedia" {
		t.Errorf("history recall = %q", v)
	}
	special(r, tea.KeyEsc)
	if r.cmd.active {
		t.Error("esc should close the command line")
	}
}

func TestCountsAndJump(t *testing.T) {
	r := testRoot(t, 100, 40)
	p := r.focus
	p.setResults(manyResults(20))
	typeKeys(r, "5j")
	if p.sel != 5 {
		t.Errorf("5j -> %d", p.sel)
	}
	typeKeys(r, "12G")
	if p.sel != 11 {
		t.Errorf("12G -> %d", p.sel)
	}
	ex(r, "3")
	if p.sel != 2 {
		t.Errorf(":3 -> %d", p.sel)
	}
	typeKeys(r, "G")
	if p.sel != 19 {
		t.Errorf("G -> %d", p.sel)
	}
}

func TestHints(t *testing.T) {
	r := testRoot(t, 100, 40)
	p := r.focus
	p.setResults(manyResults(6))
	r.layout()
	typeKeys(r, "f")
	if !p.hint.active || len(p.hint.labels) == 0 {
		t.Fatal("f should start hint mode")
	}
	if !strings.Contains(r.View(), "HINT") {
		t.Error("status should show HINT")
	}
	label := p.hint.byIdx[3]
	typeKeys(r, label)
	if p.hint.active || p.sel != 3 || p.mode != modeDetail {
		t.Errorf("typing hint %q: active=%v sel=%d mode=%d", label, p.hint.active, p.sel, p.mode)
	}
	if labels := hintLabels(30, "asdf"); len(labels) != 16 || labels[0] != "aa" {
		t.Errorf("two-character labels: %v", labels)
	}
}

func TestVisualMode(t *testing.T) {
	r := testRoot(t, 120, 30)
	r.app.Cfg.Clipboard = "none" // don't write OSC 52 to the test's terminal
	p := r.focus
	p.setResults([]engine.Result{{Title: "Hello world from search", URL: "https://example.com/x", Snippet: "alpha beta gamma delta"}})
	typeKeys(r, "v")
	if p.mode != modeVisual {
		t.Fatal("v should enter visual mode")
	}
	typeKeys(r, "e") // to the end of "Hello"
	if got := p.vis.selection(); got != "Hello" {
		t.Errorf("ve selects %q", got)
	}
	typeKeys(r, "w")
	if got := p.vis.selection(); got != "Hello w" {
		t.Errorf("vew selects %q", got)
	}
	typeKeys(r, "fs") // to the s of "search"
	if got := p.vis.selection(); got != "Hello world from s" {
		t.Errorf("fs selects %q", got)
	}
	typeKeys(r, "o") // cursor back to the start, anchor at the s
	if p.vis.cur.col != 0 || p.vis.anc.col != 17 {
		t.Errorf("o should swap ends: cur=%v anc=%v", p.vis.cur, p.vis.anc)
	}
	typeKeys(r, "$")
	if got := p.vis.selection(); got != "search" {
		t.Errorf("o then $ selects %q", got)
	}
	out := r.View()
	if !strings.Contains(out, "VISUAL") {
		t.Error("status should show VISUAL")
	}
	typeKeys(r, "V")
	typeKeys(r, "j")
	if got := p.vis.selection(); got != "Hello world from search\nhttps://example.com/x" {
		t.Errorf("Vj selects %q", got)
	}
	special(r, tea.KeyEsc)
	if p.mode == modeVisual {
		t.Error("esc should leave visual mode")
	}
	// t and ; / , repeat.
	typeKeys(r, "v")
	typeKeys(r, "to")
	if p.vis.cur.col != 3 {
		t.Errorf("to -> col %d", p.vis.cur.col)
	}
	typeKeys(r, ";")
	if p.vis.cur.col != 6 {
		t.Errorf("; -> col %d", p.vis.cur.col)
	}
	typeKeys(r, "y")
	if p.mode == modeVisual {
		t.Error("y should copy and leave visual mode")
	}
}

func TestNewStatesNeverSetBackground(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	r := testRoot(t, 120, 30)
	r.focus.setResults(manyResults(8))
	r.layout()
	typeKeys(r, "f")
	views := []string{r.View()}
	special(r, tea.KeyEsc)
	typeKeys(r, "vjw")
	views = append(views, r.View())
	special(r, tea.KeyEsc)
	typeKeys(r, ":tabn")
	special(r, tea.KeyTab)
	views = append(views, r.View())
	special(r, tea.KeyEsc)
	ex(r, "tabnew")
	views = append(views, r.View())
	for i, v := range views {
		if setsBackground(v) {
			t.Errorf("view %d sets a background", i)
		}
		for _, l := range strings.Split(v, "\n") {
			if lipgloss.Width(l) > 120 {
				t.Errorf("view %d: line wider than the screen", i)
				break
			}
		}
	}
}

func TestAbbreviations(t *testing.T) {
	cases := map[string]string{
		"q": "quit", "qa": "quitall", "quita": "quitall", "clo": "close", "on": "only",
		"sp": "split", "vs": "vsplit", "vsp": "vsplit", "new": "new", "vne": "vnew",
		"res": "resize", "tabn": "tabnext", "tabnew": "tabnew", "tabe": "tabnew", "tabc": "tabclose",
		"tabo": "tabonly", "tabp": "tabprevious", "tabN": "tabprevious", "tabfir": "tabfirst",
		"tabr": "tabfirst", "tabl": "tablast", "tabm": "tabmove", "s": "search", "se": "set",
		"sea": "search", "e": "edit", "eng": "engine", "engines": "engines", "h": "help",
		"o": "open", "n": "next", "N": "previous", "sum": "summarise", "the": "theme",
		"ve": "version", "ya": "yank", "co": "copy", "his": "history", "wel": "welcome",
	}
	for in, want := range cases {
		c, ok := lookup(in)
		if !ok || c.name != want {
			t.Errorf(":%s -> %q (ok=%v), want %q", in, c.name, ok, want)
		}
	}
	for _, bad := range []string{"t", "c", "v", "r", "ex", "tab"} {
		if c, ok := lookup(bad); ok {
			t.Errorf(":%s should be ambiguous/unknown, got %q", bad, c.name)
		}
	}
	in := parse("vert aboveleft split rust lifetimes")
	if !in.vertical || !in.before || in.name != "split" || in.args != "rust lifetimes" {
		t.Errorf("parse = %+v", in)
	}
	if in := parse("q!"); in.name != "q" || !in.bang {
		t.Errorf("parse q! = %+v", in)
	}
}

func TestBurstKeysAreSplit(t *testing.T) {
	r := testRoot(t, 100, 40)
	p := r.focus
	p.setResults(manyResults(10))
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3j")})
	if p.sel != 3 {
		t.Errorf("burst \"3j\" -> sel %d", p.sel)
	}
	ex(r, "tabnew")
	special(r, tea.KeyEsc)
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gt")})
	if r.cur != 0 {
		t.Error("burst \"gt\" should switch tabs")
	}
	// Pasted text in the search bar is not split.
	r.focus.mode = modeInsert
	r.focus.input.Focus()
	r.focus.input.SetValue("")
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello"), Paste: true})
	if r.focus.input.Value() != "hello" {
		t.Errorf("paste = %q", r.focus.input.Value())
	}
}

func TestTabMove(t *testing.T) {
	r := testRoot(t, 120, 30)
	ex(r, "tabnew one")
	ex(r, "tabnew two")
	names := func() string {
		var out []string
		for _, tb := range r.tabs {
			out = append(out, tb.focus.input.Value())
		}
		return strings.Join(out, ",")
	}
	if names() != "kubernetes,one,two" || r.cur != 2 {
		t.Fatalf("tabs = %s cur %d", names(), r.cur)
	}
	ex(r, "tabmove 0")
	if names() != "two,kubernetes,one" || r.cur != 0 {
		t.Errorf(":tabmove 0 -> %s cur %d", names(), r.cur)
	}
	ex(r, "tabm +1")
	if names() != "kubernetes,two,one" || r.cur != 1 {
		t.Errorf(":tabm +1 -> %s cur %d", names(), r.cur)
	}
	ex(r, "tabm")
	if names() != "kubernetes,one,two" || r.cur != 2 {
		t.Errorf(":tabm -> %s cur %d", names(), r.cur)
	}
}
