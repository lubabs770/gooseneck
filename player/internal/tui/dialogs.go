package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lubabs770/gooseneck/player/internal/catalog"
	"github.com/lubabs770/gooseneck/player/internal/config"
	"github.com/lubabs770/gooseneck/player/internal/mpv"
)

// dialog is a modal box drawn over the frame.
type dialog interface {
	update(a *App, msg tea.KeyMsg) tea.Cmd
	view(a *App) string
}

// command is a palette / slash-command entry.
type command struct {
	name, desc, key string
	run             func(a *App) tea.Cmd
}

func commands(a *App) []command {
	ctl := func(fn func(p *mpv.Player) (string, error)) func(*App) tea.Cmd {
		return func(a *App) tea.Cmd { return a.control(fn) }
	}
	return []command{
		{"library", "browse every artist", "2", func(a *App) tea.Cmd { a.goTo(newLibrary(a)); return nil }},
		{"queue", "what's playing next", "3", func(a *App) tea.Cmd { a.goTo(newQueuePage(a)); return nil }},
		{"history", "recently played & top artists", "4", func(a *App) tea.Cmd {
			p, cmd := newHistoryPage(a)
			a.goTo(p)
			return cmd
		}},
		{"downloads", "tracks saved for offline play", "5", func(a *App) tea.Cmd {
			p, cmd := newDownloadsPage(a)
			a.goTo(p)
			return cmd
		}},
		{"lyrics", "lyrics of the playing track (lrclib.net)", "6", func(a *App) tea.Cmd {
			p, cmd := newLyricsPage(a)
			a.goTo(p)
			return cmd
		}},
		{"home", "back to the start", "1", func(a *App) tea.Cmd { a.goTo(nil); return nil }},
		{"theme", "switch theme (live preview)", "t", func(a *App) tea.Cmd { a.dialog = newThemePicker(a); return nil }},
		{"pause", "play / pause", "space", ctl(func(p *mpv.Player) (string, error) { _, err := p.TogglePause(); return "", err })},
		{"next", "next track", "n", ctl(func(p *mpv.Player) (string, error) { return "", p.Next() })},
		{"prev", "previous track", "N", ctl(func(p *mpv.Player) (string, error) { return "", p.Prev() })},
		{"shuffle", "shuffle the queue", "", ctl(func(p *mpv.Player) (string, error) { return "⤨ queue shuffled", p.Shuffle() })},
		{"repeat", "cycle repeat off / all / one", "r", func(a *App) tea.Cmd { cmd, _ := a.transportKey("r"); return cmd }},
		{"clear", "clear the queue (keeps the current track)", "", ctl(func(p *mpv.Player) (string, error) { return "queue cleared", p.Clear() })},
		{"stop", "stop playback", "", ctl(func(p *mpv.Player) (string, error) { return "stopped", p.Stop() })},
		{"radio", "mix of the current artist and related artists", "m", func(a *App) tea.Cmd {
			id, name := "", ""
			if ap, ok := a.top().(*artistPage); ok {
				id, name = ap.id, ap.name
			} else if a.playerUp && a.now.Track.ArtistID != "" {
				id, name = a.now.Track.ArtistID, a.now.Track.Artist
			}
			if id == "" {
				a.notify("open an artist (or play something) first", true)
				return nil
			}
			return a.radio(id, name)
		}},
		{"stop-radio", "end the radio session (the queue keeps playing)", "", func(a *App) tea.Cmd {
			if !a.lib.RadioStatus().Active {
				a.notify("no radio playing", false)
				return nil
			}
			a.lib.StopRadio()
			a.notify("radio off", false)
			return nil
		}},
		{"add-to-catalog", "add this artist to your personal catalog", "", func(a *App) tea.Cmd {
			ap, ok := a.top().(*artistPage)
			if !ok || ap.artist == nil {
				a.notify("open an artist first", true)
				return nil
			}
			var dest *config.CatalogSource
			for i := range a.lib.Cfg.Catalogs {
				if c := a.lib.Cfg.Catalogs[i]; c.URL == "" && !c.Disabled {
					dest = &a.lib.Cfg.Catalogs[i]
					break
				}
			}
			if dest == nil {
				a.notify("no personal catalog yet: goose catalog new mine", true)
				return nil
			}
			ar, ok := a.lib.Cat.Get(ap.id)
			if !ok {
				ar = catalog.Artist{ID: ap.id, Name: ap.artist.Name, Thumbnail: ap.artist.Thumb}
			}
			if _, err := catalog.AddTo(dest.File(), ar); err != nil {
				a.notify(err.Error(), true)
				return nil
			}
			if cat, err := catalog.LoadAll(a.lib.Cfg); err == nil {
				a.lib.Cat = cat // pick it up right away
			}
			a.notify("added "+ar.Name+" to "+dest.Name, false)
			return nil
		}},
		{"random", "play a random popular artist", "", func(a *App) tea.Cmd {
			tg, _ := catalog.FindTag("famous")
			list := a.lib.Cat.Filter(tg)
			if len(list) == 0 {
				return nil
			}
			ar := list[a.frame*7919%len(list)]
			return a.playArtist(ar.ID, ar.Name, mpv.Replace, true)
		}},
		{"cancel-downloads", "stop downloading (keeps finished tracks)", "", func(a *App) tea.Cmd {
			if a.dl.active == nil {
				a.notify("no downloads running", false)
				return nil
			}
			a.cancelDownloads()
			a.notify("downloads cancelled", false)
			return nil
		}},
		{"sidebar", "toggle the now-playing sidebar", "b", func(a *App) tea.Cmd { a.sidebar = !a.sidebar; return nil }},
		{"art", "toggle artist & album art", "i", func(a *App) tea.Cmd { a.showImages = !a.showImages; return nil }},
		{"view", "toggle grid / list", "v", func(a *App) tea.Cmd {
			if c := pageCollection(a.top()); c != nil && c.canGrid {
				c.grid = !c.grid
			}
			return nil
		}},
		// Unlike the view toggles this is saved: the scrobbler is a separate
		// background process that only learns about it from the config file.
		{"scrobble", "turn scrobbling on/off, saved (set up with `goose scrobble login`)", "", func(a *App) tea.Cmd {
			on := !config.On(a.lib.Cfg.Scrobble, true)
			if err := config.Update(func(c *config.Config) error { c.Scrobble = config.BoolPtr(on); return nil }); err != nil {
				a.notify(err.Error(), true)
				return nil
			}
			a.lib.Cfg.Scrobble = config.BoolPtr(on)
			if !on {
				a.notify("scrobbling off", false)
				return nil
			}
			if len(a.lib.ScrobbleServices()) == 0 {
				a.notify("scrobbling on, but no service is set up: goose scrobble login", true)
				return nil
			}
			go a.lib.EnsureDaemon()
			a.notify("scrobbling on", false)
			return nil
		}},
		{"hints", "toggle key hints in the status bar", "", func(a *App) tea.Cmd { a.showHelp = !a.showHelp; return nil }},
		{"help", "keyboard shortcuts", "?", func(a *App) tea.Cmd { a.dialog = newHelp(a); return nil }},
		{"quit", "exit (music keeps playing)", "q", func(a *App) tea.Cmd { return tea.Quit }},
		{"quit-and-stop", "exit and stop the music", "", func(a *App) tea.Cmd {
			_ = a.lib.Player.Quit()
			a.playerUp = false
			return tea.Quit
		}},
	}
}

// box renders a dialog panel: title, "esc" hint, body.
func box(a *App, title string, w int, body string) string {
	t := a.theme
	head := spread(t.bold().Render(title), t.muted().Render("esc"), w-4)
	content := head + "\n\n" + body
	inner := paint(indent(content, 2), t.Panel, w, 0)
	pad := paint("", t.Panel, w, 1)
	return pad + "\n" + inner + "\n" + pad
}

// pickList renders a list of rows with one highlighted.
func pickList(a *App, rows []string, rights []string, cursor, w, h int) string {
	t := a.theme
	top := 0
	if cursor >= h {
		top = cursor - h + 1
	}
	var out []string
	for i := top; i < len(rows) && i < top+h; i++ {
		right := ""
		if i < len(rights) {
			right = rights[i]
		}
		if i == cursor {
			// paint keeps the highlight under styled spans (swatches, muted text)
			line := " " + spread(fg(t.ink()).Bold(true).Render(rows[i]), right, w-2) + " "
			out = append(out, paint(line, t.Primary, w, 1))
		} else {
			out = append(out, " "+spread(t.text().Render(rows[i]), t.muted().Render(right), w-2)+" ")
		}
	}
	return strings.Join(out, "\n")
}

// ---- command palette ----------------------------------------------------------

type cmdPalette struct {
	in     textinput.Model
	all    []command
	shown  []command
	cursor int
}

func newPalette(a *App) *cmdPalette {
	p := &cmdPalette{all: commands(a)}
	p.in = textinput.New()
	p.in.Prompt = ""
	p.in.Placeholder = "Search commands…"
	p.in.Focus()
	p.filter()
	return p
}

func (p *cmdPalette) filter() {
	q := strings.TrimPrefix(strings.TrimSpace(p.in.Value()), "/")
	if q == "" {
		p.shown = p.all
	} else {
		p.shown = catalog.Rank(q, p.all, func(c command) string { return c.name + " " + c.desc })
	}
	p.cursor = clamp(p.cursor, 0, len(p.shown)-1)
}

func (p *cmdPalette) update(a *App, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+p":
		a.dialog = nil
		return nil
	case "up", "ctrl+k":
		p.cursor = clamp(p.cursor-1, 0, len(p.shown)-1)
		return nil
	case "down", "ctrl+j", "ctrl+n", "tab":
		p.cursor = clamp(p.cursor+1, 0, len(p.shown)-1)
		return nil
	case "enter":
		a.dialog = nil
		if p.cursor < len(p.shown) {
			return p.shown[p.cursor].run(a)
		}
		return nil
	}
	var cmd tea.Cmd
	p.in, cmd = p.in.Update(msg)
	p.filter()
	return cmd
}

func (p *cmdPalette) view(a *App) string {
	t := a.theme
	w := min(64, a.w-4)
	p.in.Width = w - 8
	p.in.TextStyle = t.text()
	p.in.PlaceholderStyle = t.subtle()
	p.in.Cursor.Style = t.primary()
	var rows, rights []string
	for _, c := range p.shown {
		rows = append(rows, c.name+t.muted().Render("  "+c.desc))
		rights = append(rights, c.key)
	}
	listH := min(len(rows), max(3, a.h/2))
	body := t.primary().Render("› ") + p.in.View() + "\n\n" + pickList(a, rows, rights, p.cursor, w-4, listH)
	if len(rows) == 0 {
		body += t.muted().Render("no matching commands")
	}
	return box(a, "Commands", w, body)
}

// ---- theme picker ----------------------------------------------------------------

type themePicker struct {
	names    []string
	cursor   int
	original string
}

func newThemePicker(a *App) *themePicker {
	tp := &themePicker{names: ThemeNames(), original: a.theme.Name}
	for i, n := range tp.names {
		if n == a.theme.Name {
			tp.cursor = i
		}
	}
	return tp
}

func (tp *themePicker) update(a *App, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		a.setTheme(tp.original)
		a.dialog = nil
	case "up", "k", "shift+tab":
		tp.cursor = clamp(tp.cursor-1, 0, len(tp.names)-1)
		a.setTheme(tp.names[tp.cursor])
	case "down", "j", "tab":
		tp.cursor = clamp(tp.cursor+1, 0, len(tp.names)-1)
		a.setTheme(tp.names[tp.cursor])
	case "enter":
		name := tp.names[tp.cursor]
		a.setTheme(name)
		a.dialog = nil
		// Session only: config.toml stays the default for the next launch.
		a.notify("theme: "+name+" (this session; keep it with `goose config set theme "+name+"`)", false)
	}
	return nil
}

func (tp *themePicker) view(a *App) string {
	w := min(44, a.w-4)
	var rows, rights []string
	for _, n := range tp.names {
		th := GetTheme(n)
		swatch := ""
		for _, c := range []lipgloss.TerminalColor{th.Primary, th.Secondary, th.Accent, th.Success} {
			swatch += fg(c).Render("●")
		}
		rows = append(rows, n)
		rights = append(rights, swatch)
	}
	h := min(len(rows), max(4, a.h-10))
	return box(a, "Themes", w, pickList(a, rows, rights, tp.cursor, w-4, h)+"\n\n"+
		a.theme.muted().Render("↑↓ preview · enter save"))
}

// ---- help -------------------------------------------------------------------------

type helpDialog struct{}

func newHelp(a *App) *helpDialog { return &helpDialog{} }

func (h *helpDialog) update(a *App, msg tea.KeyMsg) tea.Cmd {
	a.dialog = nil
	return nil
}

type helpSection struct {
	title string
	keys  [][2]string // key, what it does
}

var helpSections = []helpSection{
	{"Navigate", [][2]string{
		{"hjkl ←↓↑→", "move"}, {"enter", "open / play from here"}, {"esc ⌫", "back"},
		{"tab", "next section / tag"}, {"g G", "top / bottom"}, {"ctrl+d/u", "half page"},
		{"/", "search / filter"}, {"1-6", "home lib queue history dl lyrics"},
	}},
	{"Play", [][2]string{
		{"p", "play selection"}, {"a", "add to queue"}, {"A", "play next"},
		{"s", "shuffle selection"}, {"P S", "play / shuffle whole artist"}, {"m", "endless radio from here"},
		{"D", "download for offline"},
	}},
	{"Player", [][2]string{
		{"space", "pause"}, {"n N", "next / previous"}, {"[ ]", "seek ∓10s"},
		{"{ }", "seek ∓60s"}, {"- +", "volume"}, {"r", "repeat"},
	}},
	{"App", [][2]string{
		{"ctrl+p :", "commands"}, {"t", "themes"}, {"v", "grid / list"}, {"i", "art on/off"},
		{"b", "sidebar"}, {"q", "quit (music keeps going)"},
	}},
	{"Mouse", [][2]string{
		{"click", "select"}, {"dbl-click", "open / play from here"}, {"wheel", "scroll"},
		{"tab, chip", "switch to it"}, {"crumb", "go back there"}, {"progress", "seek"},
	}},
}

func (h *helpDialog) view(a *App) string {
	t := a.theme
	w := min(78, a.w-4)
	colW := (w - 6) / 2
	sections := helpSections
	if remapped := a.keys.pairs(); len(remapped) > 0 {
		sections = append(sections[:len(sections):len(sections)], helpSection{"Your keys (config.toml)", remapped})
	}
	var cols [2][]string
	for i, s := range sections {
		c := i % 2
		cols[c] = append(cols[c], t.primary().Bold(true).Render(s.title))
		for _, k := range s.keys {
			cols[c] = append(cols[c], t.text().Render(padRight(k[0], 11))+t.muted().Render(truncate(k[1], colW-12)))
		}
		cols[c] = append(cols[c], "")
	}
	left := paint(strings.Join(cols[0], "\n"), t.Panel, colW, 0)
	right := paint(strings.Join(cols[1], "\n"), t.Panel, colW, 0)
	h1, h2 := lipgloss.Height(left), lipgloss.Height(right)
	if h1 < h2 {
		left = paint(strings.Join(cols[0], "\n"), t.Panel, colW, h2)
	} else if h2 < h1 {
		right = paint(strings.Join(cols[1], "\n"), t.Panel, colW, h1)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, paint("", t.Panel, 2, max(h1, h2)), right)
	body += "\n" + t.muted().Render("every action is a command too — run `goose --help`")
	return box(a, "Keys", w, body)
}
