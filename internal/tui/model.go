// Package tui implements the interactive terminal interface.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/heywoodlh/search-tui/internal/app"
	"github.com/heywoodlh/search-tui/internal/engine"
	"github.com/heywoodlh/search-tui/internal/safe"
	"github.com/heywoodlh/search-tui/internal/sys"
)

type mode int

const (
	modeInsert mode = iota
	modeNormal
	modeDetail
	modeHelp
	modeVisual
)

// Model is one search pane: an independent search session with its own
// query, engine, results and view. The root model (panes.go) arranges any
// number of them in vim-style splits.
type Model struct {
	pid     int  // pane id, used to route async messages
	focused bool // whether this pane has keyboard focus
	// quitRequested is set when the user asks to quit (q); the root decides
	// whether that closes this pane or the whole program.
	quitRequested bool
	// windowPending shows pane-command hints after the ctrl+w prefix.
	windowPending bool
	// multi is true when more than one pane is open (adds pane hints).
	multi bool
	// rootCmd asks the root to run an ex command (tab switching etc.).
	rootCmd string

	// Vim-style key state: a pending count (5j) and multi-key sequence (gg).
	count   int
	pending []string
	hint    hintState
	vis     visualState
	// helpSections maps help topics to their line in the help view.
	helpSections map[string]int

	app  *app.App
	keys keyMap
	st   styles

	input   textinput.Model
	detail  viewport.Model
	help    viewport.Model
	spinner spinner.Model

	width, height int
	mode, prev    mode

	opts         app.PlanOptions
	initOpts     app.PlanOptions // restored when esc returns to the start page
	picker       picker
	plan         app.Plan
	previousPlan app.Plan // plan for results still on screen while another page loads
	results      []engine.Result
	sel          int
	offset       int

	summary     app.Summary
	showSummary bool
	extSummary  string

	loading     bool
	searchID    int
	cancel      context.CancelFunc
	busy        string // background activity label (fetching page...)
	pendingPage string // URL whose page fetch currently owns the activity indicator

	err      error
	flash    string
	flashID  int
	pages    map[string]app.Page
	pageErrs map[string]error

	history []string
	histIdx int

	// cache of rendered list items keyed by width
	items      [][]string
	itemsWidth int
}

// Run starts the TUI.
func Run(a *app.App, query string, opts app.PlanOptions) error {
	m := NewRoot(a, query, opts)
	popts := []tea.ProgramOption{tea.WithAltScreen()}
	if *a.Cfg.UI.Mouse {
		popts = append(popts, tea.WithMouseCellMotion())
	}
	_, err := tea.NewProgram(m, popts...).Run()
	return err
}

// New creates the model.
func New(a *app.App, query string, opts app.PlanOptions) *Model {
	km, unknown := newKeyMap(a.Cfg.Keys)
	ti := textinput.New()
	ti.Placeholder = "Search… (!bangs: !w !gh !ghc !so · tab: engines)"
	ti.Prompt = ""
	ti.CharLimit = 2048
	ti.SetValue(query)
	ti.Focus()
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	if a.Cfg.LowPower {
		sp.Spinner = spinner.Spinner{Frames: []string{"…"}, FPS: time.Hour}
	}
	m := &Model{
		app:         a,
		keys:        km,
		st:          newStyles(a.Cfg.UI, a.Theme),
		input:       ti,
		spinner:     sp,
		opts:        opts,
		initOpts:    opts,
		showSummary: *a.Cfg.UI.ShowSummary && a.Cfg.SummaryEnabled(),
		pages:       map[string]app.Page{},
		pageErrs:    map[string]error{},
		width:       80,
		height:      24,
		focused:     true,
	}
	m.detail = viewport.New(0, 0)
	m.help = viewport.New(0, 0)
	notes := append([]string{}, a.Warnings...)
	if len(unknown) > 0 {
		notes = append(notes, "unknown [keys] actions: "+strings.Join(unknown, ", "))
	}
	m.flash = strings.Join(notes, "; ")
	return m
}

// ---------------------------------------------------------------- messages

// Every pane-originated message carries the pane id (pid) so the root model
// can route it back to the pane that started the work.

type resultsMsg struct {
	pid     int
	id      int
	plan    app.Plan
	results []engine.Result
	err     error
}

type extSummaryMsg struct {
	pid  int
	id   int
	text string
	err  error
}

type pageMsg struct {
	pid  int
	url  string
	page app.Page
	err  error
	// then is an optional follow-up (e.g. open in editor).
	then string
}

type codeMsg struct {
	pid    int
	result engine.Result
	code   app.Code
	err    error
}

type flashMsg struct{ pid, id int }

type editorDoneMsg struct {
	pid     int
	purpose string // "compose" | "view"
	path    string
	cleanup func()
	err     error
}

type browserDoneMsg struct {
	pid int
	err error
}

// paneID routes messages to panes.
func (m resultsMsg) paneID() int     { return m.pid }
func (m extSummaryMsg) paneID() int  { return m.pid }
func (m pageMsg) paneID() int        { return m.pid }
func (m codeMsg) paneID() int        { return m.pid }
func (m flashMsg) paneID() int       { return m.pid }
func (m editorDoneMsg) paneID() int  { return m.pid }
func (m browserDoneMsg) paneID() int { return m.pid }

type paneTargeted interface{ paneID() int }

// ---------------------------------------------------------------- init

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink}
	if strings.TrimSpace(m.input.Value()) != "" {
		cmds = append(cmds, m.submit())
	}
	if m.flash != "" {
		cmds = append(cmds, m.flashLater())
	}
	return tea.Batch(cmds...)
}

// ---------------------------------------------------------------- helpers

func (m *Model) setFlash(s string) tea.Cmd {
	m.flash = s
	return m.flashLater()
}

func (m *Model) flashLater() tea.Cmd {
	m.flashID++
	id := m.flashID
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return flashMsg{pid: m.pid, id: id} })
}

func (m *Model) current() (engine.Result, bool) {
	if m.sel >= 0 && m.sel < len(m.results) {
		return m.results[m.sel], true
	}
	return engine.Result{}, false
}

func (m *Model) timeout() time.Duration { return m.app.Cfg.TimeoutDuration() + 5*time.Second }

// submit parses the input and starts a search.
func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return nil
	}
	if len(m.history) == 0 || m.history[len(m.history)-1] != text {
		m.history = append(m.history, text)
	}
	m.histIdx = len(m.history)
	m.opts.Page = 1
	return m.search(text)
}

func (m *Model) search(text string) tea.Cmd {
	plan, err := m.app.Plan(text, m.opts)
	if err != nil {
		m.err = err
		return nil
	}
	m.err = nil
	if plan.OpenURL != "" {
		m.mode = modeNormal
		m.input.Blur()
		return m.openURL(plan.OpenURL)
	}
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout())
	m.cancel = cancel
	m.searchID++
	id := m.searchID
	m.loading = true
	m.previousPlan = m.plan
	m.plan = plan
	m.mode = modeNormal
	m.input.Blur()
	a := m.app
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		defer cancel()
		rs, err := a.Search(ctx, &plan)
		return resultsMsg{pid: m.pid, id: id, plan: plan, results: rs, err: err}
	})
}

func (m *Model) rerun() tea.Cmd {
	if strings.TrimSpace(m.plan.Query.Text) == "" && strings.TrimSpace(m.input.Value()) == "" {
		return nil
	}
	text := m.plan.Query.Text
	if text == "" {
		text = m.input.Value()
	}
	return m.search(text)
}

func (m *Model) openURL(u string) tea.Cmd {
	if err := safe.WebURL(u); err != nil {
		m.err = err
		return nil
	}
	args := sys.BrowserCommand(m.app.Cfg.Browser, u)
	if sys.IsTerminalBrowser(args) {
		c := exec.Command(args[0], args[1:]...) //nolint:gosec,noctx // G204: user-configured terminal browser; it owns the TTY until it exits
		return tea.ExecProcess(c, func(err error) tea.Msg { return browserDoneMsg{pid: m.pid, err: err} })
	}
	if err := sys.OpenURL(m.app.Cfg.Browser, u); err != nil {
		m.err = err
		return nil
	}
	return m.setFlash("opened " + truncate(u, 60))
}

func (m *Model) copy(text, what string) tea.Cmd {
	if text == "" {
		return m.setFlash("nothing to copy")
	}
	how, err := sys.Copy(text, m.app.Cfg.Clipboard, m.app.Cfg.ClipboardCommand)
	if err != nil {
		m.err = err
		return nil
	}
	return m.setFlash(fmt.Sprintf("copied %s (%s)", what, how))
}

func (m *Model) fetchPage(u, then string) tea.Cmd {
	if p, ok := m.pages[u]; ok {
		return func() tea.Msg { return pageMsg{pid: m.pid, url: u, page: p, then: then} }
	}
	m.busy = "fetching page…"
	m.pendingPage = u
	a, q, d := m.app, m.plan.Query.Text, m.timeout()
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), d)
		defer cancel()
		p, err := a.FetchPage(ctx, u, q)
		return pageMsg{pid: m.pid, url: u, page: p, err: err, then: then}
	})
}

func (m *Model) editContent(name, content string, line int) tea.Cmd {
	p, cleanup, err := sys.TempFile(name, content)
	if err != nil {
		m.err = err
		return nil
	}
	c := sys.EditorCmd(m.app.Cfg.Editor, p, line)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorDoneMsg{pid: m.pid, purpose: "view", path: p, cleanup: cleanup, err: err}
	})
}

func (m *Model) compose() tea.Cmd {
	p, cleanup, err := sys.TempFile("query.md", sys.ComposeTemplate(m.input.Value()))
	if err != nil {
		m.err = err
		return nil
	}
	c := sys.EditorCmd(m.app.Cfg.Editor, p, 1)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorDoneMsg{pid: m.pid, purpose: "compose", path: p, cleanup: cleanup, err: err}
	})
}

func (m *Model) editCurrent() tea.Cmd {
	r, ok := m.current()
	if !ok {
		return nil
	}
	if r.Code != nil && r.Code.RawURL != "" {
		m.busy = "fetching file…"
		a, d := m.app, m.timeout()
		return tea.Batch(m.spinner.Tick, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), d)
			defer cancel()
			c, err := a.FetchCode(ctx, r)
			return codeMsg{pid: m.pid, result: r, code: c, err: err}
		})
	}
	if r.Code != nil {
		return m.editContent("result.md", app.ResultMarkdown(m.sel+1, r), 0)
	}
	return m.fetchPage(r.URL, "edit")
}

func (m *Model) setResults(rs []engine.Result) {
	m.results = rs
	m.sel, m.offset = 0, 0
	m.items = nil
	m.summary = app.Summary{}
	m.extSummary = ""
	if m.app.Cfg.SummaryEnabled() {
		m.summary = m.app.Summarize(m.plan.Query.Text, rs)
	}
}

func (m *Model) typeCycle() tea.Cmd {
	cur := m.plan.Query.Kind
	if cur == "" {
		cur = engine.KindWeb
	}
	idx := 0
	for i, k := range engine.Kinds {
		if k == cur {
			idx = i
		}
	}
	next := engine.Kinds[(idx+1)%len(engine.Kinds)]
	m.opts.Kind = next
	m.opts.Engine = "" // the default engine for the new type...
	if m.plan.Engine != nil && m.plan.Engine.Supports(next) {
		m.opts.Engine = m.plan.Engine.Name() // ...unless the current one supports it
	}
	m.stripBang()
	if strings.TrimSpace(m.input.Value()) == "" {
		return m.setFlash("type: " + string(next))
	}
	m.opts.Page = 1
	return m.search(m.input.Value())
}

// stripBang removes a !bang from the input so explicit engine choices win.
func (m *Model) stripBang() {
	if m.plan.Bang != "" && m.plan.Query.Text != "" {
		m.input.SetValue(m.plan.Query.Text)
	}
}

// ---------------------------------------------------------------- update

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.items = nil
		m.resize()
		m.ensureVisible()
		return m, nil

	case spinner.TickMsg:
		if !m.loading && m.busy == "" {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case flashMsg:
		if msg.id == m.flashID {
			m.flash = ""
		}
		return m, nil

	case resultsMsg:
		if msg.id != m.searchID {
			return m, nil
		}
		m.loading = false
		if msg.err == nil && msg.plan.Engine != nil && msg.plan.Engine.Name() == "bing" &&
			m.previousPlan.Engine != nil && msg.plan.Query.Page > 1 && msg.plan.Query.Page != m.previousPlan.Query.Page &&
			len(msg.results) > 0 && len(msg.results) == len(m.results) {
			same := true
			for i := range msg.results {
				if msg.results[i].URL != m.results[i].URL {
					same = false
					break
				}
			}
			if same {
				m.plan = m.previousPlan
				m.opts.Page = m.plan.Query.Page
				m.err = fmt.Errorf("bing returned the same results for page %d; pagination may be blocked", msg.plan.Query.Page)
				m.resize()
				return m, nil
			}
		}
		m.plan = msg.plan
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				return m, nil
			}
			m.err = msg.err
			m.setResults(nil)
			return m, nil
		}
		m.err = nil
		m.setResults(msg.results)
		m.refreshDetail()
		var cmds []tea.Cmd
		if msg.plan.Notice != "" {
			cmds = append(cmds, m.setFlash(msg.plan.Notice))
		}
		if msg.plan.Lucky && len(msg.results) > 0 {
			cmds = append(cmds, m.openURL(msg.results[0].URL))
		}
		if m.app.Cfg.Summary.Command != "" && m.showSummary && len(msg.results) > 0 {
			a, q, rs, id := m.app, msg.plan.Query.Text, msg.results, msg.id
			cmds = append(cmds, func() tea.Msg {
				t, err := a.SummarizeExternal(context.Background(), q, rs)
				return extSummaryMsg{pid: m.pid, id: id, text: t, err: err}
			})
		}
		return m, tea.Batch(cmds...)

	case extSummaryMsg:
		if msg.id == m.searchID {
			if msg.err != nil {
				return m, m.setFlash(msg.err.Error())
			}
			m.extSummary = msg.text
		}
		return m, nil

	case pageMsg:
		if msg.url == m.pendingPage {
			m.busy = ""
			m.pendingPage = ""
		}
		if msg.err != nil {
			m.pageErrs[msg.url] = msg.err
			if msg.then == "edit" {
				if r, ok := m.current(); ok {
					return m, tea.Batch(m.setFlash("page fetch failed; showing result"), m.editContent("result.md", app.ResultMarkdown(m.sel+1, r), 0))
				}
			}
			m.refreshDetail()
			return m, m.setFlash("fetch failed: " + msg.err.Error())
		}
		m.pages[msg.url] = msg.page
		delete(m.pageErrs, msg.url)
		m.refreshDetail()
		if msg.then == "edit" {
			return m, m.editContent("page.md", app.PageMarkdown(msg.page), 0)
		}
		return m, nil

	case codeMsg:
		m.busy = ""
		if msg.err != nil {
			return m, tea.Batch(m.setFlash("file fetch failed; showing fragments"), m.editContent("result.md", app.ResultMarkdown(m.sel+1, msg.result), 0))
		}
		return m, m.editContent(msg.code.Name, msg.code.Content, msg.code.Line)

	case editorDoneMsg:
		defer func() {
			if msg.cleanup != nil {
				msg.cleanup()
			}
		}()
		if msg.err != nil {
			m.err = fmt.Errorf("editor: %w", msg.err)
			return m, nil
		}
		if msg.purpose == "compose" {
			b, err := os.ReadFile(msg.path)
			if err != nil {
				m.err = err
				return m, nil
			}
			q := sys.ParseComposed(string(b))
			if q == "" {
				return m, m.setFlash("empty query; cancelled")
			}
			m.input.SetValue(q)
			m.input.CursorEnd()
			return m, m.submit()
		}
		return m, nil

	case browserDoneMsg:
		if msg.err != nil {
			m.err = msg.err
		}
		return m, nil

	case tea.MouseMsg:
		return m, m.handleMouse(msg)

	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}

	if m.mode == modeInsert {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	k := m.keys
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	if m.picker.open {
		return m.pickerKey(msg)
	}
	switch m.mode {
	case modeHelp:
		switch {
		case k.is(msg, ActHelp), k.is(msg, ActBack), k.is(msg, ActQuit):
			m.mode = m.prev
		case k.is(msg, ActDown):
			m.help.ScrollDown(1)
		case k.is(msg, ActUp):
			m.help.ScrollUp(1)
		case k.is(msg, ActPageDown):
			m.help.HalfPageDown()
		case k.is(msg, ActPageUp):
			m.help.HalfPageUp()
		}
		return nil

	case modeInsert:
		switch msg.String() {
		case "enter":
			return m.submit()
		case "esc":
			// Back to the results (or, on the start page, to normal mode so
			// ? and other bindings work).
			m.mode = modeNormal
			m.input.Blur()
			return nil
		case "ctrl+l":
			m.input.CursorEnd()
			return nil
		case "ctrl+e":
			return m.compose()
		case "tab":
			if m.completeBang() {
				return nil
			}
			m.openPicker(0)
			return nil
		case "shift+tab":
			m.openPicker(0)
			return nil
		case "up", "ctrl+p":
			if m.histIdx > 0 {
				m.histIdx--
				m.input.SetValue(m.history[m.histIdx])
				m.input.CursorEnd()
			}
			return nil
		case "down", "ctrl+n":
			if m.histIdx < len(m.history)-1 {
				m.histIdx++
				m.input.SetValue(m.history[m.histIdx])
			} else {
				m.histIdx = len(m.history)
				m.input.SetValue("")
			}
			m.input.CursorEnd()
			return nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return cmd
	}

	if m.hint.active {
		return m.hintKey(msg)
	}
	if m.mode == modeVisual {
		return m.visualKey(msg)
	}

	// Vim-style counts (5j, 3G) and multi-key sequences (gg, gt).
	var key fmt.Stringer = msg
	s := msg.String()
	if len(m.pending) == 0 && len(s) == 1 && s[0] >= '0' && s[0] <= '9' && (s != "0" || m.count > 0) && !k.isExact(s) {
		m.count = min(m.count*10+int(s[0]-'0'), 9999)
		return nil
	}
	if len(m.pending) > 0 {
		seq := strings.Join(append(m.pending, s), " ")
		switch {
		case k.isPrefix(seq) && !k.isExact(seq):
			m.pending = append(m.pending, s)
			return nil
		case k.isExact(seq):
			m.pending = nil
			key = seqKey(seq)
		default:
			// Unknown sequence: drop it, as vim does.
			m.pending, m.count = nil, 0
			return nil
		}
	} else if k.isPrefix(s) && !k.isExact(s) {
		m.pending = []string{s}
		return nil
	}
	n, counted := max(m.count, 1), m.count > 0
	m.count = 0

	switch {
	case k.is(key, ActVisual), k.is(key, ActVisualLine):
		return m.enterVisual(k.is(key, ActVisualLine))
	case k.is(key, ActHint), k.is(key, ActHintOpen):
		return m.startHint(k.is(key, ActHintOpen))
	case k.is(key, ActNextTab):
		m.rootCmd = "tabnext"
		if counted {
			m.rootCmd = fmt.Sprintf("tabnext %d", n)
		}
		return nil
	case k.is(key, ActPrevTab):
		m.rootCmd = fmt.Sprintf("tabprevious %d", n)
		return nil
	}

	// Normal and detail modes share most bindings.
	switch {
	case k.is(key, ActQuit):
		if m.mode == modeDetail {
			m.mode = modeNormal
			return nil
		}
		m.quitRequested = true
		return tea.Quit
	case k.is(key, ActHelp):
		m.prev = m.mode
		m.mode = modeHelp
		m.refreshHelp()
		return nil
	case k.is(key, ActSearch):
		m.mode = modeInsert
		m.input.Focus()
		m.input.CursorEnd()
		return textinput.Blink
	case k.is(key, ActCompose):
		return m.compose()
	case k.is(key, ActBack):
		return m.back()
	case k.is(key, ActCloseDetails):
		if m.mode == modeDetail {
			m.mode = modeNormal
		}
		return nil
	}

	if m.mode == modeDetail {
		switch {
		case k.is(key, ActDown):
			m.detail.ScrollDown(n)
			return nil
		case k.is(key, ActUp):
			m.detail.ScrollUp(n)
			return nil
		case k.is(key, ActPageDown):
			m.detail.HalfPageDown()
			return nil
		case k.is(key, ActPageUp):
			m.detail.HalfPageUp()
			return nil
		case k.is(key, ActTop):
			m.detail.GotoTop()
			return nil
		case k.is(key, ActBottom):
			m.detail.GotoBottom()
			return nil
		}
	}

	r, hasResult := m.current()
	switch {
	case k.is(key, ActDown):
		m.move(n)
	case k.is(key, ActUp):
		m.move(-n)
	case k.is(key, ActTop), k.is(key, ActBottom):
		switch {
		case counted:
			m.jump(n - 1) // 5G / 5gg: result 5
		case k.is(key, ActTop):
			m.move(-len(m.results))
		default:
			m.move(len(m.results))
		}
	case k.is(key, ActPageDown):
		m.move(n * max(m.listHeight()/m.itemHeightGuess(), 1))
	case k.is(key, ActPageUp):
		m.move(-n * max(m.listHeight()/m.itemHeightGuess(), 1))
	case k.is(key, ActDetails):
		if hasResult && m.mode == modeNormal {
			return m.showDetails()
		}
	case k.is(key, ActOpen):
		if hasResult {
			return m.openURL(r.URL)
		}
	case k.is(key, ActOpenSearchPage):
		if m.plan.Engine != nil {
			return m.openURL(m.plan.Engine.SearchURL(m.plan.Query))
		}
	case k.is(key, ActYankURL):
		if hasResult {
			return m.copy(r.URL, "URL")
		}
	case k.is(key, ActYankSnippet):
		if hasResult {
			what := "snippet"
			if r.Code != nil {
				what = "code"
			}
			return m.copy(app.SnippetText(r), what)
		}
	case k.is(key, ActYankMarkdown):
		if hasResult {
			return m.copy(fmt.Sprintf("[%s](%s)", r.Title, r.URL), "Markdown link")
		}
	case k.is(key, ActEdit):
		return m.editCurrent()
	case k.is(key, ActEditResults):
		if len(m.results) > 0 {
			s := m.summary
			s.Text = m.extSummary
			return m.editContent("results.md", app.ResultsMarkdown(m.plan.Query.Text, m.plan.Engine.Name(), m.plan.Query.Kind, m.plan.Query.Page, m.results, s), 0)
		}
	case k.is(key, ActSummarizePage):
		if hasResult {
			if r.Code != nil {
				return m.setFlash("summaries apply to web pages; press e to open the file")
			}
			if m.mode == modeNormal {
				m.mode = modeDetail
			}
			delete(m.pageErrs, r.URL)
			return m.fetchPage(r.URL, "")
		}
	case k.is(key, ActToggleSummary):
		m.showSummary = !m.showSummary
		m.ensureVisible()
	case k.is(key, ActNextPage):
		if m.plan.Engine != nil {
			m.opts.Page = m.plan.Query.Page + 1
			return m.rerunPage()
		}
	case k.is(key, ActPrevPage):
		if m.plan.Engine != nil && m.plan.Query.Page > 1 {
			m.opts.Page = m.plan.Query.Page - 1
			return m.rerunPage()
		}
	case k.is(key, ActNextEngine):
		m.openPicker(0)
	case k.is(key, ActPrevEngine):
		m.openPicker(0)
	case k.is(key, ActCycleType):
		return m.typeCycle()
	case k.is(key, ActRefresh):
		m.opts.Page = m.plan.Query.Page
		return m.rerun()
	}
	return nil
}

func (m *Model) rerunPage() tea.Cmd {
	m.opts.Engine = m.plan.Engine.Name()
	m.opts.Kind = m.plan.Query.Kind
	m.stripBang()
	return m.search(m.plan.Query.Text)
}

// completeBang completes a partially typed !bang at the end of the input.
func (m *Model) completeBang() bool {
	v := m.input.Value()
	i := strings.LastIndexByte(v, ' ') + 1
	word := v[i:]
	if !strings.HasPrefix(word, "!") || len(word) < 2 {
		return false
	}
	c := m.app.Bangs.Complete(word)
	if len(c) == 0 {
		return false
	}
	if c[0] == word && len(c) > 1 {
		c[0] = c[1]
	}
	m.input.SetValue(v[:i] + c[0] + " ")
	m.input.CursorEnd()
	return true
}

func (m *Model) showDetails() tea.Cmd {
	r, ok := m.current()
	if !ok {
		return nil
	}
	m.mode = modeDetail
	m.refreshDetail()
	m.detail.GotoTop()
	if r.Code == nil && m.pendingPage != r.URL {
		if _, cached := m.pages[r.URL]; !cached {
			delete(m.pageErrs, r.URL)
			return m.fetchPage(r.URL, "")
		}
	}
	return nil
}

func (m *Model) move(d int) {
	if len(m.results) == 0 {
		return
	}
	m.sel = min(max(m.sel+d, 0), len(m.results)-1)
	m.ensureVisible()
	m.refreshDetail()
	if m.mode == modeDetail {
		m.detail.GotoTop()
	}
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if m.picker.open {
		return m.pickerMouse(msg)
	}
	l := m.layout()
	switch msg.Button {
	case tea.MouseButtonWheelDown, tea.MouseButtonWheelUp:
		d := 1
		if msg.Button == tea.MouseButtonWheelUp {
			d = -1
		}
		switch m.mode {
		case modeHelp:
			if d > 0 {
				m.help.ScrollDown(3)
			} else {
				m.help.ScrollUp(3)
			}
		case modeDetail:
			if d > 0 {
				m.detail.ScrollDown(3)
			} else {
				m.detail.ScrollUp(3)
			}
		default:
			m.move(d)
		}
		return nil
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return nil
		}
		if msg.Y < l.inputH {
			m.mode = modeInsert
			m.input.Focus()
			return textinput.Blink
		}
		if m.mode == modeHelp {
			m.mode = m.prev
			return nil
		}
		if m.mode == modeDetail {
			return nil
		}
		y := msg.Y - l.listTop
		if y < 0 || y >= l.listH {
			return nil
		}
		idx := m.itemAtLine(y + m.offset)
		if idx < 0 {
			return nil
		}
		if m.mode == modeInsert {
			m.mode = modeNormal
			m.input.Blur()
		}
		if idx == m.sel {
			return m.showDetails()
		}
		m.sel = idx
		m.ensureVisible()
		m.refreshDetail()
	}
	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if n < 2 {
		n = 2
	}
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// back steps out one level in normal mode: details → results → start page
// (no results, empty search bar). It never enters insert mode; on the start
// page it does nothing (press / or i to type).
func (m *Model) back() tea.Cmd {
	m.pending, m.count = nil, 0
	switch {
	case m.mode == modeDetail:
		m.mode = modeNormal
	case len(m.results) > 0 || m.plan.Engine != nil || m.err != nil || m.loading:
		m.reset()
	}
	return nil
}

// reset returns to the start page.
func (m *Model) reset() {
	if m.cancel != nil {
		m.cancel()
	}
	m.searchID++ // ignore in-flight results
	m.loading, m.busy, m.err = false, "", nil
	m.plan = app.Plan{}
	m.opts = m.initOpts
	m.setResults(nil)
	m.histIdx = len(m.history)
	m.input.SetValue("")
	m.mode = modeNormal
	m.input.Blur()
	m.refreshDetail()
}

// jump selects result i (clamped).
func (m *Model) jump(i int) {
	if len(m.results) == 0 {
		return
	}
	m.sel = min(max(i, 0), len(m.results)-1)
	m.ensureVisible()
	m.refreshDetail()
}

// showText shows a titled list in the help view (used by :engines, :bangs,
// :history and :theme).
func (m *Model) showText(title string, lines []string) {
	if m.mode != modeHelp {
		m.prev = m.mode
	}
	m.mode = modeHelp
	body := m.st.accent.Render(title) + "\n\n" + strings.Join(lines, "\n")
	m.help.SetContent(lipgloss.NewStyle().Padding(0, 2).Render(body))
	m.help.GotoTop()
}

// showHelp opens the help view, scrolled to a topic heading.
func (m *Model) showHelp(topic string) {
	if m.mode != modeHelp {
		m.prev = m.mode
	}
	m.mode = modeHelp
	m.refreshHelp()
	m.help.GotoTop()
	if line, ok := m.helpSections[strings.ToLower(topic)]; ok {
		m.help.SetYOffset(line)
	}
}

// runKeyAction runs a normal-mode action as if its key had been pressed.
func (m *Model) runKeyAction(a Action) tea.Cmd {
	b := m.keys.b[a]
	ks := b.Keys()
	if len(ks) == 0 {
		return nil
	}
	saved := m.mode
	if m.mode == modeInsert || m.mode == modeHelp {
		m.mode = modeNormal
	}
	cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.ReplaceAll(ks[0], " ", ""))})
	if m.mode == modeNormal && saved == modeHelp {
		m.mode = saved
	}
	return cmd
}
