package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Omarchy (https://omarchy.org) keeps the active theme's generated files in a
// "current theme" directory. Omarchy 4 uses ~/.local/state/omarchy/current/theme;
// Omarchy 3 used ~/.config/omarchy/current/theme. Both carry a colors.toml
// (older themes may only have alacritty.toml, which we fall back to).

// omarchyThemeDir returns the current Omarchy theme directory, or "" when
// Omarchy isn't installed.
func omarchyThemeDir() string {
	if d, ok := os.LookupEnv("GOOSENECK_OMARCHY_THEME"); ok { // override (tests, unusual setups)
		if hasThemeFiles(d) {
			return d
		}
		return ""
	}
	home, _ := os.UserHomeDir()
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	for _, d := range []string{
		filepath.Join(state, "omarchy", "current", "theme"),
		filepath.Join(config, "omarchy", "current", "theme"),
	} {
		if hasThemeFiles(d) {
			return d
		}
	}
	return ""
}

func hasThemeFiles(d string) bool {
	for _, f := range []string{"colors.toml", "alacritty.toml"} {
		if _, err := os.Stat(filepath.Join(d, f)); err == nil {
			return true
		}
	}
	return false
}

// OmarchyDetected reports whether an Omarchy theme is available.
func OmarchyDetected() bool { return omarchyThemeDir() != "" }

// omarchyName is the human name of the current Omarchy theme ("Tokyo Night").
func omarchyName(dir string) string {
	if b, err := os.ReadFile(filepath.Join(filepath.Dir(dir), "theme.name")); err == nil {
		if n := strings.TrimSpace(string(b)); n != "" {
			return n
		}
	}
	return filepath.Base(resolve(dir))
}

func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// omarchySignature changes whenever the user switches Omarchy themes, so the
// TUI can repaint live. It is cheap: a few stats.
func omarchySignature() string {
	dir := omarchyThemeDir()
	if dir == "" {
		return ""
	}
	sig := resolve(dir)
	for _, f := range []string{"colors.toml", "alacritty.toml", "../theme.name"} {
		if st, err := os.Stat(filepath.Join(dir, f)); err == nil {
			sig += fmt.Sprintf("|%s:%d:%d", f, st.ModTime().UnixNano(), st.Size())
		}
	}
	return sig
}

// omarchyColors is Omarchy's colors.toml.
type omarchyColors struct {
	Mode              string `toml:"mode"`
	Accent            string `toml:"accent"`
	Selection         string `toml:"selection"`
	Muted             string `toml:"muted"`
	Background        string `toml:"background"`
	DarkBackground    string `toml:"dark_background"`
	LighterBackground string `toml:"lighter_background"`
	Foreground        string `toml:"foreground"`
	DarkForeground    string `toml:"dark_foreground"`
	Red               string `toml:"red"`
	Yellow            string `toml:"yellow"`
	Green             string `toml:"green"`
	Cyan              string `toml:"cyan"`
	Blue              string `toml:"blue"`
	Magenta           string `toml:"magenta"`
}

// alacrittyColors is the subset of an Alacritty theme we can use.
type alacrittyColors struct {
	Colors struct {
		Primary struct {
			Background string `toml:"background"`
			Foreground string `toml:"foreground"`
		} `toml:"primary"`
		Normal map[string]string `toml:"normal"`
		Bright map[string]string `toml:"bright"`
	} `toml:"colors"`
}

// loadOmarchyTheme builds a Theme from the current Omarchy theme.
func loadOmarchyTheme() (Theme, error) {
	dir := omarchyThemeDir()
	if dir == "" {
		return Theme{}, fmt.Errorf("no Omarchy theme found")
	}
	var c omarchyColors
	if _, err := toml.DecodeFile(filepath.Join(dir, "colors.toml"), &c); err != nil || c.Background == "" {
		var a alacrittyColors
		if _, err := toml.DecodeFile(filepath.Join(dir, "alacritty.toml"), &a); err != nil {
			return Theme{}, fmt.Errorf("read Omarchy theme: %w", err)
		}
		n, b := a.Colors.Normal, a.Colors.Bright
		c = omarchyColors{
			Background: a.Colors.Primary.Background, Foreground: a.Colors.Primary.Foreground,
			Accent: n["blue"], Muted: b["black"], DarkForeground: b["black"],
			Red: n["red"], Yellow: n["yellow"], Green: n["green"], Cyan: n["cyan"], Blue: n["blue"], Magenta: n["magenta"],
		}
	}
	return omarchyTheme(c), nil
}

// omarchyTheme maps Omarchy's palette onto gooseneck's tokens, deriving any
// layer the theme doesn't define by nudging the background.
func omarchyTheme(c omarchyColors) Theme {
	light := strings.EqualFold(c.Mode, "light") || (c.Mode == "" && luminance(c.Background) > 0.5)
	or := func(v, def string) string {
		if isHex(v) {
			return v
		}
		return def
	}
	bg := or(c.Background, "#1a1b26")
	fgc := or(c.Foreground, "#c0caf5")
	panel := or(c.DarkBackground, shade(bg, light, -0.04))
	element := or(c.LighterBackground, or(c.Selection, shade(bg, light, 0.08)))
	muted := or(c.DarkForeground, or(c.Muted, mixHex(fgc, bg, 0.45)))
	subtle := or(c.Muted, mixHex(fgc, bg, 0.65))
	accent := or(c.Accent, or(c.Blue, "#7aa2f7"))
	p := palette{
		bg: bg, panel: panel, element: element, border: or(c.Selection, element),
		text: fgc, muted: muted, subtle: subtle,
		primary: accent, secondary: or(c.Blue, accent), accent: or(c.Magenta, accent),
		err: or(c.Red, "#e06c75"), warn: or(c.Yellow, "#e5c07b"), ok: or(c.Green, "#98c379"), info: or(c.Cyan, "#56b6c2"),
		light: light,
	}
	return p.theme("omarchy")
}

func isHex(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	var r, g, b int
	_, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b)
	return err == nil
}

func rgb(s string) (float64, float64, float64) {
	var r, g, b int
	fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b)
	return float64(r), float64(g), float64(b)
}

func luminance(s string) float64 {
	if !isHex(s) {
		return 0
	}
	r, g, b := rgb(s)
	return (0.2126*r + 0.7152*g + 0.0722*b) / 255
}

// mixHex blends a toward b by t (0..1).
func mixHex(a, b string, t float64) string {
	if !isHex(a) || !isHex(b) {
		return a
	}
	ar, ag, ab := rgb(a)
	br, bg, bb := rgb(b)
	m := func(x, y float64) int { return int(x + (y-x)*t + 0.5) }
	return fmt.Sprintf("#%02x%02x%02x", m(ar, br), m(ag, bg), m(ab, bb))
}

// shade lightens (dark themes) or darkens (light themes) a background by amt;
// a negative amt goes the other way.
func shade(bg string, light bool, amt float64) string {
	target := "#ffffff"
	if light {
		target = "#000000"
	}
	if amt < 0 {
		amt = -amt
		if light {
			target = "#ffffff"
		} else {
			target = "#000000"
		}
	}
	return mixHex(bg, target, amt)
}
