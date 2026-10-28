package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lubabs770/gooseneck/player/internal/bidi"
	"github.com/lubabs770/gooseneck/player/internal/lyrics"
)

// lyricsPage shows the playing track's lyrics from LRCLIB. Synced lyrics
// follow playback with the sung line highlighted; plain ones scroll with j/k.
type lyricsPage struct {
	id      string // video id the lyrics belong to
	loading bool
	ly      *lyrics.Lyrics
	lines   []lyrics.Line
	err     error
	top     int // first line shown (plain lyrics)
}

type lyricsLoaded struct {
	id  string
	ly  *lyrics.Lyrics
	err error
}

func newLyricsPage(a *App) (*lyricsPage, tea.Cmd) {
	p := &lyricsPage{}
	return p, p.sync(a, false)
}

// sync fetches lyrics when the playing track changed (called on every poll).
func (p *lyricsPage) sync(a *App, fresh bool) tea.Cmd {
	if !a.playerUp || a.now.Idle || (a.now.VideoID == p.id && !fresh) {
		return nil
	}
	p.id, p.loading, p.ly, p.lines, p.err, p.top = a.now.VideoID, true, nil, nil, nil, 0
	a.busy++
	l, t := a.lib, a.now.Track
	return func() tea.Msg {
		ly, err := l.Lyrics(context.Background(), t, fresh)
		return lyricsLoaded{id: t.VideoID, ly: ly, err: err}
	}
}

func (p *lyricsPage) title() string { return "Lyrics" }
func (p *lyricsPage) mode() string  { return "LYRICS" }
func (p *lyricsPage) hints(a *App) []hint {
	return []hint{{"j/k", "scroll"}, {"R", "look up again"}}
}

func (p *lyricsPage) update(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case lyricsLoaded:
		if msg.id == p.id {
			p.loading, p.ly, p.err, p.lines = false, msg.ly, msg.err, msg.ly.Lines()
		}
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
			return p.sync(a, true)
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
	head := t.bold().Render(bidi.Vis(orDefault(np.Track.Title, np.Title)))
	if np.Track.Artist != "" {
		head += t.muted().Render(" · " + bidi.Vis(np.Track.Artist))
	}
	body := h - 2
	msg := ""
	switch {
	case p.loading:
		msg = "Looking up lyrics on lrclib.net…"
	case p.err != nil:
		msg = "Couldn't reach lrclib.net: " + p.err.Error()
	case p.ly != nil && p.ly.Instrumental:
		msg = "♪ Instrumental"
	case len(p.lines) == 0:
		msg = "No lyrics on lrclib.net for this track. Add them there and press R."
	}
	if msg != "" {
		return head + "\n\n" + t.muted().Render(msg)
	}

	cur := lyrics.Current(p.lines, np.Pos)
	synced := p.lines[0].At >= 0
	if synced { // keep the sung line a third of the way down
		p.top = max(0, cur-body/3)
	}
	p.top = min(p.top, max(0, len(p.lines)-body))
	var rows []string
	for i := p.top; i < len(p.lines) && len(rows) < body; i++ {
		st := t.text()
		switch {
		case i == cur:
			st = t.primary().Bold(true)
		case synced && i < cur:
			st = t.subtle()
		case synced:
			st = t.muted()
		}
		rows = append(rows, lipgloss.PlaceHorizontal(w, lipgloss.Center, st.Render(truncate(bidi.Vis(p.lines[i].Text), w))))
	}
	return head + "\n\n" + strings.Join(rows, "\n")
}
