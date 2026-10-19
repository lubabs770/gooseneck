package library

import (
	"context"
	"math/rand"
	"sync"

	"github.com/lubabs770/gooseneck/player/internal/ytm"
)

// Radio builds a mix seeded by an artist: some of their own songs plus the
// top songs of related artists, shuffled so the same artist rarely plays twice
// in a row. Related artists that are in the curated catalog are preferred, which
// keeps the mix on-genre. The whole queue is built up front, so no background
// process is needed to keep the music going.
func (l *Lib) Radio(ctx context.Context, seed *ytm.Artist, size int) ([]ytm.Song, error) {
	if size <= 0 {
		size = 50
	}
	own, err := l.ArtistTracks(ctx, seed)
	if err != nil && len(seed.TopSongs) == 0 {
		return nil, err
	}
	own = append(append([]ytm.Song{}, seed.TopSongs...), shuffled(own)...)

	related := seed.Related
	var inCat []ytm.ArtistRef
	for _, r := range related {
		if _, ok := l.Cat.Get(r.ID); ok {
			inCat = append(inCat, r)
		}
	}
	if len(inCat) >= 3 {
		related = inCat
	}
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

	var pool [][]ytm.Song // one bucket per artist, own songs first
	pool = append(pool, dedupe(own))
	for _, p := range pages {
		if p != nil && len(p.TopSongs) > 0 {
			pool = append(pool, shuffled(p.TopSongs))
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
