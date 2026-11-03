// Package sys wraps OS integrations: clipboard, browser and editor.
package sys

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	osc52 "github.com/aymanbagabas/go-osc52/v2"
)

// Copy places text on the clipboard. mode is "auto", "osc52", "command" or
// "none"; command is an optional custom copy command reading stdin.
// It returns a short description of the method used.
func Copy(text, mode, command string) (string, error) {
	switch mode {
	case "none", "off":
		return "", errors.New("clipboard disabled")
	case "osc52":
		return "osc52", copyOSC52(text)
	}
	if command != "" {
		return command, pipeTo(SplitCommand(command), text)
	}
	if args := clipboardCommand(); args != nil {
		if err := pipeTo(args, text); err == nil {
			// Over SSH the local tool may be pointless; also emit OSC52.
			if os.Getenv("SSH_TTY") != "" && mode == "auto" {
				_ = copyOSC52(text)
			}
			return args[0], nil
		}
	}
	if mode == "command" {
		return "", errors.New("no clipboard command found (install wl-copy, xclip, xsel or set clipboard_command)")
	}
	return "osc52", copyOSC52(text)
}

func clipboardCommand() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"pbcopy"}
	case "windows":
		return []string{"clip"}
	}
	cands := [][]string{}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		cands = append(cands, []string{"wl-copy"})
	}
	if os.Getenv("DISPLAY") != "" {
		cands = append(cands, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
	}
	if os.Getenv("TERMUX_VERSION") != "" || os.Getenv("PREFIX") != "" && strings.Contains(os.Getenv("PREFIX"), "com.termux") {
		cands = append(cands, []string{"termux-clipboard-set"})
	}
	for _, c := range cands {
		if _, err := exec.LookPath(c[0]); err == nil {
			return c
		}
	}
	return nil
}

func pipeTo(args []string, text string) error {
	if len(args) == 0 {
		return errors.New("empty command")
	}
	c := exec.Command(args[0], args[1:]...)
	c.Stdin = strings.NewReader(text)
	return c.Run()
}

func copyOSC52(text string) error {
	seq := osc52.New(text)
	if os.Getenv("TMUX") != "" {
		seq = seq.Tmux()
	} else if strings.HasPrefix(os.Getenv("TERM"), "screen") {
		seq = seq.Screen()
	}
	out := io.Writer(os.Stderr)
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		defer tty.Close()
		out = tty
	}
	_, err := seq.WriteTo(out)
	return err
}

// BrowserCommand returns the command used to open a URL.
func BrowserCommand(configured, url string) []string {
	if configured == "" {
		configured = os.Getenv("BROWSER")
	}
	if configured != "" {
		args := SplitCommand(configured)
		replaced := false
		for i, a := range args {
			if strings.Contains(a, "%s") || strings.Contains(a, "{url}") {
				args[i] = strings.NewReplacer("%s", url, "{url}", url).Replace(a)
				replaced = true
			}
		}
		if !replaced {
			args = append(args, url)
		}
		return args
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{"open", url}
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", url}
	}
	if _, err := exec.LookPath("termux-open-url"); err == nil {
		return []string{"termux-open-url", url}
	}
	if _, err := exec.LookPath("wslview"); err == nil && os.Getenv("WSL_DISTRO_NAME") != "" {
		return []string{"wslview", url}
	}
	return []string{"xdg-open", url}
}

// IsTerminalBrowser reports whether the browser command is a terminal
// program (w3m, lynx...) that must take over the TTY.
func IsTerminalBrowser(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch filepath.Base(args[0]) {
	case "w3m", "lynx", "links", "elinks", "browsh", "carbonyl", "chawan", "cha":
		return true
	}
	return false
}

// OpenURL launches a graphical browser in the background.
func OpenURL(configured, url string) error {
	args := BrowserCommand(configured, url)
	c := exec.Command(args[0], args[1:]...)
	c.Stdout, c.Stderr = nil, nil
	if err := c.Start(); err != nil {
		return fmt.Errorf("open browser (%s): %w", args[0], err)
	}
	go func() { _ = c.Wait() }()
	return nil
}

// Editor returns the editor command (config > $VISUAL > $EDITOR > default).
func Editor(configured string) []string {
	for _, e := range []string{configured, os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
		if strings.TrimSpace(e) != "" {
			return SplitCommand(e)
		}
	}
	if runtime.GOOS == "windows" {
		return []string{"notepad"}
	}
	for _, e := range []string{"nvim", "vim", "vi", "nano"} {
		if _, err := exec.LookPath(e); err == nil {
			return []string{e}
		}
	}
	return []string{"vi"}
}

// EditorCmd builds the command to open file (optionally at line).
func EditorCmd(configured, file string, line int) *exec.Cmd {
	args := Editor(configured)
	name := strings.TrimSuffix(filepath.Base(args[0]), ".exe")
	if line > 0 {
		switch name {
		case "vi", "vim", "nvim", "nano", "emacs", "emacsclient", "micro", "kak", "mg", "joe", "ne", "pico", "gvim", "mvim":
			args = append(args, fmt.Sprintf("+%d", line), file)
		case "hx", "helix", "subl", "sublime_text", "zed":
			args = append(args, fmt.Sprintf("%s:%d", file, line))
		case "code", "codium", "code-insiders", "cursor":
			args = append(args, "-g", fmt.Sprintf("%s:%d", file, line))
		default:
			args = append(args, file)
		}
	} else {
		args = append(args, file)
	}
	c := exec.Command(args[0], args[1:]...)
	return c
}

// SplitCommand splits a command line honouring simple quotes.
func SplitCommand(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	has := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			has = true
		case r == ' ' || r == '\t':
			if cur.Len() > 0 || has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 || has {
		out = append(out, cur.String())
	}
	return out
}

// TempFile writes content to a new temporary file whose name ends in name
// (so editors pick the right syntax). The returned cleanup removes it.
func TempFile(name, content string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "tui-search-")
	if err != nil {
		return "", nil, err
	}
	if name == "" {
		name = "search.md"
	}
	p := filepath.Join(dir, filepath.Base(name))
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}
	return p, func() { os.RemoveAll(dir) }, nil
}

// ParseComposed extracts a query from a composed markdown file: comment
// blocks ("<!-- ... -->") are ignored and remaining lines
// are joined with spaces.
func ParseComposed(content string) string {
	var parts []string
	inComment := false
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if inComment {
			if strings.Contains(t, "-->") {
				inComment = false
			}
			continue
		}
		if strings.HasPrefix(t, "<!--") {
			if !strings.Contains(t, "-->") {
				inComment = true
			}
			continue
		}
		if t == "" {
			continue
		}
		parts = append(parts, t)
	}
	return strings.Join(parts, " ")
}

// ComposeTemplate is the initial content of a query composition file.
func ComposeTemplate(current string) string {
	return current + "\n\n<!--\nWrite your search query above. Lines are joined with spaces.\nUse !bangs anywhere (e.g. !gh, !ghc, !w). Save and quit to search;\nan empty file cancels.\n-->\n"
}
