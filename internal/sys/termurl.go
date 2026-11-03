package sys

import (
	"encoding/base64"
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
)

// Terminal URL opening: instead of launching a browser on this machine, ask
// the terminal emulator to open the link on the device it runs on, using
// iTerm2's OSC 1337 OpenURL sequence. rootshell (iPhone/iPad/Mac) and iTerm2
// support it, so `search` running over SSH, or as WASM inside rootshell,
// opens links in the iPad's default browser.
//
// rootshell requires Settings → Terminal → "Open Links from Programs".

// terminalBrowserNames are browser values that select this mechanism.
var terminalBrowserNames = map[string]bool{
	"terminal": true, "osc": true, "osc1337": true, "rootshell": true, "iterm": true, "iterm2": true,
}

// UsesTerminalURL reports whether links should be opened via the terminal.
// It is used when configured (or $BROWSER) names the mechanism, or
// automatically when no browser is configured, the terminal is rootshell or
// iTerm2, and this process cannot open a local browser itself (an SSH
// session without a desktop, or a WASI sandbox).
func UsesTerminalURL(configured string) bool {
	c := strings.ToLower(strings.TrimSpace(configured))
	if terminalBrowserNames[c] {
		return true
	}
	if c != "" {
		return false
	}
	if b := strings.ToLower(os.Getenv("BROWSER")); terminalBrowserNames[b] {
		return true
	}
	if os.Getenv("BROWSER") != "" {
		return false
	}
	return supportsOpenURL() && !canOpenLocally()
}

// supportsOpenURL detects terminals known to implement OSC 1337 OpenURL.
// rootshell sets LC_TERMINAL=rootshell; iTerm2 sets LC_TERMINAL=iTerm2 (LC_*
// variables are forwarded by default OpenSSH configurations).
func supportsOpenURL() bool {
	switch strings.ToLower(os.Getenv("LC_TERMINAL")) {
	case "rootshell", "iterm2":
		return true
	}
	return os.Getenv("TERM_PROGRAM") == "iTerm.app"
}

// canOpenLocally reports whether a local browser could plausibly be shown.
func canOpenLocally() bool {
	if runtime.GOOS == "wasip1" || runtime.GOOS == "js" {
		return false
	}
	remote := os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != ""
	switch runtime.GOOS {
	case "darwin", "windows":
		return !remote
	}
	if os.Getenv("TERMUX_VERSION") != "" {
		return true
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// OpenURLSequence builds the escape sequence asking the terminal to open
// url, wrapped for tmux passthrough when inside tmux (requires
// `set -g allow-passthrough on`).
func OpenURLSequence(url string, tmux bool) string {
	seq := "\x1b]1337;OpenURL=:" + base64.StdEncoding.EncodeToString([]byte(url)) + "\a"
	if tmux {
		seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return seq
}

// OpenURLInTerminal writes the OpenURL request to the controlling terminal.
func OpenURLInTerminal(url string) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return errors.New("only http(s) links can be opened via the terminal")
	}
	var out io.Writer = os.Stdout
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		defer tty.Close()
		out = tty
	}
	_, err := io.WriteString(out, OpenURLSequence(url, os.Getenv("TMUX") != ""))
	return err
}
