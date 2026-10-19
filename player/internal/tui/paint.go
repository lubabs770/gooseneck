package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const reset = "\x1b[0m"

// bgSeq returns the SGR sequence that sets bg in the current color profile
// ("" for no color / no background).
func bgSeq(bg lipgloss.TerminalColor) string {
	if _, none := bg.(lipgloss.NoColor); none || bg == nil {
		return ""
	}
	s := lipgloss.NewStyle().Background(bg).Render("x")
	if i := strings.Index(s, "x"); i > 0 {
		return s[:i]
	}
	return ""
}

// paint fills a block with a background color edge to edge: every line is
// padded/truncated to exactly width, and the color is re-applied after every
// SGR reset inside the line, so styled spans (which end in a reset) don't punch
// holes in the background. Paint inner surfaces first, then outer ones: an
// inner surface's own color always follows the reset, so it wins.
func paint(block string, bg lipgloss.TerminalColor, width, height int) string {
	seq := bgSeq(bg)
	lines := strings.Split(block, "\n")
	if height > 0 {
		if len(lines) > height {
			lines = lines[:height]
		}
		for len(lines) < height {
			lines = append(lines, "")
		}
	}
	for i, ln := range lines {
		w := lipgloss.Width(ln)
		if w > width {
			ln = ansi.Truncate(ln, width, "")
			w = lipgloss.Width(ln)
		}
		if seq != "" {
			ln = strings.ReplaceAll(ln, reset, reset+seq)
			ln = strings.ReplaceAll(ln, "\x1b[m", "\x1b[m"+seq)
		}
		lines[i] = seq + ln + strings.Repeat(" ", max(0, width-w)) + reset
	}
	return strings.Join(lines, "\n")
}

// overlay draws box on top of base with its top-left corner at (x, y).
func overlay(base, box string, x, y int) string {
	bl := strings.Split(base, "\n")
	for i, line := range strings.Split(box, "\n") {
		row := y + i
		if row < 0 || row >= len(bl) {
			continue
		}
		w := lipgloss.Width(line)
		left := ansi.Truncate(bl[row], x, "")
		if pad := x - lipgloss.Width(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		right := ansi.TruncateLeft(bl[row], x+w, "")
		bl[row] = left + reset + line + reset + right
	}
	return strings.Join(bl, "\n")
}

// center places box in the middle of a w×h area (returns x, y).
func center(box string, w, h int) (int, int) {
	bw, bh := lipgloss.Size(box)
	return max(0, (w-bw)/2), max(0, (h-bh)/3)
}

// truncate shortens s (which may contain ANSI) to w cells with an ellipsis.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// padRight pads s with spaces to width w (truncating if longer).
func padRight(s string, w int) string {
	s = truncate(s, w)
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

// spread puts left and right on one line of width w, right-aligned.
func spread(left, right string, w int) string {
	rw := lipgloss.Width(right)
	left = truncate(left, max(0, w-rw-1))
	gap := w - lipgloss.Width(left) - rw
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// blankBlock is a w×h block of spaces.
func blankBlock(w, h int) string {
	if h <= 0 {
		return ""
	}
	line := strings.Repeat(" ", max(0, w))
	lines := make([]string, h)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// wrap word-wraps plain text to width w, returning at most n lines.
func wrap(s string, w, n int) []string {
	if w <= 0 {
		return nil
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case lipgloss.Width(line)+1+lipgloss.Width(word) <= w:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
		if n > 0 && len(out) == n {
			break
		}
	}
	if line != "" && (n <= 0 || len(out) < n) {
		out = append(out, line)
	}
	if n > 0 && len(out) == n && len(strings.Fields(s)) > 0 {
		joined := strings.Join(out, " ")
		if len(joined) < len(strings.Join(strings.Fields(s), " ")) {
			out[n-1] = truncate(out[n-1]+" …", w)
		}
	}
	for i := range out {
		out[i] = truncate(out[i], w)
	}
	return out
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
