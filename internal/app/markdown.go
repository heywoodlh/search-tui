package app

import (
	"fmt"
	"strings"

	"github.com/heywoodlh-bot/tui-search/internal/engine"
)

// ResultsMarkdown renders a result set (and optional summary) as Markdown,
// suitable for viewing in $EDITOR or piping elsewhere.
func ResultsMarkdown(query, engineName string, kind engine.Kind, page int, rs []engine.Result, s Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Search: %s\n\n", query)
	fmt.Fprintf(&b, "_engine: %s · type: %s · page %d · %d results_\n\n", engineName, kind, page, len(rs))
	if !s.Empty() {
		b.WriteString("## Summary\n\n")
		if s.Text != "" {
			b.WriteString(s.Text + "\n\n")
		}
		for _, sn := range s.Sentences {
			fmt.Fprintf(&b, "- %s [%d]\n", sn.Text, sn.Source)
		}
		if len(s.Terms) > 0 {
			fmt.Fprintf(&b, "\n_Related: %s_\n", strings.Join(s.Terms, ", "))
		}
		b.WriteString("\n")
	}
	b.WriteString("## Results\n\n")
	for i, r := range rs {
		b.WriteString(ResultMarkdown(i+1, r))
		b.WriteString("\n")
	}
	return b.String()
}

// ResultMarkdown renders one result.
func ResultMarkdown(n int, r engine.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### %d. [%s](%s)\n\n", n, escapeMD(r.Title), r.URL)
	if r.Meta != "" {
		fmt.Fprintf(&b, "_%s_\n\n", r.Meta)
	}
	if r.Code != nil {
		for _, f := range r.Code.Fragments {
			if f.Line > 0 {
				fmt.Fprintf(&b, "Line %d:\n\n", f.Line)
			}
			fmt.Fprintf(&b, "```%s\n%s\n```\n\n", r.Code.Language, f.Text)
		}
		if len(r.Code.Fragments) == 0 && r.Snippet != "" {
			fmt.Fprintf(&b, "%s\n\n", r.Snippet)
		}
	} else if r.Snippet != "" {
		fmt.Fprintf(&b, "%s\n\n", r.Snippet)
	}
	return b.String()
}

// PageMarkdown renders a fetched page with its summary.
func PageMarkdown(p Page) string {
	var b strings.Builder
	title := p.Title
	if title == "" {
		title = p.URL
	}
	fmt.Fprintf(&b, "# %s\n\n<%s>\n\n", title, p.URL)
	if len(p.Summary) > 0 {
		b.WriteString("## Summary\n\n")
		for _, s := range p.Summary {
			fmt.Fprintf(&b, "- %s\n", s.Text)
		}
		b.WriteString("\n---\n\n")
	}
	b.WriteString(p.Markdown)
	b.WriteString("\n")
	return b.String()
}

func escapeMD(s string) string {
	return strings.NewReplacer("[", `\[`, "]", `\]`).Replace(s)
}
