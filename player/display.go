package main

import (
	"strings"

	"golang.org/x/text/unicode/bidi"
)

// mirrored glyphs that must flip when an RTL run is reversed for display.
var mirrored = map[rune]rune{
	'(': ')', ')': '(', '[': ']', ']': '[',
	'{': '}', '}': '{', '<': '>', '>': '<',
}

// vis converts a logical-order string to visual order for a terminal, which
// renders left-to-right and does no BiDi reordering of its own. Hebrew (and
// other RTL) runs come out readable. Pure-LTR strings are returned untouched.
func vis(s string) string {
	if !hasRTL(s) {
		return s
	}
	p := &bidi.Paragraph{}
	if _, err := p.SetString(s); err != nil {
		return s
	}
	order, err := p.Order()
	if err != nil {
		return s
	}
	var b strings.Builder
	for i := 0; i < order.NumRuns(); i++ {
		run := order.Run(i)
		rs := run.String()
		if run.Direction() == bidi.RightToLeft {
			rs = reverseMirror(rs)
		}
		b.WriteString(rs)
	}
	return b.String()
}

func hasRTL(s string) bool {
	for _, r := range s {
		if r >= 0x0590 && r <= 0x08FF { // Hebrew + Arabic blocks
			return true
		}
	}
	return false
}

func reverseMirror(s string) string {
	rr := []rune(s)
	for i, j := 0, len(rr)-1; i < j; i, j = i+1, j-1 {
		rr[i], rr[j] = rr[j], rr[i]
	}
	for i, r := range rr {
		if m, ok := mirrored[r]; ok {
			rr[i] = m
		}
	}
	return string(rr)
}
