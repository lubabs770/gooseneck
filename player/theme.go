package main

import "github.com/charmbracelet/lipgloss"

// Theme is a small palette rendered into lipgloss styles. Add entries to themes
// to grow the set; `t` in the UI cycles through themeOrder.
type Theme struct {
	Name     string
	Accent   lipgloss.Color // selection / highlights
	Fg       lipgloss.Color // normal text
	Dim      lipgloss.Color // subtitles, help
	CardBg   lipgloss.Color // grid card background
	SelBg    lipgloss.Color // selected card/row background
	SelFg    lipgloss.Color
}

var themes = map[string]Theme{
	"default": {
		Name: "default", Accent: "212", Fg: "252", Dim: "244",
		CardBg: "236", SelBg: "212", SelFg: "16",
	},
	"gruvbox": {
		Name: "gruvbox", Accent: "208", Fg: "223", Dim: "245",
		CardBg: "237", SelBg: "208", SelFg: "235",
	},
	"nord": {
		Name: "nord", Accent: "110", Fg: "252", Dim: "244",
		CardBg: "238", SelBg: "110", SelFg: "236",
	},
	"mono": {
		Name: "mono", Accent: "255", Fg: "250", Dim: "240",
		CardBg: "235", SelBg: "255", SelFg: "16",
	},
}

var themeOrder = []string{"default", "gruvbox", "nord", "mono"}

func getTheme(name string) Theme {
	if t, ok := themes[name]; ok {
		return t
	}
	return themes["default"]
}

func nextTheme(name string) string {
	for i, n := range themeOrder {
		if n == name {
			return themeOrder[(i+1)%len(themeOrder)]
		}
	}
	return themeOrder[0]
}

// styles holds the derived lipgloss styles for the active theme.
type styles struct {
	title    lipgloss.Style
	help     lipgloss.Style
	dim      lipgloss.Style
	card     lipgloss.Style
	cardSel  lipgloss.Style
	row      lipgloss.Style
	rowSel   lipgloss.Style
	filter   lipgloss.Style
	loading  lipgloss.Style
}

func buildStyles(t Theme) styles {
	base := lipgloss.NewStyle().Foreground(t.Fg)
	return styles{
		title:   lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		help:    lipgloss.NewStyle().Foreground(t.Dim),
		dim:     lipgloss.NewStyle().Foreground(t.Dim),
		card:    base.Padding(0, 1).Margin(0, 1),
		cardSel: lipgloss.NewStyle().Foreground(t.SelFg).Background(t.SelBg).Bold(true).Padding(0, 1).Margin(0, 1),
		row:     base,
		rowSel:  lipgloss.NewStyle().Foreground(t.SelFg).Background(t.SelBg).Bold(true),
		filter:  lipgloss.NewStyle().Foreground(t.Accent),
		loading: lipgloss.NewStyle().Foreground(t.Accent).Italic(true),
	}
}
