package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lubabs770/gooseneck/player/internal/catalog"
	"github.com/lubabs770/gooseneck/player/internal/library"
	"github.com/lubabs770/gooseneck/player/internal/ytm"
)

// StartPages are the pages the TUI can open on (`goose tui <page>`).
var StartPages = []string{"home", "library", "queue", "history", "downloads", "lyrics", "artist", "album"}

// filterPages accept --filter (the pages with a search prompt).
var filterPages = map[string]bool{"home": true, "library": true, "downloads": true, "artist": true, "album": true}

// artistTabKeys name the artist page's sections. A section only exists when
// the artist has something in it, so a missing one is reported in a toast.
var artistTabKeys = []tabName{
	{"albums", []string{"Albums", "Uploads"}},
	{"singles", []string{"Singles & EPs", "Singles", "EPs"}},
	{"songs", []string{"Top songs", "Top"}},
	{"related", []string{"Fans also like", "Fans", "Similar"}},
}

type tabName struct {
	key   string
	names []string // other spellings, matched like the key
}

// StartError is a launch option that doesn't make sense (a usage error), as
// opposed to a network failure while looking something up.
type StartError struct{ Err error }

func (e *StartError) Error() string { return e.Err.Error() }
func (e *StartError) Unwrap() error { return e.Err }

// start is the resolved form of the launch options.
type start struct {
	page    string
	tab     int    // library tag (0 = all) or history tab
	section string // artist section key ("" = first)
	artist  catalog.Artist
	release ytm.Release
}

// StartTabs lists the --tab values valid on a page, for help and completion.
func StartTabs(page string) []string {
	var out []string
	for _, t := range tabNames(page) {
		out = append(out, t.key)
	}
	return out
}

func tabNames(page string) []tabName {
	switch page {
	case "library":
		ts := []tabName{{"all", []string{"All"}}}
		for _, t := range catalog.Tags {
			ts = append(ts, tabName{t.Key, []string{t.Label}})
		}
		return ts
	case "history":
		return []tabName{{"recent", nil}, {"top", []string{"Top artists"}}, {"listens", nil}}
	case "artist", "album":
		return artistTabKeys
	}
	return nil
}

// Resolve checks the launch options and looks up the artist and album they
// name, so mistakes are reported before the screen is taken over. Run calls
// it if the caller hasn't.
func (o *Options) Resolve(ctx context.Context, l *library.Lib) error {
	err := o.resolve(ctx, l)
	if err != nil {
		var ne netError
		if errors.As(err, &ne) {
			return ne.error
		}
		return &StartError{err}
	}
	return nil
}

// netError marks lookups that failed for reasons other than bad input.
type netError struct{ error }

func (o *Options) resolve(ctx context.Context, l *library.Lib) error {
	s := start{page: strings.ToLower(strings.TrimSpace(o.Page))}
	switch {
	case s.page == "" && o.Album != "":
		s.page = "album"
	case s.page == "" && o.Artist != "":
		s.page = "artist"
	case s.page == "":
		s.page = "home"
	}
	if !contains(StartPages, s.page) {
		return fmt.Errorf("unknown page %q (pages: %s)", o.Page, strings.Join(StartPages, ", "))
	}
	if (s.page == "artist" || s.page == "album") && o.Artist == "" {
		return fmt.Errorf("the %s page needs an artist", s.page)
	}
	if s.page == "album" && o.Album == "" {
		return fmt.Errorf("the album page needs an album (title, fragment or number)")
	}
	if o.Album != "" && s.page != "album" {
		return fmt.Errorf("--album only works on the album page")
	}
	if o.Artist != "" && s.page != "artist" && s.page != "album" {
		return fmt.Errorf("--artist only works on the artist and album pages")
	}
	if o.Filter != "" && !filterPages[s.page] {
		return fmt.Errorf("the %s page has no filter", s.page)
	}
	if o.Catalog != "" {
		if s.page != "library" {
			return fmt.Errorf("--catalog only works on the library page")
		}
		if !contains(l.Cat.Names(), o.Catalog) {
			return fmt.Errorf("unknown catalog %q (catalogs: %s)", o.Catalog, strings.Join(l.Cat.Names(), ", "))
		}
	}
	if o.Tab != "" {
		if s.page == "album" {
			return fmt.Errorf("the album page has no tabs")
		}
		names := tabNames(s.page)
		if names == nil {
			return fmt.Errorf("the %s page has no tabs", s.page)
		}
		i, err := matchTab(o.Tab, names)
		if err != nil {
			return fmt.Errorf("%s page: %w", s.page, err)
		}
		if s.page == "artist" {
			s.section = names[i].key
		} else {
			s.tab = i
		}
	}
	if o.Artist != "" {
		ar, err := l.ResolveArtist(o.Artist)
		if err != nil {
			return err
		}
		s.artist = ar
	}
	if s.page == "album" {
		page, err := l.Artist(ctx, s.artist.ID, false)
		if err != nil {
			return netError{err}
		}
		rel, err := library.ResolveRelease(page, o.Album)
		if err != nil {
			return err
		}
		s.release = rel
	}
	o.start = &s
	return nil
}

// matchTab finds q among a page's tab names: exact first, then a unique prefix.
func matchTab(q string, names []tabName) (int, error) {
	nq := squash(q)
	var prefix []string // keys of tabs that nq is a prefix of
	at := -1
	for i, t := range names {
		hit := false
		for _, n := range append([]string{t.key}, t.names...) {
			if squash(n) == nq {
				return i, nil
			}
			hit = hit || (nq != "" && strings.HasPrefix(squash(n), nq))
		}
		if hit {
			prefix, at = append(prefix, t.key), i
		}
	}
	switch {
	case len(prefix) == 1:
		return at, nil
	case len(prefix) > 1:
		return 0, fmt.Errorf("tab %q is ambiguous (%s)", q, strings.Join(prefix, ", "))
	}
	keys := make([]string, len(names))
	for i, t := range names {
		keys[i] = t.key
	}
	return 0, fmt.Errorf("no tab %q (tabs: %s)", q, strings.Join(keys, ", "))
}

// squash lower-cases s and drops everything but letters and digits, so
// "Singles & EPs", "singles-eps" and "SinglesEPs" all compare equal.
func squash(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// openStart builds the navigation stack the launch options asked for.
func (a *App) openStart() tea.Cmd {
	s, o := a.opts.start, a.opts
	if s == nil {
		return nil
	}
	var cmd tea.Cmd
	switch s.page {
	case "home":
		if o.Filter != "" {
			a.top().(filterable).setFilter(o.Filter)
			a.focusSearch()
		}
		return nil
	case "library":
		p := newLibrary(a)
		p.tag, p.cat = s.tab, o.Catalog
		p.load(a)
		a.push(p)
	case "queue":
		a.push(newQueuePage(a))
	case "history":
		var p *historyPage
		p, cmd = newHistoryPage(a)
		p.tab = s.tab
		a.push(p)
	case "downloads":
		var p *downloadsPage
		p, cmd = newDownloadsPage(a)
		a.push(p)
	case "lyrics":
		var p *lyricsPage
		p, cmd = newLyricsPage(a)
		a.push(p)
	case "artist":
		var p *artistPage
		p, cmd = newArtistPage(a, s.artist.ID, s.artist.Name)
		p.want = &artistWant{section: s.section, filter: o.Filter}
		p.applyWant(a)
		a.push(p)
		return cmd
	case "album":
		ap, c1 := newArtistPage(a, s.artist.ID, s.artist.Name)
		a.push(ap) // esc from the album goes back to its artist
		p, c2 := newAlbumPage(a, s.release.BrowseID, s.release.Title, s.artist.ID, s.artist.Name)
		a.push(p)
		cmd = tea.Batch(c1, c2)
	}
	if o.Filter != "" {
		if f, ok := a.top().(filterable); ok {
			f.setFilter(o.Filter)
		}
	}
	return cmd
}

// artistWant is a section/filter to select once the artist's tabs exist.
type artistWant struct{ section, filter string }

func (p *artistPage) applyWant(a *App) {
	if p.want == nil || len(p.tabs) == 0 {
		return
	}
	w := p.want
	p.want = nil
	if w.section != "" {
		i, err := matchTab(w.section, artistTabKeys)
		found := false
		if err == nil {
			for ti, tb := range p.tabs {
				if contains(artistTabKeys[i].names, tb.label) {
					p.tab, found = ti, true
					break
				}
			}
		}
		if !found {
			a.notify(fmt.Sprintf("%s has no %s", p.name, w.section), true)
		}
	}
	if w.filter != "" {
		p.setFilter(w.filter)
	}
}
