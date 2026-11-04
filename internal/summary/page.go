package summary

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// ExtractPage converts an HTML page into readable Markdown, keeping links
// as [text](url) (relative links are resolved against pageURL; only
// http(s) links are kept) and dropping
// navigation, scripts and other boilerplate.
func ExtractPage(body, pageURL string) (title, markdown string) {
	base, _ := url.Parse(pageURL)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return "", body
	}
	title = strings.TrimSpace(doc.Find("title").First().Text())
	doc.Find("script,style,noscript,nav,header,footer,aside,form,svg,iframe,button,template,[role=navigation],[aria-hidden=true],.sidebar,.nav,.menu,.cookie,.advert,.ads").Remove()
	root := doc.Find("article").First()
	if root.Length() == 0 || len(strings.TrimSpace(root.Text())) < 200 {
		root = doc.Find("main, [role=main], #content, .content").First()
	}
	if root.Length() == 0 || len(strings.TrimSpace(root.Text())) < 200 {
		root = doc.Find("body")
	}
	var b strings.Builder
	const blocks = "h1,h2,h3,h4,h5,h6,p,li,pre,blockquote,dt,dd"
	root.Find(blocks).Each(func(_ int, s *goquery.Selection) {
		if s.ParentsFiltered("p,li,pre,blockquote,dd").Length() > 0 {
			return
		}
		tag := goquery.NodeName(s)
		if tag == "pre" {
			code := strings.TrimRight(s.Text(), "\n ")
			if code != "" {
				fmt.Fprintf(&b, "```\n%s\n```\n\n", code)
			}
			return
		}
		txt := inlineText(s, base)
		if txt == "" {
			return
		}
		switch tag {
		case "h1", "h2", "h3", "h4", "h5", "h6":
			fmt.Fprintf(&b, "%s %s\n\n", strings.Repeat("#", int(tag[1]-'0')), txt)
		case "li":
			fmt.Fprintf(&b, "- %s\n", txt)
		case "blockquote":
			fmt.Fprintf(&b, "> %s\n\n", txt)
		default:
			fmt.Fprintf(&b, "%s\n\n", txt)
		}
	})
	markdown = strings.TrimSpace(b.String())
	if markdown == "" {
		markdown = strings.Join(strings.Fields(root.Text()), " ")
	}
	return title, markdown
}

// inlineText renders text with inline code preserved as `code`.
func inlineText(s *goquery.Selection, base *url.URL) string {
	var out strings.Builder
	var walk func(n *html.Node, b *strings.Builder)
	walk = func(n *html.Node, b *strings.Builder) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
		case html.ElementNode:
			switch n.Data {
			case "a":
				if href := resolveLink(base, attr(n, "href")); href != "" {
					var inner strings.Builder
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						walk(c, &inner)
					}
					text := strings.Join(strings.Fields(strings.NewReplacer("[", "(", "]", ")").Replace(inner.String())), " ")
					if text != "" {
						fmt.Fprintf(b, "[%s](%s)", text, href)
					}
					return
				}
			case "code", "kbd":
				b.WriteString("`")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, b)
				}
				b.WriteString("`")
				return
			case "br":
				b.WriteString(" ")
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, b)
			}
		}
	}
	for _, n := range s.Nodes {
		walk(n, &out)
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

// External runs a user-configured summariser command (for example a local
// LLM via ollama or llama.cpp), passing the prompt on stdin.
func External(ctx context.Context, command, prompt string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/C", command) //nolint:gosec // G204: user-configured summary command
	} else {
		c = exec.CommandContext(ctx, "sh", "-c", command) //nolint:gosec // G204: user-configured summary command
	}
	c.Stdin = strings.NewReader(prompt)
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("summary command: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Prompt builds a compact prompt for an external summariser.
func Prompt(query string, docs []Doc) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Summarise the following search results for the query %q in 3-5 concise sentences. Cite sources as [n].\n\n", query)
	for _, d := range docs {
		fmt.Fprintf(&b, "[%d] %s\n", d.Source, d.Text)
	}
	return b.String()
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// resolveLink makes href absolute and keeps only http(s) links that point
// somewhere other than the page itself.
func resolveLink(base *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	u.Fragment = ""
	if strings.ContainsAny(u.String(), " ()") {
		return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29").Replace(u.String())
	}
	return u.String()
}

// MarkdownLinkRE matches inline Markdown links produced by ExtractPage.
var MarkdownLinkRE = regexp.MustCompile(`\[([^\]]*)\]\((https?://[^)\s]+)\)`)

// StripLinks replaces [text](url) with text.
func StripLinks(s string) string { return MarkdownLinkRE.ReplaceAllString(s, "$1") }
