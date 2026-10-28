package tui

import "testing"

func TestMarquee(t *testing.T) {
	s := "abcdefghij" // 10 cells into a 4-cell window
	for _, c := range []struct {
		elapsed float64
		want    string
	}{{0, "abcd"}, {4, "defg"}, {8, "ghij"}, {20, "ghij"}} {
		if got := marquee(s, 4, c.elapsed, 10); got != c.want {
			t.Errorf("marquee(%v) = %q, want %q", c.elapsed, got, c.want)
		}
	}
	if got := marquee("short", 10, 3, 10); got != "short" {
		t.Errorf("short line scrolled: %q", got)
	}
	if got := marquee("אבגדהוזחטי", 4, 0, 10); got != "זחטי" { // visual order: starts at the right
		t.Errorf("rtl start = %q", got)
	}
}
