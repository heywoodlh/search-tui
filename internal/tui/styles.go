package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/heywoodlh-bot/tui-search/internal/config"
)

type styles struct {
	accent, title, selTitle, url, meta, snippet, muted, code, lineNo lipgloss.Style
	err, ok, badge, modeIns, modeNorm, modeDet, statusBar, key       lipgloss.Style
	border, sumTitle, bar                                            lipgloss.Style
	selBar, bullet                                                   string
}

func newStyles(ui config.UI) styles {
	accent := lipgloss.Color(ui.AccentColor)
	muted := lipgloss.Color(ui.MutedColor)
	s := styles{
		accent:    lipgloss.NewStyle().Foreground(accent).Bold(true),
		title:     lipgloss.NewStyle().Bold(true),
		selTitle:  lipgloss.NewStyle().Bold(true).Foreground(accent),
		url:       lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "25", Dark: "39"}),
		meta:      lipgloss.NewStyle().Foreground(muted).Italic(true),
		snippet:   lipgloss.NewStyle(),
		muted:     lipgloss.NewStyle().Foreground(muted),
		code:      lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "150"}),
		lineNo:    lipgloss.NewStyle().Foreground(muted),
		err:       lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true),
		ok:        lipgloss.NewStyle().Foreground(lipgloss.Color("78")),
		badge:     lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(accent).Padding(0, 1).Bold(true),
		modeIns:   lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("78")).Padding(0, 1).Bold(true),
		modeNorm:  lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("75")).Padding(0, 1).Bold(true),
		modeDet:   lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("219")).Padding(0, 1).Bold(true),
		statusBar: lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "236", Dark: "252"}).Background(lipgloss.AdaptiveColor{Light: "254", Dark: "236"}),
		key:       lipgloss.NewStyle().Foreground(accent).Bold(true),
		border:    lipgloss.NewStyle().Foreground(muted),
		sumTitle:  lipgloss.NewStyle().Foreground(accent).Bold(true),
		bar:       lipgloss.NewStyle().Foreground(accent),
		selBar:    "▌",
		bullet:    "•",
	}
	if ui.ASCII {
		s.selBar = ">"
		s.bullet = "*"
	}
	return s
}
