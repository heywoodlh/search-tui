package tui

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/heywoodlh-bot/tui-search/internal/engine"
)

type layoutKind int

const (
	layoutStacked layoutKind = iota
	layoutSplit
)

type layoutInfo struct {
	kind                    layoutKind
	inputH, sumH, listTop   int
	listH, listW, previewW  int
	statusH, errH           int
	compact, showDetailFull bool
}

func (m *Model) layoutMode() layoutKind {
	switch m.app.Cfg.UI.Layout {
	case "split":
		if m.width >= 60 {
			return layoutSplit
		}
	case "stacked":
		return layoutStacked
	default:
		if m.width >= m.app.Cfg.UI.SplitMinWidth && m.height >= 12 {
			return layoutSplit
		}
	}
	return layoutStacked
}

func (m *Model) layout() layoutInfo {
	l := layoutInfo{kind: m.layoutMode(), inputH: 1}
	if m.height >= 12 {
		l.inputH = 2 // input + rule
	}
	if *m.app.Cfg.UI.StatusBar {
		l.statusH = 1
	}
	if m.err != nil {
		l.errH = 1
	}
	l.listW = m.width
	if l.kind == layoutSplit {
		l.listW = max(m.width*45/100, 40)
		l.previewW = m.width - l.listW
	}
	l.showDetailFull = m.mode == modeDetail && l.kind == layoutStacked
	if m.showSummary && !m.summaryEmpty() && !l.showDetailFull && m.height >= 16 && m.mode != modeHelp {
		l.sumH = len(m.summaryLines(m.width))
	}
	l.listTop = l.inputH + l.sumH
	l.listH = max(m.height-l.listTop-l.statusH-l.errH, 0)
	l.compact = l.listH < 10 || l.listW < 40
	return l
}

func (m *Model) listHeight() int { return max(m.layout().listH, 1) }

func (m *Model) resize() {
	l := m.layout()
	badge := lipgloss.Width(m.badgeText())
	m.input.Width = max(m.width-badge-3, 5)
	m.help.Width, m.help.Height = m.width, max(m.height-l.statusH, 1)
	m.refreshDetail()
	m.refreshHelp()
}

// ---------------------------------------------------------------- list

func (m *Model) contentWidth() int { return max(m.layout().listW-3, 8) }

func (m *Model) renderItems() [][]string {
	w := m.contentWidth()
	if m.items != nil && m.itemsWidth == w {
		return m.items
	}
	compact := m.layout().compact
	snipLines := m.app.Cfg.UI.SnippetLines
	items := make([][]string, len(m.results))
	for i, r := range m.results {
		var lines []string
		num := fmt.Sprintf("%d. ", i+1)
		lines = append(lines, num+ansi.Truncate(oneLine(r.Title), w-len(num), "…"))
		sub := displayURL(r.URL)
		if r.Code != nil {
			sub = r.Code.Repo
			if r.Code.Line > 0 {
				sub += fmt.Sprintf(" · L%d", r.Code.Line)
			}
			if r.Code.Language != "" {
				sub += " · " + r.Code.Language
			}
		}
		lines = append(lines, m.st.url.Render(ansi.Truncate(sub, w, "…")))
		if compact {
			items[i] = lines
			continue
		}
		if r.Meta != "" && r.Code == nil {
			lines = append(lines, m.st.meta.Render(ansi.Truncate(r.Meta, w, "…")))
		}
		if r.Code != nil && len(r.Code.Fragments) > 0 {
			lines = append(lines, m.codeLines(engine.Focus(r.Code.Fragments[0], m.plan.Query.Text, snipLines+1), w, 0, false)...)
		} else if r.Snippet != "" {
			wrapped := strings.Split(ansi.Wordwrap(r.Snippet, w, ""), "\n")
			if len(wrapped) > snipLines {
				wrapped = wrapped[:snipLines]
				wrapped[snipLines-1] = ansi.Truncate(wrapped[snipLines-1], w-1, "") + "…"
			}
			for _, l := range wrapped {
				lines = append(lines, m.st.snippet.Render(l))
			}
		}
		items[i] = lines
	}
	m.items, m.itemsWidth = items, w
	return items
}

// codeLines renders a fragment with line numbers. When wrap is false lines
// are truncated, otherwise hard-wrapped (for narrow detail views).
func (m *Model) codeLines(f engine.Fragment, w, maxLines int, wrap bool) []string {
	var out []string
	src := strings.Split(strings.ReplaceAll(f.Text, "\t", "    "), "\n")
	gutter := 5
	for j, l := range src {
		if maxLines > 0 && len(out) >= maxLines {
			out = append(out, m.st.muted.Render("     ⋮"))
			break
		}
		num := "    "
		if f.Line > 0 {
			num = fmt.Sprintf("%4d", f.Line+j)
		}
		cw := max(w-gutter-1, 4)
		if wrap {
			for k, part := range strings.Split(ansi.Hardwrap(l, cw, true), "\n") {
				n := num
				if k > 0 {
					n = "   ↪"
				}
				out = append(out, m.st.lineNo.Render(n+"│")+" "+m.st.code.Render(part))
			}
		} else {
			out = append(out, m.st.lineNo.Render(num+"│")+" "+m.st.code.Render(ansi.Truncate(l, cw, "…")))
		}
	}
	return out
}

func (m *Model) sepLines() int {
	if m.layout().compact {
		return 0
	}
	return 1
}

func (m *Model) itemStarts() []int {
	items := m.renderItems()
	starts := make([]int, len(items)+1)
	sep := m.sepLines()
	for i, it := range items {
		starts[i+1] = starts[i] + len(it) + sep
	}
	return starts
}

func (m *Model) itemHeightGuess() int {
	items := m.renderItems()
	if len(items) == 0 {
		return 1
	}
	return max(len(items[0])+m.sepLines(), 1)
}

func (m *Model) itemAtLine(line int) int {
	starts := m.itemStarts()
	for i := 0; i < len(starts)-1; i++ {
		if line >= starts[i] && line < starts[i+1] {
			return i
		}
	}
	return -1
}

func (m *Model) ensureVisible() {
	if len(m.results) == 0 {
		m.offset = 0
		return
	}
	starts := m.itemStarts()
	h := m.listHeight()
	top, bottom := starts[m.sel], starts[m.sel+1]-m.sepLines()
	if top < m.offset {
		m.offset = top
	}
	if bottom > m.offset+h {
		m.offset = bottom - h
	}
	total := starts[len(starts)-1]
	m.offset = max(min(m.offset, total-h), 0)
}

func (m *Model) viewList(l layoutInfo) string {
	if len(m.results) == 0 {
		return m.viewEmpty(l)
	}
	items := m.renderItems()
	sep := m.sepLines()
	var all []string
	for i, it := range items {
		for j, line := range it {
			prefix := "  "
			if i == m.sel {
				prefix = m.st.bar.Render(m.st.selBar) + " "
				if j == 0 {
					line = m.st.selTitle.Render(line)
				}
			} else if j == 0 {
				line = m.st.title.Render(line)
			}
			all = append(all, prefix+line)
		}
		for s := 0; s < sep; s++ {
			all = append(all, "")
		}
	}
	end := min(m.offset+l.listH, len(all))
	vis := all[min(m.offset, end):end]
	for len(vis) < l.listH {
		vis = append(vis, "")
	}
	return lipgloss.NewStyle().Width(l.listW).MaxWidth(l.listW).Render(strings.Join(vis, "\n"))
}

func (m *Model) viewEmpty(l layoutInfo) string {
	w := l.listW
	var lines []string
	switch {
	case m.loading:
		lines = append(lines, "  "+m.spinner.View()+" searching "+m.plan.Engine.Name()+"…")
	case m.plan.Engine != nil && m.err == nil:
		lines = append(lines, m.st.muted.Render("  No results."))
	default:
		tips := []string{
			m.st.accent.Render("search") + m.st.muted.Render(" — type a query and press enter"),
			"",
			m.st.muted.Render("Bangs pick an engine:"),
			"  !ddg duckduckgo   !b bing   !w wikipedia",
			"  !so stackoverflow   !hn hackernews",
			"  !gh repos   !ghc code   !ghi issues   !sg sourcegraph",
			"  !yt !aw !nix !hm !mdn … open in your browser",
			"  !g !k !br search in-app once configured, else open the site",
			"",
			m.st.muted.Render("tab picks an engine · ctrl+e writes the query in $EDITOR · esc then ? for help"),
		}
		for _, t := range tips {
			lines = append(lines, "  "+ansi.Truncate(t, max(w-2, 1), "…"))
		}
	}
	if len(lines) > l.listH {
		lines = lines[:l.listH]
	}
	for len(lines) < l.listH {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().Width(w).MaxWidth(w).Render(strings.Join(lines, "\n"))
}

// ---------------------------------------------------------------- summary

func (m *Model) summaryEmpty() bool { return m.summary.Empty() && m.extSummary == "" }

func (m *Model) summaryLines(width int) []string {
	w := max(width-4, 10)
	maxLines := max(min(m.height/4, 9), 3)
	var lines []string
	head := m.st.sumTitle.Render("Summary")
	if len(m.summary.Terms) > 0 && width > 50 {
		head += m.st.muted.Render(" · related: " + strings.Join(m.summary.Terms, ", "))
	}
	lines = append(lines, " "+ansi.Truncate(head, width-2, "…"))
	body := []string{}
	if m.extSummary != "" {
		body = append(body, strings.Split(ansi.Wordwrap(m.extSummary, w, ""), "\n")...)
	} else {
		for _, s := range m.summary.Sentences {
			txt := fmt.Sprintf("%s %s %s", m.st.bullet, s.Text, m.st.muted.Render(fmt.Sprintf("[%d]", s.Source)))
			wrapped := strings.Split(ansi.Wordwrap(txt, w, ""), "\n")
			for i := 1; i < len(wrapped); i++ {
				wrapped[i] = "  " + wrapped[i]
			}
			body = append(body, wrapped...)
		}
	}
	if len(body) > maxLines-1 {
		body = body[:maxLines-1]
		body[len(body)-1] = ansi.Truncate(body[len(body)-1], w-1, "") + "…"
	}
	for _, b := range body {
		lines = append(lines, "  "+b)
	}
	lines = append(lines, m.st.border.Render(strings.Repeat("─", max(width, 1))))
	return lines
}

// ---------------------------------------------------------------- detail

func (m *Model) refreshDetail() {
	l := m.layout()
	w, h := l.previewW-2, l.listH
	if l.kind == layoutStacked {
		w = m.width - 1
	}
	if w < 4 || h < 1 {
		return
	}
	m.detail.Width, m.detail.Height = w, h
	r, ok := m.current()
	if !ok {
		m.detail.SetContent(m.st.muted.Render("No result selected."))
		return
	}
	m.detail.SetContent(m.detailContent(r, w))
}

func (m *Model) detailContent(r engine.Result, w int) string {
	var b []string
	add := func(s ...string) { b = append(b, s...) }
	wrap := func(s string) []string { return strings.Split(ansi.Wordwrap(s, w, ""), "\n") }
	for _, l := range wrap(r.Title) {
		add(m.st.selTitle.Render(l))
	}
	add(m.st.url.Render(ansi.Hardwrap(r.URL, w, true)))
	if r.Meta != "" {
		for _, l := range wrap(r.Meta) {
			add(m.st.meta.Render(l))
		}
	}
	add("")
	if r.Code != nil {
		if len(r.Code.Fragments) == 0 && r.Snippet != "" {
			add(wrap(r.Snippet)...)
		}
		for i, f := range r.Code.Fragments {
			if i > 0 {
				add(m.st.muted.Render("     ⋯"))
			}
			add(m.codeLines(f, w, 0, true)...)
		}
		add("", m.st.muted.Render(ansi.Wordwrap(fmt.Sprintf("%s copy code · %s open file in editor · %s open in browser", m.keys.first(ActYankSnippet), m.keys.first(ActEdit), m.keys.first(ActOpen)), w, "")))
		return strings.Join(b, "\n")
	}
	if r.Snippet != "" {
		add(wrap(r.Snippet)...)
		add("")
	}
	if p, ok := m.pages[r.URL]; ok {
		if len(p.Summary) > 0 {
			add(m.st.sumTitle.Render("Page summary"))
			for _, s := range p.Summary {
				lines := wrap(m.st.bullet + " " + s.Text)
				for i := 1; i < len(lines); i++ {
					lines[i] = "  " + lines[i]
				}
				add(lines...)
			}
			add("")
		}
		add(m.st.sumTitle.Render("Page"), m.st.border.Render(strings.Repeat("─", max(w, 1))))
		md := p.Markdown
		limit := 400
		if m.app.Cfg.LowPower {
			limit = 120
		}
		mdLines := strings.Split(md, "\n")
		if len(mdLines) > limit {
			mdLines = append(mdLines[:limit], "…", m.st.muted.Render(fmt.Sprintf("(truncated; press %s to read it all in $EDITOR)", m.keys.first(ActEdit))))
		}
		for _, l := range mdLines {
			if strings.HasPrefix(l, "#") {
				add(m.st.title.Render(ansi.Wordwrap(l, w, "")))
				continue
			}
			add(wrap(l)...)
		}
	} else if err, ok := m.pageErrs[r.URL]; ok {
		add(m.st.err.Render(ansi.Wordwrap("page fetch failed: "+err.Error(), w, "")))
	} else if m.busy != "" {
		add(m.st.muted.Render(m.busy))
	} else {
		add(m.st.muted.Render(ansi.Wordwrap(fmt.Sprintf("%s fetch & summarise page · %s read in $EDITOR · %s open in browser · %s copy URL",
			m.keys.first(ActSummarizePage), m.keys.first(ActEdit), m.keys.first(ActOpen), m.keys.first(ActYankURL)), w, "")))
	}
	return strings.Join(b, "\n")
}

func (m *Model) viewDetail(l layoutInfo, w int) string {
	content := m.detail.View()
	return lipgloss.NewStyle().Width(w).MaxWidth(w).Height(l.listH).MaxHeight(l.listH).Render(content)
}

// ---------------------------------------------------------------- help

func (m *Model) refreshHelp() {
	w := max(m.width-4, 10)
	var b []string
	b = append(b, m.st.accent.Render("Keybindings")+m.st.muted.Render(" (configure in [keys])"), "")
	for _, kv := range m.keys.helpLines() {
		k := m.st.key.Render(kv[0])
		pad := strings.Repeat(" ", max(24-lipgloss.Width(k), 1))
		b = append(b, ansi.Truncate(k+pad+kv[1], w, "…"))
	}
	b = append(b, "",
		m.st.accent.Render("Insert mode"),
		"enter search · esc back to results / normal mode · ↑/↓ history",
		"tab complete !bang, otherwise open the engine picker · ctrl+e compose in $EDITOR",
		"",
		m.st.accent.Render("Engine picker"),
		"↑/↓, j/k or tab/shift+tab move · enter select · letter jumps · esc close",
		"Engines that need credentials appear once configured (search --list-engines)",
		"",
		m.st.accent.Render("Mouse"),
		"wheel scrolls · click selects · click again for details · click query to edit",
		"",
		m.st.accent.Render("Bangs"),
	)
	var bangs []string
	for _, e := range m.app.Bangs.List() {
		bangs = append(bangs, e.Bang)
	}
	b = append(b, strings.Split(ansi.Wordwrap(strings.Join(bangs, " "), w, ""), "\n")...)
	if p := m.app.Cfg.Path(); p != "" {
		b = append(b, "", m.st.muted.Render("config: "+p))
	}
	m.help.SetContent(lipgloss.NewStyle().Padding(0, 2).Render(strings.Join(b, "\n")))
}

// ---------------------------------------------------------------- frame

func (m *Model) badgeText() string {
	name := m.app.Cfg.DefaultEngine
	kind := ""
	if m.plan.Engine != nil {
		name = m.plan.Engine.Name()
		kind = string(m.plan.Query.Kind)
	} else if m.opts.Engine != "" {
		name = m.opts.Engine
	}
	if m.opts.Kind != "" && m.plan.Engine == nil {
		kind = string(m.opts.Kind)
	}
	label := m.app.Reg.Display(name)
	if kind != "" && kind != string(engine.KindWeb) {
		label += "·" + kind
	}
	if m.width < 40 && len(label) > 10 {
		label = label[:10]
	}
	return m.st.badge.Render(label)
}

func (m *Model) viewHeader(l layoutInfo) string {
	line := m.badgeText() + " " + m.input.View()
	line = ansi.Truncate(line, m.width, "")
	if l.inputH > 1 {
		return line + "\n" + m.st.border.Render(strings.Repeat("─", max(m.width, 1)))
	}
	return line
}

func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	lines := strings.Split(m.render(), "\n")
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	lines = m.overlayPicker(lines)
	return strings.Join(lines, "\n")
}

func (m *Model) render() string {
	if m.mode == modeHelp {
		out := m.help.View()
		if *m.app.Cfg.UI.StatusBar {
			out = lipgloss.NewStyle().Height(max(m.height-1, 0)).MaxHeight(max(m.height-1, 0)).Render(out)
			out += "\n" + m.viewStatus()
		}
		return out
	}
	l := m.layout()
	var parts []string
	parts = append(parts, m.viewHeader(l))
	if l.sumH > 0 {
		parts = append(parts, strings.Join(m.summaryLines(m.width), "\n"))
	}
	if l.listH > 0 {
		switch {
		case l.showDetailFull:
			parts = append(parts, m.viewDetail(l, m.width))
		case l.kind == layoutSplit:
			left := m.viewList(l)
			sepStyle := m.st.border
			if m.mode == modeDetail {
				sepStyle = m.st.bar
			}
			sep := sepStyle.Render(strings.TrimRight(strings.Repeat("│\n", l.listH), "\n"))
			right := m.viewDetail(l, l.previewW-2)
			parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Top, left, sep, " ", right))
		default:
			parts = append(parts, m.viewList(l))
		}
	}
	if l.errH > 0 {
		parts = append(parts, m.st.err.Render(ansi.Truncate(" ✗ "+oneLine(m.err.Error()), m.width, "…")))
	}
	if l.statusH > 0 {
		parts = append(parts, m.viewStatus())
	}
	return strings.Join(parts, "\n")
}

func (m *Model) viewStatus() string {
	var modeBadge string
	switch m.mode {
	case modeInsert:
		modeBadge = m.st.modeIns.Render("INS")
	case modeDetail:
		modeBadge = m.st.modeDet.Render("VIEW")
	case modeHelp:
		modeBadge = m.st.modeDet.Render("HELP")
	default:
		modeBadge = m.st.modeNorm.Render("NOR")
	}
	bar := m.st.statusBar
	left := modeBadge
	info := ""
	if len(m.results) > 0 {
		info = fmt.Sprintf(" %d/%d", m.sel+1, len(m.results))
		if m.plan.Query.Page > 1 {
			info += fmt.Sprintf(" p%d", m.plan.Query.Page)
		}
	}
	if m.loading {
		info += " " + m.spinner.View() + " searching"
	} else if m.busy != "" {
		info += " " + m.spinner.View() + " " + m.busy
	}
	left += bar.Render(info + " ")
	avail := m.width - lipgloss.Width(left)
	var right string
	if m.flash != "" {
		right = bar.Render(ansi.Truncate(" "+m.flash+" ", max(avail, 0), "…"))
	} else {
		right = m.renderHints(m.statusHints(), avail)
	}
	fill := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 0)
	return ansi.Truncate(left+bar.Render(strings.Repeat(" ", fill))+right, m.width, "")
}

func (m *Model) statusHints() [][2]string {
	if m.picker.open {
		return [][2]string{{"↑↓/jk/tab", "move"}, {"enter", "select"}, {"esc", "close"}, {"a-z", "jump"}}
	}
	switch m.mode {
	case modeInsert:
		v := m.input.Value()
		word := v[strings.LastIndexByte(v, ' ')+1:]
		if strings.HasPrefix(word, "!") && len(word) > 1 {
			var hs [][2]string
			for _, c := range m.app.Bangs.Complete(word) {
				hs = append(hs, [2]string{c, ""})
				if len(hs) >= 12 {
					break
				}
			}
			if len(hs) > 0 {
				return append([][2]string{{"tab", "complete"}}, hs...)
			}
		}
		return [][2]string{{"enter", "search"}, {"esc", "normal"}, {"tab", "engines"}, {"ctrl+e", "editor"}, {"↑↓", "history"}, {"ctrl+c", "quit"}}
	case modeDetail:
		return append([][2]string{{m.keys.first(ActBack), "back"}, {"enter", "open"}}, m.keys.hints(ActEdit, ActSummarizePage, ActYankURL, ActYankSnippet, ActHelp)...)
	case modeHelp:
		return [][2]string{{m.keys.first(ActBack), "close"}, {"j/k", "scroll"}}
	}
	return m.keys.hints(ActSearch, ActDetails, ActOpen, ActYankURL, ActYankSnippet, ActEdit, ActSummarizePage, ActNextPage, ActNextEngine, ActCycleType, ActHelp, ActQuit)
}

func (m *Model) renderHints(hs [][2]string, avail int) string {
	bar := m.st.statusBar
	var out string
	w := 0
	for _, h := range hs {
		seg := bar.Render(" ") + m.st.key.Inherit(bar).Render(h[0])
		if h[1] != "" {
			seg += bar.Render(" " + h[1])
		}
		sw := lipgloss.Width(seg)
		if w+sw+1 > avail {
			break
		}
		out += seg
		w += sw
	}
	return out + bar.Render(" ")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func displayURL(u string) string {
	p, err := url.Parse(u)
	if err != nil || p.Host == "" {
		return u
	}
	s := strings.TrimPrefix(p.Host, "www.") + p.EscapedPath()
	if p.RawQuery != "" {
		s += "?" + p.RawQuery
	}
	return strings.TrimSuffix(s, "/")
}
