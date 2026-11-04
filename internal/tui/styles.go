package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/heywoodlh/search-tui/internal/config"
	"github.com/heywoodlh/search-tui/internal/theme"
)

// styles only ever set foreground colours and text attributes, never a
// background, so the terminal's own background is always preserved.
type styles struct {
	accent, title, selTitle, url, meta, snippet, muted, code, lineNo lipgloss.Style
	err, ok, badge, modeIns, modeNorm, modeDet, statusBar, key       lipgloss.Style
	border, sumTitle, bar                                            lipgloss.Style
	accentColor                                                      lipgloss.TerminalColor
	selBar, bullet, sep, check, pointer                              string
}

func newStyles(ui config.UI, t theme.Theme) styles {
	fg := theme.Fg
	s := styles{
		accent:      fg(t.Accent).Bold(true),
		title:       fg(t.Title).Bold(true),
		selTitle:    fg(t.Accent).Bold(true),
		url:         fg(t.URL),
		meta:        fg(t.Muted), // no italics: some terminals (tmux on screen-256color) render them as reverse video
		snippet:     fg(t.Text),
		muted:       fg(t.Muted),
		code:        fg(t.Code),
		lineNo:      fg(t.Muted),
		err:         fg(t.Error).Bold(true),
		ok:          fg(t.Success),
		badge:       fg(t.Accent).Bold(true),
		modeIns:     fg(t.Insert).Bold(true),
		modeNorm:    fg(t.Normal).Bold(true),
		modeDet:     fg(t.View).Bold(true),
		statusBar:   fg(t.Text),
		key:         fg(t.Key).Bold(true),
		border:      fg(t.Border),
		sumTitle:    fg(t.Accent).Bold(true),
		bar:         fg(t.Accent),
		accentColor: t.Accent,
		selBar:      "▌",
		bullet:      "•",
		sep:         "›",
		check:       "✓",
		pointer:     "▸",
	}
	if ui.ASCII {
		s.selBar, s.bullet, s.sep, s.check, s.pointer = ">", "*", ">", "*", ">"
	}
	return s
}
