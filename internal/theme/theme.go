// Package theme defines foreground colour palettes. Themes never set a
// background colour: the terminal's own background always shows through.
package theme

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme is a foreground palette. A nil colour means "terminal default".
type Theme struct {
	Name    string
	Accent  lipgloss.TerminalColor // selection, headings, engine label
	Title   lipgloss.TerminalColor // result titles
	Text    lipgloss.TerminalColor // snippets and body text
	URL     lipgloss.TerminalColor
	Muted   lipgloss.TerminalColor // metadata, hints, line numbers
	Border  lipgloss.TerminalColor // rules and separators
	Code    lipgloss.TerminalColor
	Key     lipgloss.TerminalColor // key names in hints
	Error   lipgloss.TerminalColor
	Success lipgloss.TerminalColor
	Insert  lipgloss.TerminalColor // mode indicators
	Normal  lipgloss.TerminalColor
	View    lipgloss.TerminalColor
}

type c = lipgloss.Color

// palette builds a theme from hex colours.
func palette(name, accent, title, text, url, muted, border, code, key, err, ok, ins, norm, view string) Theme {
	col := func(s string) lipgloss.TerminalColor {
		if s == "" {
			return nil
		}
		return c(s)
	}
	return Theme{name, col(accent), col(title), col(text), col(url), col(muted), col(border), col(code), col(key), col(err), col(ok), col(ins), col(norm), col(view)}
}

var themes = map[string]Theme{}

func add(t Theme, aliases ...string) {
	themes[t.Name] = t
	for _, a := range aliases {
		themes[a] = t
	}
}

func init() {
	add(Theme{
		Name:    "default",
		Accent:  c("#ea580c"),
		URL:     lipgloss.AdaptiveColor{Light: "25", Dark: "39"},
		Muted:   c("244"),
		Border:  c("244"),
		Code:    lipgloss.AdaptiveColor{Light: "28", Dark: "150"},
		Key:     c("#ea580c"),
		Error:   c("203"),
		Success: c("78"),
		Insert:  c("78"),
		Normal:  c("75"),
		View:    c("219"),
	})
	// Uses the terminal's 16 ANSI colours, so it follows whatever theme the
	// terminal emulator itself is configured with.
	add(Theme{
		Name: "ansi", Accent: c("5"), URL: c("4"), Muted: c("8"), Border: c("8"),
		Code: c("2"), Key: c("3"), Error: c("1"), Success: c("2"),
		Insert: c("2"), Normal: c("4"), View: c("5"),
	}, "terminal", "base16")
	add(Theme{Name: "mono", Muted: c("8"), Border: c("8")}, "none", "monochrome")

	//              name                 accent     title      text       url        muted      border     code       key        error      success    insert     normal     view
	add(palette("catppuccin-mocha", "#cba6f7", "#cdd6f4", "", "#89b4fa", "#7f849c", "#585b70", "#a6e3a1", "#fab387", "#f38ba8", "#a6e3a1", "#a6e3a1", "#89b4fa", "#f5c2e7"), "catppuccin")
	add(palette("catppuccin-macchiato", "#c6a0f6", "#cad3f5", "", "#8aadf4", "#8087a2", "#5b6078", "#a6da95", "#f5a97f", "#ed8796", "#a6da95", "#a6da95", "#8aadf4", "#f5bde6"))
	add(palette("catppuccin-frappe", "#ca9ee6", "#c6d0f5", "", "#8caaee", "#838ba7", "#626880", "#a6d189", "#ef9f76", "#e78284", "#a6d189", "#a6d189", "#8caaee", "#f4b8e4"))
	add(palette("catppuccin-latte", "#8839ef", "#4c4f69", "", "#1e66f5", "#8c8fa1", "#acb0be", "#40a02b", "#fe640b", "#d20f39", "#40a02b", "#40a02b", "#1e66f5", "#ea76cb"))
	add(palette("dracula", "#bd93f9", "#f8f8f2", "", "#8be9fd", "#6272a4", "#44475a", "#50fa7b", "#ffb86c", "#ff5555", "#50fa7b", "#50fa7b", "#8be9fd", "#ff79c6"))
	add(palette("nord", "#88c0d0", "#eceff4", "", "#81a1c1", "#616e88", "#4c566a", "#a3be8c", "#ebcb8b", "#bf616a", "#a3be8c", "#a3be8c", "#81a1c1", "#b48ead"))
	add(palette("gruvbox-dark", "#fe8019", "#ebdbb2", "", "#83a598", "#928374", "#665c54", "#b8bb26", "#fabd2f", "#fb4934", "#b8bb26", "#b8bb26", "#83a598", "#d3869b"), "gruvbox")
	add(palette("gruvbox-light", "#af3a03", "#3c3836", "", "#076678", "#928374", "#bdae93", "#79740e", "#b57614", "#9d0006", "#79740e", "#79740e", "#076678", "#8f3f71"))
	add(palette("solarized-dark", "#cb4b16", "#93a1a1", "", "#268bd2", "#586e75", "#586e75", "#859900", "#b58900", "#dc322f", "#859900", "#859900", "#268bd2", "#d33682"), "solarized")
	add(palette("solarized-light", "#cb4b16", "#586e75", "", "#268bd2", "#93a1a1", "#93a1a1", "#859900", "#b58900", "#dc322f", "#859900", "#859900", "#268bd2", "#d33682"))
	add(palette("tokyo-night", "#bb9af7", "#c0caf5", "", "#7aa2f7", "#565f89", "#3b4261", "#9ece6a", "#e0af68", "#f7768e", "#9ece6a", "#9ece6a", "#7aa2f7", "#ff9e64"), "tokyonight")
	add(palette("tokyo-night-day", "#9854f1", "#3760bf", "", "#2e7de9", "#848cb5", "#a8aecb", "#587539", "#8c6c3e", "#f52a65", "#587539", "#587539", "#2e7de9", "#b15c00"))
	add(palette("one-dark", "#c678dd", "#abb2bf", "", "#61afef", "#5c6370", "#4b5263", "#98c379", "#e5c07b", "#e06c75", "#98c379", "#98c379", "#61afef", "#c678dd"), "onedark")
	add(palette("one-light", "#a626a4", "#383a42", "", "#4078f2", "#a0a1a7", "#c8c9cc", "#50a14f", "#986801", "#e45649", "#50a14f", "#50a14f", "#4078f2", "#a626a4"))
	add(palette("monokai", "#f92672", "#f8f8f2", "", "#66d9ef", "#75715e", "#75715e", "#a6e22e", "#e6db74", "#f92672", "#a6e22e", "#a6e22e", "#66d9ef", "#ae81ff"))
	add(palette("everforest", "#a7c080", "#d3c6aa", "", "#7fbbb3", "#859289", "#4f585e", "#a7c080", "#dbbc7f", "#e67e80", "#a7c080", "#a7c080", "#7fbbb3", "#d699b6"))
	add(palette("rose-pine", "#c4a7e7", "#e0def4", "", "#9ccfd8", "#6e6a86", "#524f67", "#ebbcba", "#f6c177", "#eb6f92", "#9ccfd8", "#ebbcba", "#9ccfd8", "#c4a7e7"), "rosepine")
	add(palette("rose-pine-dawn", "#907aa9", "#575279", "", "#56949f", "#9893a5", "#cecacd", "#d7827e", "#ea9d34", "#b4637a", "#56949f", "#d7827e", "#56949f", "#907aa9"))
	add(palette("kanagawa", "#957fb8", "#dcd7ba", "", "#7e9cd8", "#727169", "#54546d", "#98bb6c", "#ffa066", "#e82424", "#98bb6c", "#98bb6c", "#7e9cd8", "#d27e99"))
	add(palette("github-dark", "#ff7b72", "#e6edf3", "", "#58a6ff", "#8b949e", "#30363d", "#7ee787", "#d29922", "#f85149", "#3fb950", "#3fb950", "#58a6ff", "#d2a8ff"))
	add(palette("github-light", "#cf222e", "#1f2328", "", "#0969da", "#656d76", "#d0d7de", "#116329", "#9a6700", "#cf222e", "#1a7f37", "#1a7f37", "#0969da", "#8250df"))
}

// Names lists canonical theme names (aliases excluded), sorted.
func Names() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range themes {
		if !seen[t.Name] {
			seen[t.Name] = true
			out = append(out, t.Name)
		}
	}
	sort.Strings(out)
	return out
}

// Get returns the named theme (case-insensitive; "_" and " " act as "-").
func Get(name string) (Theme, bool) {
	n := strings.NewReplacer("_", "-", " ", "-").Replace(strings.ToLower(strings.TrimSpace(name)))
	if n == "" {
		n = "default"
	}
	t, ok := themes[n]
	return t, ok
}

// Resolve returns the named theme with colour overrides applied. Keys are
// field names in snake case (accent, title, text, url, muted, border, code,
// key, error, success, insert, normal, view). An empty value resets a colour
// to the terminal default.
func Resolve(name string, overrides map[string]string) (Theme, error) {
	t, ok := Get(name)
	var err error
	if !ok {
		t, _ = Get("default")
		err = fmt.Errorf("unknown theme %q (available: %s)", name, strings.Join(Names(), ", "))
	}
	for k, v := range overrides {
		var col lipgloss.TerminalColor
		if v != "" {
			col = c(v)
		}
		switch strings.ToLower(k) {
		case "accent":
			t.Accent = col
		case "title":
			t.Title = col
		case "text":
			t.Text = col
		case "url":
			t.URL = col
		case "muted":
			t.Muted = col
		case "border":
			t.Border = col
		case "code":
			t.Code = col
		case "key":
			t.Key = col
		case "error":
			t.Error = col
		case "success":
			t.Success = col
		case "insert":
			t.Insert = col
		case "normal":
			t.Normal = col
		case "view":
			t.View = col
		default:
			if err == nil {
				err = fmt.Errorf("unknown colour %q in [ui.colors]", k)
			}
		}
	}
	return t, err
}

// Fg returns a style with only the foreground set (nil = terminal default).
func Fg(col lipgloss.TerminalColor) lipgloss.Style {
	s := lipgloss.NewStyle()
	if col != nil {
		s = s.Foreground(col)
	}
	return s
}
