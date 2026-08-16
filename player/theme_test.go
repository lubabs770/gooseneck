package main

import "testing"

func TestGetTheme(t *testing.T) {
	// known names resolve to themselves
	for _, name := range themeOrder {
		if got := getTheme(name); got.Name != name {
			t.Errorf("getTheme(%q).Name = %q, want %q", name, got.Name, name)
		}
	}
	// unknown / empty fall back to default
	for _, name := range []string{"", "bogus", "Default"} {
		if got := getTheme(name); got.Name != "default" {
			t.Errorf("getTheme(%q).Name = %q, want default", name, got.Name)
		}
	}
}

func TestNextTheme(t *testing.T) {
	// cycles through themeOrder in order and wraps around to the first
	for i, name := range themeOrder {
		want := themeOrder[(i+1)%len(themeOrder)]
		if got := nextTheme(name); got != want {
			t.Errorf("nextTheme(%q) = %q, want %q", name, got, want)
		}
	}
	// unknown name restarts at the first theme
	if got := nextTheme("bogus"); got != themeOrder[0] {
		t.Errorf("nextTheme(bogus) = %q, want %q", got, themeOrder[0])
	}
}
