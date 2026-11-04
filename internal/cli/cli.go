// Package cli implements the command line interface.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/heywoodlh/search-tui/internal/app"
	"github.com/heywoodlh/search-tui/internal/config"
	"github.com/heywoodlh/search-tui/internal/engine"
	"github.com/heywoodlh/search-tui/internal/safe"
	"github.com/heywoodlh/search-tui/internal/sys"
	"github.com/heywoodlh/search-tui/internal/theme"
	"github.com/heywoodlh/search-tui/internal/tui"
	flag "github.com/spf13/pflag"
)

// Version is set at build time from the VERSION file (-ldflags -X). For
// `go install module@vX.Y.Z` builds it falls back to the module version.
var Version = "dev"

func version() string {
	if Version != "dev" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return Version
}

type options struct {
	engine, typ, lang, repo, config string
	theme, browser, field           string
	listThemes                      bool
	code, jsonOut, md, open, lucky  bool
	printURL, tui, compose, edit    bool
	summary, noSummary, noColor     bool
	listEngines, listBangs, defCfg  bool
	version                         bool
	limit, page, result             int
}

// Main runs the CLI and returns the process exit code.
func Main(args []string) int {
	if err := run(args, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "search:", safe.Text(err.Error()))
		return 1
	}
	return 0
}

func run(args []string, stdout, stderr io.Writer) error {
	var o options
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVarP(&o.engine, "engine", "e", "", "engine name or bang (e.g. ddg, g, gh, sg)")
	fs.BoolVarP(&o.code, "code", "c", false, "code search (uses default_code_engine unless --engine supports code)")
	fs.StringVarP(&o.typ, "type", "t", "", "search type: web, code, repos, issues")
	fs.StringVarP(&o.lang, "lang", "l", "", "code language filter")
	fs.StringVarP(&o.repo, "repo", "r", "", "code repository filter (owner/name)")
	fs.IntVarP(&o.limit, "limit", "n", 0, "number of results")
	fs.IntVarP(&o.page, "page", "p", 1, "results page")
	fs.IntVar(&o.result, "result", 0, "select result N (1-based; works with --json/--markdown)")
	fs.StringVar(&o.field, "field", "", "print one result field: url, title, snippet, code (implies --result 1)")
	fs.BoolVarP(&o.jsonOut, "json", "j", false, "print results as JSON")
	fs.BoolVarP(&o.md, "markdown", "m", false, "print results as Markdown")
	fs.BoolVarP(&o.open, "open", "o", false, "open the search page in a browser")
	fs.BoolVarP(&o.lucky, "lucky", "1", false, "open the first result in a browser")
	fs.BoolVarP(&o.printURL, "url", "u", false, "print the engine's search URL and exit")
	fs.BoolVarP(&o.tui, "tui", "i", false, "open the TUI (with the query, if given)")
	fs.BoolVarP(&o.compose, "compose", "E", false, "compose the query in $EDITOR (temporary Markdown file)")
	fs.BoolVar(&o.edit, "edit", false, "open the results in $EDITOR as Markdown")
	fs.BoolVarP(&o.summary, "summary", "s", false, "print the on-device summary")
	fs.BoolVar(&o.noSummary, "no-summary", false, "do not print a summary")
	fs.BoolVar(&o.noColor, "no-color", false, "disable colours")
	fs.StringVar(&o.theme, "theme", "", "colour theme (see --list-themes)")
	fs.StringVar(&o.browser, "browser", "", "browser command for opening URLs (default: config, $BROWSER, then the system default)")
	fs.BoolVar(&o.listThemes, "list-themes", false, "list colour themes and exit")
	fs.StringVar(&o.config, "config", "", "config file (default "+config.DefaultPath()+")")
	fs.BoolVar(&o.listEngines, "list-engines", false, "list engines and exit")
	fs.BoolVar(&o.listBangs, "list-bangs", false, "list bangs and exit")
	fs.BoolVar(&o.defCfg, "example-config", false, "print an example config and exit")
	fs.BoolVarP(&o.version, "version", "v", false, "print version and exit")
	fs.Usage = func() {
		fmt.Fprintf(stderr, `search — a terminal search client

Usage:
  search                       open the TUI
  search [flags] <query...>    search inline (supports !bangs, e.g. '!gh bubbletea')

Examples:
  search "nixos flakes tutorial"
  search '!w alan turing'      quote !bangs: bash/zsh expand ! in history
  search --engine gh --code "tea.NewProgram" --lang go
  search -c -r charmbracelet/bubbletea WithMouseCellMotion
  search -E                    write the query in $EDITOR

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if o.result < 0 {
		return errors.New("--result must be a positive 1-based number")
	}
	if o.field != "" {
		switch o.field {
		case "url", "title", "snippet", "code":
		default:
			return fmt.Errorf("unknown --field %q (use url, title, snippet, code)", o.field)
		}
		if o.jsonOut || o.md || o.edit || o.open || o.lucky || o.tui {
			return errors.New("--field cannot be combined with --json, --markdown, --edit, --open, --lucky or --tui")
		}
	}
	if o.version {
		fmt.Fprintln(stdout, "search", version())
		return nil
	}
	if o.defCfg {
		fmt.Fprint(stdout, config.Example)
		return nil
	}
	if o.noColor || os.Getenv("NO_COLOR") != "" {
		lipgloss.SetColorProfile(0)
	}

	cfg, err := config.Load(o.config)
	if err != nil {
		return err
	}
	if o.theme != "" {
		cfg.UI.Theme = o.theme
	}
	if o.browser != "" {
		cfg.Browser = o.browser
	}
	a, err := app.New(cfg)
	if err != nil {
		return err
	}
	if !o.tui && len(fs.Args()) > 0 || o.listEngines {
		for _, w := range a.Warnings {
			fmt.Fprintln(stderr, "search: warning:", w)
		}
	}
	if o.listThemes {
		return listThemes(stdout, cfg.UI.Theme)
	}
	if o.listEngines {
		return listEngines(a, stdout)
	}
	if o.listBangs {
		tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
		for _, e := range a.Bangs.List() {
			fmt.Fprintf(tw, "%s\t%s\n", e.Bang, e.Target)
		}
		return tw.Flush()
	}

	kind := engine.Kind("")
	if o.typ != "" {
		if kind, err = engine.ParseKind(o.typ); err != nil {
			return err
		}
	}
	if o.code {
		kind = engine.KindCode
	}
	popts := app.PlanOptions{Engine: o.engine, Kind: kind, Page: o.page, Limit: o.limit, Lang: o.lang, Repo: o.repo}
	query := strings.Join(fs.Args(), " ")

	if o.compose {
		q, err := composeQuery(cfg, query)
		if err != nil {
			return err
		}
		if q == "" {
			return errors.New("empty query; cancelled")
		}
		query = q
	}
	if query == "" && !o.tui && !stdinIsTerminal() {
		b, _ := io.ReadAll(io.LimitReader(os.Stdin, 64*1024))
		query = strings.TrimSpace(string(b))
	}
	if o.tui || query == "" {
		tui.Version = version()
		return tui.Run(a, query, popts)
	}

	plan, err := a.Plan(query, popts)
	if err != nil {
		return err
	}
	if plan.OpenURL != "" {
		if o.printURL || o.jsonOut {
			fmt.Fprintln(stdout, plan.OpenURL)
			return nil
		}
		fmt.Fprintln(stderr, "opening", plan.OpenURL)
		return sys.OpenURL(cfg.Browser, plan.OpenURL)
	}
	if o.printURL {
		fmt.Fprintln(stdout, plan.Engine.SearchURL(plan.Query))
		return nil
	}
	if o.open {
		return sys.OpenURL(cfg.Browser, plan.Engine.SearchURL(plan.Query))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	// Bound the whole search: streaming engines (Sourcegraph) have no HTTP
	// client timeout and would otherwise wait forever on a stalled server.
	ctx, cancel := context.WithTimeout(ctx, cfg.TimeoutDuration()+10*time.Second)
	defer cancel()
	rs, err := a.Search(ctx, &plan)
	if err != nil {
		var ae *engine.AuthError
		if errors.As(err, &ae) {
			return fmt.Errorf("%w\n(search URL: %s)", err, plan.Engine.SearchURL(plan.Query))
		}
		return err
	}
	if plan.Notice != "" {
		fmt.Fprintln(stderr, "search:", plan.Notice)
	}
	if o.field != "" {
		n := o.result
		if n == 0 {
			n = 1
		}
		selected, err := selectResult(rs, n)
		if err != nil {
			return err
		}
		return printField(stdout, selected, o.field)
	}
	if o.result > 0 {
		selected, err := selectResult(rs, o.result)
		if err != nil {
			return err
		}
		rs = []engine.Result{selected}
	}
	if plan.Lucky || o.lucky {
		if len(rs) == 0 {
			return errors.New("no results")
		}
		return sys.OpenURL(cfg.Browser, rs[0].URL)
	}

	var sum app.Summary
	wantSummary := !o.noSummary && (o.summary || cfg.SummaryEnabled())
	showSummary := wantSummary && (o.summary || stdoutIsTerminal())
	if wantSummary && (showSummary || o.md || o.edit || o.jsonOut) {
		sum = a.Summarize(plan.Query.Text, rs)
		if cfg.Summary.Command != "" && !o.jsonOut {
			if t, err := a.SummarizeExternal(ctx, plan.Query.Text, rs); err != nil {
				fmt.Fprintln(stderr, "search: warning:", err)
			} else {
				sum.Text = t
			}
		}
	}

	switch {
	case o.jsonOut:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		out := struct {
			Query   string          `json:"query"`
			Engine  string          `json:"engine"`
			Type    engine.Kind     `json:"type"`
			Page    int             `json:"page"`
			Results []engine.Result `json:"results"`
			Summary []string        `json:"summary,omitempty"`
		}{plan.Query.Text, plan.Engine.Name(), plan.Query.Kind, plan.Query.Page, rs, nil}
		if o.summary {
			for _, s := range sum.Sentences {
				out.Summary = append(out.Summary, s.Text)
			}
		}
		return enc.Encode(out)
	case o.md || o.edit:
		md := app.ResultsMarkdown(plan.Query.Text, plan.Engine.Name(), plan.Query.Kind, plan.Query.Page, rs, sum)
		if o.md {
			fmt.Fprint(stdout, md)
			return nil
		}
		return viewInEditor(cfg, "results.md", md, 0)
	}
	printResults(stdout, a.Theme, plan, rs, sum, showSummary)
	return nil
}

func selectResult(rs []engine.Result, n int) (engine.Result, error) {
	if n < 1 || n > len(rs) {
		return engine.Result{}, fmt.Errorf("result %d out of range (got %d results)", n, len(rs))
	}
	return rs[n-1], nil
}

func printField(w io.Writer, r engine.Result, field string) error {
	var value string
	switch field {
	case "url":
		value = r.URL
	case "title":
		value = r.Title
	case "snippet":
		value = r.Snippet
	case "code":
		if r.Code == nil {
			return errors.New("selected result has no code")
		}
		value = app.SnippetText(r)
	}
	_, err := fmt.Fprintln(w, safe.Text(value))
	return err
}

func listEngines(a *app.App, w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ENGINE\tBANGS\tTYPES\tAUTH\tDESCRIPTION")
	for _, n := range a.Reg.Names() {
		info := a.Reg.Info(n)
		ks := make([]string, len(info.Kinds))
		for i, k := range info.Kinds {
			ks[i] = string(k)
		}
		auth := info.Auth
		if auth == "" {
			auth = "-"
		}
		if info.BrowserOnly {
			ks = []string{"browser"}
		}
		def := ""
		if n == a.Cfg.DefaultEngine {
			def = " (default)"
		}
		fmt.Fprintf(tw, "%s%s\t!%s\t%s\t%s\t%s\n", n, def, strings.Join(info.Bangs, " !"), strings.Join(ks, ","), auth, info.Description)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if un := a.Reg.Unconfigured(); len(un) > 0 {
		fmt.Fprintln(w, "\nHidden until configured (their bangs open the site in a browser):")
		for _, u := range un {
			fmt.Fprintf(w, "  %-12s set [engines.%s] %s\n", u.Name, u.Name, u.Missing)
		}
	}
	return nil
}

func composeQuery(cfg *config.Config, initial string) (string, error) {
	p, cleanup, err := sys.TempFile("query.md", sys.ComposeTemplate(initial))
	if err != nil {
		return "", err
	}
	defer cleanup()
	c := sys.EditorCmd(cfg.Editor, p, 1)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("editor: %w", err)
	}
	b, err := os.ReadFile(p) //nolint:gosec // G304: p is the temp file we just created
	if err != nil {
		return "", err
	}
	return sys.ParseComposed(string(b)), nil
}

func viewInEditor(cfg *config.Config, name, content string, line int) error {
	p, cleanup, err := sys.TempFile(name, content)
	if err != nil {
		return err
	}
	defer cleanup()
	c := sys.EditorCmd(cfg.Editor, p, line)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

func stdoutIsTerminal() bool { return term.IsTerminal(os.Stdout.Fd()) }
func stdinIsTerminal() bool  { return term.IsTerminal(os.Stdin.Fd()) }

func termWidth() int {
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		return w
	}
	return 80
}

func printResults(w io.Writer, th theme.Theme, plan app.Plan, rs []engine.Result, sum app.Summary, showSummary bool) {
	width := termWidth()
	accent := theme.Fg(th.Accent).Bold(true)
	title := theme.Fg(th.Title).Bold(true)
	link := theme.Fg(th.URL)
	muted := theme.Fg(th.Muted)
	code := theme.Fg(th.Code)
	wrap := theme.Fg(th.Text).Width(max(width-4, 20))

	if len(rs) == 0 {
		fmt.Fprintln(w, muted.Render("No results."))
		return
	}
	if showSummary && !sum.Empty() {
		fmt.Fprintln(w, accent.Render("Summary"))
		if sum.Text != "" {
			fmt.Fprintln(w, indent(wrap.Render(sum.Text), "  "))
		}
		for _, s := range sum.Sentences {
			fmt.Fprintln(w, indent(wrap.Render(fmt.Sprintf("• %s %s", s.Text, muted.Render(fmt.Sprintf("[%d]", s.Source)))), "  "))
		}
		if len(sum.Terms) > 0 {
			fmt.Fprintln(w, indent(muted.Render("related: "+strings.Join(sum.Terms, ", ")), "  "))
		}
		fmt.Fprintln(w)
	}
	for i, r := range rs {
		fmt.Fprintf(w, "%s %s\n", accent.Render(fmt.Sprintf("%2d.", i+1)), title.Render(r.Title))
		fmt.Fprintf(w, "    %s\n", link.Render(r.URL))
		if r.Meta != "" {
			fmt.Fprintf(w, "    %s\n", muted.Render(r.Meta))
		}
		if r.Code != nil && len(r.Code.Fragments) > 0 {
			for _, f := range r.Code.Fragments[:min(2, len(r.Code.Fragments))] {
				f = engine.Focus(f, plan.Query.Text, 0)
				for j, l := range strings.Split(f.Text, "\n") {
					if j >= 6 {
						fmt.Fprintf(w, "    %s\n", muted.Render("…"))
						break
					}
					num := "    "
					if f.Line > 0 {
						num = fmt.Sprintf("%4d", f.Line+j)
					}
					fmt.Fprintf(w, "    %s %s\n", muted.Render(num+" │"), code.Render(truncate(strings.ReplaceAll(l, "\t", "    "), width-14)))
				}
			}
		} else if r.Snippet != "" {
			fmt.Fprintln(w, indent(wrap.Render(r.Snippet), "    "))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, muted.Render(fmt.Sprintf("%s · %s · page %d · %s", plan.Engine.Name(), plan.Query.Kind, plan.Query.Page, plan.Engine.SearchURL(plan.Query))))
}

func indent(s, pre string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pre + strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

func truncate(s string, n int) string {
	if n < 4 {
		n = 4
	}
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// listThemes prints every theme with a colour preview.
func listThemes(w io.Writer, current string) error {
	cur, _ := theme.Get(current)
	for _, n := range theme.Names() {
		t, _ := theme.Get(n)
		mark := "  "
		if n == cur.Name {
			mark = "* "
		}
		preview := theme.Fg(t.Accent).Bold(true).Render("accent") + " " +
			theme.Fg(t.Title).Bold(true).Render("title") + " " +
			theme.Fg(t.URL).Render("url") + " " +
			theme.Fg(t.Code).Render("code") + " " +
			theme.Fg(t.Muted).Render("muted") + " " +
			theme.Fg(t.Error).Render("error")
		fmt.Fprintf(w, "%s%-22s %s\n", mark, n, preview)
	}
	return nil
}
