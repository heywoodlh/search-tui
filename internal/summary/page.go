package summary

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// ExtractPage converts an HTML page into readable Markdown, dropping
// navigation, scripts and other boilerplate.
func ExtractPage(body string) (title, markdown string) {
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
		txt := inlineText(s)
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
func inlineText(s *goquery.Selection) string {
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
		case html.ElementNode:
			if n.Data == "code" || n.Data == "kbd" {
				b.WriteString("`")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
				b.WriteString("`")
				return
			}
			if n.Data == "br" {
				b.WriteString(" ")
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
	}
	for _, n := range s.Nodes {
		walk(n)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// External runs a user-configured summariser command (for example a local
// LLM via ollama or llama.cpp), passing the prompt on stdin.
func External(ctx context.Context, command, prompt string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		c = exec.CommandContext(ctx, "sh", "-c", command)
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
