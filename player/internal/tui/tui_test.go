package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestThemes(t *testing.T) {
	names := ThemeNames()
	if names[0] != "opencode" || names[len(names)-1] != "system" || len(names) < 10 {
		t.Fatalf("ThemeNames = %v", names)
	}
	for _, n := range names {
		if !HasTheme(n) || GetTheme(n).Name != n {
			t.Errorf("theme %q does not round-trip", n)
		}
	}
	if GetTheme("default").Name != "opencode" || GetTheme("bogus").Name != "opencode" {
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
	if squareURL("https://yt3.googleusercontent.com/abc=w2880-h1200-p", 320) != "https://yt3.googleusercontent.com/abc=s320-c" {
		t.Fatal("squareURL google")
	}
	if squareURL("https://example.com/a=b", 320) != "https://example.com/a=b" {
		t.Fatal("squareURL non-google")
	}
}
