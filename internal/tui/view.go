package tui

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/heywoodlh/tui-search/internal/engine"
	"github.com/heywoodlh/tui-search/internal/safe"
)

type layoutInfo struct {
	inputH, sumH, listTop   int
	listH, listW            int
	statusH, errH           int
	compact, showDetailFull bool
}

func (m *Model) layout() layoutInfo {
	l := layoutInfo{inputH: 1}
	if m.height >= 12 {
		l.inputH = 2 // input + rule
	}
	if *m.app.Cfg.UI.StatusBar {
		l.statusH = 1
	}
	if m.err != nil {
		l.errH = 1
	}
	// Details are an on-demand full-width view; normal browsing never
	// allocates a permanent second column for an empty preview.
	l.listW = m.width
	l.showDetailFull = m.mode == modeDetail || m.mode == modeVisual
	if m.showSummary && !m.summaryEmpty() && !l.showDetailFull && m.height >= 16 && m.mode != modeHelp {
		l.sumH = len(m.summaryLines(m.width))
	}
	if len(m.results) > 0 && !l.showDetailFull && m.mode != modeHelp && m.height >= 9 {
		l.sumH = max(l.sumH, 3) + 1 // pinned peek plus a separate divider
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
		ref := fmt.Sprintf("[%d] ", i+1) // link number for :open-link / :copy-link
		lines = append(lines, m.st.muted.Render(ref)+m.st.url.Render(ansi.Truncate(sub, max(w-len(ref), 1), "…")))
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
			if j == 0 && m.hint.active {
				if label, ok := m.hint.byIdx[i]; ok {
					// Typed characters are dimmed; the rest is what to type.
					typed := ""
					if strings.HasPrefix(label, m.hint.typed) {
						typed, label = m.hint.typed, label[len(m.hint.typed):]
					}
					line = m.st.muted.Render(typed) + m.st.key.Underline(true).Render(label) + " " + line
				}
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

func (m *Model) compactPeek() []string {
	r, ok := m.current()
	if !ok {
		return nil
	}
	w := max(m.width-4, 1)
	return []string{
		" " + m.st.selTitle.Render(ansi.Truncate(r.Title, w, "…")),
		" " + m.st.snippet.Render(ansi.Truncate(oneLine(r.Snippet), w, "…")),
		" " + m.st.muted.Render(fmt.Sprintf("[%d] ", m.sel+1)) + m.st.url.Render(ansi.Truncate(displayURL(r.URL), max(w-6, 1), "…")),
	}
}

// ---------------------------------------------------------------- detail

func (m *Model) refreshDetail() {
	l := m.layout()
	w, h := m.width-1, l.listH
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
	add(m.st.muted.Render(fmt.Sprintf("[%d] ", m.sel+1)) + m.st.url.Render(ansi.Hardwrap(r.URL, max(w-6, 8), true)))
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
		nums := m.pageLinkNumbers()
		md := m.renderPageLinks(p.Markdown, nums)
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
		if links := m.links()[len(m.results):]; len(links) > 0 {
			add("", m.st.sumTitle.Render("Links")+m.st.muted.Render(" (:open-link N, :copy-link N)"))
			for _, l := range links {
				add(ansi.Truncate(m.st.muted.Render(fmt.Sprintf("[%d] ", l.n))+m.st.url.Render(l.url), w, "…"))
			}
		}
	} else if err, ok := m.pageErrs[r.URL]; ok {
		add(m.st.err.Render(ansi.Wordwrap("page fetch failed: "+err.Error(), w, "")))
	} else if m.busy != "" {
		add(m.st.muted.Render(m.busy))
	} else {
		add(m.st.muted.Render(ansi.Wordwrap(fmt.Sprintf("%s read in $EDITOR · %s open in browser · %s copy URL",
			m.keys.first(ActEdit), m.keys.first(ActOpen), m.keys.first(ActYankURL)), w, "")))
	}
	return strings.Join(b, "\n")
}

func (m *Model) viewDetail(l layoutInfo, w int) string {
	content := m.detail.View()
	if m.mode == modeVisual {
		content = m.visualView(w, l.listH)
	}
	return lipgloss.NewStyle().Width(w).MaxWidth(w).Height(l.listH).MaxHeight(l.listH).Render(content)
}

// ---------------------------------------------------------------- help

func (m *Model) refreshHelp() {
	w := max(m.width-4, 10)
	var b []string
	m.helpSections = map[string]int{}
	section := func(topic, title string, lines ...string) {
		if len(b) > 0 {
			b = append(b, "")
		}
		m.helpSections[topic] = len(b)
		b = append(b, m.st.accent.Render(title))
		b = append(b, lines...)
	}
	section("keys", "Keybindings"+m.st.muted.Render(" (configure in [keys]; counts like 5j and 3G work)"))
	b = append(b, "")
	for _, kv := range m.keys.helpLines() {
		k := m.st.key.Render(kv[0])
		pad := strings.Repeat(" ", max(24-lipgloss.Width(k), 1))
		b = append(b, ansi.Truncate(k+pad+kv[1], w, "…"))
	}
	section("insert", "Insert mode (search bar)",
		"enter search · esc normal mode · ↑/↓ history · ctrl+w delete word",
		"tab complete !bang, otherwise open the engine picker · ctrl+e compose in $EDITOR")
	section("search", "Engine picker",
		"↑/↓, j/k or tab/shift+tab move · enter select · letter jumps · esc close",
		"Engines that need credentials appear once configured (:engines lists them)")
	section("hints", "Hints (f / F)",
		"f labels every visible result; type a label to jump to it and show its details",
		"F does the same but opens the result in the browser · esc cancels")
	section("visual", "Visual mode (v / V)",
		"Select text in the selected result's details, then y to copy it (Y copies whole lines)",
		"h j k l · w b e · 0 ^ $ · gg G · ctrl+d ctrl+u · f F t T {char}, ; and , repeat",
		"counts (3w) · o swaps ends · v/V switch character/line selection · esc cancels")
	section("panes", "Panes (ctrl+w …)",
		"s split · v vsplit · n new empty pane · h/j/k/l or arrows focus · w/W next/previous",
		"q or c close · o only · T move to a new tab · + - resize height · < > width",
		"_ | maximise · = equal sizes · q closes the pane when several are open")
	section("tabs", "Tabs",
		"gt / gT next / previous tab · Ngt tab N · :tabnew · :tabclose · click a tab to switch")
	section("commands", "Commands (:)",
		m.st.muted.Render("tab completes · ↑/↓ history · abbreviations like :sp :vs :tabn work · :N jumps to result N"),
		m.st.muted.Render("modifiers: :vertical / :aboveleft / :belowright / :tab {cmd}, e.g. :vert new rust"))
	for _, c := range commands {
		name := ":" + c.name
		if c.usage != "" {
			name += " " + c.usage
		}
		k := m.st.key.Render(name)
		pad := strings.Repeat(" ", max(28-lipgloss.Width(k), 1))
		desc := c.desc
		if len(c.aliases) > 0 {
			desc += m.st.muted.Render(" (" + strings.Join(c.aliases, ", ") + ")")
		}
		b = append(b, ansi.Truncate(k+pad+desc, w, "…"))
	}
	section("mouse", "Mouse",
		"wheel scrolls · click selects · click again for details · click a pane to focus it · click the query to edit")
	section("bangs", "Bangs")
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
	return m.st.badge.Render(label) + " " + m.st.muted.Render(m.st.sep)
}

func (m *Model) viewHeader(l layoutInfo) string {
	badge := m.badgeText() + " "
	line := badge + m.inputView(m.width-lipgloss.Width(badge)-1)
	line = ansi.Truncate(line, m.width, "")
	if l.inputH > 1 {
		rule := m.st.border
		if m.focused && m.multi {
			rule = m.st.bar // highlight the focused pane
		}
		return line + "\n" + rule.Render(strings.Repeat("─", max(m.width, 1)))
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
		var lines []string
		if m.showSummary && !m.summaryEmpty() {
			lines = m.summaryLines(m.width)
		}
		peek := m.compactPeek()
		bodyH := l.sumH
		if len(peek) > 0 {
			bodyH--
		}
		if len(lines) > bodyH {
			lines = lines[:bodyH]
		}
		if len(lines) < bodyH {
			lines = append(lines, make([]string, bodyH-len(lines))...)
		}
		if len(peek) > 0 {
			copy(lines[len(lines)-len(peek):], peek)
			lines = append(lines, m.st.border.Render(strings.Repeat("─", max(m.width, 1))))
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	if l.listH > 0 {
		if l.showDetailFull {
			parts = append(parts, m.viewDetail(l, m.width))
		} else {
			parts = append(parts, m.viewList(l))
		}
	}
	if l.errH > 0 {
		parts = append(parts, m.st.err.Render(ansi.Truncate(" ✗ "+oneLine(safe.Line(m.err.Error())), m.width, "…")))
	}
	if l.statusH > 0 {
		parts = append(parts, m.viewStatus())
	}
	return strings.Join(parts, "\n")
}

// viewStatus is the pane's own status line (like a vim window's): engine,
// query, position and activity. The focused pane's is drawn in the accent
// colour with a marker when several panes are open; others are muted.
func (m *Model) viewStatus() string {
	parts := []string{m.app.Reg.Display(m.currentEngine())}
	if k := m.plan.Query.Kind; k != "" && k != engine.KindWeb {
		parts[0] += "·" + string(k)
	}
	if q := strings.TrimSpace(m.plan.Query.Text); q != "" {
		parts = append(parts, q)
	}
	if len(m.results) > 0 {
		pos := fmt.Sprintf("%d/%d", m.sel+1, len(m.results))
		if m.plan.Query.Page > 1 {
			pos += fmt.Sprintf(" p%d", m.plan.Query.Page)
		}
		parts = append(parts, pos)
	}
	switch {
	case m.loading:
		parts = append(parts, m.spinner.View()+" searching")
	case m.busy != "":
		parts = append(parts, m.spinner.View()+" "+m.busy)
	}
	marker, style := " ", m.st.statusBar
	switch {
	case m.focused && m.multi:
		marker, style = m.st.pointer, m.st.accent
	case !m.focused:
		style = m.st.muted
	}
	line := ansi.Truncate(marker+" "+strings.Join(parts, " · "), max(m.width, 1), "…")
	return style.Render(line + strings.Repeat(" ", max(m.width-lipgloss.Width(line), 0)))
}

// modeBadge names the focused pane's mode for the bottom line, like vim's
// "-- INSERT --".
func (m *Model) modeBadge() string {
	switch {
	case m.mode == modeVisual && m.vis.line:
		return m.st.modeDet.Render("V-LINE")
	case m.mode == modeVisual:
		return m.st.modeDet.Render("VISUAL")
	case m.hint.active && m.hint.open:
		return m.st.modeDet.Render("OPEN")
	case m.hint.active:
		return m.st.modeDet.Render("HINT")
	}
	switch m.mode {
	case modeInsert:
		return m.st.modeIns.Render("INSERT")
	case modeDetail:
		return m.st.modeDet.Render("VIEW")
	case modeHelp:
		return m.st.modeDet.Render("HELP")
	}
	return m.st.modeNorm.Render("NORMAL")
}

// showcmd is the count and keys typed so far, like vim's 'showcmd'.
func (m *Model) showcmd() string {
	s := ""
	if m.count > 0 {
		s = fmt.Sprint(m.count)
	}
	s += strings.Join(m.pending, "")
	switch {
	case m.mode == modeVisual:
		s = fmt.Sprintf("%d:%d", m.vis.cur.row+1, m.vis.cur.col+1)
		if m.vis.line {
			a, b := m.vis.bounds()
			s += fmt.Sprintf(" (%d lines)", b.row-a.row+1)
		}
		s += " " + m.vis.pending
	case m.hint.active:
		s = m.hint.typed
	case m.windowPending:
		s = "^W"
	}
	return strings.TrimSpace(s)
}

// bottomLine is the line at the very bottom of the application: the mode
// at the left (like vim), pending keys, messages, and key hints at the
// right. The root renders it once for the focused pane.
func (m *Model) bottomLine(width int) string {
	left := " " + m.modeBadge()
	if sc := m.showcmd(); sc != "" {
		left += " " + m.st.statusBar.Render(sc)
	}
	left += " "
	avail := width - lipgloss.Width(left)
	var right string
	if m.flash != "" {
		right = m.st.statusBar.Render(ansi.Truncate(safe.Line(m.flash)+" ", max(avail, 0), "…"))
	} else {
		right = m.renderHints(m.statusHints(), avail)
	}
	fill := max(width-lipgloss.Width(left)-lipgloss.Width(right), 0)
	return ansi.Truncate(left+strings.Repeat(" ", fill)+right, width, "")
}

func (m *Model) statusHints() [][2]string {
	if m.windowPending {
		return [][2]string{{"s", "split"}, {"v", "vsplit"}, {"hjkl", "focus"}, {"w", "next"}, {"q", "close"}, {"o", "only"}, {"esc", "cancel"}}
	}
	if m.picker.open {
		return [][2]string{{"↑↓/jk/tab", "move"}, {"enter", "select"}, {"esc", "close"}, {"letter", "jump"}}
	}
	if m.hint.active {
		return [][2]string{{"label", "jump"}, {"backspace", "undo"}, {"esc", "cancel"}}
	}
	if m.mode == modeVisual {
		return [][2]string{{"y", "copy"}, {"hjkl/w/b/e", "move"}, {"f/t", "find"}, {"o", "other end"}, {"v/V", "char/line"}, {"esc", "cancel"}}
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
		return append([][2]string{{m.keys.first(ActBack), "back"}}, m.keys.hints(ActOpen, ActEdit, ActYankURL, ActYankSnippet, ActHelp)...)
	case modeHelp:
		return [][2]string{{m.keys.first(ActBack), "close"}, {"j/k", "scroll"}}
	}
	hs := m.keys.hints(ActSearch, ActDetails, ActOpen, ActYankURL, ActYankSnippet, ActEdit)
	if k := m.keys.first(ActWindow); k != "" {
		label := "split"
		if m.multi {
			label = "panes"
		}
		hs = append(hs, [2]string{k, label})
	}
	return append(hs, m.keys.hints(ActNextPage, ActPrevPage, ActNextEngine, ActCycleType, ActHelp, ActQuit)...)
}

func (m *Model) renderHints(hs [][2]string, avail int) string {
	bar := m.st.statusBar
	var out string
	w := 0
	for _, h := range hs {
		seg := bar.Render(" ") + m.st.key.Render(h[0])
		if h[1] != "" {
			seg += m.st.muted.Render(" " + h[1])
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

// inputView renders the search bar. The bubbles cursor uses reverse video
// (a background fill), so the cursor is drawn as an underlined accent
// character instead, keeping the terminal background untouched.
func (m *Model) inputView(width int) string {
	width = max(width, 1)
	val := []rune(m.input.Value())
	focused := m.input.Focused() && m.focused
	cursor := m.st.accent.Underline(true)
	if len(val) == 0 {
		ph := []rune(m.input.Placeholder)
		if len(ph) > width {
			ph = append(ph[:max(width-1, 0)], '…')
		}
		if !focused || len(ph) == 0 {
			return m.st.muted.Render(string(ph))
		}
		return cursor.Render(string(ph[:1])) + m.st.muted.Render(string(ph[1:]))
	}
	pos := min(m.input.Position(), len(val))
	start := 0
	if pos >= width {
		start = pos - width + 1
	}
	end := min(start+width, len(val))
	text := m.st.snippet
	if !focused {
		return text.Render(string(val[start:end]))
	}
	before := string(val[start:pos])
	at, after := " ", ""
	if pos < len(val) {
		at = string(val[pos])
		after = string(val[pos+1 : end])
	}
	return text.Render(before) + cursor.Render(at) + text.Render(after)
}
