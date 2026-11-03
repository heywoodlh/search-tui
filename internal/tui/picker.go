package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The engine picker is a dropdown under the search bar listing every
// usable engine (unconfigured ones are never registered, so never shown).

type picker struct {
	open  bool
	idx   int
	items []string
}

// engineNames lists engines that can return results in the app.
func (m *Model) engineNames() []string {
	var names []string
	for _, n := range m.app.Reg.Names() {
		if !m.app.Reg.Info(n).BrowserOnly {
			names = append(names, n)
		}
	}
	return names
}

func (m *Model) currentEngine() string {
	switch {
	case m.plan.Engine != nil:
		return m.plan.Engine.Name()
	case m.opts.Engine != "":
		if e, ok := m.app.Reg.Get(m.opts.Engine); ok {
			return e.Name()
		}
	}
	return m.app.Cfg.DefaultEngine
}

// openPicker opens the dropdown, or moves the highlight by dir when it is
// already open (so repeated tab presses walk the list).
func (m *Model) openPicker(dir int) {
	if m.picker.open {
		m.pickerMove(dir)
		return
	}
	m.picker = picker{open: true, items: m.engineNames()}
	if len(m.picker.items) == 0 {
		m.picker.open = false
		return
	}
	cur := m.currentEngine()
	for i, n := range m.picker.items {
		if n == cur {
			m.picker.idx = i
		}
	}
	m.pickerMove(dir)
}

func (m *Model) pickerMove(d int) {
	n := len(m.picker.items)
	if n == 0 {
		return
	}
	m.picker.idx = ((m.picker.idx+d)%n + n) % n
}

func (m *Model) closePicker() { m.picker.open = false }

func (m *Model) pickerKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "down", "j", "tab", "ctrl+n":
		m.pickerMove(1)
	case "up", "k", "shift+tab", "ctrl+p":
		m.pickerMove(-1)
	case "g", "home":
		m.picker.idx = 0
	case "G", "end":
		m.picker.idx = len(m.picker.items) - 1
	case "enter", "l", "right", " ":
		name := m.picker.items[m.picker.idx]
		m.closePicker()
		return m.selectEngine(name)
	case "esc", "q", "h", "left":
		m.closePicker()
	default:
		// Type a letter to jump to the next engine starting with it.
		if r := msg.Runes; len(r) == 1 {
			n := len(m.picker.items)
			for i := 1; i <= n; i++ {
				j := (m.picker.idx + i) % n
				if strings.HasPrefix(m.picker.items[j], strings.ToLower(string(r))) {
					m.picker.idx = j
					break
				}
			}
		}
	}
	return nil
}

// selectEngine switches engine and re-runs the current query.
func (m *Model) selectEngine(name string) tea.Cmd {
	e, ok := m.app.Reg.Get(name)
	if !ok {
		return nil
	}
	m.opts.Engine = e.Name()
	if m.opts.Kind != "" && !e.Supports(m.opts.Kind) {
		m.opts.Kind = ""
	}
	if m.plan.Query.Kind != "" && e.Supports(m.plan.Query.Kind) {
		m.opts.Kind = m.plan.Query.Kind
	}
	m.stripBang()
	if strings.TrimSpace(m.input.Value()) == "" {
		m.plan.Engine = nil
		return m.setFlash("engine: " + m.app.Reg.Display(e.Name()))
	}
	m.opts.Page = 1
	return m.search(m.input.Value())
}

// pickerGeometry returns the dropdown's top row and visible window.
func (m *Model) pickerGeometry() (top, start, count int) {
	l := m.layout()
	top = max(l.inputH, 1)
	avail := m.height - top - l.statusH - 2 // borders
	count = max(min(len(m.picker.items), avail), 1)
	start = 0
	if m.picker.idx >= count {
		start = m.picker.idx - count + 1
	}
	return top, start, count
}

func (m *Model) pickerLines() []string {
	_, start, count := m.pickerGeometry()
	cur := m.currentEngine()
	nameW := 0
	for _, n := range m.picker.items {
		nameW = max(nameW, lipgloss.Width(m.app.Reg.Display(n)))
	}
	maxW := 0
	for _, n := range m.picker.items {
		info := m.app.Reg.Info(n)
		w := 2 + nameW + 9 + len(info.Kinds)*7
		maxW = max(maxW, w)
	}
	maxW = max(min(maxW+2, m.width-2), 8)
	var rows []string
	for i := start; i < min(start+count, len(m.picker.items)); i++ {
		n := m.picker.items[i]
		info := m.app.Reg.Info(n)
		mark := "  "
		if n == cur {
			mark = m.st.check + " "
		}
		kinds := make([]string, len(info.Kinds))
		for j, k := range info.Kinds {
			kinds[j] = string(k)
		}
		bangs := ""
		if len(info.Bangs) > 0 {
			bangs = "!" + info.Bangs[0]
		}
		name := fmt.Sprintf("%-*s", nameW, m.app.Reg.Display(n))
		extra := fmt.Sprintf("  %-5s %s", bangs, strings.Join(kinds, "/"))
		row := ansi.Truncate(mark+name+extra, maxW-2, "…")
		row += strings.Repeat(" ", max(maxW-2-lipgloss.Width(row), 0))
		rest := strings.TrimPrefix(row, mark+name)
		if i == m.picker.idx {
			// Highlight with colour and a pointer only: no background fill.
			row = m.st.selTitle.Render(mark+name) + m.st.accent.UnsetBold().Render(rest)
			rows = append(rows, m.st.bar.Render(m.st.pointer)+row+" ")
		} else {
			row = mark + m.st.title.Render(name) + m.st.muted.Render(rest)
			rows = append(rows, " "+row+" ")
		}
	}
	border := lipgloss.RoundedBorder()
	if m.app.Cfg.UI.ASCII {
		border = lipgloss.NormalBorder()
	}
	box := lipgloss.NewStyle().Border(border).BorderForeground(m.st.accentColor).Render(strings.Join(rows, "\n"))
	return strings.Split(box, "\n")
}

// overlayPicker draws the dropdown over the frame lines.
func (m *Model) overlayPicker(lines []string) []string {
	if !m.picker.open || m.height < 4 {
		return lines
	}
	top, _, _ := m.pickerGeometry()
	for i, bl := range m.pickerLines() {
		row := top + i
		if row >= len(lines) {
			break
		}
		bw := lipgloss.Width(bl)
		if bw >= m.width {
			lines[row] = ansi.Truncate(bl, m.width, "")
			continue
		}
		lines[row] = bl + ansi.TruncateLeft(lines[row], bw, "")
	}
	return lines
}

func (m *Model) pickerMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.Button {
	case tea.MouseButtonWheelDown:
		m.pickerMove(1)
	case tea.MouseButtonWheelUp:
		m.pickerMove(-1)
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return nil
		}
		top, start, count := m.pickerGeometry()
		w := lipgloss.Width(m.pickerLines()[0])
		i := msg.Y - top - 1 // border
		if msg.X < w && i >= 0 && i < count && start+i < len(m.picker.items) {
			name := m.picker.items[start+i]
			m.closePicker()
			return m.selectEngine(name)
		}
		m.closePicker()
	}
	return nil
}
