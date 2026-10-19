package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestThemes(t *testing.T) {
	t.Setenv("GOOSENECK_OMARCHY_THEME", t.TempDir()) // no Omarchy
	names := ThemeNames()
	if names[0] != "opencode" || names[len(names)-1] != "system" || len(names) < 10 {
		t.Fatalf("ThemeNames = %v", names)
	}
	for _, n := range names {
		if !HasTheme(n) || GetTheme(n).Name != n {
			t.Errorf("theme %q does not round-trip", n)
		}
	}
	if GetTheme("default").Name != "opencode" || GetTheme("bogus").Name != "opencode" || GetTheme("auto").Name != "opencode" {
		t.Error("legacy/unknown names should map to opencode")
	}
	if HasTheme("bogus") {
		t.Error("bogus theme reported as present")
	}
}

func TestPaintIsRectangular(t *testing.T) {
	th := GetTheme("opencode")
	block := th.primary().Render("hi") + " there\nsecond line that is far too long for the box"
	out := paint(block, th.Bg, 20, 4)
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want 4", len(lines))
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 20 {
			t.Errorf("line %d width %d, want 20", i, w)
		}
	}
}

func TestOverlay(t *testing.T) {
	base := strings.Repeat("..........\n", 4) + ".........."
	out := overlay(base, "AB\nCD", 3, 1)
	lines := strings.Split(out, "\n")
	if got := stripANSI(lines[1]); got != "...AB....." {
		t.Fatalf("row 1 = %q", got)
	}
	if got := stripANSI(lines[2]); got != "...CD....." {
		t.Fatalf("row 2 = %q", got)
	}
	if stripANSI(lines[0]) != ".........." {
		t.Fatal("row 0 changed")
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			esc = true
		case esc && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestCollectionNavAndFilter(t *testing.T) {
	c := newCollection(true, false, "")
	var items []entry
	for _, n := range []string{"Avi Perets", "Shlomo Gavriel", "8th Day", "Avraham Fried", "Mordechai Ben David"} {
		items = append(items, entry{key: n, title: n})
	}
	c.set(items)
	c.cols = 2
	c.key("l", false)
	c.key("j", false)
	if c.cursor != 3 {
		t.Fatalf("cursor = %d, want 3 after right+down in a 2-col grid", c.cursor)
	}
	c.key("G", false)
	if e, _ := c.selected(); e.title != "Mordechai Ben David" {
		t.Fatalf("G -> %q", e.title)
	}
	c.setFilter("avr")
	if e, ok := c.selected(); !ok || e.title != "Avraham Fried" {
		t.Fatalf("filter avr -> %q", e.title)
	}
	// set keeps the selection on the same key
	c.setFilter("")
	c.cursor = 2
	c.set(append([]entry{{key: "new", title: "New"}}, items...))
	if e, _ := c.selected(); e.key != "8th Day" {
		t.Fatalf("selection moved to %q", e.key)
	}
	// replacing with fewer items never panics
	c.set(nil)
	if _, ok := c.selected(); ok {
		t.Fatal("empty collection has a selection")
	}
}

func TestLogoAndHelpers(t *testing.T) {
	l := logo(GetTheme("opencode"))
	if lipgloss.Height(l) != 3 || lipgloss.Width(l) != 9*4+8 {
		t.Fatalf("logo is %dx%d", lipgloss.Width(l), lipgloss.Height(l))
	}
	if got := wrap("one two three four five six", 9, 2); len(got) != 2 {
		t.Fatalf("wrap = %q", got)
	}
	if initials("8th Day") != "8D" || initials("שלום") != "♪" {
		t.Fatalf("initials: %q %q", initials("8th Day"), initials("שלום"))
	}
}

func TestOmarchyTheme(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "theme")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(root, "theme.name"), []byte("tokyo-night\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "colors.toml"), []byte(`mode = "dark"
accent = "#7aa2f7"
background = "#1a1b26"
dark_background = "#13141c"
lighter_background = "#24283b"
foreground = "#a9b1d6"
dark_foreground = "#565f89"
red = "#f7768e"
`), 0o644)
	t.Setenv("GOOSENECK_OMARCHY_THEME", dir)

	if !OmarchyDetected() || ThemeNames()[0] != "omarchy" {
		t.Fatalf("omarchy not offered: %v", ThemeNames())
	}
	for _, n := range []string{"auto", "", "gruvbvox", "default", "omarchy"} {
		if got := ResolveThemeName(n); got != "omarchy" {
			t.Errorf("ResolveThemeName(%q) = %q, want omarchy", n, got)
		}
	}
	if ResolveThemeName("nord") != "nord" {
		t.Error("explicit palettes must still win")
	}
	th := GetTheme("auto")
	if th.Name != "omarchy" || th.Primary != lipgloss.Color("#7aa2f7") || th.Bg != lipgloss.Color("#1a1b26") ||
		th.Panel != lipgloss.Color("#13141c") || th.Error != lipgloss.Color("#f7768e") || !th.Dark {
		t.Fatalf("omarchy theme = %+v", th)
	}
	if omarchyName(dir) != "tokyo-night" {
		t.Fatalf("name = %q", omarchyName(dir))
	}

	// switching themes changes the signature (that's what drives live reload)
	sig := omarchySignature()
	os.WriteFile(filepath.Join(dir, "colors.toml"), []byte("mode = \"light\"\nbackground = \"#eff1f5\"\nforeground = \"#4c4f69\"\naccent = \"#1e66f5\"\n"), 0o644)
	if omarchySignature() == sig {
		t.Fatal("signature did not change")
	}
	if th := GetTheme("omarchy"); th.Dark || th.Primary != lipgloss.Color("#1e66f5") {
		t.Fatalf("reloaded theme = %+v", th)
	}

	// Omarchy 3-era theme with only alacritty.toml
	os.Remove(filepath.Join(dir, "colors.toml"))
	os.WriteFile(filepath.Join(dir, "alacritty.toml"), []byte(`[colors.primary]
background = "#282828"
foreground = "#ebdbb2"
[colors.normal]
blue = "#83a598"
red = "#fb4934"
`), 0o644)
	if th := GetTheme("omarchy"); th.Bg != lipgloss.Color("#282828") || th.Primary != lipgloss.Color("#83a598") {
		t.Fatalf("alacritty fallback = %+v", th)
	}
}
