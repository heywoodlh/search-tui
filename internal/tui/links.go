package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/heywoodlh/tui-search/internal/safe"
	"github.com/heywoodlh/tui-search/internal/summary"
)

// Numbered links, like Markdown reference links: results are [1]..[N] and,
// when the selected result's page has been fetched, the links on that page
// continue the numbering ([N+1]...). :open-link N and :copy-link N use the
// same numbers.

type link struct {
	n     int
	url   string
	label string
}

// links lists every numbered link in the pane.
func (m *Model) links() []link {
	out := make([]link, 0, len(m.results))
	for i, r := range m.results {
		out = append(out, link{n: i + 1, url: r.URL, label: r.Title})
	}
	if r, ok := m.current(); ok {
		if p, ok := m.pages[r.URL]; ok {
			seen := map[string]bool{}
			for _, mt := range summary.MarkdownLinkRE.FindAllStringSubmatch(p.Markdown, -1) {
				if seen[mt[2]] {
					continue
				}
				seen[mt[2]] = true
				out = append(out, link{n: len(out) + 1, url: mt[2], label: mt[1]})
			}
		}
	}
	return out
}

// pageLinkNumbers maps the current page's link URLs to their numbers.
func (m *Model) pageLinkNumbers() map[string]int {
	nums := map[string]int{}
	for _, l := range m.links()[len(m.results):] {
		nums[l.url] = l.n
	}
	return nums
}

// renderPageLinks turns [text](url) into "text[n]" for display.
func (m *Model) renderPageLinks(md string, nums map[string]int) string {
	return summary.MarkdownLinkRE.ReplaceAllStringFunc(md, func(s string) string {
		mt := summary.MarkdownLinkRE.FindStringSubmatch(s)
		if n, ok := nums[mt[2]]; ok {
			return fmt.Sprintf("%s[%d]", mt[1], n)
		}
		return mt[1]
	})
}

// resolveLink turns ":open-link" arguments into a URL: a link number, or a
// URL (a scheme-less "example.com/x" gets https://).
func (m *Model) resolveLink(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", errCmd("E471: Argument required (a link number or URL; press tab to list links)")
	}
	if n, err := strconv.Atoi(strings.Trim(arg, "[]")); err == nil {
		for _, l := range m.links() {
			if l.n == n {
				return l.url, nil
			}
		}
		return "", fmt.Errorf("no link [%d] in this pane", n)
	}
	if !strings.Contains(arg, "://") && strings.Contains(arg, ".") && !strings.Contains(arg, ":") {
		arg = "https://" + arg
	}
	if err := safe.WebURL(arg); err != nil {
		return "", err
	}
	return arg, nil
}
