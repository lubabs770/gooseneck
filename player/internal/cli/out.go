package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/lubabs770/gooseneck/player/internal/bidi"
	"github.com/muesli/termenv"
)

// printer writes human output (styled on a terminal, plain when piped) or JSON.
type printer struct {
	w     io.Writer
	tty   bool
	json  bool
	width int
	r     *lipgloss.Renderer

	title, accent, dim, ok, warn, bad, badge lipgloss.Style
}

func newPrinter(w io.Writer, asJSON, noColor bool) *printer {
	p := &printer{w: w, json: asJSON, width: 100}
	if f, ok := w.(*os.File); ok && term.IsTerminal(f.Fd()) {
		p.tty = true
		if wd, _, err := term.GetSize(f.Fd()); err == nil && wd > 20 {
			p.width = wd
		}
	}
	p.r = lipgloss.NewRenderer(w)
	if noColor || os.Getenv("NO_COLOR") != "" || !p.tty {
		p.r.SetColorProfile(termenv.Ascii)
	}
	accent := lipgloss.Color("#fab283")
	p.title = p.r.NewStyle().Bold(true).Foreground(lipgloss.Color("#eeeeee"))
	p.accent = p.r.NewStyle().Foreground(accent)
	p.dim = p.r.NewStyle().Foreground(lipgloss.Color("#808080"))
	p.ok = p.r.NewStyle().Foreground(lipgloss.Color("#7fd88f"))
	p.warn = p.r.NewStyle().Foreground(lipgloss.Color("#f5a742"))
	p.bad = p.r.NewStyle().Foreground(lipgloss.Color("#e06c75"))
	p.badge = p.r.NewStyle().Bold(true).Foreground(lipgloss.Color("#0a0a0a")).Background(accent).Padding(0, 1)
	return p
}

func (p *printer) println(a ...any)                     { fmt.Fprintln(p.w, a...) }
func (p *printer) printf(f string, a ...any)            { fmt.Fprintf(p.w, f, a...) }
func (p *printer) S(st lipgloss.Style, s string) string { return st.Render(s) }

// JSON writes v indented. Used for every --json output.
func (p *printer) JSON(v any) error {
	enc := json.NewEncoder(p.w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// heading prints a bold title with an optional dim suffix.
func (p *printer) heading(title, suffix string) {
	line := p.title.Render(bidi.Vis(title))
	if suffix != "" {
		line += "  " + p.dim.Render(suffix)
	}
	p.println(line)
}

// row prints "  left ......... right" fitted to the terminal width.
func (p *printer) row(prefix, left, right string) {
	left = bidi.Vis(left)
	if !p.tty {
		parts := []string{}
		for _, s := range []string{strings.TrimSpace(ansi.Strip(prefix)), left, ansi.Strip(right)} {
			if s != "" {
				parts = append(parts, s)
			}
		}
		p.println(strings.Join(parts, "\t"))
		return
	}
	avail := p.width - lipgloss.Width(prefix) - lipgloss.Width(right) - 2
	if avail < 10 {
		avail = 10
	}
	left = ansi.Truncate(left, avail, "…")
	gap := p.width - lipgloss.Width(prefix) - lipgloss.Width(left) - lipgloss.Width(right)
	if right == "" || gap < 2 {
		gap = 2
	}
	p.println(prefix + left + strings.Repeat(" ", gap) + right)
}

// bar draws a progress bar of width w.
func (p *printer) bar(frac float64, w int) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	n := int(frac * float64(w))
	return p.accent.Render(strings.Repeat("━", n)) + p.dim.Render(strings.Repeat("─", w-n))
}
