package library

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/mpv"
	"github.com/lubabs770/gooseneck/player/internal/ytm"
)

// Radio is an endless mix seeded by an artist. Starting it queues a first
// batch; after that the background daemon (`goose daemon`) tops the queue up
// whenever it runs low, drifting through related artists from the curated
// catalog and never repeating a track within the session. Playing something
// else (a replace) ends it, as does `goose radio stop`.

const (
	radioKey       = "radio:state"
	radioFirst     = 30 // tracks in the first batch
	radioBatch     = 25 // tracks added each top-up
	radioLowWater  = 3  // top up when this many tracks (or fewer) remain
	radioMemory    = 2000
	radioSeedTries = 4
)

// RadioState is the persisted radio session.
type RadioState struct {
	Active    bool            `json:"active"`
	SeedID    string          `json:"seed_id"`
	SeedName  string          `json:"seed_name"`
	StartedAt time.Time       `json:"started_at"`
	Batches   int             `json:"batches"`
	Played    []string        `json:"played"`  // video ids already queued this session
	Pool      []ytm.ArtistRef `json:"pool"`    // candidate seeds (related artists in the catalog)
	Recent    []string        `json:"recent"`  // seeds used lately (avoid repeating them)
	Current   string          `json:"current"` // seed of the latest batch
}

// Radio returns the current radio state (Active=false when off).
func (l *Lib) RadioStatus() RadioState {
	var st RadioState
	l.DB.GetJSON(radioKey, 365*24*time.Hour, &st)
	return st
}

func (l *Lib) saveRadio(st RadioState) { _ = l.DB.PutJSON(radioKey, st) }

// StopRadio ends the radio session (the queued tracks keep playing).
func (l *Lib) StopRadio() {
	st := l.RadioStatus()
	if st.Active {
		st.Active = false
		l.saveRadio(st)
	}
}

// StartRadio replaces the queue with a mix seeded by artist id and turns on
// endless top-ups. Returns the first batch.
func (l *Lib) StartRadio(ctx context.Context, id string, size int) (*ytm.Artist, []ytm.Song, error) {
	if size <= 0 {
		size = radioFirst
	}
	seed, err := l.Artist(ctx, id, false)
	if err != nil {
		return nil, nil, err
	}
	songs, err := l.Radio(ctx, seed, size, nil)
	if err != nil {
		return nil, nil, err
	}
	if err := l.Play(PlayRequest{Songs: songs, Mode: mpv.Replace, Radio: true,
		ArtistID: seed.ID, Artist: seed.Name, Album: "Radio"}); err != nil {
		return nil, nil, err
	}
	st := RadioState{Active: true, SeedID: seed.ID, SeedName: seed.Name, StartedAt: time.Now(),
		Batches: 1, Current: seed.ID, Recent: []string{seed.ID}, Pool: l.catalogRelated(seed)}
	for _, s := range songs {
		st.Played = append(st.Played, s.VideoID)
	}
	l.saveRadio(st)
	l.EnsureDaemon()
	return seed, songs, nil
}

// catalogRelated returns an artist's related artists, preferring catalog ones.
func (l *Lib) catalogRelated(a *ytm.Artist) []ytm.ArtistRef {
	var in, all []ytm.ArtistRef
	for _, r := range a.Related {
		all = append(all, r)
		if _, ok := l.Cat.Get(r.ID); ok {
			in = append(in, r)
		}
	}
	if len(in) >= 3 {
		return in
	}
	return all
}

// nextSeed picks the next batch's seed: a third of the time the original
// artist, otherwise a related artist not used recently.
func (st *RadioState) nextSeed() string {
	if len(st.Pool) == 0 || rand.Intn(3) == 0 {
		return st.SeedID
	}
	recent := map[string]bool{}
	for _, id := range st.Recent {
		recent[id] = true
	}
	var fresh []string
	for _, r := range st.Pool {
		if !recent[r.ID] {
			fresh = append(fresh, r.ID)
		}
	}
	if len(fresh) == 0 {
		return st.Pool[rand.Intn(len(st.Pool))].ID
	}
	return fresh[rand.Intn(len(fresh))]
}

// ExtendRadio appends another batch when the radio is on and the queue is
// running low. It reports how many tracks it added.
func (l *Lib) ExtendRadio(ctx context.Context) (int, error) {
	st := l.RadioStatus()
	if !st.Active {
		return 0, nil
	}
	ps, err := l.Player.Status()
	if err != nil {
		return 0, err
	}
	if !ps.Idle && ps.Count-ps.Index-1 > radioLowWater {
		return 0, nil // plenty left
	}
	played := map[string]bool{}
	for _, id := range st.Played {
		played[id] = true
	}
	var songs []ytm.Song
	var seed *ytm.Artist
	for try := 0; try < radioSeedTries && len(songs) < 5; try++ {
		id := st.nextSeed()
		a, err := l.Artist(ctx, id, false)
		if err != nil {
			continue
		}
		seed = a
		songs, _ = l.Radio(ctx, a, radioBatch, played)
	}
	if len(songs) == 0 || seed == nil {
		return 0, errors.New("radio ran out of new tracks")
	}
	mode := mpv.Append
	if ps.Idle {
		mode = mpv.Replace // queue already finished: start again
	}
	if err := l.Play(PlayRequest{Songs: songs, Mode: mode, Radio: true, NoLog: true}); err != nil {
		return 0, err
	}
	// the user may have stopped the radio while we were fetching
	if cur := l.RadioStatus(); !cur.Active || cur.StartedAt != st.StartedAt {
		return len(songs), nil
	}
	st.Batches++
	st.Current = seed.ID
	st.Recent = append(st.Recent, seed.ID)
	if len(st.Recent) > 5 {
		st.Recent = st.Recent[len(st.Recent)-5:]
	}
	// widen the pool with the new seed's relatives (keeps the drift going)
	known := map[string]bool{st.SeedID: true}
	for _, r := range st.Pool {
		known[r.ID] = true
	}
	for _, r := range l.catalogRelated(seed) {
		if !known[r.ID] && len(st.Pool) < 60 {
			st.Pool = append(st.Pool, r)
			known[r.ID] = true
		}
	}
	for _, s := range songs {
		st.Played = append(st.Played, s.VideoID)
	}
	if len(st.Played) > radioMemory {
		st.Played = st.Played[len(st.Played)-radioMemory:]
	}
	l.saveRadio(st)
	return len(songs), nil
}

// Radio builds a mix seeded by an artist: some of their own songs plus the
// top songs of related artists, shuffled so the same artist rarely plays twice
// in a row. Related artists that are in the curated catalog are preferred,
// which keeps the mix on-genre. Songs in exclude are skipped.
func (l *Lib) Radio(ctx context.Context, seed *ytm.Artist, size int, exclude map[string]bool) ([]ytm.Song, error) {
	if size <= 0 {
		size = 50
	}
	own, err := l.ArtistTracks(ctx, seed)
	if err != nil && len(seed.TopSongs) == 0 {
		return nil, err
	}
	own = append(append([]ytm.Song{}, seed.TopSongs...), shuffled(own)...)

	related := l.catalogRelated(seed)
	if len(related) > 10 {
		related = related[:10]
	}
	pages := make([]*ytm.Artist, len(related))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, r := range related {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pages[i], _ = l.Artist(ctx, id, false)
		}(i, r.ID)
	}
	wg.Wait()

	keep := func(in []ytm.Song) []ytm.Song {
		var out []ytm.Song
		for _, s := range in {
			if !exclude[s.VideoID] {
				out = append(out, s)
			}
		}
		return out
	}
	pool := [][]ytm.Song{keep(dedupe(own))} // one bucket per artist, own songs first
	for _, p := range pages {
		if p != nil && len(p.TopSongs) > 0 {
			pool = append(pool, keep(shuffled(p.TopSongs)))
		}
	}
	return mix(pool, size), nil
}

// mix draws songs round-robin from shuffled buckets, giving the seed artist
// about a third of the slots, and never repeats a video.
func mix(pool [][]ytm.Song, size int) []ytm.Song {
	seen := map[string]bool{}
	var out []ytm.Song
	take := func(b int) bool {
		for len(pool[b]) > 0 {
			s := pool[b][0]
			pool[b] = pool[b][1:]
			if !seen[s.VideoID] {
				seen[s.VideoID] = true
				out = append(out, s)
				return true
			}
		}
		return false
	}
	others := make([]int, 0, len(pool))
	for i := 1; i < len(pool); i++ {
		others = append(others, i)
	}
	rand.Shuffle(len(others), func(i, j int) { others[i], others[j] = others[j], others[i] })
	for len(out) < size {
		progress := false
		if take(0) { // seed artist
			progress = true
		}
		for k := 0; k < 2 && len(others) > 0; k++ { // then two related artists
			b := others[0]
			others = append(others[1:], b)
			if take(b) {
				progress = true
			}
		}
		if !progress {
			break
		}
	}
	if len(out) > size {
		out = out[:size]
	}
	return out
}

func dedupe(in []ytm.Song) []ytm.Song {
	seen := map[string]bool{}
	var out []ytm.Song
	for _, s := range in {
		if !seen[s.VideoID] {
			seen[s.VideoID] = true
			out = append(out, s)
		}
	}
	return out
}
