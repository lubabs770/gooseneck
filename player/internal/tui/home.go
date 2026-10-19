package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lubabs770/gooseneck/player/internal/catalog"
	"github.com/lubabs770/gooseneck/player/internal/mpv"
	"github.com/lubabs770/gooseneck/player/internal/store"
)

// font is a 4×3 block alphabet for the logo (only the letters we need).
var font = map[rune][3]string{
	'g': {"█▀▀▀", "█ ▀█", "▀▀▀▀"},
	'o': {"█▀▀█", "█  █", "▀▀▀▀"},
	's': {"█▀▀▀", "▀▀▀█", "▀▀▀▀"},
	'e': {"█▀▀▀", "█▀▀ ", "▀▀▀▀"},
	'n': {"█▀▀▄", "█  █", "▀  ▀"},
	'c': {"█▀▀▀", "█   ", "▀▀▀▀"},
	'k': {"█  █", "█▀▀▄", "▀  ▀"},
}

// logo renders "goose" muted and "neck" bright, like OpenCode's open|code.
func logo(t Theme) string {
	rows := make([]string, 3)
	for i, r := range "gooseneck" {
		st := t.muted()
		if i >= 5 {
			st = t.bold()
		}
		for y := 0; y < 3; y++ {
			sep := " "
			if i == 0 {
				sep = ""
			}
			rows[y] += sep + st.Render(font[r][y])
		}
	}
	return strings.Join(rows, "\n")
}

// homePage: logo, the search prompt, and live results (artists, or commands
// when the query starts with "/").
type homePage struct {
	filter  string
	shown   string // query the results were last built for
	results *collection
	recent  []store.ArtistStat
}

func newHome(a *App) *homePage {
	h := &homePage{results: newCollection(false, true, "")}
	h.results.empty = "no artists match"
	h.refresh(a)
	return h
}

func (h *homePage) title() string       { return "Home" }
func (h *homePage) mode() string        { return "HOME" }
func (h *homePage) coll() *collection   { return h.results }
func (h *homePage) filterText() string  { return h.filter }
func (h *homePage) placeholder() string { return "Search artists, or / for commands…" }
func (h *homePage) setFilter(f string)  { h.filter = f }

func (h *homePage) hints(a *App) []hint {
	if a.searching {
		return []hint{{"enter", "open"}, {"↑↓", "select"}, {"esc", "shortcuts"}}
	}
	return []hint{{"/", "search"}, {"2", "library"}, {"?", "help"}}
}

// refresh rebuilds the result list for the current query.
func (h *homePage) refresh(a *App) {
	q := strings.TrimSpace(h.filter)
	var items []entry
	switch {
	case strings.HasPrefix(q, "/"):
		h.results.images = false
		for _, c := range catalog.Rank(strings.TrimPrefix(q, "/"), commands(a), func(c command) string { return c.name }) {
			c := c
			items = append(items, entry{key: c.name, title: "/" + c.name, sub: c.desc, right: c.key, data: c})
		}
	case q == "":
		h.results.images = true
		h.recent, _ = a.lib.DB.RecentArtists(6)
		for _, r := range h.recent {
			ar, ok := a.lib.Cat.Get(r.ArtistID)
			if !ok {
				ar = catalog.Artist{ID: r.ArtistID, Name: r.Artist}
			}
			items = append(items, entry{key: ar.ID, title: ar.Name, sub: fmt.Sprintf("played %s", agoShort(r.Last)), img: ar.Thumbnail, data: ar})
		}
	default:
		h.results.images = true
		res := a.lib.Cat.Search(q, nil)
		for i, ar := range res {
			if i == 50 {
				break
			}
			items = append(items, artistEntry(ar))
		}
	}
	h.results.set(items)
	h.results.cursor, h.results.top = 0, 0
	h.shown = h.filter
}

func artistEntry(ar catalog.Artist) entry {
	return entry{key: ar.ID, title: ar.Name, sub: strings.Join(ar.TagLabels(), " · "), img: ar.Thumbnail, data: ar}
}

func (h *homePage) update(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		k := msg.String()
		if h.results.key(k, true) {
			return nil
		}
		switch k {
		case "enter", "l", "right":
			return h.open(a)
		case "p":
			if e, ok := h.results.selected(); ok {
				if ar, ok := e.data.(catalog.Artist); ok {
					return a.playArtist(ar.ID, ar.Name, mpv.Replace, false)
				}
			}
		case "s":
			if e, ok := h.results.selected(); ok {
				if ar, ok := e.data.(catalog.Artist); ok {
					return a.playArtist(ar.ID, ar.Name, mpv.Replace, true)
				}
			}
		}
	case tea.MouseMsg:
		scrollMouse(h.results, msg)
	}
	return nil
}

func (h *homePage) open(a *App) tea.Cmd {
	e, ok := h.results.selected()
	if !ok {
		return nil
	}
	switch d := e.data.(type) {
	case catalog.Artist:
		p, cmd := newArtistPage(a, d.ID, d.Name)
		a.push(p)
		return cmd
	case command:
		h.filter = ""
		a.input.SetValue("")
		a.blurSearch()
		return d.run(a)
	}
	return nil
}

func (h *homePage) view(a *App, w, hgt int) string {
	t := a.theme
	if h.shown != h.filter { // setFilter has no App, so results catch up here
		h.refresh(a)
	}
	colW := min(w, 76)
	var parts []string
	lg := logo(t)
	tag := t.muted().Render(fmt.Sprintf("%d artists", a.lib.Cat.Len()))
	if a.opts.Version != "" {
		tag = t.subtle().Render(a.opts.Version) + t.muted().Render("  ·  ") + tag
	}
	parts = append(parts, lipgloss.PlaceHorizontal(colW, lipgloss.Center, lg), "",
		lipgloss.PlaceHorizontal(colW, lipgloss.Center, tag), "")
	parts = append(parts, a.homePrompt(colW))

	used := lipgloss.Height(strings.Join(parts, "\n"))
	q := strings.TrimSpace(h.filter)
	label := ""
	switch {
	case strings.HasPrefix(q, "/"):
		label = "COMMANDS"
	case q == "" && len(h.recent) > 0:
		label = "RECENTLY PLAYED"
	case q != "":
		label = fmt.Sprintf("ARTISTS · %d", len(a.lib.Cat.Search(q, nil)))
	}
	topPad := max(1, (hgt-used-14)/3)
	if label != "" {
		parts = append(parts, "", t.subtle().Bold(true).Render(label))
		listH := max(2, hgt-topPad-used-3)
		h.results.grid = false
		parts = append(parts, paint(h.results.view(a, colW, listH), t.Bg, colW, 0))
	} else if q == "" {
		parts = append(parts, "", h.tips(a, colW))
	}
	// left-align inside the column, then center the column as one block
	block := paint(strings.Join(parts, "\n"), t.Bg, colW, 0)
	return strings.Repeat("\n", topPad) + indent(block, max(0, (w-colW)/2))
}

func (h *homePage) tips(a *App, w int) string {
	t := a.theme
	rows := [][2]string{
		{"type", "search ~" + fmt.Sprint(a.lib.Cat.Len()) + " artists"},
		{"/", "commands (try /library, /theme)"},
		{"2", "browse the library by tag"},
		{"space", "play / pause from anywhere"},
		{"ctrl+p", "command palette"},
	}
	var out []string
	for _, r := range rows {
		out = append(out, "  "+t.primary().Render(padRight(r[0], 8))+t.muted().Render(r[1]))
	}
	return strings.Join(out, "\n")
}

// homePrompt is the large centered prompt on the home screen.
func (a *App) homePrompt(w int) string {
	t := a.theme
	barSt := fg(t.Primary).Background(t.Element)
	if !a.searching {
		barSt = fg(t.Border).Background(t.Element)
	}
	a.input.Width = max(10, w-6)
	in := a.input.View()
	if !a.searching {
		v := a.input.Value()
		if v == "" {
			in = fg(t.Subtle).Background(t.Element).Render(a.input.Placeholder)
		} else {
			in = fg(t.Text).Background(t.Element).Render(v)
		}
	}
	lines := []string{"", in, ""}
	for i, l := range lines {
		lines[i] = barSt.Render("┃") + "  " + l
	}
	box := paint(strings.Join(lines, "\n"), t.Element, w, 3)
	var hs []hint
	if a.searching {
		hs = []hint{{"enter", "open"}, {"↑↓", "select"}, {"esc", "shortcuts"}}
	} else {
		hs = []hint{{"/", "search"}, {"ctrl+p", "commands"}, {"q", "quit"}}
	}
	return box + "\n" + a.renderHints(hs)
}

func scrollMouse(c *collection, msg tea.MouseMsg) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		c.scroll(-1)
	case tea.MouseButtonWheelDown:
		c.scroll(1)
	}
}
