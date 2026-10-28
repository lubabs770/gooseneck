// Package tui is gooseneck's full-screen interface. Its look follows
// OpenCode's TUI: solid themed background layers, a block-letter logo on the
// home screen, a prompt with a thick accent bar, a command palette, a mode
// badge in the status bar, and a sidebar. Every action it offers is also a
// CLI command (see internal/cli); both go through internal/library.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
	"github.com/lubabs770/gooseneck/player/internal/bidi"
	"github.com/lubabs770/gooseneck/player/internal/config"
	"github.com/lubabs770/gooseneck/player/internal/library"
	"github.com/lubabs770/gooseneck/player/internal/mpv"
	"github.com/lubabs770/gooseneck/player/internal/ytm"
)

// Options tweak how the TUI starts.
type Options struct {
	Theme   string
	Version string

	// Where to open (see StartPages and Resolve). All optional.
	Page    string // home, library, queue, history, downloads, artist, album
	Artist  string // artist id or name (artist and album pages)
	Album   string // album title, fragment or number (album page)
	Tab     string // library tag, artist section, or history tab
	Filter  string // pre-filled filter / search
	Catalog string // library: show only this catalog

	start *start // set by Resolve
}

// page is one screen on the navigation stack.
type page interface {
	title() string
	mode() string
	update(a *App, msg tea.Msg) tea.Cmd
	view(a *App, w, h int) string
	hints(a *App) []hint
}

// filterable pages accept the bottom search prompt.
type filterable interface {
	setFilter(string)
	filterText() string
	placeholder() string
}

type hint struct{ key, desc string }

// App is the root Bubble Tea model. It is used by pointer, so View may keep
// render bookkeeping (scroll offsets, wanted images) without the value-copy
// trap.
type App struct {
	lib  *library.Lib
	opts Options

	theme      Theme
	w, h       int
	img        *images
	showImages bool
	sidebar    bool
	lyricsAt   string // live lyrics: "off" | "sidebar" | "line" (y cycles)
	lyr        lyricsState
	showHelp   bool // key hints in the status bar

	stack     []page
	dialog    dialog
	input     textinput.Model
	searching bool

	now      library.NowPlaying
	queue    []library.QueueItem
	playerUp bool
	polling  bool
	lastPoll time.Time
	busy     int // async jobs in flight (spinner)
	frame    int

	toast    string
	toastErr bool
	toastAt  time.Time

	omarchySig string // last seen Omarchy theme signature (live theme reload)

	scrob    *library.ScrobblerStatus // nil when no scrobbler is running
	dl       dlState                  // background downloads
	quitWarn bool                     // q pressed once while downloads were running
	keys     keyMap                   // [keys] overrides from config.toml

	zones *zone.Manager // clickable regions of the last frame (mouse.go)
	click lastClick     // previous click, for double clicks
}

// Run starts the TUI and blocks until it exits.
func Run(l *library.Lib, opts Options) error {
	// Themes set every color explicitly, so skip lipgloss's OSC 11 background
	// query: terminals that never answer it would stall startup for seconds.
	lipgloss.SetHasDarkBackground(true)
	if opts.start == nil {
		if err := opts.Resolve(context.Background(), l); err != nil {
			return err
		}
	}
	out := &syncWriter{f: os.Stdout}
	a := newApp(l, opts, out)
	defer a.zones.Close()
	p := tea.NewProgram(a, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithOutput(out), tea.WithFPS(30))
	_, err := p.Run()
	if a.img.kitty {
		fmt.Fprint(os.Stdout, kittyDeleteAll())
	}
	if a.playerUp && !a.now.Idle {
		fmt.Fprintf(os.Stderr, "♪ still playing %s — `goose stop` to stop, `goose` to come back\n", a.now.Track.Title)
	}
	return err
}

func newApp(l *library.Lib, opts Options, out *syncWriter) *App {
	themeName := l.Cfg.Theme
	if opts.Theme != "" {
		themeName = opts.Theme
	}
	a := &App{
		lib:        l,
		opts:       opts,
		theme:      GetTheme(themeName),
		img:        newImages(anyThumbs(l.Cfg), out),
		showImages: anyThumbs(l.Cfg),
		sidebar:    config.On(l.Cfg.Sidebar, true),
		lyricsAt:   l.Cfg.Lyrics,
		showHelp:   config.On(l.Cfg.ShowHelp, true),
		zones:      zone.New(),
	}
	var bad []string
	a.keys, bad = newKeyMap(l.Cfg.KeyMap)
	if len(bad) > 0 {
		a.notify("[keys] in config.toml: "+strings.Join(bad, "; "), true)
	}
	if bad := checkViews(l.Cfg.Views); len(bad) > 0 {
		a.notify("[views] in config.toml: "+strings.Join(bad, "; "), true)
	}
	a.omarchySig = omarchySignature()
	a.input = textinput.New()
	a.input.Prompt = ""
	a.input.CharLimit = 120
	a.applyInputTheme()
	home := newHome(a)
	a.stack = []page{home}
	a.focusSearch()
	return a
}

func (a *App) applyInputTheme() {
	t := a.theme
	a.input.TextStyle = fg(t.Text).Background(t.Element)
	a.input.PlaceholderStyle = fg(t.Subtle).Background(t.Element)
	a.input.Cursor.Style = fg(t.Primary)
	a.input.Cursor.TextStyle = fg(t.Text).Background(t.Element)
}

func (a *App) setTheme(name string) {
	a.theme = GetTheme(name)
	a.applyInputTheme()
}

func (a *App) top() page { return a.stack[len(a.stack)-1] }

func (a *App) push(p page) {
	// Opening something ends the search that found it: clear it so it neither
	// filters the new page nor is still typed when you come back.
	if f, ok := a.top().(filterable); ok {
		f.setFilter("")
	}
	a.input.SetValue("")
	a.stack = append(a.stack, p)
	a.blurSearch()
}

func (a *App) pop() {
	if len(a.stack) == 1 {
		return // already home
	}
	a.stack = a.stack[:len(a.stack)-1]
	a.blurSearch()
	if _, ok := a.top().(*homePage); ok {
		a.focusSearch()
	}
}

// popTo goes back to stack[i], dropping everything above it.
func (a *App) popTo(i int) {
	if i < 0 || i >= len(a.stack)-1 {
		return
	}
	a.stack = a.stack[:i+1]
	a.blurSearch()
	if _, ok := a.top().(*homePage); ok {
		a.focusSearch()
	}
}

// goTo replaces the stack above home with p (used by the 1-4 shortcuts).
func (a *App) goTo(p page) {
	a.stack = a.stack[:1]
	if p != nil {
		a.push(p)
	} else {
		a.focusSearch()
	}
}

func (a *App) focusSearch() tea.Cmd {
	f, ok := a.top().(filterable)
	if !ok {
		return nil
	}
	a.searching = true
	a.input.Placeholder = f.placeholder()
	a.input.SetValue(f.filterText())
	a.input.CursorEnd()
	return a.input.Focus()
}

func (a *App) blurSearch() {
	a.searching = false
	a.input.Blur()
}

func (a *App) notify(msg string, isErr bool) {
	a.toast, a.toastErr, a.toastAt = msg, isErr, time.Now()
}

// ---- messages ----------------------------------------------------------------

type tickMsg time.Time

type pollMsg struct {
	now   library.NowPlaying
	queue []library.QueueItem
	scrob *library.ScrobblerStatus
	err   error
}

type doneMsg struct {
	ok  string
	err error
}

type artistLoaded struct {
	id   string
	page *ytm.Artist
	err  error
}

type albumLoaded struct {
	id    string
	album *ytm.Album
	err   error
}

func (a *App) Init() tea.Cmd {
	go a.lib.EnsureDaemon() // music may already be playing from an earlier session
	cmds := []tea.Cmd{a.tick(), a.poll(), textinput.Blink}
	cmds = append(cmds, a.openStart())
	return tea.Batch(cmds...)
}

func (a *App) tick() tea.Cmd {
	d := 1000 * time.Millisecond
	switch {
	case a.busy > 0 || a.img.pending():
		d = 120 * time.Millisecond
	case a.playerUp && !a.now.Idle && !a.now.Paused:
		d = 500 * time.Millisecond
	}
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// poll reads the player state (and queue) off the UI goroutine.
func (a *App) poll() tea.Cmd {
	if a.polling {
		return nil
	}
	a.polling = true
	a.lastPoll = time.Now()
	l := a.lib
	return func() tea.Msg {
		np, err := l.Now()
		var q []library.QueueItem
		if err == nil && !np.Idle {
			q, _ = l.Queue()
		}
		var sc *library.ScrobblerStatus
		if st, e := library.QueryScrobbler(); e == nil {
			sc = &st
		}
		return pollMsg{np, q, sc, err}
	}
}

// job runs fn asynchronously with the spinner on, reporting via a toast.
func (a *App) job(fn func() (string, error)) tea.Cmd {
	a.busy++
	return func() tea.Msg {
		ok, err := fn()
		return doneMsg{ok, err}
	}
}

// play queues songs asynchronously.
func (a *App) play(req library.PlayRequest, label string) tea.Cmd {
	l := a.lib
	return a.job(func() (string, error) {
		if err := l.Play(req); err != nil {
			return "", err
		}
		verb := map[mpv.Mode]string{mpv.Replace: "▶ playing", mpv.Append: "+ queued", mpv.Next: "↳ up next:"}[req.Mode]
		if req.Shuffle {
			verb = "⤨ shuffling"
		}
		return fmt.Sprintf("%s %s", verb, label), nil
	})
}

// playArtist queues everything an artist has released.
func (a *App) playArtist(id, name string, mode mpv.Mode, shuffle bool) tea.Cmd {
	l := a.lib
	return a.job(func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		page, err := l.Artist(ctx, id, false)
		if err != nil {
			return "", err
		}
		songs, err := l.ArtistTracks(ctx, page)
		if err != nil {
			return "", err
		}
		if err := l.Play(library.PlayRequest{Songs: songs, Mode: mode, Shuffle: shuffle, ArtistID: page.ID, Artist: page.Name}); err != nil {
			return "", err
		}
		verb := "▶ playing"
		if shuffle {
			verb = "⤨ shuffling"
		} else if mode == mpv.Append {
			verb = "+ queued"
		}
		return fmt.Sprintf("%s %s (%d tracks)", verb, page.Name, len(songs)), nil
	})
}

// radio starts an endless radio session seeded by an artist.
func (a *App) radio(id, name string) tea.Cmd {
	l := a.lib
	a.notify("tuning radio: "+name+"…", false)
	return a.job(func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		seed, songs, err := l.StartRadio(ctx, id, 0)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("📻 radio: %s (%d tracks, keeps going)", seed.Name, len(songs)), nil
	})
}

func (a *App) control(fn func(p *mpv.Player) (string, error)) tea.Cmd {
	p := a.lib.Player
	return func() tea.Msg {
		msg, err := fn(p)
		if errors.Is(err, mpv.ErrNotRunning) {
			err = errors.New("nothing playing")
		}
		return doneMsg{msg, err}
	}
}

// ---- update ------------------------------------------------------------------

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if cmd, ok := a.updateDL(msg); ok {
		return a, tea.Batch(cmd, a.img.requests())
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height

	case tickMsg:
		a.frame++
		if a.theme.Name == "omarchy" && a.dialog == nil {
			if sig := omarchySignature(); sig != a.omarchySig {
				a.omarchySig = sig
				a.setTheme("omarchy") // the user switched Omarchy themes: repaint live
				a.notify("omarchy theme: "+omarchyName(omarchyThemeDir()), false)
			}
		}
		if a.toast != "" && time.Since(a.toastAt) > 3500*time.Millisecond {
			a.toast = ""
		}
		every := 1500 * time.Millisecond
		if a.playerUp && !a.now.Idle {
			every = 450 * time.Millisecond
		}
		if time.Since(a.lastPoll) >= every {
			cmds = append(cmds, a.poll())
		}
		cmds = append(cmds, a.tick())

	case pollMsg:
		a.polling = false
		a.scrob = msg.scrob
		a.playerUp = msg.err == nil
		if msg.err == nil {
			a.now, a.queue = msg.now, msg.queue
		} else {
			a.now, a.queue = library.NowPlaying{}, nil
		}
		cmds = append(cmds, a.syncLyrics()) // fetches when the track changed

	case lyricsLoaded:
		a.busy = max(0, a.busy-1)
		if msg.id == a.lyr.id {
			a.lyr = lyricsState{id: msg.id, ly: msg.ly, lines: msg.ly.Lines(), err: msg.err}
		}

	case doneMsg:
		a.busy = max(0, a.busy-1)
		if msg.err != nil {
			a.notify(msg.err.Error(), true)
		} else if msg.ok != "" {
			a.notify(msg.ok, false)
		}
		cmds = append(cmds, a.poll())

	case imageReady:
		a.img.store(msg)

	case tea.MouseMsg:
		if a.dialog == nil {
			cmds = append(cmds, a.mouse(msg))
		}

	case tea.KeyMsg:
		cmds = append(cmds, a.key(msg))

	default:
		switch msg.(type) {
		case artistLoaded, albumLoaded:
			a.busy = max(0, a.busy-1) // counted here, not in pages, which may be gone
		}
		// Data for pages (artistLoaded, albumLoaded, …): every page on the
		// stack sees it and picks what's addressed to it.
		for _, p := range a.stack {
			cmds = append(cmds, p.update(a, msg))
		}
	}
	cmds = append(cmds, a.img.requests())
	return a, tea.Batch(cmds...)
}

func (a *App) key(msg tea.KeyMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	msg = a.keys.remap(msg, a.typing())
	k := msg.String()
	if a.dialog != nil {
		return a.dialog.update(a, msg)
	}
	if k == "ctrl+p" { // the palette is reachable from everywhere, even mid-search
		a.blurSearch()
		a.dialog = newPalette(a)
		return nil
	}
	if a.searching {
		return a.searchKey(msg)
	}
	if k != "q" {
		a.quitWarn = false
	}
	switch k {
	case "q":
		if a.dl.active != nil && !a.quitWarn {
			a.quitWarn = true
			a.notify("downloads are running — q again to quit and cancel them", true)
			return nil
		}
		a.cancelDownloads()
		return tea.Quit
	case "ctrl+p", ":":
		a.dialog = newPalette(a)
		return nil
	case "?":
		a.dialog = newHelp(a)
		return nil
	case "t":
		a.dialog = newThemePicker(a)
		return nil
	case "/":
		return a.focusSearch()
	case "esc", "backspace":
		if f, ok := a.top().(filterable); ok && f.filterText() != "" {
			f.setFilter("")
			return nil
		}
		a.pop()
		return nil
	case "1":
		a.goTo(nil)
		return nil
	case "2":
		a.goTo(newLibrary(a))
		return nil
	case "3":
		a.goTo(newQueuePage(a))
		return nil
	case "4":
		p, cmd := newHistoryPage(a)
		a.goTo(p)
		return cmd
	case "5":
		p, cmd := newDownloadsPage(a)
		a.goTo(p)
		return cmd
	case "6":
		p, cmd := newLyricsPage(a)
		a.goTo(p)
		return cmd
	case "b":
		a.sidebar = !a.sidebar
		return nil
	case "y":
		return a.cycleLyrics()
	case "i":
		a.showImages = !a.showImages
		a.notify(map[bool]string{true: "art on", false: "art off"}[a.showImages], false)
		return nil
	case "v":
		if c := pageCollection(a.top()); c != nil && c.canGrid {
			c.grid = !c.grid
			c.top = 0
		}
		return nil
	}
	if cmd, ok := a.transportKey(k); ok {
		return cmd
	}
	return a.top().update(a, msg)
}

// transportKey handles playback keys, which work on every page.
func (a *App) transportKey(k string) (tea.Cmd, bool) {
	switch k {
	case " ":
		return a.control(func(p *mpv.Player) (string, error) {
			paused, err := p.TogglePause()
			if paused {
				return "⏸ paused", err
			}
			return "", err
		}), true
	case "n", ">", ".":
		return a.control(func(p *mpv.Player) (string, error) { return "", p.Next() }), true
	case "N", "<", ",":
		return a.control(func(p *mpv.Player) (string, error) { return "", p.Prev() }), true
	case "]", "[", "}", "{":
		d := map[string]float64{"]": 10, "[": -10, "}": 60, "{": -60}[k]
		return a.control(func(p *mpv.Player) (string, error) { return "", p.Seek(d, false) }), true
	case "=", "+", "-":
		d := 5.0
		if k == "-" {
			d = -5
		}
		return a.control(func(p *mpv.Player) (string, error) {
			v, err := p.Volume(d, false)
			return fmt.Sprintf("volume %d%%", int(v)), err
		}), true
	case "r":
		next := map[string]string{"off": "all", "all": "one", "one": "off"}[a.now.Repeat]
		if next == "" {
			next = "all"
		}
		return a.control(func(p *mpv.Player) (string, error) { return "repeat " + next, p.Repeat(next) }), true
	}
	return nil, false
}

// typing reports whether keys go into a text box (search or the palette).
func (a *App) typing() bool {
	if a.dialog != nil {
		_, palette := a.dialog.(*cmdPalette)
		return palette
	}
	return a.searching
}

func (a *App) searchKey(msg tea.KeyMsg) tea.Cmd {
	f, _ := a.top().(filterable)
	switch msg.String() {
	case "esc":
		if f != nil {
			f.setFilter("")
		}
		a.input.SetValue("")
		a.blurSearch()
		return nil
	case "enter", "up", "down", "ctrl+n", "ctrl+k", "tab", "shift+tab", "pgup", "pgdown":
		if msg.String() == "enter" {
			if _, home := a.top().(*homePage); !home {
				a.blurSearch()
				return nil
			}
		}
		cmd := a.top().update(a, msg)
		if f, ok := a.top().(filterable); ok && f.filterText() != a.input.Value() {
			f.setFilter(a.input.Value()) // tab may have switched to another section
		}
		return cmd
	}
	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	if f != nil {
		f.setFilter(a.input.Value())
	}
	return cmd
}

func pageCollection(p page) *collection {
	if cp, ok := p.(interface{ coll() *collection }); ok {
		return cp.coll()
	}
	return nil
}

// ---- view ------------------------------------------------------------------------

const sidebarW = 36

func (a *App) View() string {
	if a.w == 0 || a.h == 0 {
		return ""
	}
	if a.w < 40 || a.h < 12 {
		return paint(a.theme.muted().Render("make the terminal a bit bigger"), a.theme.Bg, a.w, a.h)
	}
	t := a.theme
	bodyH := a.h - 1
	lyricLine := a.lyricsAt == "line" && a.playerUp && !a.now.Idle
	if lyricLine {
		bodyH--
	}
	side := a.sidebar && a.w >= 100
	mainW := a.w
	if side {
		mainW = a.w - sidebarW
	}

	// main column: page, then (search prompt | now-playing line)
	contentH := bodyH
	var bottom []string
	if a.searching || a.filterActive() {
		if _, home := a.top().(*homePage); !home {
			bottom = append(bottom, a.prompt(mainW-4))
		}
	}
	if !side && a.playerUp && !a.now.Idle {
		bottom = append(bottom, a.nowLine(mainW-4))
	}
	for _, b := range bottom {
		contentH -= lipgloss.Height(b) + 1
	}
	if len(bottom) > 0 {
		contentH-- // bottom margin
	}
	pageView := a.top().view(a, mainW-4, contentH-1)
	main := "\n" + paint(pageView, t.Bg, mainW-4, contentH-1) // top margin
	for _, b := range bottom {
		main += "\n\n" + b
	}
	main = paint(indent(main, 2), t.Bg, mainW, bodyH)

	body := main
	if side {
		body = lipgloss.JoinHorizontal(lipgloss.Top, main, a.sidebarView(sidebarW, bodyH))
	}
	frame := body + "\n" + a.statusBar()
	if lyricLine {
		frame += "\n" + a.lyricLine(a.w)
	}
	if a.dialog != nil {
		box := a.dialog.view(a)
		x, y := center(box, a.w, a.h)
		frame = overlay(frame, box, x, y)
	}
	return a.zones.Scan(frame)
}

func (a *App) filterActive() bool {
	f, ok := a.top().(filterable)
	return ok && f.filterText() != ""
}

func indent(block string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(block, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

// prompt is the OpenCode-style input: a raised panel with a thick accent bar.
func (a *App) prompt(w int) string {
	t := a.theme
	barSt := fg(t.Primary).Background(t.Element)
	if !a.searching {
		barSt = fg(t.Border).Background(t.Element)
	}
	a.input.Width = max(10, w-6)
	inputLine := a.input.View()
	if !a.searching {
		inputLine = fg(t.Text).Background(t.Element).Render(a.input.Value())
	}
	lines := []string{"", inputLine, ""}
	for i, l := range lines {
		lines[i] = barSt.Render("┃") + "  " + l
	}
	box := a.mark("prompt", paint(strings.Join(lines, "\n"), t.Element, w, 3))
	n := ""
	if c := pageCollection(a.top()); c != nil {
		n = fmt.Sprintf("%d matches", c.len())
	}
	hintLine := spread(a.renderHints([]hint{{"enter", "done"}, {"esc", "clear"}, {"↑↓", "move"}}), t.muted().Render(n), w)
	return box + "\n" + hintLine
}

// nowLine is the one-line player used when the sidebar is hidden.
func (a *App) nowLine(w int) string {
	t := a.theme
	np := a.now
	icon := "▶"
	if np.Paused {
		icon = "⏸"
	}
	title := np.Track.Title
	if title == "" {
		title = np.Title
	}
	left := t.primary().Render(icon+" ") + t.bold().Render(bidi.Vis(title))
	if np.Track.Artist != "" {
		left += t.muted().Render(" · " + bidi.Vis(np.Track.Artist))
	}
	times := t.muted().Render(fmt.Sprintf(" %s / %s", ytm.FormatDuration(int(np.Pos)), ytm.FormatDuration(int(np.Duration))))
	barW := clamp(w/3, 10, 40)
	right := a.mark("seek:line", progress(t, np.Pos, np.Duration, barW)) + times
	return spread(left, right, w)
}

func progress(t Theme, pos, dur float64, w int) string {
	frac := 0.0
	if dur > 0 {
		frac = pos / dur
	}
	n := clamp(int(frac*float64(w)), 0, w)
	return t.primary().Render(strings.Repeat("━", n)) + t.subtle().Render(strings.Repeat("─", w-n))
}

func (a *App) renderHints(hs []hint) string {
	t := a.theme
	var parts []string
	for _, h := range hs {
		parts = append(parts, t.text().Render(h.key)+" "+t.muted().Render(h.desc))
	}
	return strings.Join(parts, t.subtle().Render("  ·  "))
}

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (a *App) spin() string { return spinner[a.frame%len(spinner)] }

// statusBar: mode badge, breadcrumbs, then toast or key hints on the right.
func (a *App) statusBar() string {
	t := a.theme
	badge := t.onPrimary().Render(" " + a.top().mode() + " ")
	var crumbs []string
	for i, p := range a.stack[1:] {
		crumbs = append(crumbs, a.mark(fmt.Sprintf("crumb:%d", i+1), bidi.Vis(p.title())))
	}
	left := a.mark("home", badge+" "+fg(t.Text).Bold(true).Render(" gooseneck"))
	if len(crumbs) > 0 {
		left += t.muted().Render(" › " + strings.Join(crumbs, " › "))
	}
	var right string
	dl := a.dlStatus()
	switch {
	case a.busy > 0:
		right = t.primary().Render(a.spin()) + t.muted().Render(" working…")
		if a.toast != "" && !a.toastErr {
			right = t.primary().Render(a.spin()) + " " + t.text().Render(a.toast)
		}
	case a.toast != "":
		icon, st := "✓ ", fg(t.Success)
		if a.toastErr {
			icon, st = "✗ ", fg(t.Error)
		}
		right = st.Render(icon) + t.text().Render(a.toast)
	case a.showHelp:
		hs := a.top().hints(a)
		hs = append(hs, hint{"ctrl+p", "commands"})
		right = a.renderHints(hs)
	}
	if dl != "" {
		if right != "" {
			right = dl + t.subtle().Render("  ·  ") + right
		} else {
			right = dl
		}
	}
	line := spread(left, right+" ", a.w)
	return paint(line, t.Panel, a.w, 1)
}

// sidebarView: now playing with art, progress and the upcoming queue.
func (a *App) sidebarView(w, h int) string {
	t := a.theme
	inner := w - 4
	var b []string
	b = append(b, "", t.subtle().Bold(true).Render("NOW PLAYING"), "")
	if !a.playerUp || a.now.Idle {
		b = append(b,
			t.muted().Render("Nothing playing."),
			"",
			t.subtle().Render(wrapJoin("Pick anything and press p to play it, or enter to open it.", inner)),
		)
		if !a.lib.Player.Managed() {
			b = append(b, "", fg(t.Warning).Render(wrapJoin("Playback control needs mpv; using "+a.lib.Cfg.Player+".", inner)))
		}
	} else {
		np := a.now
		artW := min(inner, 24)
		artH := artW / 2
		if h < 30 {
			artW, artH = 16, 8
		}
		if a.artFor("sidebar") && np.Track.Thumb != "" {
			art := a.img.block(t, np.Track.Thumb, np.Track.Album, artW, artH, a.dialog != nil)
			b = append(b, strings.Split(art, "\n")...)
			b = append(b, "")
		}
		title := np.Track.Title
		if title == "" {
			title = np.Title
		}
		for _, l := range wrap(bidi.Vis(title), inner, 2) {
			b = append(b, t.bold().Render(l))
		}
		if np.Track.Artist != "" {
			b = append(b, t.primary().Render(truncate(bidi.Vis(np.Track.Artist), inner)))
		}
		if np.Track.Album != "" && np.Track.Album != title {
			b = append(b, t.muted().Render(truncate(bidi.Vis(np.Track.Album), inner)))
		}
		b = append(b, "", a.mark("seek:side", progress(t, np.Pos, np.Duration, inner)),
			spread(t.muted().Render(ytm.FormatDuration(int(np.Pos))), t.muted().Render(ytm.FormatDuration(int(np.Duration))), inner))
		state := "▶ playing"
		if np.Paused {
			state = "⏸ paused"
		}
		if np.Buffer {
			state = a.spin() + " buffering"
		}
		meta := []string{state, fmt.Sprintf("vol %d", int(np.Volume))}
		if a.scrob != nil && a.scrob.Radio != "" {
			b = append(b, fg(t.Accent).Render(truncate("📻 radio · "+bidi.Vis(a.scrob.Radio), inner)))
		}
		if np.Repeat != "off" {
			meta = append(meta, "⟳ "+np.Repeat)
		}
		b = append(b, t.muted().Render(strings.Join(meta, " · ")))
		if sc := a.scrob; sc != nil && sc.Current != nil && sc.Current.VideoID == np.VideoID {
			if sc.Scrobbled {
				b = append(b, fg(t.Success).Render("✓ scrobbled"))
			} else {
				b = append(b, t.subtle().Render(fmt.Sprintf("♫ scrobble in %s", ytm.FormatDuration(int(sc.Needed-sc.Played)))))
			}
		}

		if a.lyricsAt == "sidebar" {
			b = append(b, a.sideLyrics(inner, h)...)
		}

		// up next
		var next []library.QueueItem
		for _, q := range a.queue {
			if q.Index > np.Index {
				next = append(next, q)
			}
		}
		if len(next) > 0 {
			b = append(b, "", t.subtle().Bold(true).Render(fmt.Sprintf("UP NEXT · %d", len(next))), "")
			room := h - len(b) - 3
			for i, q := range next {
				if i >= room {
					break
				}
				d := ""
				if q.Track.Duration > 0 {
					d = ytm.FormatDuration(q.Track.Duration)
				}
				b = append(b, spread(t.text().Render(bidi.Vis(q.Track.Title)), t.subtle().Render(d), inner))
			}
		}
	}
	// footer
	b = strings.Split(strings.Join(b, "\n"), "\n")
	for len(b) < h-2 {
		b = append(b, "")
	}
	b = b[:h-2]
	b = append(b, t.subtle().Render(fmt.Sprintf("%d artists", a.lib.Cat.Len())), "")
	return paint(indent(strings.Join(b, "\n"), 2), t.Panel, w, h)
}

func wrapJoin(s string, w int) string { return strings.Join(wrap(s, w, 0), "\n") }
