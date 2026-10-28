package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lubabs770/gooseneck/player/internal/bidi"
	"github.com/lubabs770/gooseneck/player/internal/lyrics"
)

// lyricsState holds the playing track's lyrics from LRCLIB, shared by the
// lyrics page, the sidebar section and the line under the status bar.
type lyricsState struct {
	id      string // video id the lyrics belong to
	loading bool
	ly      *lyrics.Lyrics
	lines   []lyrics.Line
	err     error
}

type lyricsLoaded struct {
	id  string
	ly  *lyrics.Lyrics
	err error
}

// syncLyrics fetches lyrics on track change when something shows them
// (called on every poll).
func (a *App) syncLyrics() tea.Cmd {
	if _, onPage := a.top().(*lyricsPage); a.lyricsAt == "off" && !onPage {
		return nil
	}
	return a.fetchLyrics(false)
}

// fetchLyrics looks the playing track up unless its lyrics are already here.
func (a *App) fetchLyrics(fresh bool) tea.Cmd {
	if !a.playerUp || a.now.Idle || (a.now.VideoID == a.lyr.id && !fresh) {
		return nil
	}
	a.lyr = lyricsState{id: a.now.VideoID, loading: true}
	a.busy++
	l, t := a.lib, a.now.Track
	return func() tea.Msg {
		ly, err := l.Lyrics(context.Background(), t, fresh)
		return lyricsLoaded{id: t.VideoID, ly: ly, err: err}
	}
}

// cycleLyrics moves live lyrics off → sidebar → line → off (session only).
func (a *App) cycleLyrics() tea.Cmd {
	a.lyricsAt = map[string]string{"off": "sidebar", "sidebar": "line", "line": "off"}[a.lyricsAt]
	if a.lyricsAt == "sidebar" {
		a.sidebar = true
	}
	a.notify("live lyrics: "+a.lyricsAt, false)
	return a.syncLyrics()
}

// lyricsNote explains why there's nothing to show ("" when there are lines).
func (a *App) lyricsNote() string {
	switch s := a.lyr; {
	case s.loading:
		return "Looking up lyrics on lrclib.net…"
	case s.err != nil:
		return "Couldn't reach lrclib.net: " + s.err.Error()
	case s.ly != nil && s.ly.Instrumental:
		return "♪ Instrumental"
	case len(s.lines) == 0:
		return "No lyrics on lrclib.net for this track."
	}
	return ""
}

func (a *App) synced() bool { return len(a.lyr.lines) > 0 && a.lyr.lines[0].At >= 0 }

// sideLyrics is the sidebar's LYRICS section: the sung line and a few around it.
func (a *App) sideLyrics(w, h int) []string {
	t := a.theme
	out := []string{"", t.subtle().Bold(true).Render("LYRICS"), ""}
	if note := a.lyricsNote(); note != "" {
		return append(out, t.muted().Render(truncate(note, w)))
	}
	if !a.synced() {
		return append(out, t.muted().Render(truncate("Not synced · 6 to read them", w)))
	}
	ls := a.lyr.lines
	rows := clamp(h/6, 3, 7)
	cur := lyrics.Current(ls, a.now.Pos)
	first := clamp(cur-1, 0, max(0, len(ls)-rows))
	for i := first; i < first+rows; i++ {
		if i >= len(ls) {
			out = append(out, "")
			continue
		}
		st := t.muted()
		if i == cur {
			st = t.primary().Bold(true)
		}
		out = append(out, st.Render(truncate(bidi.Vis(ls[i].Text), w)))
	}
	return out
}

// lyricLine is the one-line lyrics strip under the status bar. A line too long
// for the terminal scrolls across while it's sung.
func (a *App) lyricLine(w int) string {
	t := a.theme
	inner := w - 4
	text, st := a.lyricsNote(), t.muted()
	if text == "" && !a.synced() {
		text = "Lyrics aren't synced for this track · 6 to read them"
	}
	if text == "" {
		ls := a.lyr.lines
		cur := lyrics.Current(ls, a.now.Pos)
		text, st = "♪", t.subtle()
		if cur >= 0 && ls[cur].Text != "" {
			end := a.now.Duration
			if cur+1 < len(ls) {
				end = ls[cur+1].At
			}
			text, st = marquee(bidi.Vis(ls[cur].Text), inner, a.now.Pos-ls[cur].At, end-ls[cur].At), t.primary().Bold(true)
		}
	}
	line := lipgloss.PlaceHorizontal(inner, lipgloss.Center, st.Render(truncate(text, inner)))
	return paint("  "+line, t.Bg, w, 1)
}

// marquee returns the w-cell window of s for elapsed seconds into a line that
// lasts span: it scrolls in reading order and finishes at 80% of the line.
// Hebrew arrives in visual order (reversed), so it scrolls the other way.
func marquee(s string, w int, elapsed, span float64) string {
	r := []rune(s)
	if len(r) <= w || w <= 0 {
		return s
	}
	f := 0.0
	if span > 0 {
		f = min(1, max(0, elapsed/span/0.8))
	}
	off := int(f * float64(len(r)-w))
	if bidi.HasRTL(s) {
		off = len(r) - w - off
	}
	return string(r[off : off+w])
}

// ---- page ------------------------------------------------------------------------

// lyricsPage shows all the lyrics. Synced ones follow playback with the sung
// line highlighted; plain ones scroll with j/k.
type lyricsPage struct {
	top   int    // first line shown
	shown string // video id top belongs to
}

func newLyricsPage(a *App) (*lyricsPage, tea.Cmd) {
	return &lyricsPage{}, a.fetchLyrics(false)
}

func (p *lyricsPage) title() string { return "Lyrics" }
func (p *lyricsPage) mode() string  { return "LYRICS" }
func (p *lyricsPage) hints(a *App) []hint {
	return []hint{{"j/k", "scroll"}, {"R", "look up again"}, {"y", "live lyrics"}}
}

func (p *lyricsPage) update(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			p.top = max(0, p.top-3)
		case tea.MouseButtonWheelDown:
			p.top += 3
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			p.top++
		case "k", "up":
			p.top = max(0, p.top-1)
		case "g", "home":
			p.top = 0
		case "R":
			return a.fetchLyrics(true)
		}
	}
	return nil
}

func (p *lyricsPage) view(a *App, w, h int) string {
	t := a.theme
	np := a.now
	if !a.playerUp || np.Idle {
		return t.muted().Render("Nothing playing. Lyrics show up here for the current track.")
	}
	if p.shown != a.lyr.id {
		p.shown, p.top = a.lyr.id, 0
	}
	head := t.bold().Render(bidi.Vis(orDefault(np.Track.Title, np.Title)))
	if np.Track.Artist != "" {
		head += t.muted().Render(" · " + bidi.Vis(np.Track.Artist))
	}
	if note := a.lyricsNote(); note != "" {
		if !a.lyr.loading && a.lyr.err == nil && a.lyr.ly == nil {
			note += " Add them there and press R."
		}
		return head + "\n\n" + t.muted().Render(note)
	}

	body := h - 2
	lines := a.lyr.lines
	cur := lyrics.Current(lines, np.Pos)
	synced := a.synced()
	if synced { // keep the sung line a third of the way down
		p.top = max(0, cur-body/3)
	}
	p.top = min(p.top, max(0, len(lines)-body))
	var rows []string
	for i := p.top; i < len(lines) && len(rows) < body; i++ {
		st := t.text()
		switch {
		case i == cur:
			st = t.primary().Bold(true)
		case synced && i < cur:
			st = t.subtle()
		case synced:
			st = t.muted()
		}
		rows = append(rows, lipgloss.PlaceHorizontal(w, lipgloss.Center, st.Render(truncate(bidi.Vis(lines[i].Text), w))))
	}
	return head + "\n\n" + strings.Join(rows, "\n")
}
