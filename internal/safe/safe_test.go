package safe

import (
	"strings"
	"testing"
)

func TestTextStripsEscapes(t *testing.T) {
	cases := map[string]string{
		"plain text":                                   "plain text",
		"keep\nlines\tand tabs":                        "keep\nlines\tand tabs",
		"clip\x1b]52;c;ZXZpbA==\x07board":              "clip]52;c;ZXZpbA==board",
		"title\x1b]0;pwned\x1b\\":                      "title]0;pwned\\",
		"csi\x1b[2J\x1b[Hclear":                        "csi[2J[Hclear",
		"c1\u009b31mred\u009d0;x\u009c":                "c131mred0;x",
		"cr\roverwrite":                                "croverwrite",
		"bell\a del\x7f":                               "bell del",
		"bidi \u202eesrever\u202c and \u2066iso\u2069": "bidi esrever and iso",
		"bad utf8 \xff\xfe":                            "bad utf8 \uFFFD",
	}
	for in, want := range cases {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLine(t *testing.T) {
	if got := Line("a\nb\tc\x1b[0m"); got != "a b c[0m" {
		t.Errorf("got %q", got)
	}
}

func TestWebURL(t *testing.T) {
	for _, ok := range []string{"https://example.com", "http://example.com/a?b=c&d=e#f", "https://[::1]:8080/x"} {
		if err := WebURL(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"", "javascript:alert(1)", "file:///etc/passwd", "smb://host/share", "ms-msdt:/id",
		"search-ms:query=x", "https:example.com", "https://", "//example.com/x",
		"https://example.com/\x1b]52", "https://example.com/a b", "https://example.com/\"&calc",
		"vbscript:x", "data:text/html,x", "HTTPS://example.com\n",
	} {
		if WebURL(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestRedactURL(t *testing.T) {
	got := RedactURL("https://user:pass@www.googleapis.com/customsearch/v1?key=SECRET&cx=abc&q=hello&token=T")
	for _, leak := range []string{"SECRET", "pass", "token=T"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
	if !strings.Contains(got, "q=hello") || !strings.Contains(got, "cx=abc") {
		t.Errorf("over-redacted: %q", got)
	}
}
