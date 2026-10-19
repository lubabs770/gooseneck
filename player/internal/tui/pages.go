package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lubabs770/gooseneck/player/internal/bidi"
	"github.com/lubabs770/gooseneck/player/internal/catalog"
	"github.com/lubabs770/gooseneck/player/internal/library"
	"github.com/lubabs770/gooseneck/player/internal/mpv"
	"github.com/lubabs770/gooseneck/player/internal/store"
	"github.com/lubabs770/gooseneck/player/internal/ytm"
)

// ---- library: every artist, filterable by tag ------------------------------------

type libraryPage struct {
	tag   int // 0 = all, else catalog.Tags[tag-1]
	items *collection
}

func newLibrary(a *App) *libraryPage {
	p := &libraryPage{items: newCollection(true, true, "no artists")}
	p.items.grid = a.lib.Cfg.View != "list"
	p.load(a)
	return p
}

func (p *libraryPage) load(a *App) {
	var tg *catalog.Tag
	if p.tag > 0 {
		tg = &catalog.Tags[p.tag-1]
	}
	list := a.lib.Cat.Filter(tg)
	items := make([]entry, len(list))
	for i, ar := range list {
		items[i] = artistEntry(ar)
	}
	p.items.set(items)
}

func (p *libraryPage) title() string       { return "Library" }
func (p *libraryPage) mode() string        { return "LIBRARY" }
func (p *libraryPage) coll() *collection   { return p.items }
func (p *libraryPage) filterText() string  { return p.items.filter }
func (p *libraryPage) setFilter(f string)  { p.items.setFilter(f) }
func (p *libraryPage) placeholder() string { return "Filter artists…" }
func (p *libraryPage) hints(a *App) []hint {
	return []hint{{"enter", "open"}, {"p", "play"}, {"tab", "tag"}, {"v", "view"}}
}

func (p *libraryPage) update(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		k := msg.String()
		if p.items.key(k, !p.items.grid) {
			return nil
		}
		switch k {
		case "tab", "shift+tab":
			d := 1
			if k == "shift+tab" {
				d = -1
			}
			p.tag = (p.tag + d + len(catalog.Tags) + 1) % (len(catalog.Tags) + 1)
			p.items.cursor, p.items.top = 0, 0
			p.load(a)
		case "enter":
			return openArtistEntry(a, p.items)
		case "p", "s", "a":
			return playArtistEntry(a, p.items, k)
		}
	case tea.MouseMsg:
		scrollMouse(p.items, msg)
	}
	return nil
}

func openArtistEntry(a *App, c *collection) tea.Cmd {
	e, ok := c.selected()
	if !ok {
		return nil
	}
	ar := e.data.(catalog.Artist)
	pg, cmd := newArtistPage(a, ar.ID, ar.Name)
	a.push(pg)
	return cmd
}

func playArtistEntry(a *App, c *collection, k string) tea.Cmd {
	e, ok := c.selected()
	if !ok {
		return nil
	}
	ar := e.data.(catalog.Artist)
	switch k {
	case "s":
		return a.playArtist(ar.ID, ar.Name, mpv.Replace, true)
	case "a":
		return a.playArtist(ar.ID, ar.Name, mpv.Append, false)
	}
	return a.playArtist(ar.ID, ar.Name, mpv.Replace, false)
}

func (p *libraryPage) view(a *App, w, h int) string {
	t := a.theme
	// tag chips
	var chips []string
	labels := append([]string{"All"}, func() []string {
		var out []string
		for _, tg := range catalog.Tags {
			out = append(out, tg.Label)
		}
		return out
	}()...)
	for i, l := range labels {
		if i == p.tag {
			chips = append(chips, t.onPrimary().Render(" "+l+" "))
		} else {
			chips = append(chips, t.muted().Render(" "+l+" "))
		}
	}
	chipLine := truncate(strings.Join(chips, " "), w)
	head := spread(t.bold().Render("Library"), t.muted().Render(fmt.Sprintf("%d artists", p.items.len())), w)
	body := p.items.view(a, w, h-4)
	return head + "\n" + chipLine + "\n\n" + body
}

// ---- artist page ---------------------------------------------------------------

type artistPage struct {
	id, name string
	artist   *ytm.Artist
	err      error
	loading  bool
	tab      int
	tabs     []artistTab
	cat      catalog.Artist
}

type artistTab struct {
	label string
	items *collection
}

func newArtistPage(a *App, id, name string) (*artistPage, tea.Cmd) {
	p := &artistPage{id: id, name: name}
	p.cat, _ = a.lib.Cat.Get(id)
	if cached, ok := a.lib.CachedArtist(id); ok {
		p.setArtist(a, cached) // instant, then refresh in the background if stale
	}
	return p, p.load(a, false)
}

func (p *artistPage) load(a *App, fresh bool) tea.Cmd {
	p.loading = true
	a.busy++
	l, id := a.lib, p.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		ar, err := l.Artist(ctx, id, fresh)
		return artistLoaded{id: id, page: ar, err: err}
	}
}

func (p *artistPage) setArtist(a *App, ar *ytm.Artist) {
	p.artist = ar
	p.name = ar.Name
	prevTab := p.tab
	p.tabs = nil
	rel := func(rs []ytm.Release) []entry {
		out := make([]entry, len(rs))
		for i, r := range rs {
			sub := strings.TrimSpace(r.Kind + " · " + r.Year)
			sub = strings.Trim(sub, " ·")
			out[i] = entry{key: r.BrowseID, title: r.Title, sub: sub, right: r.Year, img: r.Thumb, data: r}
		}
		return out
	}
	if len(ar.Albums) > 0 {
		c := newCollection(true, true, "")
		c.grid = a.lib.Cfg.View != "list"
		c.set(rel(ar.Albums))
		label := "Albums"
		if ar.Albums[0].Kind == "Uploads" {
			label = "Uploads"
		}
		p.tabs = append(p.tabs, artistTab{label, c})
	}
	if len(ar.Singles) > 0 {
		c := newCollection(true, true, "")
		c.grid = a.lib.Cfg.View != "list"
		c.set(rel(ar.Singles))
		p.tabs = append(p.tabs, artistTab{"Singles & EPs", c})
	}
	if len(ar.TopSongs) > 0 {
		c := newCollection(false, false, "")
		var items []entry
		for _, s := range ar.TopSongs {
			items = append(items, songEntry(s, false))
		}
		c.set(items)
		p.tabs = append(p.tabs, artistTab{"Top songs", c})
	}
	if len(ar.Related) > 0 {
		c := newCollection(true, true, "")
		c.grid = a.lib.Cfg.View != "list"
		var items []entry
		for _, r := range ar.Related {
			cat, ok := a.lib.Cat.Get(r.ID)
			if !ok {
				cat = catalog.Artist{ID: r.ID, Name: r.Name, Thumbnail: r.Thumb}
			}
			items = append(items, entry{key: r.ID, title: r.Name, sub: r.Subtitle, img: r.Thumb, data: cat})
		}
		c.set(items)
		p.tabs = append(p.tabs, artistTab{"Fans also like", c})
	}
	p.tab = clamp(prevTab, 0, len(p.tabs)-1)
}

func songEntry(s ytm.Song, numbered bool) entry {
	right := ""
	if s.Duration > 0 {
		right = ytm.FormatDuration(s.Duration)
	}
	sub := s.Plays
	if !numbered && s.Album != "" {
		sub = s.Album
	}
	return entry{key: s.VideoID, title: s.Title, sub: sub, right: right, num: fmt.Sprint(s.Index), data: s}
}

func (p *artistPage) title() string { return p.name }
func (p *artistPage) mode() string  { return "ARTIST" }
func (p *artistPage) coll() *collection {
	if p.tab < len(p.tabs) {
		return p.tabs[p.tab].items
	}
	return nil
}
func (p *artistPage) filterText() string {
	if c := p.coll(); c != nil {
		return c.filter
	}
	return ""
}
func (p *artistPage) setFilter(f string) {
	if c := p.coll(); c != nil {
		c.setFilter(f)
	}
}
func (p *artistPage) placeholder() string { return "Filter " + strings.ToLower(p.tabLabel()) + "…" }
func (p *artistPage) tabLabel() string {
	if p.tab < len(p.tabs) {
		return p.tabs[p.tab].label
	}
	return ""
}

func (p *artistPage) hints(a *App) []hint {
	return []hint{{"enter", "open"}, {"p", "play"}, {"P", "play all"}, {"S", "shuffle all"}, {"tab", "section"}}
}

func (p *artistPage) update(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case artistLoaded:
		if msg.id != p.id {
			return nil
		}
		p.loading = false
		if msg.err != nil {
			if p.artist == nil {
				p.err = msg.err
			}
			a.notify(msg.err.Error(), true)
			return nil
		}
		p.err = nil
		p.setArtist(a, msg.page)
		return nil
	case tea.MouseMsg:
		if c := p.coll(); c != nil {
			scrollMouse(c, msg)
		}
	case tea.KeyMsg:
		k := msg.String()
		c := p.coll()
		if c != nil && c.key(k, !c.grid) {
			return nil
		}
		switch k {
		case "tab", "shift+tab":
			if len(p.tabs) > 0 {
				d := 1
				if k == "shift+tab" {
					d = -1
				}
				p.tab = (p.tab + d + len(p.tabs)) % len(p.tabs)
			}
		case "R":
			return p.load(a, true)
		case "P":
			return a.playArtist(p.id, p.name, mpv.Replace, false)
		case "S":
			return a.playArtist(p.id, p.name, mpv.Replace, true)
		case "enter", "p", "a", "A", "s":
			if c == nil {
				return nil
			}
			e, ok := c.selected()
			if !ok {
				return nil
			}
			return p.act(a, c, e, k)
		}
	}
	return nil
}

func (p *artistPage) act(a *App, c *collection, e entry, k string) tea.Cmd {
	mode := map[string]mpv.Mode{"a": mpv.Append, "A": mpv.Next}[k]
	switch d := e.data.(type) {
	case ytm.Release:
		if k == "enter" {
			pg, cmd := newAlbumPage(a, d.BrowseID, d.Title, p.id, p.name)
			a.push(pg)
			return cmd
		}
		return a.playRelease(d.BrowseID, p.id, p.name, mode, k == "s")
	case ytm.Song:
		songs := []ytm.Song{d}
		if k == "enter" || k == "p" {
			songs = nil
			for _, e := range c.from() {
				songs = append(songs, e.data.(ytm.Song))
			}
		}
		return a.play(library.PlayRequest{Songs: songs, Mode: mode, Shuffle: k == "s", ArtistID: p.id, Artist: p.name}, d.Title)
	case catalog.Artist:
		if k == "enter" {
			pg, cmd := newArtistPage(a, d.ID, d.Name)
			a.push(pg)
			return cmd
		}
		return a.playArtist(d.ID, d.Name, mode, k == "s")
	}
	return nil
}

// playRelease fetches an album (usually cached) and queues it.
func (a *App) playRelease(browseID, artistID, artist string, mode mpv.Mode, shuffle bool) tea.Cmd {
	l := a.lib
	return a.job(func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		al, err := l.Album(ctx, browseID, false)
		if err != nil {
			return "", err
		}
		if err := l.Play(library.PlayRequest{Songs: al.Tracks, Mode: mode, Shuffle: shuffle,
			ArtistID: artistID, Artist: artist, Album: al.Title}); err != nil {
			return "", err
		}
		verb := map[mpv.Mode]string{mpv.Replace: "▶ playing", mpv.Append: "+ queued", mpv.Next: "↳ up next:"}[mode]
		if shuffle {
			verb = "⤨ shuffling"
		}
		return fmt.Sprintf("%s %s (%d tracks)", verb, al.Title, len(al.Tracks)), nil
	})
}

func (p *artistPage) view(a *App, w, h int) string {
	t := a.theme
	if p.artist == nil {
		if p.err != nil {
			return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center,
				fg(t.Error).Render("✗ "+p.err.Error())+"\n\n"+t.muted().Render("R to retry · esc to go back"))
		}
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center,
			t.primary().Render(a.spin())+" "+t.muted().Render("loading "+bidi.Vis(p.name)+"…"))
	}
	ar := p.artist
	// header: picture + name, stats, bio, tags
	artH := 8
	if h < 30 {
		artH = 6
	}
	if !a.showImages {
		artH = 0
	}
	artW := artH * 2
	textW := w - artW - 3
	if artH == 0 {
		textW = w
	}
	var info []string
	name := t.bold().Render(bidi.Vis(ar.Name))
	if p.loading {
		name += " " + t.primary().Render(a.spin())
	}
	info = append(info, name)
	var meta []string
	if ar.Subscribers != "" {
		meta = append(meta, ar.Subscribers+" subscribers")
	}
	if ar.Views != "" {
		meta = append(meta, ar.Views)
	}
	rels := len(ar.Albums) + len(ar.Singles)
	if rels > 0 {
		meta = append(meta, fmt.Sprintf("%d releases", rels))
	}
	info = append(info, t.muted().Render(truncate(strings.Join(meta, " · "), textW)))
	if tags := p.cat.TagLabels(); len(tags) > 0 {
		var chips []string
		for _, tg := range tags {
			chips = append(chips, fg(t.Accent).Render("#"+strings.ToLower(strings.ReplaceAll(tg, " ", ""))))
		}
		info = append(info, strings.Join(chips, " "))
	}
	if ar.Description != "" && artH > 0 {
		info = append(info, "")
		for _, l := range wrap(bidi.Vis(ar.Description), textW, max(1, artH-len(info)-1)) {
			info = append(info, t.subtle().Render(l))
		}
	}
	header := strings.Join(info, "\n")
	if artH > 0 {
		pic := a.img.block(t, ar.Thumb, ar.Name, artW, artH, a.dialog != nil)
		header = lipgloss.JoinHorizontal(lipgloss.Top, pic, "   ", header)
	}
	header = paint(header, t.Bg, w, max(artH, lipgloss.Height(header)))

	// tabs
	var tabs []string
	for i, tb := range p.tabs {
		label := fmt.Sprintf("%s %d", tb.label, len(tb.items.items))
		if i == p.tab {
			tabs = append(tabs, t.primary().Bold(true).Render(label))
		} else {
			tabs = append(tabs, t.muted().Render(label))
		}
	}
	tabLine := strings.Join(tabs, t.subtle().Render("   "))
	underline := t.subtle().Render(strings.Repeat("─", w))
	used := lipgloss.Height(header) + 3
	body := ""
	if c := p.coll(); c != nil {
		body = c.view(a, w, h-used-1)
	} else {
		body = t.muted().Render("No releases found.")
	}
	return header + "\n\n" + tabLine + "\n" + underline + "\n" + body
}

// ---- album page ------------------------------------------------------------------

type albumPage struct {
	id, name           string
	artistID, artistNm string
	album              *ytm.Album
	err                error
	tracks             *collection
}

func newAlbumPage(a *App, browseID, title, artistID, artist string) (*albumPage, tea.Cmd) {
	p := &albumPage{id: browseID, name: title, artistID: artistID, artistNm: artist,
		tracks: newCollection(false, false, "no tracks")}
	p.tracks.numbered = true
	a.busy++
	l := a.lib
	return p, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		al, err := l.Album(ctx, browseID, false)
		return albumLoaded{id: browseID, album: al, err: err}
	}
}

func (p *albumPage) title() string       { return p.name }
func (p *albumPage) mode() string        { return "ALBUM" }
func (p *albumPage) coll() *collection   { return p.tracks }
func (p *albumPage) filterText() string  { return p.tracks.filter }
func (p *albumPage) setFilter(f string)  { p.tracks.setFilter(f) }
func (p *albumPage) placeholder() string { return "Filter tracks…" }
func (p *albumPage) hints(a *App) []hint {
	return []hint{{"enter", "play from here"}, {"a", "queue"}, {"A", "play next"}, {"P", "play album"}, {"s", "shuffle"}}
}

func (p *albumPage) update(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case albumLoaded:
		if msg.id != p.id {
			return nil
		}
		if msg.err != nil {
			p.err = msg.err
			a.notify(msg.err.Error(), true)
			return nil
		}
		p.album = msg.album
		p.name = msg.album.Title
		var items []entry
		for _, s := range msg.album.Tracks {
			items = append(items, songEntry(s, true))
		}
		p.tracks.set(items)
	case tea.MouseMsg:
		scrollMouse(p.tracks, msg)
	case tea.KeyMsg:
		k := msg.String()
		if p.tracks.key(k, true) {
			return nil
		}
		if p.album == nil {
			return nil
		}
		req := library.PlayRequest{ArtistID: p.artistID, Artist: p.artistNm, Album: p.album.Title}
		switch k {
		case "P":
			req.Songs = p.album.Tracks
			return a.play(req, p.album.Title)
		case "s":
			req.Songs, req.Shuffle = p.album.Tracks, true
			return a.play(req, p.album.Title)
		case "enter", "p", "a", "A":
			e, ok := p.tracks.selected()
			if !ok {
				return nil
			}
			song := e.data.(ytm.Song)
			switch k {
			case "a":
				req.Songs, req.Mode = []ytm.Song{song}, mpv.Append
			case "A":
				req.Songs, req.Mode = []ytm.Song{song}, mpv.Next
			default:
				for _, e := range p.tracks.from() {
					req.Songs = append(req.Songs, e.data.(ytm.Song))
				}
			}
			return a.play(req, song.Title)
		case "o":
			pg, cmd := newArtistPage(a, p.artistID, p.artistNm)
			a.push(pg)
			return cmd
		}
	}
	return nil
}

func (p *albumPage) view(a *App, w, h int) string {
	t := a.theme
	if p.album == nil {
		if p.err != nil {
			return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, fg(t.Error).Render("✗ "+p.err.Error()))
		}
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center,
			t.primary().Render(a.spin())+" "+t.muted().Render("loading "+bidi.Vis(p.name)+"…"))
	}
	al := p.album
	// mark the playing track
	for i := range p.tracks.items {
		p.tracks.items[i].active = a.playerUp && !a.now.Idle && p.tracks.items[i].key == a.now.VideoID
	}
	artH := 6
	if !a.showImages {
		artH = 0
	}
	var info []string
	info = append(info, t.subtle().Bold(true).Render(strings.ToUpper(orDefault(al.Kind, "album"))))
	info = append(info, t.bold().Render(bidi.Vis(al.Title)))
	info = append(info, t.primary().Render(bidi.Vis(orDefault(al.Artist, p.artistNm))))
	var meta []string
	for _, s := range []string{al.Year, fmt.Sprintf("%d songs", len(al.Tracks)), al.Length} {
		if s != "" {
			meta = append(meta, s)
		}
	}
	info = append(info, t.muted().Render(strings.Join(meta, " · ")))
	header := strings.Join(info, "\n")
	if artH > 0 {
		header = lipgloss.JoinHorizontal(lipgloss.Top, a.img.block(t, al.Thumb, al.Title, artH*2, artH, a.dialog != nil), "   ", header)
	}
	header = paint(header, t.Bg, w, max(artH, 4))
	underline := t.subtle().Render(strings.Repeat("─", w))
	used := lipgloss.Height(header) + 2
	return header + "\n\n" + underline + "\n" + p.tracks.view(a, w, h-used-1)
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// ---- queue ------------------------------------------------------------------------

type queuePage struct{ items *collection }

func newQueuePage(a *App) *queuePage {
	p := &queuePage{items: newCollection(false, false, "The queue is empty. Press p on an album or artist to fill it.")}
	p.items.numbered = true
	return p
}

func (p *queuePage) title() string     { return "Queue" }
func (p *queuePage) mode() string      { return "QUEUE" }
func (p *queuePage) coll() *collection { return p.items }
func (p *queuePage) hints(a *App) []hint {
	return []hint{{"enter", "play"}, {"d", "remove"}, {"c", "clear"}, {"S", "shuffle"}}
}

func (p *queuePage) update(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		scrollMouse(p.items, msg)
	case tea.KeyMsg:
		k := msg.String()
		if p.items.key(k, true) {
			return nil
		}
		e, ok := p.items.selected()
		switch k {
		case "enter", "p":
			if ok {
				i := e.data.(library.QueueItem).Index
				return a.control(func(pl *mpv.Player) (string, error) { return "", pl.Jump(i) })
			}
		case "d", "x", "delete":
			if ok {
				i := e.data.(library.QueueItem).Index
				return a.control(func(pl *mpv.Player) (string, error) { return "removed", pl.Remove(i) })
			}
		case "c":
			return a.control(func(pl *mpv.Player) (string, error) { return "queue cleared", pl.Clear() })
		case "S":
			return a.control(func(pl *mpv.Player) (string, error) { return "⤨ queue shuffled", pl.Shuffle() })
		}
	}
	return nil
}

func (p *queuePage) view(a *App, w, h int) string {
	t := a.theme
	var items []entry
	total := 0
	for _, q := range a.queue {
		right := ""
		if q.Track.Duration > 0 {
			right = ytm.FormatDuration(q.Track.Duration)
			total += q.Track.Duration
		}
		items = append(items, entry{key: fmt.Sprint(q.Index, q.Track.VideoID), title: q.Track.Title,
			sub: q.Track.Artist, right: right, num: fmt.Sprint(q.Index + 1), active: q.Current, data: q})
	}
	first := p.items.len() == 0
	p.items.set(items)
	if first { // open on the playing track
		for i, it := range items {
			if it.active {
				p.items.cursor = i
			}
		}
	}
	head := spread(t.bold().Render("Queue"), t.muted().Render(fmt.Sprintf("%d tracks · %s", len(items), ytm.FormatDuration(total))), w)
	return head + "\n\n" + p.items.view(a, w, h-2)
}

// ---- history ---------------------------------------------------------------------

type historyPage struct {
	items *collection
	top   bool
}

type historyLoaded struct {
	plays []store.Play
	top   []store.ArtistStat
}

func newHistoryPage(a *App) (*historyPage, tea.Cmd) {
	p := &historyPage{items: newCollection(false, false, "Nothing played yet.")}
	return p, p.load(a)
}

func (p *historyPage) load(a *App) tea.Cmd {
	l := a.lib
	return func() tea.Msg {
		h, _ := l.DB.History(200)
		top, _ := l.DB.TopArtists(50)
		return historyLoaded{h, top}
	}
}

func (p *historyPage) title() string     { return "History" }
func (p *historyPage) mode() string      { return "HISTORY" }
func (p *historyPage) coll() *collection { return p.items }
func (p *historyPage) hints(a *App) []hint {
	return []hint{{"enter", "open artist"}, {"tab", "recent/top"}}
}

func (p *historyPage) update(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case historyLoaded:
		var items []entry
		if p.top {
			for i, s := range msg.top {
				items = append(items, entry{key: s.ArtistID + s.Artist, title: s.Artist, num: fmt.Sprint(i + 1),
					sub: fmt.Sprintf("%d plays · %d tracks", s.Plays, s.Tracks), right: agoShort(s.Last), data: s.ArtistID})
			}
			p.items.numbered = true
		} else {
			for i, e := range msg.plays {
				title := e.Artist
				sub := ""
				switch {
				case e.Track != "":
					title, sub = e.Track, e.Artist
				case e.Album != "":
					title, sub = e.Album, e.Artist
				case e.Count > 1:
					sub = fmt.Sprintf("%d tracks", e.Count)
				}
				items = append(items, entry{key: fmt.Sprint(i), title: title, sub: sub, right: agoShort(e.At), data: e.ArtistID})
			}
			p.items.numbered = false
		}
		p.items.set(items)
	case tea.MouseMsg:
		scrollMouse(p.items, msg)
	case tea.KeyMsg:
		k := msg.String()
		if p.items.key(k, true) {
			return nil
		}
		switch k {
		case "tab", "shift+tab":
			p.top = !p.top
			p.items.cursor, p.items.top = 0, 0
			return p.load(a)
		case "enter":
			if e, ok := p.items.selected(); ok {
				if id, _ := e.data.(string); id != "" {
					name := id
					if ar, ok := a.lib.Cat.Get(id); ok {
						name = ar.Name
					}
					pg, cmd := newArtistPage(a, id, name)
					a.push(pg)
					return cmd
				}
			}
		}
	}
	return nil
}

func (p *historyPage) view(a *App, w, h int) string {
	t := a.theme
	recent, top := t.primary().Bold(true).Render("Recent"), t.muted().Render("Top artists")
	if p.top {
		recent, top = t.muted().Render("Recent"), t.primary().Bold(true).Render("Top artists")
	}
	return recent + "   " + top + "\n" + t.subtle().Render(strings.Repeat("─", w)) + "\n" + p.items.view(a, w, h-2)
}

func agoShort(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Format("Jan 2006")
}
