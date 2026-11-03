package tui

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/heywoodlh/tui-search/internal/engine"
)

// Visual mode: select text in the selected result's details (title, URL,
// snippet or code, fetched page) with vim motions and copy it with y.
// v is characterwise, V linewise. Motions: h j k l, w b e, 0 ^ $, gg G,
// ctrl+d ctrl+u, f F t T {char} with ; and , to repeat, counts, o to swap
// ends. The selection is drawn with colour and underline only (no
// background), like the rest of the UI.

type pos struct{ row, col int }

type visualState struct {
	line     bool
	lines    [][]rune
	cur, anc pos
	want     int    // desired column for vertical moves
	top      int    // first visible screen row
	pending  string // "g" or a find operator awaiting its character
	count    int
	lastFind struct {
		op string
		ch rune
	}
	prevMode mode
}

// visualLines builds the plain-text buffer for a result. Logical lines stay
// unwrapped so copied text has no display line breaks.
func (m *Model) visualLines(r engine.Result) [][]rune {
	var out []string
	add := func(s ...string) { out = append(out, s...) }
	add(r.Title, r.URL)
	if r.Meta != "" {
		add(r.Meta)
	}
	add("")
	if r.Code != nil && len(r.Code.Fragments) > 0 {
		for i, f := range r.Code.Fragments {
			if i > 0 {
				add("")
			}
			add(strings.Split(f.Text, "\n")...)
		}
	} else if r.Snippet != "" {
		add(r.Snippet)
	}
	if p, ok := m.pages[r.URL]; ok {
		if len(p.Summary) > 0 {
			add("", "Page summary")
			for _, s := range p.Summary {
				add("• " + s.Text)
			}
		}
		add("")
		add(strings.Split(p.Markdown, "\n")...)
	}
	lines := make([][]rune, len(out))
	for i, l := range out {
		lines[i] = []rune(strings.ReplaceAll(l, "\t", "    "))
	}
	return lines
}

func (m *Model) enterVisual(linewise bool) tea.Cmd {
	r, ok := m.current()
	if !ok {
		return m.setFlash("nothing to select")
	}
	m.vis = visualState{line: linewise, lines: m.visualLines(r), prevMode: m.mode}
	m.mode = modeVisual
	return nil
}

func (m *Model) exitVisual() {
	m.mode = m.vis.prevMode
	if m.mode == modeVisual {
		m.mode = modeNormal
	}
}

func (v *visualState) lineLen(row int) int { return len(v.lines[row]) }

func (v *visualState) clamp() {
	v.cur.row = min(max(v.cur.row, 0), len(v.lines)-1)
	v.cur.col = min(max(v.cur.col, 0), max(v.lineLen(v.cur.row)-1, 0))
}

func charClass(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return 2
	}
	return 1
}

// at returns the rune at p, or ' ' past the end of a line (line breaks act
// as whitespace for word motions).
func (v *visualState) at(p pos) rune {
	if p.col < v.lineLen(p.row) {
		return v.lines[p.row][p.col]
	}
	return ' '
}

func (v *visualState) step(p pos, d int) (pos, bool) {
	if d > 0 {
		if p.col < v.lineLen(p.row)-1 {
			return pos{p.row, p.col + 1}, true
		}
		if p.row < len(v.lines)-1 {
			return pos{p.row + 1, 0}, true
		}
		return p, false
	}
	if p.col > 0 {
		return pos{p.row, p.col - 1}, true
	}
	if p.row > 0 {
		return pos{p.row - 1, max(v.lineLen(p.row-1)-1, 0)}, true
	}
	return p, false
}

func (v *visualState) wordForward() {
	p := v.cur
	cls := charClass(v.at(p))
	ok := true
	for ok && charClass(v.at(p)) == cls && cls != 0 {
		prev := p
		if p, ok = v.step(p, 1); ok && p.row != prev.row {
			break
		}
	}
	for ok && charClass(v.at(p)) == 0 && v.lineLen(p.row) > 0 {
		p, ok = v.step(p, 1)
	}
	v.cur = p
}

func (v *visualState) wordBackward() {
	p, ok := v.step(v.cur, -1)
	for ok && charClass(v.at(p)) == 0 {
		p, ok = v.step(p, -1)
	}
	cls := charClass(v.at(p))
	for {
		q, ok := v.step(p, -1)
		if !ok || q.row != p.row || charClass(v.at(q)) != cls {
			break
		}
		p = q
	}
	v.cur = p
}

func (v *visualState) wordEnd() {
	p, ok := v.step(v.cur, 1)
	for ok && charClass(v.at(p)) == 0 {
		p, ok = v.step(p, 1)
	}
	cls := charClass(v.at(p))
	for {
		q, ok := v.step(p, 1)
		if !ok || q.row != p.row || charClass(v.at(q)) != cls {
			break
		}
		p = q
	}
	v.cur = p
}

// find moves to the n-th occurrence of ch on the current line (f F t T).
func (v *visualState) find(op string, ch rune, n int) bool {
	line := v.lines[v.cur.row]
	col := v.cur.col
	for ; n > 0; n-- {
		found := -1
		switch op {
		case "f", "t":
			start := col + 1
			if op == "t" {
				start++
			}
			for i := start; i < len(line); i++ {
				if line[i] == ch {
					found = i
					break
				}
			}
		case "F", "T":
			start := col - 1
			if op == "T" {
				start--
			}
			for i := start; i >= 0; i-- {
				if line[i] == ch {
					found = i
					break
				}
			}
		}
		if found < 0 {
			return false
		}
		switch op {
		case "t":
			col = found - 1 // till: stop just before the character
		case "T":
			col = found + 1
		default:
			col = found
		}
	}
	v.cur.col = col
	return true
}

var reverseFind = map[string]string{"f": "F", "F": "f", "t": "T", "T": "t"}

func (m *Model) visualKey(msg tea.KeyMsg) tea.Cmd {
	v := &m.vis
	s := msg.String()
	if len(v.lines) == 0 {
		m.exitVisual()
		return nil
	}
	n := max(v.count, 1)
	if v.pending != "" {
		op := v.pending
		v.pending = ""
		v.count = 0
		switch {
		case op == "g" && s == "g":
			v.cur = pos{0, 0}
		case op != "g" && len(msg.Runes) == 1:
			v.lastFind.op, v.lastFind.ch = op, msg.Runes[0]
			v.find(op, msg.Runes[0], n)
			v.want = v.cur.col
		}
		return nil
	}
	if len(s) == 1 && s[0] >= '0' && s[0] <= '9' && (s != "0" || v.count > 0) {
		v.count = min(v.count*10+int(s[0]-'0'), 9999)
		return nil
	}
	v.count = 0
	horizontal := true
	switch s {
	case "esc", "q":
		m.exitVisual()
		return nil
	case "h", "left":
		v.cur.col -= n
	case "l", "right":
		v.cur.col += n
	case "j", "down":
		v.cur.row += n
		horizontal = false
	case "k", "up":
		v.cur.row -= n
		horizontal = false
	case "w":
		for ; n > 0; n-- {
			v.wordForward()
		}
	case "b":
		for ; n > 0; n-- {
			v.wordBackward()
		}
	case "e":
		for ; n > 0; n-- {
			v.wordEnd()
		}
	case "0", "home":
		v.cur.col = 0
	case "^":
		v.cur.col = 0
		for v.cur.col < v.lineLen(v.cur.row)-1 && unicode.IsSpace(v.lines[v.cur.row][v.cur.col]) {
			v.cur.col++
		}
	case "$", "end":
		v.cur.col = v.lineLen(v.cur.row) - 1
	case "g":
		v.pending = "g"
		return nil
	case "G":
		v.cur.row = len(v.lines) - 1
		if n > 1 {
			v.cur.row = n - 1
		}
		horizontal = false
	case "ctrl+d", "pgdown":
		v.cur.row += max(m.layout().listH/2, 1) * n
		horizontal = false
	case "ctrl+u", "pgup":
		v.cur.row -= max(m.layout().listH/2, 1) * n
		horizontal = false
	case "f", "F", "t", "T":
		v.pending = s
		v.count = n
		return nil
	case ";", ",":
		if v.lastFind.op != "" {
			op := v.lastFind.op
			if s == "," {
				op = reverseFind[op]
			}
			v.find(op, v.lastFind.ch, n)
		}
	case "o":
		v.cur, v.anc = v.anc, v.cur
	case "v":
		if !v.line {
			m.exitVisual()
			return nil
		}
		v.line = false
	case "V":
		if v.line {
			m.exitVisual()
			return nil
		}
		v.line = true
	case "y", "enter":
		text := v.selection()
		m.exitVisual()
		return m.copy(text, "selection")
	case "Y":
		v.line = true
		text := v.selection()
		m.exitVisual()
		return m.copy(text, "lines")
	default:
		return nil
	}
	if horizontal {
		v.clamp()
		v.want = v.cur.col
	} else {
		v.cur.row = min(max(v.cur.row, 0), len(v.lines)-1)
		v.cur.col = v.want
		v.clamp()
	}
	return nil
}

func (v *visualState) bounds() (pos, pos) {
	a, b := v.anc, v.cur
	if b.row < a.row || b.row == a.row && b.col < a.col {
		a, b = b, a
	}
	return a, b
}

func (v *visualState) selected(p pos) bool {
	a, b := v.bounds()
	if p.row < a.row || p.row > b.row {
		return false
	}
	if v.line {
		return true
	}
	if p.row == a.row && p.col < a.col || p.row == b.row && p.col > b.col {
		return false
	}
	return true
}

// selection returns the selected text.
func (v *visualState) selection() string {
	a, b := v.bounds()
	if v.line {
		rows := make([]string, 0, b.row-a.row+1)
		for r := a.row; r <= b.row; r++ {
			rows = append(rows, string(v.lines[r]))
		}
		return strings.Join(rows, "\n")
	}
	var parts []string
	for r := a.row; r <= b.row; r++ {
		line := v.lines[r]
		from, to := 0, len(line)
		if r == a.row {
			from = min(a.col, len(line))
		}
		if r == b.row {
			to = min(b.col+1, len(line))
		}
		parts = append(parts, string(line[from:max(to, from)]))
	}
	return strings.Join(parts, "\n")
}

// visualView renders the buffer with the selection and cursor, soft-wrapped
// to w columns and scrolled to keep the cursor visible.
func (m *Model) visualView(w, h int) string {
	v := &m.vis
	w, h = max(w, 4), max(h, 1)
	sel := m.st.accent.Underline(true)
	cursor := m.st.ok.Bold(true).Underline(true)
	var rows []string
	curRow := 0
	for i, line := range v.lines {
		var b strings.Builder
		width := 0
		flush := func() {
			rows = append(rows, b.String())
			b.Reset()
			width = 0
		}
		cells := len(line)
		if cells == 0 {
			cells = 1 // an empty line still holds the cursor
		}
		for j := 0; j < cells; j++ {
			ch := " "
			if j < len(line) {
				ch = string(line[j])
			}
			cw := max(ansi.StringWidth(ch), 1)
			if width+cw > w {
				flush()
			}
			p := pos{i, j}
			style := m.st.snippet
			switch {
			case p == v.cur:
				style = cursor
				curRow = len(rows)
			case v.selected(p) && j < len(line):
				style = sel
			}
			b.WriteString(style.Render(ch))
			width += cw
		}
		flush()
	}
	if curRow < v.top {
		v.top = curRow
	}
	if curRow >= v.top+h {
		v.top = curRow - h + 1
	}
	v.top = min(v.top, max(len(rows)-h, 0))
	end := min(v.top+h, len(rows))
	out := rows[v.top:end]
	for len(out) < h {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}

// visualStatus is the left part of the status line in visual mode.
func (m *Model) visualStatus() string {
	label := " VISUAL"
	if m.vis.line {
		label = " V-LINE"
	}
	a, b := m.vis.bounds()
	info := fmt.Sprintf(" %d:%d", m.vis.cur.row+1, m.vis.cur.col+1)
	if m.vis.line {
		info += fmt.Sprintf(" (%d lines)", b.row-a.row+1)
	}
	if m.vis.pending != "" {
		info += " " + m.vis.pending
	}
	return m.st.modeDet.Render(label) + m.st.statusBar.Render(info+" ")
}
