package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/art"
	"github.com/lubabs770/gooseneck/player/internal/catalog"
	"github.com/lubabs770/gooseneck/player/internal/config"
	"github.com/lubabs770/gooseneck/player/internal/library"
	"github.com/lubabs770/gooseneck/player/internal/mpv"
	"github.com/lubabs770/gooseneck/player/internal/scrobble"
	"github.com/lubabs770/gooseneck/player/internal/store"
	"github.com/lubabs770/gooseneck/player/internal/ytm"
)

func (s *Server) routes(m *http.ServeMux) {
	// browse
	m.HandleFunc("GET /api/home", s.api(s.home))
	m.HandleFunc("GET /api/search", s.api(s.search))
	m.HandleFunc("GET /api/tags", s.api(s.tags))
	m.HandleFunc("GET /api/catalogs", s.api(s.catalogs))
	m.HandleFunc("GET /api/artist/{id}", s.api(s.artist))
	m.HandleFunc("GET /api/album/{id}", s.api(s.album))

	// playback
	m.HandleFunc("GET /api/now", s.api(s.now))
	m.HandleFunc("GET /api/queue", s.api(s.queue))
	m.HandleFunc("GET /api/events", s.events)
	m.HandleFunc("POST /api/play", s.api(s.play))
	m.HandleFunc("POST /api/control/{action}", s.api(s.control))
	m.HandleFunc("POST /api/seek", s.api(s.seek))
	m.HandleFunc("POST /api/volume", s.api(s.volume))
	m.HandleFunc("POST /api/repeat", s.api(s.repeat))
	m.HandleFunc("POST /api/queue/jump/{n}", s.api(s.queueJump))
	m.HandleFunc("POST /api/queue/rm/{n}", s.api(s.queueRemove))
	m.HandleFunc("POST /api/queue/clear", s.api(s.queueClear))
	m.HandleFunc("GET /api/radio", s.api(s.radioStatus))
	m.HandleFunc("POST /api/radio", s.api(s.radioStart))
	m.HandleFunc("DELETE /api/radio", s.api(s.radioStop))
	m.HandleFunc("GET /api/lyrics", s.api(s.lyrics))

	// history & stats
	m.HandleFunc("GET /api/history", s.api(s.history))
	m.HandleFunc("GET /api/scrobbles", s.api(s.scrobbles))
	m.HandleFunc("GET /api/scrobbles/top/{kind}", s.api(s.scrobblesTop))
	m.HandleFunc("GET /api/scrobbles/stats", s.api(s.scrobblesStats))

	// downloads
	m.HandleFunc("GET /api/downloads", s.api(s.downloads))
	m.HandleFunc("POST /api/download", s.api(s.download))
	m.HandleFunc("DELETE /api/downloads", s.api(s.removeDownloads))

	// catalogs & config (these swap lib.Cat / lib.Cfg)
	m.HandleFunc("POST /api/catalogs/{name}/artists", s.apiExclusive(s.catalogAdd))
	m.HandleFunc("DELETE /api/catalogs/{name}/artists/{id}", s.apiExclusive(s.catalogRemove))
	m.HandleFunc("POST /api/catalogs/update", s.apiExclusive(s.catalogUpdate))
	m.HandleFunc("GET /api/config", s.api(s.config))
	m.HandleFunc("POST /api/config", s.apiExclusive(s.setConfig))

	m.HandleFunc("GET /art", s.art)
}

// ---- views ------------------------------------------------------------------

type artistView struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Thumb    string   `json:"thumb,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Catalogs []string `json:"catalogs,omitempty"`
}

func viewArtist(a catalog.Artist) artistView {
	v := artistView{ID: a.ID, Name: a.Name, Thumb: a.Thumbnail, Catalogs: a.Catalogs}
	for _, t := range catalog.Tags {
		if t.Has(a) {
			v.Tags = append(v.Tags, t.Key)
		}
	}
	return v
}

// radioView drops the session's bookkeeping (played ids, seed pool).
func radioView(st library.RadioState) map[string]any {
	return map[string]any{"active": st.Active, "seed_id": st.SeedID, "seed_name": st.SeedName,
		"started_at": st.StartedAt, "batches": st.Batches, "tracks": len(st.Played), "current": st.Current}
}

func ctx(r *http.Request) context.Context { return r.Context() }

func fresh(r *http.Request) bool { return r.URL.Query().Get("fresh") != "" }

func limit(r *http.Request, def int) int {
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		return n
	}
	return def
}

// ---- browse -----------------------------------------------------------------

type albumCard struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist,omitempty"`
	ArtistID string `json:"artist_id,omitempty"`
	Thumb    string `json:"thumb,omitempty"`
}

// home: Quick picks (recently heard tracks), Listen again (their albums) and
// favourite artists, all from the local listening log and play history.
func (s *Server) home(r *http.Request) (any, error) {
	picks := []store.Track{}
	albums := []albumCard{}
	if lo, err := s.lib.LocalScrobbles(); err == nil {
		recent, _ := lo.Recent(scrobble.Range{}, 300)
		seenT, seenA := map[string]bool{}, map[string]bool{}
		for _, l := range recent {
			t, ok := s.lib.DB.Track(l.VideoID)
			if !ok {
				t = store.Track{VideoID: l.VideoID, Title: l.Track, Artist: l.Artist, Album: l.Album, Duration: l.Duration}
			}
			if !seenT[t.VideoID] && len(picks) < 12 {
				seenT[t.VideoID] = true
				picks = append(picks, t)
			}
			if t.AlbumID != "" && !seenA[t.AlbumID] && len(albums) < 12 {
				seenA[t.AlbumID] = true
				albums = append(albums, albumCard{ID: t.AlbumID, Title: t.Album, Artist: t.Artist, ArtistID: t.ArtistID, Thumb: t.Thumb})
			}
		}
	}
	artists := []map[string]any{}
	top, err := s.lib.DB.TopArtists(16)
	if err != nil {
		return nil, err
	}
	for _, st := range top {
		v := map[string]any{"id": st.ArtistID, "name": st.Artist, "plays": st.Plays}
		if a, ok := s.lib.Cat.Get(st.ArtistID); ok {
			v["thumb"] = a.Thumbnail
		}
		artists = append(artists, v)
	}
	return map[string]any{"quick_picks": picks, "albums": albums, "artists": artists}, nil
}

func (s *Server) search(r *http.Request) (any, error) {
	q := r.URL.Query()
	cat := s.lib.Cat
	if name := q.Get("catalog"); name != "" {
		sub, err := cat.Subset(name)
		if err != nil {
			return nil, httpError{http.StatusNotFound, err.Error()}
		}
		cat = sub
	}
	var tag *catalog.Tag
	if k := q.Get("tag"); k != "" {
		t, err := catalog.FindTag(k)
		if err != nil {
			return nil, bad("%v", err)
		}
		tag = t
	}
	found := cat.Search(q.Get("q"), tag)
	n := limit(r, len(found))
	out := make([]artistView, 0, min(n, len(found)))
	for _, a := range found[:min(n, len(found))] {
		out = append(out, viewArtist(a))
	}
	return out, nil
}

func (s *Server) tags(r *http.Request) (any, error) {
	out := []map[string]string{}
	for _, t := range catalog.Tags {
		out = append(out, map[string]string{"key": t.Key, "label": t.Label})
	}
	return out, nil
}

func (s *Server) catalogs(r *http.Request) (any, error) {
	out := []map[string]any{{"name": catalog.MainName, "editable": false}}
	for _, src := range s.lib.Cfg.Catalogs {
		out = append(out, map[string]any{"name": src.Name, "editable": src.URL == "", "url": src.URL, "disabled": src.Disabled})
	}
	for _, c := range out {
		if sub, err := s.lib.Cat.Subset(c["name"].(string)); err == nil {
			c["count"] = sub.Len()
		}
	}
	return out, nil
}

func (s *Server) artist(r *http.Request) (any, error) {
	a, err := s.lib.Artist(ctx(r), r.PathValue("id"), fresh(r))
	if err != nil {
		return nil, err
	}
	out := map[string]any{"artist": a, "downloaded": s.lib.IsDownloaded(a.TopSongs)}
	if c, ok := s.lib.Cat.Get(a.ID); ok {
		out["catalog"] = viewArtist(c)
	}
	return out, nil
}

func (s *Server) album(r *http.Request) (any, error) {
	al, err := s.lib.Album(ctx(r), r.PathValue("id"), fresh(r))
	if err != nil {
		return nil, err
	}
	return map[string]any{"album": al, "downloaded": s.lib.IsDownloaded(al.Tracks)}, nil
}

// ---- playback ---------------------------------------------------------------

func (s *Server) now(r *http.Request) (any, error) {
	np, err := s.lib.Now()
	if err != nil && !errors.Is(err, mpv.ErrNotRunning) {
		return nil, err
	}
	return np, nil // not running: running=false
}

func (s *Server) queue(r *http.Request) (any, error) {
	q, err := s.lib.Queue()
	if errors.Is(err, mpv.ErrNotRunning) || q == nil && err == nil {
		return []library.QueueItem{}, nil
	}
	return q, err
}

// target is what /api/play and /api/download act on, most specific first:
// explicit songs, an album (from track index Track), or an artist (all
// releases, or just top songs).
type target struct {
	Songs    []ytm.Song `json:"songs"`
	AlbumID  string     `json:"album_id"`
	Track    int        `json:"track"` // 0-based start within the album
	ArtistID string     `json:"artist_id"`
	Top      bool       `json:"top"`
}

func (s *Server) resolve(c context.Context, t target) (library.PlayRequest, error) {
	switch {
	case len(t.Songs) > 0:
		return library.PlayRequest{Songs: t.Songs}, nil
	case t.AlbumID != "":
		al, err := s.lib.Album(c, t.AlbumID, false)
		if err != nil {
			return library.PlayRequest{}, err
		}
		if t.Track < 0 || t.Track >= max(len(al.Tracks), 1) {
			return library.PlayRequest{}, bad("track %d out of range (album has %d)", t.Track, len(al.Tracks))
		}
		return library.PlayRequest{Songs: al.Tracks[t.Track:], ArtistID: al.ArtistID, Artist: al.Artist, Album: al.Title}, nil
	case t.ArtistID != "":
		a, err := s.lib.Artist(c, t.ArtistID, false)
		if err != nil {
			return library.PlayRequest{}, err
		}
		req := library.PlayRequest{ArtistID: a.ID, Artist: a.Name, Songs: a.TopSongs}
		if !t.Top {
			req.Songs, err = s.lib.ArtistTracks(c, a)
		}
		return req, err
	}
	return library.PlayRequest{}, bad("nothing to play: give songs, album_id or artist_id")
}

func (s *Server) play(r *http.Request) (any, error) {
	var in struct {
		target
		Mode    string `json:"mode"` // replace | append | next
		Shuffle bool   `json:"shuffle"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	mode, ok := map[string]mpv.Mode{"": mpv.Replace, "replace": mpv.Replace, "append": mpv.Append, "next": mpv.Next}[in.Mode]
	if !ok {
		return nil, bad("mode must be replace, append or next")
	}
	req, err := s.resolve(ctx(r), in.target)
	if err != nil {
		return nil, err
	}
	req.Mode, req.Shuffle = mode, in.Shuffle
	if err := s.lib.Play(req); err != nil {
		return nil, err
	}
	return map[string]any{"queued": len(req.Songs)}, nil
}

func (s *Server) control(r *http.Request) (any, error) {
	p := s.lib.Player
	switch r.PathValue("action") {
	case "pause":
		return nil, p.SetPause(true)
	case "resume":
		return nil, p.SetPause(false)
	case "toggle":
		paused, err := p.TogglePause()
		return map[string]bool{"paused": paused}, err
	case "next":
		return nil, p.Next()
	case "prev":
		return nil, p.Prev()
	case "stop":
		return nil, p.Stop()
	case "shuffle":
		return nil, p.Shuffle()
	}
	return nil, httpError{http.StatusNotFound, "unknown action"}
}

func (s *Server) seek(r *http.Request) (any, error) {
	var in struct {
		Sec      float64 `json:"sec"`
		Absolute bool    `json:"absolute"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	return nil, s.lib.Player.Seek(in.Sec, in.Absolute)
}

func (s *Server) volume(r *http.Request) (any, error) {
	var in struct {
		V        float64 `json:"v"`
		Absolute bool    `json:"absolute"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	v, err := s.lib.Player.Volume(in.V, in.Absolute)
	return map[string]float64{"volume": v}, err
}

func (s *Server) repeat(r *http.Request) (any, error) {
	var in struct {
		Mode string `json:"mode"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	if in.Mode != "off" && in.Mode != "all" && in.Mode != "one" {
		return nil, bad("repeat mode must be off, all or one")
	}
	return nil, s.lib.Player.Repeat(in.Mode)
}

func pathIndex(r *http.Request) (int, error) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 {
		return 0, bad("want a 0-based queue index")
	}
	return n, nil
}

func (s *Server) queueJump(r *http.Request) (any, error) {
	n, err := pathIndex(r)
	if err != nil {
		return nil, err
	}
	return nil, s.lib.Player.Jump(n)
}

func (s *Server) queueRemove(r *http.Request) (any, error) {
	n, err := pathIndex(r)
	if err != nil {
		return nil, err
	}
	return nil, s.lib.Player.Remove(n)
}

func (s *Server) queueClear(r *http.Request) (any, error) { return nil, s.lib.Player.Clear() }

func (s *Server) radioStatus(r *http.Request) (any, error) {
	return radioView(s.lib.RadioStatus()), nil
}

func (s *Server) radioStart(r *http.Request) (any, error) {
	var in struct {
		ArtistID string `json:"artist_id"`
		Size     int    `json:"size"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	if in.ArtistID == "" {
		return nil, bad("artist_id required")
	}
	if in.Size <= 0 {
		in.Size = 30
	}
	seed, songs, err := s.lib.StartRadio(ctx(r), in.ArtistID, in.Size)
	if err != nil {
		return nil, err
	}
	return map[string]any{"seed": seed.Name, "queued": len(songs)}, nil
}

func (s *Server) radioStop(r *http.Request) (any, error) {
	s.lib.StopRadio()
	return nil, nil
}

// lyrics for the current track (or ?video_id=, which must have metadata).
func (s *Server) lyrics(r *http.Request) (any, error) {
	var t store.Track
	if id := r.URL.Query().Get("video_id"); id != "" {
		var ok bool
		if t, ok = s.lib.DB.Track(id); !ok {
			return nil, httpError{http.StatusNotFound, "unknown track"}
		}
	} else {
		np, err := s.lib.Now()
		if err != nil {
			return nil, err
		}
		if np.Track.VideoID == "" {
			return nil, httpError{http.StatusNotFound, "nothing playing"}
		}
		t = np.Track
	}
	ly, err := s.lib.Lyrics(ctx(r), t, fresh(r))
	if err != nil {
		return nil, err
	}
	out := map[string]any{"video_id": t.VideoID, "found": ly != nil}
	if ly != nil {
		out["synced"] = ly.Synced != ""
		out["instrumental"] = ly.Instrumental
		out["lines"] = ly.Lines()
	}
	return out, nil
}

// ---- history & stats --------------------------------------------------------

func (s *Server) history(r *http.Request) (any, error) {
	h, err := s.lib.DB.History(limit(r, 100))
	if h == nil {
		h = []store.Play{}
	}
	return h, err
}

func (s *Server) local(r *http.Request) (*scrobble.Local, scrobble.Range, error) {
	lo, err := s.lib.LocalScrobbles()
	if err != nil {
		return nil, scrobble.Range{}, err
	}
	rg, err := scrobble.ParseSince(r.URL.Query().Get("since"), time.Now())
	if err != nil {
		return nil, rg, bad("%v", err)
	}
	return lo, rg, nil
}

func (s *Server) scrobbles(r *http.Request) (any, error) {
	lo, rg, err := s.local(r)
	if err != nil {
		return nil, err
	}
	list, err := lo.Recent(rg, limit(r, 200))
	if list == nil {
		list = []scrobble.Listen{}
	}
	return list, err
}

func (s *Server) scrobblesTop(r *http.Request) (any, error) {
	kind := r.PathValue("kind")
	if kind != "artist" && kind != "track" && kind != "album" {
		return nil, bad("top of what? artist, track or album")
	}
	lo, rg, err := s.local(r)
	if err != nil {
		return nil, err
	}
	top, err := lo.Top(kind, rg, limit(r, 10))
	if err != nil {
		return nil, err
	}
	if top == nil {
		top = []scrobble.Count{}
	}
	return top, nil
}

func (s *Server) scrobblesStats(r *http.Request) (any, error) {
	lo, rg, err := s.local(r)
	if err != nil {
		return nil, err
	}
	return lo.Totals(rg)
}

// ---- downloads --------------------------------------------------------------

func (s *Server) downloads(r *http.Request) (any, error) {
	list, err := s.lib.DB.Downloads()
	if list == nil {
		list = []store.Download{}
	}
	return map[string]any{"dir": s.lib.Cfg.Downloads(), "tracks": list}, err
}

// download resolves the target now and runs the job in the background, one
// job at a time; progress goes out as "download" events.
func (s *Server) download(r *http.Request) (any, error) {
	var in target
	if err := body(r, &in); err != nil {
		return nil, err
	}
	req, err := s.resolve(ctx(r), in)
	if err != nil {
		return nil, err
	}
	songs := req.Songs
	go func() {
		s.dl.Lock()
		defer s.dl.Unlock()
		res := s.lib.Download(context.Background(), songs, func(ev library.DownloadEvent) {
			if ev.Err != nil {
				ev.Error = ev.Err.Error()
			}
			s.hub.publish("download", ev, false)
		})
		s.hub.publish("download_done", map[string]any{"downloaded": res.Downloaded, "skipped": res.Skipped,
			"failed": res.Failed, "bytes": res.Bytes}, false)
	}()
	return map[string]int{"queued": len(songs)}, nil
}

func (s *Server) removeDownloads(r *http.Request) (any, error) {
	var in struct {
		VideoIDs []string `json:"video_ids"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	n, err := s.lib.RemoveDownloads(in.VideoIDs)
	return map[string]int{"removed": n}, err
}

// ---- catalogs & config ------------------------------------------------------

func (s *Server) reloadCatalog() error {
	cat, err := catalog.LoadAll(s.lib.Cfg)
	if err != nil {
		return err
	}
	s.lib.Cat = cat
	return nil
}

func (s *Server) catalogAdd(r *http.Request) (any, error) {
	src, err := library.EditableCatalog(s.lib.Cfg, r.PathValue("name"))
	if err != nil {
		return nil, bad("%v", err)
	}
	var in struct {
		ID string `json:"id"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	ar, ok := s.lib.Cat.Get(in.ID) // keep curated name, picture and tags
	if !ok {
		page, err := s.lib.Artist(ctx(r), in.ID, false)
		if err != nil {
			return nil, err
		}
		ar = catalog.Artist{ID: page.ID, Name: page.Name, Thumbnail: page.Thumb}
	}
	added, err := catalog.AddTo(src.File(), ar)
	if err != nil {
		return nil, err
	}
	return map[string]bool{"added": added}, s.reloadCatalog()
}

func (s *Server) catalogRemove(r *http.Request) (any, error) {
	src, err := library.EditableCatalog(s.lib.Cfg, r.PathValue("name"))
	if err != nil {
		return nil, bad("%v", err)
	}
	removed, err := catalog.RemoveFrom(src.File(), r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return map[string]bool{"removed": removed}, s.reloadCatalog()
}

func (s *Server) catalogUpdate(r *http.Request) (any, error) {
	res := map[string]any{}
	err := library.UpdateCatalogs(s.lib.Cfg, func(name string, n int, err error) {
		if err != nil {
			res[name] = err.Error()
		} else {
			res[name] = n
		}
	})
	if len(res) == 0 && err != nil {
		return nil, err
	}
	return res, s.reloadCatalog()
}

// webKeys are the settings the web UI may read and change. Anything with a
// path or a secret stays CLI-only.
var webKeys = []string{"volume", "scrobble", "local_scrobble"}

func (s *Server) config(r *http.Request) (any, error) {
	out := map[string]any{}
	for _, k := range webKeys {
		out[k], _ = s.lib.Cfg.Get(k)
	}
	var services []string
	for _, svc := range s.lib.ScrobbleServices() {
		services = append(services, svc.Name())
	}
	out["scrobble_services"] = services
	return out, nil
}

func (s *Server) setConfig(r *http.Request) (any, error) {
	var in struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	allowed := false
	for _, k := range webKeys {
		allowed = allowed || k == in.Key
	}
	if !allowed {
		return nil, bad("%q can't be changed from the web", in.Key)
	}
	if err := s.lib.Cfg.Set(in.Key, in.Value); err != nil {
		return nil, bad("%v", err)
	}
	if err := config.Update(func(c *config.Config) error { return c.Set(in.Key, in.Value) }); err != nil {
		return nil, err
	}
	return s.config(r)
}

// ---- art ----------------------------------------------------------------------

// artHosts are the only image hosts /art fetches from, so it can't be used
// as an open proxy.
var artHosts = []string{"ggpht.com", "googleusercontent.com", "ytimg.com"}

func (s *Server) art(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("u")
	u, err := url.Parse(raw)
	ok := err == nil && u.Scheme == "https"
	if ok {
		ok = false
		for _, h := range artHosts {
			ok = ok || u.Hostname() == h || strings.HasSuffix(u.Hostname(), "."+h)
		}
	}
	if !ok {
		http.Error(w, "image host not allowed", http.StatusBadRequest)
		return
	}
	path, err := art.Fetch(raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	http.ServeFile(w, r, path)
}
