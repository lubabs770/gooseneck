package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lubabs770/gooseneck/player/internal/config"
	"github.com/lubabs770/gooseneck/player/internal/library"
	"github.com/lubabs770/gooseneck/player/internal/mpv"
	"github.com/lubabs770/gooseneck/player/internal/store"
	"github.com/lubabs770/gooseneck/player/internal/ytm"
)

// mouseApp builds a real App over a small offline catalog, sized and drawn.
func mouseApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GOOSENECK_HOME", dir)
	t.Setenv("GOOSENECK_OMARCHY_THEME", t.TempDir())
	var artists []string
	for i := 0; i < 12; i++ {
		artists = append(artists, fmt.Sprintf(`{"id":"UC%02d","name":"Artist %02d","isChasid":%v}`, i, i, i%2 == 0))
	}
	cat := filepath.Join(dir, "artists.json")
	if err := os.WriteFile(cat, []byte(`{"count":12,"artists":[`+strings.Join(artists, ",")+`]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.ArtistsJSON = cat
	cfg.Theme = "opencode"
	cfg.Thumbnails = config.BoolPtr(false)
	l, err := library.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	devnull, _ := os.Open(os.DevNull)
	a := newApp(l, Options{}, &syncWriter{f: devnull})
	t.Cleanup(func() { a.zones.Close(); l.Close(); devnull.Close() })
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return a
}

// clickZone redraws the screen, waits for zone id from that frame, and
// left-clicks it at column dx inside the zone.
func clickZone(t *testing.T, a *App, id string, dx int) tea.Cmd {
	t.Helper()
	prev := a.zones.Get(id)
	a.View()
	deadline := time.Now().Add(2 * time.Second)
	z := a.zones.Get(id)
	for z == nil || z == prev {
		if time.Now().After(deadline) {
			t.Fatalf("zone %q never rendered", id)
		}
		time.Sleep(2 * time.Millisecond)
		z = a.zones.Get(id)
	}
	_, cmd := a.Update(tea.MouseMsg{X: z.StartX + dx, Y: z.StartY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	return cmd
}

func TestMouseListAndBreadcrumbs(t *testing.T) {
	a := mouseApp(t)
	lib := newLibrary(a)
	a.goTo(lib)
	c := lib.items

	clickZone(t, a, c.zone(2), 1)
	if c.cursor != 2 {
		t.Fatalf("click selected %d, want 2", c.cursor)
	}
	if _, ok := a.top().(*libraryPage); !ok {
		t.Fatal("a single click should only select")
	}
	clickZone(t, a, c.zone(2), 1) // second click on it: open
	ar, ok := a.top().(*artistPage)
	if !ok || ar.name != "Artist 02" {
		t.Fatalf("double click opened %T %+v", a.top(), a.top().title())
	}

	clickZone(t, a, "crumb:1", 0)
	if a.top() != page(lib) {
		t.Fatalf("breadcrumb went to %T", a.top())
	}
	clickZone(t, a, "home", 0)
	if len(a.stack) != 1 {
		t.Fatalf("home badge left %d pages", len(a.stack))
	}
}

func TestMouseSlowClicksDoNotOpen(t *testing.T) {
	a := mouseApp(t)
	lib := newLibrary(a)
	a.goTo(lib)
	clickZone(t, a, lib.items.zone(1), 0)
	a.click.at = a.click.at.Add(-time.Second) // long gap
	clickZone(t, a, lib.items.zone(1), 0)
	if _, ok := a.top().(*libraryPage); !ok {
		t.Fatalf("two slow clicks opened %T", a.top())
	}
}

func TestMouseTabs(t *testing.T) {
	a := mouseApp(t)
	lib := newLibrary(a)
	a.goTo(lib)
	clickZone(t, a, "libtag:2", 1) // All, Popular, Chasidish
	if lib.tag != 2 || lib.items.len() != 6 {
		t.Fatalf("tag chip: tag %d, %d artists", lib.tag, lib.items.len())
	}

	h, _ := newHistoryPage(a)
	a.goTo(h)
	if cmd := clickZone(t, a, "histtab:2", 0); cmd == nil || h.tab != 2 {
		t.Fatalf("history tab = %d (cmd %v)", h.tab, cmd != nil)
	}
}

func TestMouseSeekAndWheel(t *testing.T) {
	a := mouseApp(t)
	a.playerUp = true
	a.now = library.NowPlaying{Status: mpv.Status{Duration: 200, Pos: 10, Title: "x"}, Track: store.Track{Title: "Song"}}
	z := "seek:side"
	a.View()
	clickZone(t, a, z, 0) // make sure the zone exists, then click its middle
	zi := a.zones.Get(z)
	clickZone(t, a, z, (zi.EndX-zi.StartX+1)/2)
	if a.now.Pos < 90 || a.now.Pos > 110 {
		t.Fatalf("seek to middle: pos %.1f", a.now.Pos)
	}

	lib := newLibrary(a)
	a.goTo(lib)
	a.View()
	a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if lib.items.cursor != lib.items.step() {
		t.Fatalf("wheel moved cursor to %d", lib.items.cursor)
	}

	clickZone(t, a, lib.items.zone(0), 0)
	z3 := a.zones.Get(lib.items.zone(3))
	a.dialog = newHelp(a) // clicks don't reach the page under a dialog
	a.Update(tea.MouseMsg{X: z3.StartX, Y: z3.StartY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if lib.items.cursor == 3 {
		t.Fatal("click went through a dialog")
	}
}

func TestMouseArtistTabs(t *testing.T) {
	a := mouseApp(t)
	p := &artistPage{id: "UC01", name: "Artist 01"}
	p.setArtist(a, &ytm.Artist{ID: "UC01", Name: "Artist 01",
		Albums:  []ytm.Release{{BrowseID: "MPa", Title: "An Album", Kind: "Album"}},
		Singles: []ytm.Release{{BrowseID: "MPs", Title: "A Single", Kind: "Single"}}})
	a.push(p)
	clickZone(t, a, "arttab:1", 0)
	if p.tab != 1 {
		t.Fatalf("tab = %d, want 1", p.tab)
	}
}

// Picking a search result with enter clears the search: the artist page
// opens unfiltered and home is empty again on the way back.
func TestEnterClearsSearch(t *testing.T) {
	a := mouseApp(t)
	a.focusSearch()
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("artist 03")})
	a.View() // home rebuilds its results while drawing
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	ar, ok := a.top().(*artistPage)
	if !ok || ar.name != "Artist 03" {
		t.Fatalf("enter opened %T", a.top())
	}
	if ar.filterText() != "" || a.input.Value() != "" {
		t.Fatalf("search leaked: page filter %q, input %q", ar.filterText(), a.input.Value())
	}
	a.pop()
	if f := a.top().(filterable).filterText(); f != "" {
		t.Fatalf("home still filtered by %q", f)
	}
}
