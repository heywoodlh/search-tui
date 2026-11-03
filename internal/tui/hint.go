package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Link hints, as in Vimium/qutebrowser: f labels every visible result with
// a short code; typing it jumps to that result and shows its details. F does
// the same but opens the result in the browser.

type hintState struct {
	active bool
	open   bool
	typed  string
	labels map[string]int // label -> result index
	byIdx  map[int]string
}

const defaultHintChars = "asdfghjkl"

// hintLabels returns n distinct, prefix-free labels from chars: single
// characters when they suffice, otherwise all two-character labels.
func hintLabels(n int, chars string) []string {
	cs := []rune(chars)
	if n <= 0 || len(cs) == 0 {
		return nil
	}
	var out []string
	if n <= len(cs) {
		for _, c := range cs[:n] {
			out = append(out, string(c))
		}
		return out
	}
	for _, a := range cs {
		for _, b := range cs {
			out = append(out, string(a)+string(b))
			if len(out) == n {
				return out
			}
		}
	}
	return out // more results than labels: the rest stay unlabelled
}

// visibleResults lists result indices currently on screen.
func (m *Model) visibleResults() []int {
	starts := m.itemStarts()
	h := m.listHeight()
	var vis []int
	for i := 0; i < len(starts)-1; i++ {
		if starts[i] >= m.offset && starts[i] < m.offset+h {
			vis = append(vis, i)
		}
	}
	return vis
}

func (m *Model) startHint(open bool) tea.Cmd {
	if m.layout().showDetailFull {
		m.mode = modeNormal // the list must be visible to pick from it
	}
	vis := m.visibleResults()
	if len(vis) == 0 {
		return m.setFlash("no results to hint")
	}
	chars := m.app.Cfg.UI.HintChars
	if strings.TrimSpace(chars) == "" {
		chars = defaultHintChars
	}
	labels := hintLabels(len(vis), chars)
	m.hint = hintState{active: true, open: open, labels: map[string]int{}, byIdx: map[int]string{}}
	for i, l := range labels {
		m.hint.labels[l] = vis[i]
		m.hint.byIdx[vis[i]] = l
	}
	return nil
}

func (m *Model) hintKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.hint = hintState{}
		return nil
	case "backspace":
		if m.hint.typed != "" {
			m.hint.typed = m.hint.typed[:len(m.hint.typed)-1]
		}
		return nil
	}
	if len(msg.Runes) != 1 {
		return nil
	}
	m.hint.typed += string(msg.Runes)
	if idx, ok := m.hint.labels[m.hint.typed]; ok {
		open := m.hint.open
		m.hint = hintState{}
		m.jump(idx)
		if open {
			return m.openURL(m.results[idx].URL)
		}
		m.mode = modeDetail
		m.refreshDetail()
		m.detail.GotoTop()
		return nil
	}
	for l := range m.hint.labels {
		if strings.HasPrefix(l, m.hint.typed) {
			return nil // keep typing
		}
	}
	m.hint = hintState{}
	return m.setFlash("no such hint")
}
