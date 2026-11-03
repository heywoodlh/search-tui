package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/heywoodlh/tui-search/internal/app"
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
}

// NewRoot creates the root with a single pane.
func NewRoot(a *app.App, query string, opts app.PlanOptions) *Root {
	p := New(a, query, opts)
	r := &Root{app: a, tree: &node{pane: p}, focus: p, nextID: 1, rects: map[*Model]rect{}, width: 80, height: 24, initOpts: opts}
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

func (r *Root) find(pid int) *Model {
	for _, p := range r.panes() {
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
	r.rects = map[*Model]rect{}
	r.place(r.tree, rect{0, 0, r.width, r.height})
	multi := len(r.rects) > 1
	for p, rc := range r.rects {
		p.focused = p == r.focus
		p.multi = multi
		p.windowPending = r.pending && p == r.focus
		p.Update(tea.WindowSizeMsg{Width: rc.w, Height: rc.h})
	}
}

func (r *Root) place(n *node, rc rect) {
	if n.pane != nil {
		r.rects[n.pane] = rc
		return
	}
	k := len(n.children)
	if n.dir == splitSideBySide {
		avail := rc.w - (k - 1) // one column separators
		x := rc.x
		for i, c := range n.children {
			w := avail / k
			if i < avail%k {
				w++
			}
			r.place(c, rect{x, rc.y, w, rc.h})
			x += w + 1
		}
		return
	}
	y := rc.y
	for i, c := range n.children {
		h := rc.h / k
		if i < rc.h%k {
			h++
		}
		r.place(c, rect{rc.x, y, rc.w, h})
		y += h
	}
}

// split opens a new pane next to the focused one. The new pane starts as a
// copy of the focused pane's search so you can compare engines side by side.
func (r *Root) split(dir splitDir) tea.Cmd {
	cur := r.rects[r.focus]
	if dir == splitSideBySide && (cur.w-1)/2 < minPaneWidth || dir == splitStacked && cur.h/2 < minPaneHeight {
		return r.focus.setFlash("not enough room to split here")
	}
	p := r.clone(r.focus)
	n := r.leaf(r.focus)
	newLeaf := &node{pane: p}
	if n.parent != nil && n.parent.dir == dir {
		par := n.parent
		idx := indexOf(par.children, n)
		par.children = append(par.children[:idx+1], append([]*node{newLeaf}, par.children[idx+1:]...)...)
		newLeaf.parent = par
	} else {
		// Turn the leaf into a split holding the old pane and the new one.
		old := &node{pane: n.pane, parent: n}
		n.pane, n.dir = nil, dir
		n.children = []*node{old, newLeaf}
		newLeaf.parent = n
	}
	r.focus = p
	r.layout()
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
	p.summary = src.summary
	p.extSummary = src.extSummary
	p.showSummary = src.showSummary
	p.history = append([]string{}, src.history...)
	p.histIdx = len(p.history)
	for k, v := range src.pages {
		p.pages[k] = v
	}
	if len(p.results) > 0 || p.plan.Engine != nil {
		p.mode = modeNormal
		p.input.Blur()
	}
	return p
}

// close removes a pane; closing the last one quits.
func (r *Root) close(p *Model) tea.Cmd {
	all := r.panes()
	if len(all) == 1 {
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
		r.layout() // splits are always equal-sized
	}
	return nil
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
		for p, rc := range r.rects {
			if rc.contains(msg.X, msg.Y) {
				if p != r.focus && msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
					r.focus = p
					r.layout()
				}
				local := msg
				local.X, local.Y = msg.X-rc.x, msg.Y-rc.y
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
	}

	// Spinner ticks, cursor blinks and the like carry their own ids; every
	// pane ignores the ones that are not its own.
	var cmds []tea.Cmd
	for _, p := range r.panes() {
		_, cmd := p.Update(msg)
		cmds = append(cmds, cmd)
	}
	return r, tea.Batch(cmds...)
}

func (r *Root) handleKey(msg tea.KeyMsg) tea.Cmd {
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
	if p.mode != modeInsert && !p.picker.open {
		k := p.keys
		switch {
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
	return cmd
}

func (r *Root) View() string {
	if r.width <= 0 || r.height <= 0 {
		return ""
	}
	return r.render(r.tree)
}

func (r *Root) render(n *node) string {
	if n.pane != nil {
		rc := r.rects[n.pane]
		return lipgloss.NewStyle().Width(rc.w).MaxWidth(rc.w).Height(rc.h).MaxHeight(rc.h).Render(n.pane.View())
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
