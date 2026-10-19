package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme is a set of semantic colors, modelled on OpenCode's theme tokens:
// three background layers (base, panel, element), text + muted text, and
// accent roles. Backgrounds are painted edge to edge, which is what gives the
// TUI its solid, app-like look.
type Theme struct {
	Name                          string
	Dark                          bool
	Primary, Secondary, Accent    lipgloss.TerminalColor
	Error, Warning, Success, Info lipgloss.TerminalColor
	Text, Muted, Subtle           lipgloss.TerminalColor
	Bg, Panel, Element            lipgloss.TerminalColor
	Border                        lipgloss.TerminalColor
}

func hexc(s string) lipgloss.TerminalColor { return lipgloss.Color(s) }

type palette struct {
	bg, panel, element, border, text, muted, subtle string
	primary, secondary, accent                      string
	err, warn, ok, info                             string
	light                                           bool
}

func (p palette) theme(name string) Theme {
	return Theme{
		Name: name, Dark: !p.light,
		Primary: hexc(p.primary), Secondary: hexc(p.secondary), Accent: hexc(p.accent),
		Error: hexc(p.err), Warning: hexc(p.warn), Success: hexc(p.ok), Info: hexc(p.info),
		Text: hexc(p.text), Muted: hexc(p.muted), Subtle: hexc(p.subtle),
		Bg: hexc(p.bg), Panel: hexc(p.panel), Element: hexc(p.element), Border: hexc(p.border),
	}
}

var palettes = map[string]palette{
	"opencode": {bg: "#0a0a0a", panel: "#141414", element: "#1e1e1e", border: "#3c3c3c", text: "#eeeeee", muted: "#808080", subtle: "#5a5a5a",
		primary: "#fab283", secondary: "#5c9cf5", accent: "#9d7cd8", err: "#e06c75", warn: "#f5a742", ok: "#7fd88f", info: "#56b6c2"},
	"tokyonight": {bg: "#1a1b26", panel: "#16161e", element: "#24283b", border: "#3b4261", text: "#c0caf5", muted: "#737aa2", subtle: "#545c7e",
		primary: "#7aa2f7", secondary: "#bb9af7", accent: "#ff9e64", err: "#f7768e", warn: "#e0af68", ok: "#9ece6a", info: "#7dcfff"},
	"catppuccin": {bg: "#1e1e2e", panel: "#181825", element: "#313244", border: "#45475a", text: "#cdd6f4", muted: "#9399b2", subtle: "#6c7086",
		primary: "#cba6f7", secondary: "#89b4fa", accent: "#f5c2e7", err: "#f38ba8", warn: "#f9e2af", ok: "#a6e3a1", info: "#94e2d5"},
	"gruvbox": {bg: "#282828", panel: "#1d2021", element: "#3c3836", border: "#504945", text: "#ebdbb2", muted: "#a89984", subtle: "#7c6f64",
		primary: "#fe8019", secondary: "#83a598", accent: "#d3869b", err: "#fb4934", warn: "#fabd2f", ok: "#b8bb26", info: "#8ec07c"},
	"nord": {bg: "#2e3440", panel: "#272c36", element: "#3b4252", border: "#4c566a", text: "#eceff4", muted: "#a0a8b7", subtle: "#6d7a92",
		primary: "#88c0d0", secondary: "#81a1c1", accent: "#b48ead", err: "#bf616a", warn: "#ebcb8b", ok: "#a3be8c", info: "#8fbcbb"},
	"everforest": {bg: "#2d353b", panel: "#272e33", element: "#3d484d", border: "#4f585e", text: "#d3c6aa", muted: "#9da9a0", subtle: "#7a8478",
		primary: "#a7c080", secondary: "#7fbbb3", accent: "#d699b6", err: "#e67e80", warn: "#dbbc7f", ok: "#a7c080", info: "#83c092"},
	"kanagawa": {bg: "#1f1f28", panel: "#16161d", element: "#2a2a37", border: "#54546d", text: "#dcd7ba", muted: "#a6a69c", subtle: "#727169",
		primary: "#7e9cd8", secondary: "#957fb8", accent: "#ffa066", err: "#e82424", warn: "#ff9e3b", ok: "#98bb6c", info: "#7fb4ca"},
	"rosepine": {bg: "#191724", panel: "#1f1d2e", element: "#26233a", border: "#403d52", text: "#e0def4", muted: "#908caa", subtle: "#6e6a86",
		primary: "#ebbcba", secondary: "#9ccfd8", accent: "#c4a7e7", err: "#eb6f92", warn: "#f6c177", ok: "#31748f", info: "#9ccfd8"},
	"onedark": {bg: "#282c34", panel: "#21252b", element: "#2c313a", border: "#3e4451", text: "#abb2bf", muted: "#7f848e", subtle: "#5c6370",
		primary: "#61afef", secondary: "#c678dd", accent: "#56b6c2", err: "#e06c75", warn: "#e5c07b", ok: "#98c379", info: "#56b6c2"},
	"dracula": {bg: "#282a36", panel: "#21222c", element: "#343746", border: "#44475a", text: "#f8f8f2", muted: "#a4a9c9", subtle: "#6272a4",
		primary: "#bd93f9", secondary: "#ff79c6", accent: "#8be9fd", err: "#ff5555", warn: "#f1fa8c", ok: "#50fa7b", info: "#8be9fd"},
	"ayu": {bg: "#0b0e14", panel: "#0d1017", element: "#131721", border: "#1b1f29", text: "#bfbdb6", muted: "#7a7f8a", subtle: "#565b66",
		primary: "#e6b450", secondary: "#59c2ff", accent: "#d2a6ff", err: "#f07178", warn: "#ffb454", ok: "#aad94c", info: "#95e6cb"},
	"matrix": {bg: "#0a0e0a", panel: "#070a07", element: "#111a10", border: "#1e3a24", text: "#62ff94", muted: "#3fa65f", subtle: "#2a6b3e",
		primary: "#2eff6a", secondary: "#00efff", accent: "#c770ff", err: "#ff4b4b", warn: "#e6ff57", ok: "#62ff94", info: "#30b3ff"},
	"mono": {bg: "#101010", panel: "#0a0a0a", element: "#1c1c1c", border: "#333333", text: "#e4e4e4", muted: "#8a8a8a", subtle: "#5e5e5e",
		primary: "#ffffff", secondary: "#bdbdbd", accent: "#d0d0d0", err: "#ff6b6b", warn: "#d7d7d7", ok: "#ffffff", info: "#bdbdbd"},
	"latte": {bg: "#eff1f5", panel: "#e6e9ef", element: "#dce0e8", border: "#bcc0cc", text: "#4c4f69", muted: "#6c6f85", subtle: "#9ca0b0",
		primary: "#8839ef", secondary: "#1e66f5", accent: "#ea76cb", err: "#d20f39", warn: "#df8e1d", ok: "#40a02b", info: "#179299", light: true},
}

// systemTheme uses the terminal's own palette and background.
func systemTheme() Theme {
	none := lipgloss.NoColor{}
	return Theme{
		Name: "system", Dark: true,
		Primary: lipgloss.Color("3"), Secondary: lipgloss.Color("4"), Accent: lipgloss.Color("5"),
		Error: lipgloss.Color("1"), Warning: lipgloss.Color("3"), Success: lipgloss.Color("2"), Info: lipgloss.Color("6"),
		Text: none, Muted: lipgloss.Color("8"), Subtle: lipgloss.Color("8"),
		Bg: none, Panel: none, Element: lipgloss.Color("0"), Border: lipgloss.Color("8"),
	}
}

// legacy theme names from gooseneck ≤0.x configs.
var aliases = map[string]string{"catppuccin-mocha": "catppuccin", "tokyo-night": "tokyonight", "rose-pine": "rosepine", "one-dark": "onedark"}

// ThemeNames lists the available themes in display order. "omarchy" (the
// live Omarchy desktop theme) is listed first when Omarchy is installed.
func ThemeNames() []string {
	names := make([]string, 0, len(palettes)+2)
	for n := range palettes {
		if n != "opencode" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	out := []string{"opencode"}
	if OmarchyDetected() {
		out = []string{"omarchy", "opencode"}
	}
	return append(append(out, names...), "system")
}

// HasTheme reports whether name (or a legacy alias) is a valid theme setting.
func HasTheme(name string) bool {
	name = strings.ToLower(name)
	if a, ok := aliases[name]; ok {
		name = a
	}
	_, ok := palettes[name]
	return ok || name == "system" || name == "omarchy" || name == "auto"
}

// ResolveThemeName turns a config value into a concrete theme name. "auto"
// (the default), "" and unknown names mean: follow Omarchy when it's
// installed, otherwise opencode.
func ResolveThemeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if a, ok := aliases[name]; ok {
		name = a
	}
	if _, ok := palettes[name]; ok || name == "system" {
		return name
	}
	if OmarchyDetected() {
		return "omarchy"
	}
	return "opencode"
}

// GetTheme returns the named theme (see ResolveThemeName).
func GetTheme(name string) Theme {
	switch name = ResolveThemeName(name); name {
	case "system":
		return systemTheme()
	case "omarchy":
		if t, err := loadOmarchyTheme(); err == nil {
			return t
		}
		return palettes["opencode"].theme("opencode")
	}
	return palettes[name].theme(name)
}

// st is a tiny style constructor bound to a foreground color.
func fg(c lipgloss.TerminalColor) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

func (t Theme) text() lipgloss.Style    { return fg(t.Text) }
func (t Theme) muted() lipgloss.Style   { return fg(t.Muted) }
func (t Theme) subtle() lipgloss.Style  { return fg(t.Subtle) }
func (t Theme) primary() lipgloss.Style { return fg(t.Primary) }
func (t Theme) bold() lipgloss.Style    { return fg(t.Text).Bold(true) }

// onPrimary is a badge: dark text on the primary color.
func (t Theme) onPrimary() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.ink()).Background(t.Primary).Bold(true)
}

// ink is the text color used on top of the primary color.
func (t Theme) ink() lipgloss.TerminalColor {
	if _, none := t.Bg.(lipgloss.NoColor); none {
		return lipgloss.Color("0")
	}
	return t.Bg
}

// selRow highlights the selected row in a list: element background.
func (t Theme) selRow() lipgloss.Style {
	return lipgloss.NewStyle().Background(t.Element)
}
