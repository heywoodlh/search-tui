package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/heywoodlh/search-tui/internal/app"
)

// Vim-style split panes. Each pane is an independent Model (search
// session). The root arranges them in a tree of splits: ctrl+w s stacks
// panes ("split"), ctrl+w v places them side by side ("vsplit"), and
// splitting in the same direction as the parent adds an equal-sized sibling.

type splitDir int

const (
	splitStacked    splitDir = iota // :split, panes above each other
	splitSideBySide                 // :vsplit, panes next to each other
)

type node struct {
	parent   *node
	dir      splitDir
	children []*node
	pane     *Model // leaf only
	// size is a fixed size along the parent's split axis set by :resize or
	// ctrl+w +/-/</>; 0 shares the space equally.
	size int
}

// tab is one tab page: its own split tree and focused pane.
type tab struct {
	tree  *node
	focus *Model
}

type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

const (
	minPaneWidth  = 24
	minPaneHeight = 6
)

// Root is the Bubble Tea model hosting one or more panes.
type Root struct {
	app      *app.App
	tree     *node
	focus    *Model
	nextID   int
	rects    map[*Model]rect
	width    int
	height   int
	pending  bool // ctrl+w prefix pressed
	initOpts app.PlanOptions

	// tabs holds every tab page; tree/focus mirror tabs[cur].
	tabs []*tab
	cur  int
	cmd  cmdline
	// login holds the :login session being collected, if any.
	login *loginSession
	// pendingCmd carries the tea.Cmd produced while executing a command.
	pendingCmd tea.Cmd
}

// NewRoot creates the root with a single pane.
func NewRoot(a *app.App, query string, opts app.PlanOptions) *Root {
	p := New(a, query, opts)
	r := &Root{app: a, tree: &node{pane: p}, focus: p, nextID: 1, rects: map[*Model]rect{}, width: 80, height: 24, initOpts: opts}
	r.tabs = []*tab{{tree: r.tree, focus: r.focus}}
	r.cmd = newCmdline()
	r.layout()
	return r
}

// panes lists panes in reading order (left-to-right, top-to-bottom).
func (r *Root) panes() []*Model {
	var out []*Model
	var walk func(n *node)
	walk = func(n *node) {
		if n.pane != nil {
			out = append(out, n.pane)
			return
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(r.tree)
	return out
}

// allPanes lists the panes of every tab.
func (r *Root) allPanes() []*Model {
	r.save()
	cur, tree := r.cur, r.tree
	var out []*Model
	for _, t := range r.tabs {
		r.tree = t.tree
		out = append(out, r.panes()...)
	}
	r.cur, r.tree = cur, tree
	return out
}

func (r *Root) save() { r.tabs[r.cur].tree, r.tabs[r.cur].focus = r.tree, r.focus }

func (r *Root) find(pid int) *Model {
	for _, p := range r.allPanes() {
		if p.pid == pid {
			return p
		}
	}
	return nil
}

func (r *Root) leaf(p *Model) *node {
	var found *node
	var walk func(n *node)
	walk = func(n *node) {
		if n.pane == p {
			found = n
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(r.tree)
	return found
}

// layout assigns a rectangle to every pane and resizes them.
func (r *Root) layout() {
	r.save()
	r.rects = map[*Model]rect{}
	top := 0
	if len(r.tabs) > 1 {
		top = 1 // tab bar
	}
	bottom := 0
	if *r.app.Cfg.UI.StatusBar || r.cmd.active {
		bottom = 1 // the app-wide mode / command line
	}
	r.place(r.tree, rect{0, top, r.width, max(r.height-top-bottom, 1)})
	multi := len(r.rects) > 1
	for p, rc := range r.rects {
		p.focused = p == r.focus
		p.multi = multi
		p.windowPending = r.pending && p == r.focus
		w, h := rc.w, rc.h
		if r.framed(rc) {
			w, h = w-2, h-2
		}
		p.Update(tea.WindowSizeMsg{Width: w, Height: h})
	}
}

func (r *Root) framed(rc rect) bool {
	return len(r.rects) > 1 && rc.w >= 4 && rc.h >= 4
}

func (r *Root) place(n *node, rc rect) {
	if n.pane != nil {
		r.rects[n.pane] = rc
		return
	}
	k := len(n.children)
	side := n.dir == splitSideBySide
	avail, minSize := rc.h, minPaneHeight
	if side {
		avail, minSize = rc.w-(k-1), minPaneWidth // one-column separators
	}
	sizes := shareSpace(n.children, avail, minSize)
	x, y := rc.x, rc.y
	for i, c := range n.children {
		if side {
			r.place(c, rect{x, rc.y, sizes[i], rc.h})
			x += sizes[i] + 1
		} else {
			r.place(c, rect{rc.x, y, rc.w, sizes[i]})
			y += sizes[i]
		}
	}
}

// shareSpace divides avail cells between children: fixed sizes (from
// :resize) are honoured as far as the others' minimum allows, the rest is
// shared equally.
func shareSpace(children []*node, avail, minSize int) []int {
	k := len(children)
	sizes := make([]int, k)
	flex := 0
	fixedTotal := 0
	for i, c := range children {
		if c.size > 0 {
			sizes[i] = c.size
			fixedTotal += c.size
		} else {
			flex++
		}
	}
	reserve := flex * minSize
	if flex == 0 {
		reserve = 0
	}
	// Shrink fixed sizes proportionally if they don't fit.
	if limit := max(avail-reserve, 0); fixedTotal > limit && fixedTotal > 0 {
		for i, c := range children {
			if c.size > 0 {
				sizes[i] = max(c.size*limit/fixedTotal, 1)
			}
		}
	}
	used := 0
	for _, sz := range sizes {
		used += sz
	}
	rest := max(avail-used, 0)
	if flex == 0 {
		sizes[k-1] += rest // the last pane absorbs any remainder
		return sizes
	}
	j := 0
	for i, c := range children {
		if c.size == 0 {
			sizes[i] = rest / flex
			if j < rest%flex {
				sizes[i]++
			}
			j++
		}
	}
	return sizes
}

// split opens a new pane next to the focused one (below/right, or
// above/left with before). The new pane copies the focused pane's search so
// you can compare engines side by side; empty starts on the start page, and
// query runs a search in the new pane.
func (r *Root) split(dir splitDir) tea.Cmd { return r.splitWith(dir, false, false, "") }

func (r *Root) splitWith(dir splitDir, before, empty bool, query string) tea.Cmd {
	cur := r.rects[r.focus]
	if dir == splitSideBySide && (cur.w-1)/2 < minPaneWidth || dir == splitStacked && cur.h/2 < minPaneHeight {
		return r.focus.setFlash("not enough room to split here")
	}
	p := r.newPane(r.focus, empty)
	n := r.leaf(r.focus)
	newLeaf := &node{pane: p}
	if n.parent != nil && n.parent.dir == dir {
		par := n.parent
		idx := indexOf(par.children, n)
		if !before {
			idx++
		}
		par.children = append(par.children[:idx], append([]*node{newLeaf}, par.children[idx:]...)...)
		newLeaf.parent = par
	} else {
		// Turn the leaf into a split holding the old pane and the new one.
		old := &node{pane: n.pane, parent: n}
		n.pane, n.dir = nil, dir
		n.children = []*node{old, newLeaf}
		if before {
			n.children = []*node{newLeaf, old}
		}
		newLeaf.parent = n
	}
	r.focus = p
	r.layout()
	return r.startPane(p, query)
}

// newPane clones src, or makes an empty pane on the start page.
func (r *Root) newPane(src *Model, empty bool) *Model {
	if !empty {
		return r.clone(src)
	}
	p := New(r.app, "", r.initOpts)
	p.pid = r.nextID
	r.nextID++
	p.flash = ""
	// New panes and tabs open in normal mode; press / or i to type.
	p.mode = modeNormal
	p.input.Blur()
	return p
}

// startPane runs query in a new pane, or initialises it.
func (r *Root) startPane(p *Model, query string) tea.Cmd {
	if strings.TrimSpace(query) != "" {
		p.input.SetValue(query)
		return p.submit()
	}
	if strings.TrimSpace(p.input.Value()) == "" {
		return p.Init()
	}
	return nil
}

func indexOf(ns []*node, n *node) int {
	for i, c := range ns {
		if c == n {
			return i
		}
	}
	return -1
}

// clone creates a pane showing the same search as src.
func (r *Root) clone(src *Model) *Model {
	p := New(r.app, "", src.opts)
	p.pid = r.nextID
	r.nextID++
	p.flash = ""
	p.initOpts = src.initOpts
	p.input.SetValue(src.input.Value())
	p.plan = src.plan
	p.results = src.results
	p.sel = src.sel
	p.history = append([]string{}, src.history...)
	p.histIdx = len(p.history)
	p.nav = append([]navEntry{}, src.nav...)
	p.navIdx = src.navIdx
	for k, v := range src.pages {
		p.pages[k] = v
	}
	p.mode = modeNormal
	p.input.Blur()
	return p
}

// close removes a pane. Closing the last pane of a tab closes the tab;
// closing the last pane of the last tab quits.
func (r *Root) close(p *Model) tea.Cmd {
	all := r.panes()
	if len(all) == 1 {
		if len(r.tabs) > 1 {
			return r.closeTab(r.cur)
		}
		return tea.Quit
	}
	if p.cancel != nil {
		p.cancel()
	}
	n := r.leaf(p)
	par := n.parent
	idx := indexOf(par.children, n)
	par.children = append(par.children[:idx], par.children[idx+1:]...)
	if len(par.children) == 1 {
		// Collapse a split with a single child into that child.
		only := par.children[0]
		par.pane, par.dir, par.children = only.pane, only.dir, only.children
		for _, c := range par.children {
			c.parent = par
		}
	}
	if r.focus == p {
		// Focus the neighbour that took its place (same index, or previous).
		sib := par
		for sib.pane == nil {
			i := min(idx, len(sib.children)-1)
			sib = sib.children[max(i, 0)]
		}
		r.focus = sib.pane
	}
	r.layout()
	return nil
}

func (r *Root) only() {
	for _, p := range r.panes() {
		if p != r.focus && p.cancel != nil {
			p.cancel()
		}
	}
	r.tree = &node{pane: r.focus}
	r.layout()
}

// ---------------------------------------------------------------- tabs

func (r *Root) switchTab(i int) {
	if i < 0 || i >= len(r.tabs) {
		return
	}
	r.save()
	r.cur = i
	r.tree, r.focus = r.tabs[i].tree, r.tabs[i].focus
	r.layout()
}

// newTab opens a tab after the current one with pane p.
func (r *Root) newTab(p *Model, query string) tea.Cmd {
	r.save()
	t := &tab{tree: &node{pane: p}, focus: p}
	r.tabs = append(r.tabs[:r.cur+1], append([]*tab{t}, r.tabs[r.cur+1:]...)...)
	r.cur++
	r.tree, r.focus = t.tree, t.focus
	r.layout()
	return r.startPane(p, query)
}

func (r *Root) closeTab(i int) tea.Cmd {
	if len(r.tabs) == 1 {
		return tea.Quit
	}
	r.save()
	cur := r.tree
	r.tree = r.tabs[i].tree
	for _, p := range r.panes() {
		if p.cancel != nil {
			p.cancel()
		}
	}
	r.tree = cur
	r.tabs = append(r.tabs[:i], r.tabs[i+1:]...)
	r.cur = min(r.cur, len(r.tabs)-1)
	if i < r.cur || i == r.cur {
		r.cur = max(min(i, len(r.tabs)-1), 0)
	}
	r.tree, r.focus = r.tabs[r.cur].tree, r.tabs[r.cur].focus
	r.layout()
	return nil
}

func (r *Root) tabOnly() {
	r.save()
	keep := r.tabs[r.cur]
	for i := range r.tabs {
		if r.tabs[i] != keep {
			r.tree = r.tabs[i].tree
			for _, p := range r.panes() {
				if p.cancel != nil {
					p.cancel()
				}
			}
		}
	}
	r.tabs, r.cur = []*tab{keep}, 0
	r.tree, r.focus = keep.tree, keep.focus
	r.layout()
}

// moveTab moves the current tab to index i (clamped).
func (r *Root) moveTab(i int) {
	r.save()
	t := r.tabs[r.cur]
	rest := make([]*tab, 0, len(r.tabs))
	rest = append(rest, r.tabs[:r.cur]...)
	rest = append(rest, r.tabs[r.cur+1:]...)
	i = min(max(i, 0), len(rest))
	moved := make([]*tab, 0, len(r.tabs))
	moved = append(moved, rest[:i]...)
	moved = append(moved, t)
	moved = append(moved, rest[i:]...)
	r.tabs = moved
	r.cur = i
	r.layout()
}

// tabBar renders the tab line and the x-range of each tab (for clicks).
func (r *Root) tabBar() (string, [][2]int) {
	st := r.focus.st
	var b strings.Builder
	var spans [][2]int
	x := 0
	for i, t := range r.tabs {
		f := t.focus
		label := r.app.Reg.Display(f.currentEngine())
		if q := strings.TrimSpace(f.plan.Query.Text); q != "" {
			label += ": " + q
		}
		label = fmt.Sprintf(" %d %s ", i+1, truncate(label, 24))
		w := lipgloss.Width(label)
		if x+w > r.width {
			break
		}
		if i == r.cur {
			b.WriteString(st.accent.Underline(true).Render(label))
		} else {
			b.WriteString(st.muted.Render(label))
		}
		spans = append(spans, [2]int{x, x + w})
		x += w
	}
	return b.String(), spans
}

// ---------------------------------------------------------------- resize

// resize changes the focused pane's height (or width when vertical) to n,
// or by n when relative. The size belongs to the nearest enclosing split
// in that direction.
func (r *Root) resize(n int, relative, vertical bool) {
	dir := splitStacked
	if vertical {
		dir = splitSideBySide
	}
	c := r.leaf(r.focus)
	for c.parent != nil && c.parent.dir != dir {
		c = c.parent
	}
	if c.parent == nil {
		return // no split in that direction
	}
	cur := r.subtreeSize(c, vertical)
	if relative {
		n += cur
	}
	c.size = max(n, 1)
	r.layout()
}

func (r *Root) subtreeSize(n *node, vertical bool) int {
	for n.pane == nil {
		n = n.children[0]
	}
	if vertical {
		return r.rects[n.pane].w
	}
	return r.rects[n.pane].h
}

// equalize resets every split to equal sizes (ctrl+w =).
func (r *Root) equalize() {
	var walk func(n *node)
	walk = func(n *node) {
		n.size = 0
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(r.tree)
	r.layout()
}

func (r *Root) cycle(d int) {
	all := r.panes()
	for i, p := range all {
		if p == r.focus {
			r.focus = all[(i+d+len(all))%len(all)]
			break
		}
	}
	r.layout()
}

// move focuses the nearest pane in a direction (h, j, k, l).
func (r *Root) move(dir string) {
	cur := r.rects[r.focus]
	var best *Model
	bestDist, bestOverlap := 1<<30, -1
	for p, rc := range r.rects {
		if p == r.focus {
			continue
		}
		var dist, overlap int
		switch dir {
		case "h":
			dist, overlap = cur.x-(rc.x+rc.w), spanOverlap(cur.y, cur.h, rc.y, rc.h)
		case "l":
			dist, overlap = rc.x-(cur.x+cur.w), spanOverlap(cur.y, cur.h, rc.y, rc.h)
		case "k":
			dist, overlap = cur.y-(rc.y+rc.h), spanOverlap(cur.x, cur.w, rc.x, rc.w)
		case "j":
			dist, overlap = rc.y-(cur.y+cur.h), spanOverlap(cur.x, cur.w, rc.x, rc.w)
		}
		if dist < 0 || overlap <= 0 {
			continue
		}
		if dist < bestDist || dist == bestDist && overlap > bestOverlap {
			best, bestDist, bestOverlap = p, dist, overlap
		}
	}
	if best != nil {
		r.focus = best
		r.layout()
	}
}

func spanOverlap(a, al, b, bl int) int { return min(a+al, b+bl) - max(a, b) }

// windowCommand handles the key after the ctrl+w prefix.
func (r *Root) windowCommand(k string) tea.Cmd {
	switch k {
	case "s", "S", "ctrl+s":
		return r.split(splitStacked)
	case "v", "ctrl+v":
		return r.split(splitSideBySide)
	case "w", "ctrl+w", "tab":
		r.cycle(1)
	case "W", "p", "shift+tab":
		r.cycle(-1)
	case "h", "left", "ctrl+h":
		r.move("h")
	case "j", "down", "ctrl+j":
		r.move("j")
	case "k", "up", "ctrl+k":
		r.move("k")
	case "l", "right", "ctrl+l":
		r.move("l")
	case "q", "ctrl+q":
		return r.close(r.focus)
	case "c":
		if len(r.panes()) == 1 {
			return r.focus.setFlash("can't close the last pane (use q to quit)")
		}
		return r.close(r.focus)
	case "o", "ctrl+o":
		r.only()
	case "=":
		r.equalize()
	case "+":
		r.resize(1, true, false)
	case "-":
		r.resize(-1, true, false)
	case ">":
		r.resize(2, true, true)
	case "<":
		r.resize(-2, true, true)
	case "_":
		r.resize(r.height, false, false)
	case "|":
		r.resize(r.width, false, true)
	case "n", "ctrl+n":
		return r.splitWith(splitStacked, false, true, "")
	case "T":
		// Move the pane to a new tab.
		if len(r.panes()) > 1 {
			p := r.focus
			r.detach(p)
			return r.newTab(p, "")
		}
	}
	return nil
}

// detach removes p from the current tab's tree without closing it.
func (r *Root) detach(p *Model) {
	n := r.leaf(p)
	par := n.parent
	idx := indexOf(par.children, n)
	par.children = append(par.children[:idx], par.children[idx+1:]...)
	if len(par.children) == 1 {
		only := par.children[0]
		par.pane, par.dir, par.children = only.pane, only.dir, only.children
		for _, c := range par.children {
			c.parent = par
		}
	}
	sib := par
	for sib.pane == nil {
		sib = sib.children[min(idx, len(sib.children)-1)]
	}
	r.focus = sib.pane
}

// ---------------------------------------------------------------- tea.Model

func (r *Root) Init() tea.Cmd { return r.focus.Init() }

func (r *Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		r.width, r.height = msg.Width, msg.Height
		r.layout()
		return r, nil

	case tea.KeyMsg:
		return r, r.handleKey(msg)

	case tea.MouseMsg:
		if len(r.tabs) > 1 && msg.Y == 0 {
			if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
				_, spans := r.tabBar()
				for i, sp := range spans {
					if msg.X >= sp[0] && msg.X < sp[1] {
						r.switchTab(i)
					}
				}
			}
			return r, nil
		}
		for p, rc := range r.rects {
			if rc.contains(msg.X, msg.Y) {
				if p != r.focus && msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
					r.focus = p
					r.layout()
				}
				local := msg
				local.X, local.Y = msg.X-rc.x, msg.Y-rc.y
				if r.framed(rc) {
					local.X--
					local.Y--
					if local.X < 0 || local.Y < 0 || local.X >= p.width || local.Y >= p.height {
						return r, nil // border click only changes focus
					}
				}
				return r, r.forward(p, local)
			}
		}
		return r, nil

	case paneTargeted:
		if p := r.find(msg.paneID()); p != nil {
			return r, r.forward(p, msg)
		}
		if e, ok := msg.(editorDoneMsg); ok && e.cleanup != nil {
			e.cleanup() // the pane was closed while the editor was open
		}
		return r, nil

	case loginDeviceMsg:
		return r, r.showDevicePrompt(msg)

	case loginDoneMsg:
		return r, r.loginDone(msg)
	}

	// Spinner ticks, cursor blinks and the like carry their own ids; every
	// pane ignores the ones that are not its own.
	var cmds []tea.Cmd
	for _, p := range r.allPanes() {
		_, cmd := p.Update(msg)
		cmds = append(cmds, cmd)
	}
	return r, tea.Batch(cmds...)
}

func (r *Root) handleKey(msg tea.KeyMsg) tea.Cmd {
	if r.cmd.active {
		return r.cmdKey(msg)
	}
	// Characters that arrive in one burst (fast typing, tmux send-keys, a
	// slow link) can come as a single multi-rune key message. Outside text
	// input, vim keys must be handled one at a time ("gt", "ew", "5j").
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste && r.focus.mode != modeInsert {
		var cmds []tea.Cmd
		for _, c := range msg.Runes {
			cmds = append(cmds, r.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{c}, Alt: msg.Alt}))
		}
		return tea.Batch(cmds...)
	}
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	p := r.focus
	if r.pending {
		r.pending = false
		r.layout()
		if msg.String() == "esc" {
			return nil
		}
		return r.windowCommand(msg.String())
	}
	// Pane commands are available outside the search bar (where ctrl+w
	// deletes a word, as in vim's insert mode) and outside the picker.
	if p.mode != modeInsert && p.mode != modeVisual && !p.picker.open && !p.hint.active && len(p.pending) == 0 {
		k := p.keys
		switch {
		case k.is(msg, ActCommand):
			p.count = 0
			r.openCmd("")
			return nil
		case k.is(msg, ActWindow):
			r.pending = true
			r.layout()
			return nil
		case k.is(msg, ActSplitHoriz):
			return r.split(splitStacked)
		case k.is(msg, ActSplitVert):
			return r.split(splitSideBySide)
		case k.is(msg, ActFocusNext):
			r.cycle(1)
			return nil
		case k.is(msg, ActClosePane):
			return r.close(p)
		}
	}
	return r.forward(p, msg)
}

// forward delivers msg to a pane, turning its quit request into closing
// the pane when others remain (vim's :q).
func (r *Root) forward(p *Model, msg tea.Msg) tea.Cmd {
	_, cmd := p.Update(msg)
	if p.quitRequested {
		p.quitRequested = false
		return r.close(p)
	}
	if c := p.rootCmd; c != "" {
		p.rootCmd = ""
		return tea.Batch(cmd, r.execute(c))
	}
	return cmd
}

func (r *Root) View() string {
	if r.width <= 0 || r.height <= 0 {
		return ""
	}
	if r.height == 1 {
		if r.cmd.active {
			return r.cmdView()
		}
		if *r.app.Cfg.UI.StatusBar {
			return r.focus.bottomLine(r.width)
		}
		return ansi.Truncate(strings.Split(r.render(r.tree), "\n")[0], r.width, "")
	}
	out := r.render(r.tree)
	if len(r.tabs) > 1 {
		bar, _ := r.tabBar()
		out = lipgloss.NewStyle().Width(r.width).MaxWidth(r.width).Render(bar) + "\n" + out
	}
	// The bottom row is shared, like vim's: the mode and messages, or the
	// command line while typing one.
	switch {
	case r.cmd.active:
		out += "\n" + r.cmdView()
	case *r.app.Cfg.UI.StatusBar:
		out += "\n" + r.focus.bottomLine(r.width)
	}
	return out
}

func (r *Root) render(n *node) string {
	if n.pane != nil {
		rc := r.rects[n.pane]
		content := lipgloss.NewStyle().Width(n.pane.width).MaxWidth(n.pane.width).Height(n.pane.height).MaxHeight(n.pane.height).Render(n.pane.View())
		if !r.framed(rc) {
			return content
		}
		style := n.pane.st.border
		if n.pane == r.focus {
			style = n.pane.st.bar
		}
		top := style.Render("┌" + strings.Repeat("─", rc.w-2) + "┐")
		bottom := style.Render("└" + strings.Repeat("─", rc.w-2) + "┘")
		lines := []string{top}
		for _, line := range strings.Split(content, "\n") {
			lines = append(lines, style.Render("│")+line+style.Render("│"))
		}
		return strings.Join(append(lines, bottom), "\n")
	}
	parts := make([]string, 0, 2*len(n.children))
	for i, c := range n.children {
		if i > 0 && n.dir == splitSideBySide {
			h := r.nodeHeight(c)
			sep := r.focus.st.border.Render(strings.TrimRight(strings.Repeat("│\n", h), "\n"))
			parts = append(parts, sep)
		}
		parts = append(parts, r.render(c))
	}
	if n.dir == splitSideBySide {
		return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// nodeHeight is the rendered height of a subtree.
func (r *Root) nodeHeight(n *node) int {
	if n.pane != nil {
		return r.rects[n.pane].h
	}
	if n.dir == splitSideBySide {
		return r.nodeHeight(n.children[0])
	}
	h := 0
	for _, c := range n.children {
		h += r.nodeHeight(c)
	}
	return h
}
