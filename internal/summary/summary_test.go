package summary

import (
	"strings"
	"testing"
)

func TestSplitSentences(t *testing.T) {
	got := SplitSentences("Go is fast. It was made at Google, e.g. by Rob Pike. Version 1.22 is out! Is it good?")
	want := []string{"Go is fast.", "It was made at Google, e.g. by Rob Pike.", "Version 1.22 is out!", "Is it good?"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q", got)
	}
}

func TestSummarizePrefersConsensus(t *testing.T) {
	docs := []Doc{
		{Source: 1, Text: "Kubernetes is an open source system for automating deployment, scaling and management of containerized applications."},
		{Source: 2, Text: "Kubernetes automates deployment and scaling of containerized applications across clusters of machines."},
		{Source: 3, Text: "Buy cheap shoes online today with free shipping on every order."},
		{Source: 4, Text: "Containerized applications are managed by Kubernetes, which handles scaling and deployment."},
	}
	s := Summarize(docs, "what is kubernetes", 2)
	if len(s) != 2 {
		t.Fatalf("want 2 sentences, got %d", len(s))
	}
	for _, x := range s {
		if x.Source == 3 {
			t.Errorf("off-topic sentence selected: %q", x.Text)
		}
	}
	if s[0].Text == s[1].Text {
		t.Error("duplicate sentences")
	}
	terms := KeyTerms(docs, "what is kubernetes", 3)
	if len(terms) == 0 || terms[0] == "Kubernetes" {
		t.Errorf("key terms = %v", terms)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	if Summarize(nil, "q", 3) != nil || Summarize([]Doc{{Text: "short"}}, "q", 3) != nil {
		t.Error("expected nil summary")
	}
}

func TestExtractPage(t *testing.T) {
	html := `<html><head><title>T</title><script>var x=1</script></head><body><nav>Menu</nav>
<article><h1>Heading</h1><p>First paragraph with <code>code</code> inside it and plenty of words to qualify as the main content of this page.</p>
<ul><li>item one</li><li>item two</li></ul><pre>fmt.Println("hi")</pre>
<p>Second paragraph that explains more about the topic at hand in some detail so that the article is long enough.</p></article>
<footer>Footer</footer></body></html>`
	title, md := ExtractPage(html, "https://example.com/docs/page")
	if title != "T" {
		t.Errorf("title = %q", title)
	}
	for _, want := range []string{"# Heading", "`code`", "- item one", "```\nfmt.Println(\"hi\")\n```"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
	for _, bad := range []string{"Menu", "Footer", "var x"} {
		if strings.Contains(md, bad) {
			t.Errorf("boilerplate %q leaked:\n%s", bad, md)
		}
	}
}

func TestExtractPageLinks(t *testing.T) {
	html := `<html><body><article><p>See the <a href="/guide">user guide</a>, <a href="https://other.example/x#frag">another site</a>,
<a href="#top">a fragment</a>, <a href="mailto:a@b.c">mail</a> and <a href="javascript:alert(1)">evil</a> for details on everything here.</p>
<p>More words so the article is long enough to be picked as the main content of this test page.</p></article></body></html>`
	_, md := ExtractPage(html, "https://example.com/docs/page")
	for _, want := range []string{"[user guide](https://example.com/guide)", "[another site](https://other.example/x)", "a fragment", "mail", "evil"} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in %s", want, md)
		}
	}
	for _, bad := range []string{"javascript:", "mailto:", "#top"} {
		if strings.Contains(md, bad) {
			t.Errorf("unsafe or useless link %q kept: %s", bad, md)
		}
	}
	if got := StripLinks("a [b](https://x.y/z) c"); got != "a b c" {
		t.Errorf("StripLinks = %q", got)
	}
}
