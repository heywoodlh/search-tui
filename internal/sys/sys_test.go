package sys

import (
	"path/filepath"
	"runtime"
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
	if strings.Join(c.Args, " ") != "nvim --cmd set nomodeline +12 /tmp/x.go" {
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
	// $BROWSER is a PATH-style list: ":" separated, ";" on Windows.
	sep, cmd := ":", "sh"
	if runtime.GOOS == "windows" {
		sep, cmd = ";", "cmd"
	}
	t.Setenv("BROWSER", "nonexistent-browser-xyz"+sep+cmd)
	if got := BrowserCommand("", "https://x"); strings.Join(got, " ") != cmd+" https://x" {
		t.Errorf("$BROWSER list: %q", got)
	}
	if got := BrowserCommand("default", "https://x"); got[len(got)-1] != "https://x" || got[0] == cmd {
		t.Errorf("default should ignore $BROWSER: %q", got)
	}
	t.Setenv("BROWSER", "")
	if got := BrowserCommand("", "https://x"); got[0] == "" || got[len(got)-1] != "https://x" {
		t.Errorf("system default: %q", got)
	}
}

func TestSafeFileName(t *testing.T) {
	cases := map[string]string{
		"main.go":                "main.go",
		"":                       "search.md",
		"..":                     "search.md",
		".":                      "search.md",
		"../../etc/passwd":       "passwd",
		"..\\..\\win.ini":        "win.ini",
		"-c:!rm -rf ~":           "c__rm_-rf__",
		".bashrc":                "bashrc",
		"a\x1b]52;b.go":          "a__52_b.go",
		"résumé.md":              "r_sum_.md",
		strings.Repeat("x", 200): strings.Repeat("x", 80),
	}
	for in, want := range cases {
		got := SafeFileName(in)
		if strings.ContainsAny(got, "/\\!\x1b ") || strings.HasPrefix(got, ".") || strings.HasPrefix(got, "-") {
			t.Errorf("SafeFileName(%q) = %q is unsafe", in, got)
		}
		if got != want {
			t.Errorf("SafeFileName(%q) = %q, want %q", in, got, want)
		}
	}
	p, cleanup, err := TempFile("../../escape.go", "x")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if filepath.Base(filepath.Dir(p)) == "" || !strings.Contains(filepath.Dir(p), "tui-search-") || filepath.Base(p) != "escape.go" {
		t.Errorf("temp file escaped its directory: %s", p)
	}
}

func TestEditorDisablesModelines(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "vim")
	c := EditorCmd("", "/tmp/x.go", 3)
	if strings.Join(c.Args, " ") != "vim --cmd set nomodeline +3 /tmp/x.go" {
		t.Errorf("args = %q", c.Args)
	}
	c = EditorCmd("emacs -nw", "/tmp/x.go", 0)
	if !strings.Contains(strings.Join(c.Args, " "), "enable-local-variables nil") {
		t.Errorf("emacs args = %q", c.Args)
	}
}

func TestOpenURLRejectsUnsafeSchemes(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "smb://evil/share", "https://x/\"&calc"} {
		if err := OpenURL("true", u); err == nil {
			t.Errorf("OpenURL(%q) should be refused", u)
		}
	}
}
