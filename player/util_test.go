package main

import "testing"

func TestClamp(t *testing.T) {
	cases := []struct{ v, lo, hi, want int }{
		{5, 0, 10, 5},   // in range
		{-3, 0, 10, 0},  // below floor
		{99, 0, 10, 10}, // above ceiling
		{5, 10, 0, 10},  // inverted range returns lo
		{0, 0, 0, 0},    // degenerate
	}
	for _, c := range cases {
		if got := clamp(c.v, c.lo, c.hi); got != c.want {
			t.Errorf("clamp(%d,%d,%d) = %d, want %d", c.v, c.lo, c.hi, got, c.want)
		}
	}
}

func TestWindowStart(t *testing.T) {
	cases := []struct{ cursor, h, n, want int }{
		{0, 10, 5, 0},  // fewer items than window: pinned to top
		{0, 4, 10, 0},  // cursor at top clamps start to 0
		{5, 4, 10, 3},  // centered: cursor - h/2
		{9, 4, 10, 6},  // near bottom clamps start to n-h
		{10, 4, 10, 6}, // cursor past end still clamped to n-h
	}
	for _, c := range cases {
		if got := windowStart(c.cursor, c.h, c.n); got != c.want {
			t.Errorf("windowStart(%d,%d,%d) = %d, want %d", c.cursor, c.h, c.n, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 10, "hello"}, // shorter than limit: untouched
		{"hello", 5, "hello"},  // exactly the limit: untouched
		{"hello", 3, "he…"},    // trimmed with ellipsis
		{"hello", 1, "h"},      // n<=1: hard cut, no ellipsis
		{"hello", 0, ""},       // non-positive: empty
		{"héllo", 3, "hé…"},    // counts runes, not bytes
	}
	for _, c := range cases {
		if got := truncate(c.s, c.n); got != c.want {
			t.Errorf("truncate(%q,%d) = %q, want %q", c.s, c.n, got, c.want)
		}
	}
}

func TestPad(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"ab", 5, "ab   "}, // padded to width
		{"ab", 2, "ab"},    // already at width
		{"abc", 2, "abc"},  // wider than width: untouched
		{"é", 3, "é  "},    // rune width, not byte width
	}
	for _, c := range cases {
		if got := pad(c.s, c.n); got != c.want {
			t.Errorf("pad(%q,%d) = %q, want %q", c.s, c.n, got, c.want)
		}
	}
}

func TestNormView(t *testing.T) {
	if got := normView("list"); got != "list" {
		t.Errorf("normView(list) = %q, want list", got)
	}
	for _, v := range []string{"grid", "", "bogus"} {
		if got := normView(v); got != "grid" {
			t.Errorf("normView(%q) = %q, want grid", v, got)
		}
	}
}

func TestSquareThumbURL(t *testing.T) {
	cases := []struct {
		url  string
		px   int
		want string
	}{
		// Google image URL: sizing suffix after '=' is replaced.
		{"https://yt3.googleusercontent.com/abc=w2880-h1200-p", 320,
			"https://yt3.googleusercontent.com/abc=s320-c"},
		{"https://i.ggpht.com/xyz=s88-c-k", 200, "https://i.ggpht.com/xyz=s200-c"},
		// Non-Google host: returned unchanged even with an '='.
		{"https://example.com/pic.jpg=w100", 320, "https://example.com/pic.jpg=w100"},
		// No '=' at all: returned unchanged.
		{"https://yt3.googleusercontent.com/abc", 320, "https://yt3.googleusercontent.com/abc"},
	}
	for _, c := range cases {
		if got := squareThumbURL(c.url, c.px); got != c.want {
			t.Errorf("squareThumbURL(%q,%d) = %q, want %q", c.url, c.px, got, c.want)
		}
	}
}
