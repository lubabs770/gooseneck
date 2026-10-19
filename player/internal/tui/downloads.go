package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lubabs770/gooseneck/player/internal/library"
	"github.com/lubabs770/gooseneck/player/internal/store"
	"github.com/lubabs770/gooseneck/player/internal/ytm"
)

// Downloads run one job at a time in the background; more requests queue up.
// Progress streams back over a channel as dlProgress messages.

type dlJob struct {
	label string
	songs []ytm.Song
	fetch func(ctx context.Context) ([]ytm.Song, error) // lazy (e.g. a whole artist)
}

type dlState struct {
	queue   []dlJob
	active  *dlJob
	ch      chan tea.Msg
	cancel  context.CancelFunc
	done    int
	total   int
	current string
	pct     float64
	failed  int
}

type dlProgress struct{ ev library.DownloadEvent }

type dlFinished struct {
	label string
	res   library.DownloadResult
	err   error
}

// download queues a job and starts it if nothing is running.
func (a *App) download(job dlJob) tea.Cmd {
	a.dl.queue = append(a.dl.queue, job)
	if a.dl.active != nil {
		a.notify(fmt.Sprintf("queued download: %s (%d waiting)", job.label, len(a.dl.queue)), false)
		return nil
	}
	return a.nextDownload()
}

func (a *App) nextDownload() tea.Cmd {
	if len(a.dl.queue) == 0 {
		a.dl.active = nil
		return nil
	}
	job := a.dl.queue[0]
	a.dl.queue = a.dl.queue[1:]
	a.dl.active = &job
	a.dl.done, a.dl.total, a.dl.failed, a.dl.pct, a.dl.current = 0, len(job.songs), 0, 0, ""
	ch := make(chan tea.Msg, 256)
	ctx, cancel := context.WithCancel(context.Background())
	a.dl.ch, a.dl.cancel = ch, cancel
	l := a.lib
	go func() {
		songs := job.songs
		if job.fetch != nil {
			var err error
			if songs, err = job.fetch(ctx); err != nil {
				ch <- dlFinished{label: job.label, err: err}
				return
			}
		}
		res := l.Download(ctx, songs, func(ev library.DownloadEvent) {
			select {
			case ch <- dlProgress{ev}:
			default: // UI behind: drop intermediate progress, never block downloads
				if ev.Done {
					ch <- dlProgress{ev}
				}
			}
		})
		ch <- dlFinished{label: job.label, res: res}
	}()
	return waitDL(ch)
}

func waitDL(ch chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }

// updateDL handles download messages; ok=false if msg isn't one.
func (a *App) updateDL(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case dlProgress:
		ev := msg.ev
		a.dl.total = ev.Total
		if ev.Done {
			a.dl.done++
			if ev.Err != nil {
				a.dl.failed++
			}
		} else {
			a.dl.current, a.dl.pct = ev.Song.Title, ev.Percent
		}
		return waitDL(a.dl.ch), true
	case dlFinished:
		switch {
		case msg.err != nil:
			a.notify("download failed: "+msg.err.Error(), true)
		case msg.res.Failed > 0:
			a.notify(fmt.Sprintf("↓ %s: %d saved, %d failed", msg.label, msg.res.Downloaded+msg.res.Skipped, msg.res.Failed), true)
		default:
			a.notify(fmt.Sprintf("↓ %s: %d tracks offline", msg.label, msg.res.Downloaded+msg.res.Skipped), false)
		}
		if a.dl.cancel != nil {
			a.dl.cancel()
		}
		cmds := []tea.Cmd{a.nextDownload()}
		for _, p := range a.stack { // refresh "downloaded" markers
			cmds = append(cmds, p.update(a, msg))
		}
		return tea.Batch(cmds...), true
	}
	return nil, false
}

// dlStatus is the status-bar text while downloading ("" when idle).
func (a *App) dlStatus() string {
	if a.dl.active == nil {
		return ""
	}
	t := a.theme
	s := fg(t.Success).Render("↓ ") + t.text().Render(fmt.Sprintf("%d/%d", a.dl.done, a.dl.total))
	if a.dl.current != "" {
		s += " " + t.muted().Render(truncate(a.dl.current, 28)+fmt.Sprintf(" %.0f%%", a.dl.pct))
	}
	if n := len(a.dl.queue); n > 0 {
		s += t.subtle().Render(fmt.Sprintf(" +%d", n))
	}
	return s
}

func (a *App) cancelDownloads() {
	a.dl.queue = nil
	if a.dl.cancel != nil {
		a.dl.cancel()
	}
}

// artistDownload is a lazy job for everything an artist released.
func (a *App) artistDownload(id, name string) dlJob {
	l := a.lib
	return dlJob{label: name, fetch: func(ctx context.Context) ([]ytm.Song, error) {
		page, err := l.Artist(ctx, id, false)
		if err != nil {
			return nil, err
		}
		return l.ArtistTracks(ctx, page)
	}}
}

// releaseDownload is a lazy job for one album/single.
func (a *App) releaseDownload(browseID, title string) dlJob {
	l := a.lib
	return dlJob{label: title, fetch: func(ctx context.Context) ([]ytm.Song, error) {
		al, err := l.Album(ctx, browseID, false)
		if err != nil {
			return nil, err
		}
		return al.Tracks, nil
	}}
}

// ---- downloads page ----------------------------------------------------------------

type downloadsPage struct{ items *collection }

type downloadsLoaded struct{ list []store.Download }

func newDownloadsPage(a *App) (*downloadsPage, tea.Cmd) {
	p := &downloadsPage{items: newCollection(false, false, "Nothing downloaded yet. Press D on an album or artist.")}
	return p, p.load(a)
}

func (p *downloadsPage) load(a *App) tea.Cmd {
	l := a.lib
	return func() tea.Msg {
		list, _ := l.DB.Downloads()
		return downloadsLoaded{list}
	}
}

func (p *downloadsPage) title() string       { return "Downloads" }
func (p *downloadsPage) mode() string        { return "OFFLINE" }
func (p *downloadsPage) coll() *collection   { return p.items }
func (p *downloadsPage) filterText() string  { return p.items.filter }
func (p *downloadsPage) setFilter(f string)  { p.items.setFilter(f) }
func (p *downloadsPage) placeholder() string { return "Filter downloads…" }
func (p *downloadsPage) hints(a *App) []hint {
	return []hint{{"enter", "play from here"}, {"s", "shuffle all"}, {"d", "delete"}}
}

func (p *downloadsPage) update(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case downloadsLoaded:
		var items []entry
		for _, d := range msg.list {
			sub := d.Artist
			if d.Album != "" {
				sub += " · " + d.Album
			}
			items = append(items, entry{key: d.VideoID, title: orDefault(d.Title, d.VideoID), sub: sub,
				right: fmt.Sprintf("%.1f MB", float64(d.Size)/1e6), offline: true, data: d})
		}
		p.items.set(items)
	case dlFinished:
		return p.load(a)
	case tea.MouseMsg:
		scrollMouse(p.items, msg)
	case tea.KeyMsg:
		k := msg.String()
		if p.items.key(k, true) {
			return nil
		}
		songs := func(es []entry) []ytm.Song {
			var out []ytm.Song
			for _, e := range es {
				d := e.data.(store.Download)
				out = append(out, ytm.Song{VideoID: d.VideoID, Title: d.Title, Artist: d.Artist, ArtistID: d.ArtistID,
					Album: d.Album, AlbumID: d.AlbumID, Duration: d.Duration, Thumb: d.Thumb})
			}
			return out
		}
		switch k {
		case "enter", "p":
			if e, ok := p.items.selected(); ok {
				return a.play(library.PlayRequest{Songs: songs(p.items.from())}, e.title)
			}
		case "s":
			return a.play(library.PlayRequest{Songs: songs(p.items.visible()), Shuffle: true}, "downloads")
		case "d", "x", "delete":
			if e, ok := p.items.selected(); ok {
				l := a.lib
				id := e.key
				reload := p.load(a)
				return tea.Batch(a.job(func() (string, error) {
					_, err := l.RemoveDownloads([]string{id})
					return "deleted " + e.title, err
				}), reload)
			}
		}
	}
	return nil
}

func (p *downloadsPage) view(a *App, w, h int) string {
	t := a.theme
	var total float64
	for _, e := range p.items.items {
		total += float64(e.data.(store.Download).Size)
	}
	head := spread(t.bold().Render("Downloads"),
		t.muted().Render(fmt.Sprintf("%d tracks · %.0f MB", len(p.items.items), total/1e6)), w)
	return head + "\n\n" + p.items.view(a, w, h-2)
}
