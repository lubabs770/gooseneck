package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lubabs770/gooseneck/player/internal/mpv"
)

// Mouse support. Views wrap clickable things in bubblezone markers
// (a.mark); View() strips them with a.zones.Scan, which records where each
// one ended up on screen. A left click is then matched against those zones:
// the status bar (home, breadcrumbs), progress bars (seek), the search
// prompt, the page's own tabs (clicker), and finally the page's collection.
// The wheel still goes to the page (scrollMouse).

// doubleClick is the longest gap between two clicks on the same entry that
// counts as a double click (which acts like enter).
const doubleClick = 400 * time.Millisecond

type lastClick struct {
	c   *collection
	idx int
	at  time.Time
}

// clicker pages handle clicks on their own controls (tabs, chips). ok
// reports whether the click hit one.
type clicker interface {
	click(a *App, msg tea.MouseMsg) (cmd tea.Cmd, ok bool)
}

// mark wraps s in a clickable zone named id.
func (a *App) mark(id, s string) string { return a.zones.Mark(id, s) }

// hit reports whether a mouse event falls inside zone id (as last rendered).
func (a *App) hit(id string, msg tea.MouseMsg) bool { return a.zones.Get(id).InBounds(msg) }

// hitIndex returns the first i < n whose zone fmt.Sprintf(format, i) contains
// the event.
func (a *App) hitIndex(format string, n int, msg tea.MouseMsg) (int, bool) {
	for i := 0; i < n; i++ {
		if a.hit(fmt.Sprintf(format, i), msg) {
			return i, true
		}
	}
	return 0, false
}

func (a *App) mouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return a.top().update(a, msg) // wheel scrolling
	}
	a.quitWarn = false

	// status bar: the badge goes home, a breadcrumb goes back to that page
	if a.hit("home", msg) {
		a.goTo(nil)
		return nil
	}
	if i, ok := a.hitIndex("crumb:%d", len(a.stack), msg); ok {
		a.popTo(i)
		return nil
	}
	for _, id := range []string{"seek:side", "seek:line"} {
		if z := a.zones.Get(id); z.InBounds(msg) {
			x, _ := z.Pos(msg)
			return a.seekFrac((float64(x) + 0.5) / float64(z.EndX-z.StartX+1))
		}
	}
	if a.hit("prompt", msg) {
		if !a.searching {
			return a.focusSearch()
		}
		return nil
	}
	if cp, ok := a.top().(clicker); ok {
		if cmd, ok := cp.click(a, msg); ok {
			if f, ok := a.top().(filterable); ok && a.searching {
				f.setFilter(a.input.Value()) // as with tab: the new section takes the search
			}
			return cmd
		}
	}
	c := pageCollection(a.top())
	if c == nil {
		return nil
	}
	i, ok := c.click(a, msg)
	if !ok {
		return nil
	}
	now := time.Now()
	double := a.click.c == c && a.click.idx == i && now.Sub(a.click.at) < doubleClick
	a.click = lastClick{c, i, now}
	if _, home := a.top().(*homePage); !home {
		a.blurSearch() // the list has focus now; the filter stays
	}
	if double {
		a.click = lastClick{} // a third click starts over
		return a.top().update(a, tea.KeyMsg{Type: tea.KeyEnter})
	}
	return nil
}

// seekFrac seeks to a fraction (0–1) of the current track.
func (a *App) seekFrac(frac float64) tea.Cmd {
	if !a.playerUp || a.now.Idle || a.now.Duration <= 0 {
		return nil
	}
	pos := clampF(frac, 0, 1) * a.now.Duration
	a.now.Pos = pos // move the bar now; the next poll confirms it
	return a.control(func(p *mpv.Player) (string, error) { return "", p.Seek(pos, true) })
}

func clampF(v, lo, hi float64) float64 {
	return min(max(v, lo), hi)
}

// zone names the screen slot of the slot'th entry drawn by this collection.
func (c *collection) zone(slot int) string { return fmt.Sprintf("item:%d:%d", c.id, slot) }

// click moves the cursor to the entry under a click; it returns that entry's
// index in the visible list.
func (c *collection) click(a *App, msg tea.MouseMsg) (int, bool) {
	for slot, i := range c.drawn {
		if i < len(c.vis) && a.hit(c.zone(slot), msg) {
			c.cursor = i
			return i, true
		}
	}
	return 0, false
}
