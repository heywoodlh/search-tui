package tui

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/heywoodlh/search-tui/internal/engine"
	"github.com/heywoodlh/search-tui/internal/theme"
)

// The ":" command line, modelled on vim and iamb (https://iamb.chat):
// window and tab commands plus search-specific ones. Commands accept vim
// abbreviations (:sp, :vs, :tabn) and modifiers (:vertical, :aboveleft,
// :belowright, :tab). Tab completes names and arguments; up/down recall
// history.

type cmdline struct {
	active bool
	input  textinput.Model
	hist   []string
	hidx   int
	// completion cycling
	comps   []string
	compIdx int
	compPre string
}

func newCmdline() cmdline {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 1024
	return cmdline{input: ti}
}

// invocation is a parsed command line.
type invocation struct {
	name     string
	args     string
	bang     bool
	vertical bool // :vertical {cmd}
	before   bool // :aboveleft / :leftabove {cmd}
	newTab   bool // :tab {cmd}
}

type command struct {
	name    string
	min     int      // shortest accepted abbreviation
	aliases []string // other accepted names
	usage   string
	desc    string
	topic   string // help section
	run     func(r *Root, in invocation) error
	// complete returns candidates for the first argument.
	complete func(r *Root) []string
}

var errCmd = errors.New

// commands is the command table, in help order.
var commands []command

func init() {
	commands = []command{
		// Windows and tabs (as in vim/iamb).
		{name: "quit", min: 1, aliases: []string{"wq", "x"}, desc: "close the pane (the tab when it is the last pane; quit when it is the last tab)", topic: "panes",
			run: func(r *Root, in invocation) error { r.pendingCmd = r.close(r.focus); return nil }},
		{name: "quitall", min: 5, aliases: []string{"qa", "qall", "wqa", "xa", "xall"}, desc: "quit search", topic: "panes",
			run: func(r *Root, in invocation) error { r.pendingCmd = tea.Quit; return nil }},
		{name: "close", min: 3, desc: "close the pane, but never the last one", topic: "panes",
			run: func(r *Root, in invocation) error {
				if len(r.panes()) == 1 && len(r.tabs) == 1 {
					return errCmd("E444: Cannot close last window")
				}
				r.pendingCmd = r.close(r.focus)
				return nil
			}},
		{name: "only", min: 2, desc: "close every other pane in this tab", topic: "panes",
			run: func(r *Root, in invocation) error { r.only(); return nil }},
		{name: "split", min: 2, usage: "[query]", desc: "split: new pane below (with :vertical, to the right) copying this search, or running query", topic: "panes",
			run: func(r *Root, in invocation) error { return r.cmdSplit(in, splitStacked, false) }},
		{name: "vsplit", min: 2, aliases: []string{"vsp"}, usage: "[query]", desc: "vsplit: new pane to the right", topic: "panes",
			run: func(r *Root, in invocation) error { return r.cmdSplit(in, splitSideBySide, false) }},
		{name: "new", min: 3, usage: "[query]", desc: "new empty pane below (start page), or searching query", topic: "panes",
			run: func(r *Root, in invocation) error { return r.cmdSplit(in, splitStacked, true) }},
		{name: "vnew", min: 3, usage: "[query]", desc: "new empty pane to the right", topic: "panes",
			run: func(r *Root, in invocation) error { return r.cmdSplit(in, splitSideBySide, true) }},
		{name: "resize", min: 3, usage: "[+-]N", desc: "set (or change by ±N) the pane height; with :vertical, the width; no N maximises", topic: "panes",
			run: func(r *Root, in invocation) error {
				a := strings.TrimSpace(in.args)
				if a == "" {
					if in.vertical {
						r.resize(r.width, false, true)
					} else {
						r.resize(r.height, false, false)
					}
					return nil
				}
				n, err := strconv.Atoi(strings.TrimPrefix(a, "+"))
				if err != nil {
					return errCmd("E475: Invalid argument: " + a)
				}
				r.resize(n, strings.HasPrefix(a, "+") || strings.HasPrefix(a, "-"), in.vertical)
				return nil
			}},
		{name: "wincmd", min: 4, usage: "{char}", desc: "run a ctrl+w command, e.g. :wincmd l", topic: "panes",
			run: func(r *Root, in invocation) error {
				if strings.TrimSpace(in.args) == "" {
					return errCmd("E471: Argument required")
				}
				r.pendingCmd = r.windowCommand(strings.TrimSpace(in.args))
				return nil
			}},
		{name: "tabnew", min: 6, aliases: []string{"tabe", "tabedit"}, usage: "[query]", desc: "open a new tab (start page, or searching query)", topic: "tabs",
			run: func(r *Root, in invocation) error {
				r.pendingCmd = r.newTab(r.newPane(r.focus, true), in.args)
				return nil
			}},
		{name: "tabclose", min: 4, usage: "[N]", desc: "close the current tab (or tab N)", topic: "tabs",
			run: func(r *Root, in invocation) error {
				i, err := r.tabArg(in.args, r.cur)
				if err != nil {
					return err
				}
				if len(r.tabs) == 1 {
					return errCmd("E784: Cannot close last tab page")
				}
				r.pendingCmd = r.closeTab(i)
				return nil
			}},
		{name: "tabonly", min: 4, desc: "close every other tab", topic: "tabs",
			run: func(r *Root, in invocation) error { r.tabOnly(); return nil }},
		{name: "tabnext", min: 4, usage: "[N]", desc: "next tab, or go to tab N (also gt / Ngt)", topic: "tabs",
			run: func(r *Root, in invocation) error {
				if strings.TrimSpace(in.args) == "" {
					r.switchTab((r.cur + 1) % len(r.tabs))
					return nil
				}
				i, err := r.tabArg(in.args, r.cur)
				if err != nil {
					return err
				}
				r.switchTab(i)
				return nil
			}},
		{name: "tabprevious", min: 4, aliases: []string{"tabN", "tabNext"}, usage: "[N]", desc: "previous tab, or go back N tabs (also gT)", topic: "tabs",
			run: func(r *Root, in invocation) error {
				n := 1
				if a := strings.TrimSpace(in.args); a != "" {
					v, err := strconv.Atoi(a)
					if err != nil {
						return errCmd("E475: Invalid argument: " + a)
					}
					n = v
				}
				k := len(r.tabs)
				r.switchTab(((r.cur-n)%k + k) % k)
				return nil
			}},
		{name: "tabfirst", min: 6, aliases: []string{"tabr", "tabrewind"}, desc: "first tab", topic: "tabs",
			run: func(r *Root, in invocation) error { r.switchTab(0); return nil }},
		{name: "tablast", min: 4, desc: "last tab", topic: "tabs",
			run: func(r *Root, in invocation) error { r.switchTab(len(r.tabs) - 1); return nil }},
		{name: "tabmove", min: 4, usage: "[N|+N|-N|$]", desc: "move the tab after tab N (0 = first, $ = last), or by ±N", topic: "tabs",
			run: func(r *Root, in invocation) error {
				a := strings.TrimSpace(in.args)
				switch {
				case a == "" || a == "$":
					r.moveTab(len(r.tabs))
				case strings.HasPrefix(a, "+") || strings.HasPrefix(a, "-"):
					n, err := strconv.Atoi(a)
					if err != nil {
						return errCmd("E475: Invalid argument: " + a)
					}
					r.moveTab(r.cur + n)
				default:
					n, err := strconv.Atoi(a)
					if err != nil {
						return errCmd("E475: Invalid argument: " + a)
					}
					r.moveTab(n)
				}
				return nil
			}},

		// Searching.
		{name: "search", min: 3, aliases: []string{"s", "find", "fin"}, usage: "{query}", desc: "search for query in this pane (!bangs work)", topic: "search",
			run: func(r *Root, in invocation) error {
				if strings.TrimSpace(in.args) == "" {
					return errCmd("E471: Argument required")
				}
				p := r.focus
				p.input.SetValue(in.args)
				r.pendingCmd = p.submit()
				return nil
			}},
		{name: "engine", min: 3, usage: "[name]", desc: "switch engine and re-run (no name: open the engine picker)", topic: "search",
			complete: func(r *Root) []string { return r.focus.engineNames() },
			run: func(r *Root, in invocation) error {
				p := r.focus
				name := strings.TrimSpace(in.args)
				if name == "" {
					p.openPicker(0)
					return nil
				}
				if _, ok := r.app.Reg.Get(name); !ok {
					return errCmd("unknown or unconfigured engine: " + name + " (:engines lists them)")
				}
				r.pendingCmd = p.selectEngine(name)
				return nil
			}},
		{name: "type", min: 2, usage: "[web|code|repos|issues]", desc: "set the search type and re-run (no argument: cycle)", topic: "search",
			complete: func(*Root) []string { return []string{"web", "code", "repos", "issues"} },
			run: func(r *Root, in invocation) error {
				p := r.focus
				if strings.TrimSpace(in.args) == "" {
					r.pendingCmd = p.typeCycle()
					return nil
				}
				k, err := engine.ParseKind(in.args)
				if err != nil {
					return err
				}
				p.opts.Kind = k
				if p.plan.Engine != nil && !p.plan.Engine.Supports(k) {
					p.opts.Engine = ""
				}
				p.stripBang()
				if strings.TrimSpace(p.input.Value()) != "" {
					p.opts.Page = 1
					r.pendingCmd = p.search(p.input.Value())
				}
				return nil
			}},
		{name: "page", min: 2, usage: "{N}", desc: "go to results page N", topic: "search",
			run: func(r *Root, in invocation) error {
				n, err := strconv.Atoi(strings.TrimSpace(in.args))
				if err != nil || n < 1 {
					return errCmd("E475: Invalid argument: " + in.args)
				}
				return r.page(n)
			}},
		{name: "next", min: 1, desc: "next results page", topic: "search",
			run: func(r *Root, in invocation) error { return r.page(r.focus.plan.Query.Page + 1) }},
		{name: "previous", min: 4, aliases: []string{"N", "Next"}, desc: "previous results page", topic: "search",
			run: func(r *Root, in invocation) error { return r.page(r.focus.plan.Query.Page - 1) }},
		{name: "refresh", min: 3, desc: "re-run the search", topic: "search",
			run: func(r *Root, in invocation) error {
				p := r.focus
				p.opts.Page = p.plan.Query.Page
				r.pendingCmd = p.rerun()
				return nil
			}},
		{name: "welcome", min: 3, aliases: []string{"home"}, desc: "back to the start page", topic: "search",
			run: func(r *Root, in invocation) error { r.focus.reset(); return nil }},

		// Acting on results ([N] = result number, default: the selected one).
		{name: "open-link", min: 9, usage: "{N|URL}", desc: "open numbered link (result or fetched page) or URL in the browser", topic: "results",
			complete: func(r *Root) []string { return r.linkCompletions() },
			run: func(r *Root, in invocation) error {
				u, err := r.focus.resolveLink(in.args)
				if err != nil {
					return err
				}
				r.pendingCmd = r.focus.openURL(u)
				return nil
			}},
		{name: "copy-link", min: 9, usage: "{N|URL}", desc: "copy numbered link or URL to the configured clipboard", topic: "results",
			complete: func(r *Root) []string { return r.linkCompletions() },
			run: func(r *Root, in invocation) error {
				u, err := r.focus.resolveLink(in.args)
				if err != nil {
					return err
				}
				r.pendingCmd = r.focus.copy(u, "link")
				return nil
			}},
		{name: "open", min: 1, usage: "[N]", desc: "open the result in the browser", topic: "results",
			run: func(r *Root, in invocation) error {
				return r.onResult(in, func(p *Model, res engine.Result) tea.Cmd { return p.openURL(res.URL) })
			}},
		{name: "yank", min: 2, usage: "[N]", desc: "copy the result URL", topic: "results",
			run: func(r *Root, in invocation) error {
				return r.onResult(in, func(p *Model, res engine.Result) tea.Cmd { return p.copy(res.URL, "URL") })
			}},
		{name: "copy", min: 2, usage: "[N]", desc: "copy the result snippet or code", topic: "results",
			run: func(r *Root, in invocation) error {
				return r.onResult(in, func(p *Model, res engine.Result) tea.Cmd { return p.copy(snippetOf(res), "snippet") })
			}},
		{name: "edit", min: 1, usage: "[N]", desc: "open the result (page as Markdown, or the code file) in $EDITOR", topic: "results",
			run: func(r *Root, in invocation) error {
				return r.onResult(in, func(p *Model, _ engine.Result) tea.Cmd { return p.editCurrent() })
			}},
		{name: "summarise", min: 3, aliases: []string{"summarize"}, usage: "[N]", desc: "fetch and summarise the result page", topic: "results",
			run: func(r *Root, in invocation) error {
				return r.onResult(in, func(p *Model, res engine.Result) tea.Cmd {
					p.mode = modeDetail
					delete(p.pageErrs, res.URL)
					return p.fetchPage(res.URL, "")
				})
			}},
		{name: "export", min: 3, desc: "open all results as Markdown in $EDITOR", topic: "results",
			run: func(r *Root, in invocation) error {
				r.pendingCmd = r.focus.runKeyAction(ActEditResults)
				return nil
			}},
		{name: "compose", min: 4, aliases: []string{"editor"}, desc: "write the query in $EDITOR", topic: "results",
			run: func(r *Root, in invocation) error { r.pendingCmd = r.focus.compose(); return nil }},

		// Interface.
		{name: "help", min: 1, usage: "[topic]", desc: "show help (topics: keys, commands, panes, tabs, search, results, visual, hints, bangs)", topic: "commands",
			complete: func(*Root) []string { return helpTopics },
			run: func(r *Root, in invocation) error {
				if in.newTab {
					r.pendingCmd = r.newTab(r.newPane(r.focus, true), "")
				}
				r.focus.showHelp(strings.TrimSpace(in.args))
				return nil
			}},
		{name: "theme", min: 3, usage: "[name]", desc: "switch the colour theme (no name: list themes)", topic: "commands",
			complete: func(*Root) []string { return theme.Names() },
			run: func(r *Root, in invocation) error {
				name := strings.TrimSpace(in.args)
				if name == "" {
					r.focus.showText("Themes", theme.Names())
					return nil
				}
				return r.setTheme(name)
			}},
		{name: "set", min: 2, usage: "{option}", desc: "set an option: summary, nosummary, layout=auto|split|stacked, results=N, snippetlines=N, theme=NAME, hintchars=CHARS", topic: "commands",
			complete: func(*Root) []string { return setOptions },
			run:      func(r *Root, in invocation) error { return r.set(in.args) }},
		{name: "engines", min: 7, desc: "list engines", topic: "commands",
			run: func(r *Root, in invocation) error {
				var lines []string
				for _, n := range r.app.Reg.Names() {
					info := r.app.Reg.Info(n)
					lines = append(lines, fmt.Sprintf("%-14s !%-6s %s", n, firstOr(info.Bangs), info.Description))
				}
				for _, u := range r.app.Reg.Unconfigured() {
					lines = append(lines, fmt.Sprintf("%-14s (not configured: set %s)", u.Name, u.Missing))
				}
				r.focus.showText("Engines", lines)
				return nil
			}},
		{name: "bangs", min: 3, desc: "list !bangs", topic: "commands",
			run: func(r *Root, in invocation) error {
				var lines []string
				for _, e := range r.app.Bangs.List() {
					lines = append(lines, fmt.Sprintf("%-12s %s", e.Bang, e.Target))
				}
				r.focus.showText("Bangs", lines)
				return nil
			}},
		{name: "history", min: 3, desc: "show this pane's query history", topic: "commands",
			run: func(r *Root, in invocation) error {
				lines := append([]string{}, r.focus.history...)
				for i := range lines {
					lines[i] = fmt.Sprintf("%3d  %s", i+1, lines[i])
				}
				if len(r.cmd.hist) > 0 {
					lines = append(lines, "", "Command history:")
					for i, h := range r.cmd.hist {
						lines = append(lines, fmt.Sprintf("%3d  :%s", i+1, h))
					}
				}
				r.focus.showText("History", lines)
				return nil
			}},
		{name: "version", min: 2, desc: "show the version", topic: "commands",
			run: func(r *Root, in invocation) error {
				r.pendingCmd = r.focus.setFlash("search " + Version)
				return nil
			}},
		{name: "redraw", min: 4, desc: "redraw the screen", topic: "commands",
			run: func(r *Root, in invocation) error { r.layout(); r.pendingCmd = tea.ClearScreen; return nil }},
	}
}

// Version is shown by :version (set from the CLI).
var Version = "dev"

var helpTopics = []string{"keys", "commands", "panes", "tabs", "search", "results", "visual", "hints", "bangs", "mouse"}

var setOptions = []string{"summary", "nosummary", "summary!", "layout=auto", "layout=split", "layout=stacked", "results=", "snippetlines=", "theme=", "hintchars="}

func firstOr(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}

func snippetOf(r engine.Result) string {
	if r.Code != nil && len(r.Code.Fragments) > 0 {
		parts := make([]string, len(r.Code.Fragments))
		for i, f := range r.Code.Fragments {
			parts[i] = f.Text
		}
		return strings.Join(parts, "\n\n")
	}
	return r.Snippet
}

// lookup finds a command by name, alias or vim-style abbreviation.
func lookup(name string) (command, bool) {
	for _, c := range commands {
		if c.name == name {
			return c, true
		}
		for _, a := range c.aliases {
			if a == name {
				return c, true
			}
		}
	}
	for _, c := range commands {
		if len(name) >= c.min && strings.HasPrefix(c.name, name) {
			return c, true
		}
	}
	return command{}, false
}

var modifiers = map[string]func(*invocation){
	"vertical": func(i *invocation) { i.vertical = true }, "vert": func(i *invocation) { i.vertical = true },
	"horizontal": func(i *invocation) { i.vertical = false }, "hor": func(i *invocation) { i.vertical = false },
	"aboveleft": func(i *invocation) { i.before = true }, "abo": func(i *invocation) { i.before = true },
	"leftabove": func(i *invocation) { i.before = true }, "lefta": func(i *invocation) { i.before = true },
	"belowright": func(i *invocation) { i.before = false }, "bel": func(i *invocation) { i.before = false },
	"rightbelow": func(i *invocation) { i.before = false }, "rightb": func(i *invocation) { i.before = false },
	"tab": func(i *invocation) { i.newTab = true },
}

// parse splits a command line into modifiers, name, bang and arguments.
func parse(line string) invocation {
	var in invocation
	rest := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), ":"))
	for {
		word, tail, _ := strings.Cut(rest, " ")
		if mod, ok := modifiers[word]; ok && strings.TrimSpace(tail) != "" {
			mod(&in)
			rest = strings.TrimSpace(tail)
			continue
		}
		break
	}
	name := rest
	if i := strings.IndexFunc(rest, func(c rune) bool { return c == ' ' || c == '!' }); i >= 0 {
		name, rest = rest[:i], rest[i:]
	} else {
		rest = ""
	}
	if strings.HasPrefix(rest, "!") {
		in.bang = true
		rest = rest[1:]
	}
	in.name, in.args = name, strings.TrimSpace(rest)
	return in
}

// execute runs a command line; errors are shown in the focused pane.
func (r *Root) execute(line string) tea.Cmd {
	line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), ":"))
	if line == "" {
		return nil
	}
	// :N jumps to result N.
	if n, err := strconv.Atoi(line); err == nil {
		r.focus.jump(n - 1)
		return nil
	}
	in := parse(line)
	c, ok := lookup(in.name)
	if !ok {
		r.focus.err = fmt.Errorf("E492: Not an editor command: %s", line)
		return nil
	}
	r.pendingCmd = nil
	r.focus.err = nil
	if in.newTab && (c.name == "split" || c.name == "vsplit" || c.name == "new" || c.name == "vnew") {
		// :tab split opens the pane in a new tab instead.
		return r.newTab(r.newPane(r.focus, c.name == "new" || c.name == "vnew"), in.args)
	}
	if err := c.run(r, in); err != nil {
		r.focus.err = err
		return nil
	}
	cmd := r.pendingCmd
	r.pendingCmd = nil
	return cmd
}

func (r *Root) cmdSplit(in invocation, dir splitDir, empty bool) error {
	if in.vertical {
		dir = splitSideBySide
	}
	r.pendingCmd = r.splitWith(dir, in.before, empty, in.args)
	return nil
}

func (r *Root) page(n int) error {
	p := r.focus
	if p.plan.Engine == nil {
		return errCmd("no search in this pane")
	}
	if n < 1 {
		return errCmd("already on the first page")
	}
	p.opts.Page = n
	r.pendingCmd = p.rerunPage()
	return nil
}

func (r *Root) tabArg(a string, def int) (int, error) {
	a = strings.TrimSpace(a)
	if a == "" {
		return def, nil
	}
	n, err := strconv.Atoi(a)
	if err != nil || n < 1 || n > len(r.tabs) {
		return 0, errCmd("E475: Invalid argument: " + a)
	}
	return n - 1, nil
}

// onResult runs fn on result N (1-based) or the selected result.
func (r *Root) onResult(in invocation, fn func(*Model, engine.Result) tea.Cmd) error {
	p := r.focus
	if a := strings.TrimSpace(in.args); a != "" {
		n, err := strconv.Atoi(a)
		if err != nil || n < 1 || n > len(p.results) {
			return errCmd("E475: Invalid result number: " + a)
		}
		p.jump(n - 1)
	}
	res, ok := p.current()
	if !ok {
		return errCmd("no result selected")
	}
	r.pendingCmd = fn(p, res)
	return nil
}

func (r *Root) setTheme(name string) error {
	t, err := theme.Resolve(name, nil)
	if err != nil {
		return err
	}
	r.app.Cfg.UI.Theme = name
	for _, p := range r.allPanes() {
		p.st = newStyles(r.app.Cfg.UI, t)
		p.items = nil
		p.refreshDetail()
		p.refreshHelp()
	}
	r.pendingCmd = r.focus.setFlash("theme: " + t.Name)
	return nil
}

func (r *Root) set(arg string) error {
	arg = strings.TrimSpace(arg)
	p := r.focus
	ui := &r.app.Cfg.UI
	if arg == "" || arg == "all" {
		r.pendingCmd = p.setFlash(fmt.Sprintf("summary=%v layout=%s results=%d snippetlines=%d theme=%s hintchars=%s",
			p.showSummary, ui.Layout, max(p.opts.Limit, r.app.Cfg.Results), ui.SnippetLines, theme.NameOr(ui.Theme), orDefault(ui.HintChars, defaultHintChars)))
		return nil
	}
	key, val, hasVal := strings.Cut(arg, "=")
	switch key {
	case "summary":
		p.showSummary = true
	case "nosummary":
		p.showSummary = false
	case "summary!", "invsummary":
		p.showSummary = !p.showSummary
	case "layout":
		if val != "auto" && val != "split" && val != "stacked" {
			return errCmd("E474: layout must be auto, split or stacked")
		}
		ui.Layout = val
	case "results":
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 {
			return errCmd("E521: Number required after =")
		}
		p.opts.Limit = n
	case "snippetlines", "snippet_lines":
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 {
			return errCmd("E521: Number required after =")
		}
		ui.SnippetLines = n
	case "theme":
		return r.setTheme(val)
	case "hintchars", "hint_chars":
		if len([]rune(val)) < 2 {
			return errCmd("E474: hintchars needs at least two characters")
		}
		ui.HintChars = val
	default:
		if !hasVal {
			return errCmd("E518: Unknown option: " + key)
		}
		return errCmd("E518: Unknown option: " + key)
	}
	for _, q := range r.allPanes() {
		q.items = nil
	}
	r.layout()
	return nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// ---------------------------------------------------------------- input

func (r *Root) openCmd(prefill string) {
	r.cmd.active = true
	r.cmd.input.SetValue(prefill)
	r.cmd.input.CursorEnd()
	r.cmd.input.Focus()
	r.cmd.hidx = len(r.cmd.hist)
	r.cmd.comps = nil
	r.layout()
}

func (r *Root) closeCmd() {
	r.cmd.active = false
	r.cmd.input.Blur()
	r.cmd.comps = nil
	r.layout()
}

func (r *Root) cmdKey(msg tea.KeyMsg) tea.Cmd {
	c := &r.cmd
	switch msg.String() {
	case "esc", "ctrl+c":
		r.closeCmd()
		return nil
	case "enter":
		line := c.input.Value()
		r.closeCmd()
		if t := strings.TrimSpace(line); t != "" && (len(c.hist) == 0 || c.hist[len(c.hist)-1] != t) {
			c.hist = append(c.hist, t)
		}
		return r.execute(line)
	case "backspace":
		if c.input.Value() == "" {
			r.closeCmd()
			return nil
		}
	case "tab", "shift+tab":
		r.complete(msg.String() == "tab")
		return nil
	case "up", "down":
		// Recall history entries starting with what was typed, like vim.
		pre := c.input.Value()
		if c.comps == nil {
			c.compPre = pre
		}
		d := -1
		if msg.String() == "down" {
			d = 1
		}
		for i := c.hidx + d; i >= 0 && i <= len(c.hist); i += d {
			if i == len(c.hist) {
				c.hidx = i
				c.input.SetValue(c.compPre)
				break
			}
			if strings.HasPrefix(c.hist[i], c.compPre) {
				c.hidx = i
				c.input.SetValue(c.hist[i])
				break
			}
		}
		c.input.CursorEnd()
		c.comps = []string{} // keep compPre while browsing
		return nil
	}
	c.comps = nil
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	return cmd
}

func (r *Root) linkCompletions() []string {
	links := r.focus.links()
	out := make([]string, 0, len(links))
	for _, l := range links {
		out = append(out, strconv.Itoa(l.n))
	}
	return out
}

// complete cycles through completions for the word being typed.
func (r *Root) complete(forward bool) {
	c := &r.cmd
	if len(c.comps) == 0 {
		v := c.input.Value()
		head, word := "", v
		if i := strings.LastIndexByte(v, ' '); i >= 0 {
			head, word = v[:i+1], v[i+1:]
		}
		var cands []string
		if head == "" || isModifierOnly(head) {
			for _, cm := range commands {
				cands = append(cands, cm.name)
			}
			for m := range modifiers {
				if len(m) > 4 {
					cands = append(cands, m)
				}
			}
		} else if cm, ok := lookup(parse(head).name); ok && cm.complete != nil {
			cands = cm.complete(r)
		}
		if cm, ok := lookup(parse(head).name); !ok || (cm.name != "open-link" && cm.name != "copy-link") {
			sort.Strings(cands)
		}
		c.comps = nil
		for _, cand := range cands {
			if strings.HasPrefix(cand, word) {
				c.comps = append(c.comps, head+cand)
			}
		}
		if len(c.comps) == 0 {
			return
		}
		c.compIdx = -1
		if !forward {
			c.compIdx = 0
		}
	}
	if forward {
		c.compIdx = (c.compIdx + 1) % len(c.comps)
	} else {
		c.compIdx = (c.compIdx - 1 + len(c.comps)) % len(c.comps)
	}
	c.input.SetValue(c.comps[c.compIdx])
	c.input.CursorEnd()
}

func isModifierOnly(head string) bool {
	for _, w := range strings.Fields(head) {
		if _, ok := modifiers[w]; !ok {
			return false
		}
	}
	return true
}

// cmdView renders the command line (no background; the cursor is an
// underlined character), with completion candidates when cycling.
func (r *Root) cmdView() string {
	c := &r.cmd
	st := r.focus.st
	val := []rune(c.input.Value())
	pos := min(c.input.Position(), len(val))
	at, after := " ", ""
	if pos < len(val) {
		at, after = string(val[pos]), string(val[pos+1:])
	}
	line := st.accent.Render(":") + st.snippet.Render(string(val[:pos])) + st.accent.Underline(true).Render(at) + st.snippet.Render(after)
	if len(c.comps) > 1 && c.compIdx >= 0 {
		var opts []string
		in := parse(c.input.Value())
		isLink := in.name == "open-link" || in.name == "copy-link"
		links := r.focus.links()
		for i, cand := range c.comps {
			cand = cand[strings.LastIndexByte(cand, ' ')+1:]
			if isLink {
				if n, err := strconv.Atoi(cand); err == nil && n > 0 && n <= len(links) {
					cand = fmt.Sprintf("[%d] %s", n, truncate(links[n-1].url, 24))
				}
			}
			if i == c.compIdx {
				cand = st.key.Render(cand)
			} else {
				cand = st.muted.Render(cand)
			}
			opts = append(opts, cand)
		}
		line += "   " + strings.Join(opts, " ")
	}
	line = ansi.Truncate(line, r.width, "…")
	return line + strings.Repeat(" ", max(r.width-lipgloss.Width(line), 0))
}
