package sys

import (
	"strings"
	"testing"
)

func TestSplitCommand(t *testing.T) {
	got := SplitCommand(`code -w "my file" ''`)
	want := []string{"code", "-w", "my file", ""}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q", got)
	}
}

func TestParseComposed(t *testing.T) {
	in := "nix flakes\n  !gh\n\n<!--\ncomment\n-->\n<!-- one line -->\n"
	if got := ParseComposed(in); got != "nix flakes !gh" {
		t.Errorf("got %q", got)
	}
	if ParseComposed(ComposeTemplate("")) != "" {
		t.Error("template alone should be empty")
	}
}

func TestEditorCmdLine(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "nvim")
	c := EditorCmd("", "/tmp/x.go", 12)
	if strings.Join(c.Args, " ") != "nvim +12 /tmp/x.go" {
		t.Errorf("args = %q", c.Args)
	}
	c = EditorCmd("code --wait", "/tmp/x.go", 3)
	if strings.Join(c.Args, " ") != "code --wait -g /tmp/x.go:3" {
		t.Errorf("args = %q", c.Args)
	}
}

func TestBrowserCommand(t *testing.T) {
	if got := BrowserCommand("firefox --new-tab %s", "https://x"); strings.Join(got, " ") != "firefox --new-tab https://x" {
		t.Errorf("got %q", got)
	}
	if !IsTerminalBrowser([]string{"/usr/bin/w3m"}) {
		t.Error("w3m is a terminal browser")
	}
}

func TestBrowserPrecedence(t *testing.T) {
	t.Setenv("BROWSER", "nonexistent-browser-xyz:sh")
	if got := BrowserCommand("", "https://x"); strings.Join(got, " ") != "sh https://x" {
		t.Errorf("$BROWSER list: %q", got)
	}
	if got := BrowserCommand("default", "https://x"); got[len(got)-1] != "https://x" || got[0] == "sh" {
		t.Errorf("default should ignore $BROWSER: %q", got)
	}
	t.Setenv("BROWSER", "")
	if got := BrowserCommand("", "https://x"); got[0] == "" || got[len(got)-1] != "https://x" {
		t.Errorf("system default: %q", got)
	}
}
