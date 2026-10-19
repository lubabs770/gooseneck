// Package library is the one service layer behind both the CLI and the TUI:
// catalog lookups, cached YouTube Music pages, queueing tracks into the
// player, and play history. Anything the TUI can do goes through here, which is
// what keeps every feature reachable from the command line too.
package library

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/catalog"
	"github.com/lubabs770/gooseneck/player/internal/config"
	"github.com/lubabs770/gooseneck/player/internal/mpv"
	"github.com/lubabs770/gooseneck/player/internal/scrobble"
	"github.com/lubabs770/gooseneck/player/internal/store"
	"github.com/lubabs770/gooseneck/player/internal/ytm"
)

const (
	artistTTL = 72 * time.Hour
	albumTTL  = 30 * 24 * time.Hour
)

// Lib bundles everything gooseneck needs at runtime.
type Lib struct {
	local *scrobble.Local // opened on first use

	Cfg    config.Config
	Cat    *catalog.Catalog
	DB     *store.Store
	YTM    *ytm.Client
	Player *mpv.Player
}

// Open loads the catalog and database for cfg.
func Open(cfg config.Config) (*Lib, error) {
	cat, err := catalog.LoadAll(cfg)
	if err != nil {
		return nil, err
	}
	db, err := store.Open(config.DBPath())
	if err != nil {
		return nil, fmt.Errorf("cache: %w", err)
	}
	_, _ = db.ImportLegacyLog(config.LegacyLogPath())
	return &Lib{Cfg: cfg, Cat: cat, DB: db, YTM: &ytm.Client{}, Player: mpv.New(cfg)}, nil
}

// Close releases the databases.
func (l *Lib) Close() error {
	if l.local != nil {
		l.local.Close()
	}
	return l.DB.Close()
}

// LocalScrobbles opens the local scrobble database (scrobbles.db).
func (l *Lib) LocalScrobbles() (*scrobble.Local, error) {
	if l.local != nil {
		return l.local, nil
	}
	lo, err := scrobble.OpenLocal(l.Cfg.ScrobblesPath())
	if err != nil {
		return nil, err
	}
	l.local = lo
	return lo, nil
}

// ---- artists & albums -----------------------------------------------------------

// Artist returns an artist page, from cache when fresh. When YouTube Music is
// unreachable a stale cached copy is returned; when the channel isn't a YouTube
// Music artist, its uploads are listed via yt-dlp as one "All uploads" album.
func (l *Lib) Artist(ctx context.Context, id string, fresh bool) (*ytm.Artist, error) {
	key := "artist:" + id
	var cached ytm.Artist
	found, stale := l.DB.GetJSON(key, artistTTL, &cached)
	if found && !stale && !fresh {
		return l.decorate(&cached), nil
	}
	a, err := l.YTM.Artist(ctx, id)
	if err != nil {
		if found {
			return l.decorate(&cached), nil
		}
		if errors.Is(err, ytm.ErrNotArtist) || ctx.Err() == nil {
			if fb, ferr := l.uploadsArtist(id); ferr == nil {
				_ = l.DB.PutJSON(key, fb)
				return l.decorate(fb), nil
			}
		}
		return nil, err
	}
	_ = l.DB.PutJSON(key, a)
	return l.decorate(a), nil
}

// decorate prefers the curated catalog name and picture when the artist is in
// the catalog (YouTube sometimes appends " - Topic" or uses another spelling).
func (l *Lib) decorate(a *ytm.Artist) *ytm.Artist {
	if c, ok := l.Cat.Get(a.ID); ok {
		if c.Name != "" {
			a.Name = c.Name
		}
		if a.Thumb == "" {
			a.Thumb = c.Thumbnail
		}
	}
	return a
}

// CachedArtist returns the cached page without touching the network.
func (l *Lib) CachedArtist(id string) (*ytm.Artist, bool) {
	var a ytm.Artist
	found, _ := l.DB.GetJSON("artist:"+id, artistTTL, &a)
	if !found {
		return nil, false
	}
	return l.decorate(&a), true
}

// Album returns an album page, from cache when fresh.
func (l *Lib) Album(ctx context.Context, browseID string, fresh bool) (*ytm.Album, error) {
	if strings.HasPrefix(browseID, uploadsPrefix) {
		return l.uploadsAlbum(strings.TrimPrefix(browseID, uploadsPrefix))
	}
	key := "album:" + browseID
	var cached ytm.Album
	found, stale := l.DB.GetJSON(key, albumTTL, &cached)
	if found && !stale && !fresh {
		return &cached, nil
	}
	al, err := l.YTM.Album(ctx, browseID)
	if err != nil {
		if found {
			return &cached, nil
		}
		return nil, err
	}
	_ = l.DB.PutJSON(key, al)
	return al, nil
}

// ArtistTracks gathers every track from an artist's albums and singles (in
// discography order, de-duplicated), fetching uncached albums in parallel.
func (l *Lib) ArtistTracks(ctx context.Context, a *ytm.Artist) ([]ytm.Song, error) {
	rels := a.Releases()
	if len(rels) == 0 {
		if len(a.TopSongs) > 0 {
			return a.TopSongs, nil
		}
		return nil, fmt.Errorf("%s has no releases", a.Name)
	}
	albums := make([]*ytm.Album, len(rels))
	errs := make([]error, len(rels))
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i, r := range rels {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			albums[i], errs[i] = l.Album(ctx, id, false)
		}(i, r.BrowseID)
	}
	wg.Wait()
	seen := map[string]bool{}
	var out []ytm.Song
	var firstErr error
	for i, al := range albums {
		if al == nil {
			if firstErr == nil {
				firstErr = errs[i]
			}
			continue
		}
		for _, t := range al.Tracks {
			if !seen[t.VideoID] {
				seen[t.VideoID] = true
				out = append(out, t)
			}
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// ---- resolving user input ---------------------------------------------------------

// ResolveArtist accepts a catalog name/fragment or a channel id ("UC…", which
// need not be in the catalog).
func (l *Lib) ResolveArtist(q string) (catalog.Artist, error) {
	q = strings.TrimSpace(q)
	if strings.HasPrefix(q, "UC") && len(q) == 24 && !strings.Contains(q, " ") {
		if a, ok := l.Cat.Get(q); ok {
			return a, nil
		}
		return catalog.Artist{ID: q, Name: q}, nil
	}
	return l.Cat.Resolve(q)
}

// ResolveRelease picks an album/single by browse id, 1-based number, or title.
func ResolveRelease(a *ytm.Artist, q string) (ytm.Release, error) {
	rels := a.Releases()
	q = strings.TrimSpace(q)
	if n, err := strconv.Atoi(q); err == nil && n >= 1 && n <= len(rels) {
		return rels[n-1], nil
	}
	for _, r := range rels {
		if r.BrowseID == q || strings.EqualFold(r.Title, q) {
			return r, nil
		}
	}
	ranked := catalog.Rank(q, rels, func(r ytm.Release) string { return r.Title })
	switch {
	case len(ranked) == 0:
		return ytm.Release{}, fmt.Errorf("%s has no album matching %q", a.Name, q)
	case len(ranked) == 1, strings.HasPrefix(catalog.Fold(ranked[0].Title), catalog.Fold(q)) &&
		!strings.HasPrefix(catalog.Fold(ranked[1].Title), catalog.Fold(q)):
		return ranked[0], nil
	}
	names := make([]string, 0, 8)
	for i, r := range ranked {
		if i == 8 {
			break
		}
		names = append(names, "  "+r.Title)
	}
	return ytm.Release{}, fmt.Errorf("%q matches several releases:\n%s", q, strings.Join(names, "\n"))
}

// ResolveTrack picks a track by 1-based number or title; returns its index.
func ResolveTrack(tracks []ytm.Song, q string) (int, error) {
	q = strings.TrimSpace(q)
	if n, err := strconv.Atoi(q); err == nil && n >= 1 && n <= len(tracks) {
		return n - 1, nil
	}
	for i, t := range tracks {
		if strings.EqualFold(t.Title, q) || t.VideoID == q {
			return i, nil
		}
	}
	ranked := catalog.Rank(q, tracks, func(t ytm.Song) string { return t.Title })
	if len(ranked) == 0 {
		return 0, fmt.Errorf("no track matches %q", q)
	}
	for i, t := range tracks {
		if t.VideoID == ranked[0].VideoID {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no track matches %q", q)
}

// ---- playback -------------------------------------------------------------------

// PlayRequest describes what to queue and how to label it in history.
type PlayRequest struct {
	Songs    []ytm.Song
	Mode     mpv.Mode
	Shuffle  bool
	ArtistID string
	Artist   string
	Album    string
	Radio    bool // part of a radio session (doesn't end it)
	NoLog    bool // don't add a history entry (radio top-ups)
}

// Play remembers the songs' metadata, queues them and logs the play.
func (l *Lib) Play(req PlayRequest) error {
	if len(req.Songs) == 0 {
		return errors.New("nothing to play")
	}
	songs := req.Songs
	if req.Shuffle {
		songs = shuffled(songs)
	}
	meta := make([]store.Track, len(songs))
	ids := make([]string, len(songs))
	for i, s := range songs {
		ids[i] = s.VideoID
		if p := l.LocalPath(s.VideoID); p != "" {
			ids[i] = p // downloaded: play the file, not the stream
		}
		meta[i] = TrackMeta(s)
	}
	_ = l.DB.PutTracks(meta)
	if err := l.Player.Load(ids, req.Mode); err != nil {
		return err
	}
	if req.Mode == mpv.Replace && !req.Radio {
		l.StopRadio() // playing something else ends the radio
	}
	l.EnsureDaemon()
	if req.NoLog {
		return nil
	}
	ev := store.Play{ArtistID: req.ArtistID, Artist: req.Artist, Album: req.Album, Count: len(songs)}
	if len(songs) == 1 {
		ev.Track, ev.VideoID = songs[0].Title, songs[0].VideoID
		if ev.Artist == "" {
			ev.Artist, ev.ArtistID = songs[0].Artist, songs[0].ArtistID
		}
		if ev.Album == "" {
			ev.Album = songs[0].Album
		}
	}
	_ = l.DB.LogPlay(ev)
	return nil
}

// TrackMeta converts a song to the stored metadata form.
func TrackMeta(s ytm.Song) store.Track {
	return store.Track{VideoID: s.VideoID, Title: s.Title, Artist: s.Artist, ArtistID: s.ArtistID,
		Album: s.Album, AlbumID: s.AlbumID, Duration: s.Duration, Thumb: s.Thumb}
}

// NowPlaying is the player status joined with the current track's metadata.
type NowPlaying struct {
	mpv.Status
	Track store.Track `json:"track"`
}

// Now returns what's playing. Not running -> mpv.ErrNotRunning.
func (l *Lib) Now() (NowPlaying, error) {
	st, err := l.Player.Status()
	if err != nil {
		return NowPlaying{Status: st}, err
	}
	np := NowPlaying{Status: st}
	if st.VideoID != "" {
		if t, ok := l.DB.Track(st.VideoID); ok {
			np.Track = t
		} else {
			np.Track = store.Track{VideoID: st.VideoID, Title: st.Title}
		}
		if np.Track.Duration == 0 && st.Duration > 0 {
			np.Track.Duration = int(st.Duration)
		}
	}
	return np, nil
}

// QueueItem is a queue entry with metadata.
type QueueItem struct {
	Index   int         `json:"index"`
	Current bool        `json:"current"`
	Track   store.Track `json:"track"`
}

// Queue lists the player's queue with metadata.
func (l *Lib) Queue() ([]QueueItem, error) {
	entries, err := l.Player.Queue()
	if err != nil {
		return nil, err
	}
	out := make([]QueueItem, len(entries))
	for i, e := range entries {
		t, ok := l.DB.Track(e.VideoID)
		if !ok {
			t = store.Track{VideoID: e.VideoID, Title: e.Title}
			if t.Title == "" {
				t.Title = e.VideoID
			}
		}
		out[i] = QueueItem{Index: e.Index, Current: e.Current, Track: t}
	}
	return out, nil
}
