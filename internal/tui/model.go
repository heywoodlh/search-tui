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
	"github.com/heywoodlh/tui-search/internal/app"
	"github.com/heywoodlh/tui-search/internal/engine"
	"github.com/heywoodlh/tui-search/internal/safe"
	"github.com/heywoodlh/tui-search/internal/sys"
)

type mode int

const (
	modeInsert mode = iota
	modeNormal
	modeDetail
	modeHelp
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

	app  *app.App
	keys keyMap
	st   styles

	input   textinput.Model
	detail  viewport.Model
	help    viewport.Model
	spinner spinner.Model

	width, height int
	mode, prev    mode

	opts     app.PlanOptions
	initOpts app.PlanOptions // restored when esc returns to the start page
	picker   picker
	plan     app.Plan
	results  []engine.Result
	sel      int
	offset   int

	summary     app.Summary
	showSummary bool
	extSummary  string

	loading  bool
	searchID int
	cancel   context.CancelFunc
	busy     string // background activity label (fetching page...)

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
		m.busy = ""
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

	// Normal and detail modes share most bindings.
	switch {
	case k.is(msg, ActQuit):
		if m.mode == modeDetail {
			m.mode = modeNormal
			return nil
		}
		m.quitRequested = true
		return tea.Quit
	case k.is(msg, ActHelp):
		m.prev = m.mode
		m.mode = modeHelp
		m.refreshHelp()
		return nil
	case k.is(msg, ActSearch):
		m.mode = modeInsert
		m.input.Focus()
		m.input.CursorEnd()
		return textinput.Blink
	case k.is(msg, ActCompose):
		return m.compose()
	case k.is(msg, ActBack):
		return m.back()
	case k.is(msg, ActCloseDetails):
		if m.mode == modeDetail {
			m.mode = modeNormal
		}
		return nil
	}

	if m.mode == modeDetail {
		switch {
		case k.is(msg, ActDown):
			m.detail.ScrollDown(1)
			return nil
		case k.is(msg, ActUp):
			m.detail.ScrollUp(1)
			return nil
		case k.is(msg, ActPageDown):
			m.detail.HalfPageDown()
			return nil
		case k.is(msg, ActPageUp):
			m.detail.HalfPageUp()
			return nil
		case k.is(msg, ActTop):
			m.detail.GotoTop()
			return nil
		case k.is(msg, ActBottom):
			m.detail.GotoBottom()
			return nil
		case msg.String() == "enter":
			if r, ok := m.current(); ok {
				return m.openURL(r.URL)
			}
		}
	}

	r, hasResult := m.current()
	switch {
	case k.is(msg, ActDown):
		m.move(1)
	case k.is(msg, ActUp):
		m.move(-1)
	case k.is(msg, ActTop):
		m.move(-len(m.results))
	case k.is(msg, ActBottom):
		m.move(len(m.results))
	case k.is(msg, ActPageDown):
		m.move(max(m.listHeight()/m.itemHeightGuess(), 1))
	case k.is(msg, ActPageUp):
		m.move(-max(m.listHeight()/m.itemHeightGuess(), 1))
	case k.is(msg, ActDetails):
		if hasResult {
			m.mode = modeDetail
			m.refreshDetail()
			m.detail.GotoTop()
		}
	case k.is(msg, ActOpen):
		if hasResult {
			return m.openURL(r.URL)
		}
	case k.is(msg, ActOpenSearchPage):
		if m.plan.Engine != nil {
			return m.openURL(m.plan.Engine.SearchURL(m.plan.Query))
		}
	case k.is(msg, ActYankURL):
		if hasResult {
			return m.copy(r.URL, "URL")
		}
	case k.is(msg, ActYankSnippet):
		if hasResult {
			what := "snippet"
			if r.Code != nil {
				what = "code"
			}
			return m.copy(app.SnippetText(r), what)
		}
	case k.is(msg, ActYankMarkdown):
		if hasResult {
			return m.copy(fmt.Sprintf("[%s](%s)", r.Title, r.URL), "Markdown link")
		}
	case k.is(msg, ActEdit):
		return m.editCurrent()
	case k.is(msg, ActEditResults):
		if len(m.results) > 0 {
			s := m.summary
			s.Text = m.extSummary
			return m.editContent("results.md", app.ResultsMarkdown(m.plan.Query.Text, m.plan.Engine.Name(), m.plan.Query.Kind, m.plan.Query.Page, m.results, s), 0)
		}
	case k.is(msg, ActSummarizePage):
		if hasResult {
			if r.Code != nil {
				return m.setFlash("summaries apply to web pages; press e to open the file")
			}
			if m.mode == modeNormal && m.layoutMode() != layoutSplit {
				m.mode = modeDetail
			}
			delete(m.pageErrs, r.URL)
			return m.fetchPage(r.URL, "")
		}
	case k.is(msg, ActToggleSummary):
		m.showSummary = !m.showSummary
		m.ensureVisible()
	case k.is(msg, ActNextPage):
		if m.plan.Engine != nil {
			m.opts.Page = m.plan.Query.Page + 1
			return m.rerunPage()
		}
	case k.is(msg, ActPrevPage):
		if m.plan.Engine != nil && m.plan.Query.Page > 1 {
			m.opts.Page = m.plan.Query.Page - 1
			return m.rerunPage()
		}
	case k.is(msg, ActNextEngine):
		m.openPicker(0)
	case k.is(msg, ActPrevEngine):
		m.openPicker(0)
	case k.is(msg, ActCycleType):
		return m.typeCycle()
	case k.is(msg, ActRefresh):
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
	inPreview := l.previewW > 0 && msg.X >= l.listW
	switch msg.Button {
	case tea.MouseButtonWheelDown, tea.MouseButtonWheelUp:
		d := 1
		if msg.Button == tea.MouseButtonWheelUp {
			d = -1
		}
		switch {
		case m.mode == modeHelp:
			if d > 0 {
				m.help.ScrollDown(3)
			} else {
				m.help.ScrollUp(3)
			}
		case m.mode == modeDetail || inPreview:
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
		if inPreview {
			m.mode = modeDetail
			return nil
		}
		if m.mode == modeDetail && l.previewW == 0 {
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
			m.mode = modeDetail
			m.refreshDetail()
			return nil
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

// back steps out one level: details → results → start page (no results,
// empty search bar). On the start page it toggles between the search bar
// and normal mode.
func (m *Model) back() tea.Cmd {
	switch {
	case m.mode == modeDetail:
		m.mode = modeNormal
	case len(m.results) > 0 || m.plan.Engine != nil || m.err != nil || m.loading:
		m.reset()
	default:
		m.mode = modeInsert
		m.input.Focus()
		return textinput.Blink
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
	m.mode = modeInsert
	m.input.Focus()
	m.refreshDetail()
}
