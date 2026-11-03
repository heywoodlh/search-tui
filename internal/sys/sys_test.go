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

func TestTerminalURL(t *testing.T) {
	seq := OpenURLSequence("https://example.com/a;b", false)
	if seq != "\x1b]1337;OpenURL=:aHR0cHM6Ly9leGFtcGxlLmNvbS9hO2I=\a" {
		t.Errorf("sequence = %q", seq)
	}
	if got := OpenURLSequence("https://x", true); !strings.HasPrefix(got, "\x1bPtmux;\x1b\x1b]1337") || !strings.HasSuffix(got, "\x1b\\") {
		t.Errorf("tmux wrap = %q", got)
	}
	for _, env := range []string{"BROWSER", "LC_TERMINAL", "TERM_PROGRAM", "SSH_CONNECTION", "SSH_TTY", "DISPLAY", "WAYLAND_DISPLAY", "TERMUX_VERSION"} {
		t.Setenv(env, "")
	}
	if !UsesTerminalURL("rootshell") || UsesTerminalURL("firefox") {
		t.Error("explicit selection")
	}
	t.Setenv("LC_TERMINAL", "rootshell")
	t.Setenv("SSH_CONNECTION", "1.2.3.4 5 6.7.8.9 22")
	if !UsesTerminalURL("") {
		t.Error("rootshell over SSH should use the terminal")
	}
	if UsesTerminalURL("default") {
		t.Error("an explicit browser setting must win")
	}
	t.Setenv("BROWSER", "w3m")
	if UsesTerminalURL("") {
		t.Error("$BROWSER should win over auto-detection")
	}
	t.Setenv("BROWSER", "")
	t.Setenv("LC_TERMINAL", "")
	if UsesTerminalURL("") {
		t.Error("unknown terminal should not use OSC 1337")
	}
	if err := OpenURLInTerminal("javascript:alert(1)"); err == nil {
		t.Error("non-http URL accepted")
	}
}
