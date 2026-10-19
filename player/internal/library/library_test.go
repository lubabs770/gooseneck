package library

import (
	"testing"

	"github.com/lubabs770/gooseneck/player/internal/ytm"
)

func page() *ytm.Artist {
	return &ytm.Artist{Name: "8th Day",
		Albums:  []ytm.Release{{BrowseID: "A1", Title: "Lucky"}, {BrowseID: "A2", Title: "Stronger Closer"}, {BrowseID: "A3", Title: "Slow Down, Vol. 7"}},
		Singles: []ytm.Release{{BrowseID: "S1", Title: "Lucky Abba Mashup"}, {BrowseID: "S2", Title: "Tut Altz"}},
	}
}

func TestResolveRelease(t *testing.T) {
	for q, want := range map[string]string{
		"lucky": "A1", // exact beats the longer "Lucky Abba Mashup"
		"2":     "A2", // by number, albums then singles
		"5":     "S2",
		"tut":   "S2",
		"S1":    "S1", // by browse id
		"slow":  "A3",
	} {
		r, err := ResolveRelease(page(), q)
		if err != nil || r.BrowseID != want {
			t.Errorf("ResolveRelease(%q) = %v, %v; want %s", q, r.BrowseID, err, want)
		}
	}
	if _, err := ResolveRelease(page(), "zzqq"); err == nil {
		t.Error("nonsense should not resolve")
	}
}

func TestResolveTrack(t *testing.T) {
	tracks := []ytm.Song{{VideoID: "a", Title: "Lucky"}, {VideoID: "b", Title: "Rollin"}, {VideoID: "c", Title: "Elika D'meir"}}
	for q, want := range map[string]int{"2": 1, "rollin": 1, "elika": 2, "c": 2} {
		i, err := ResolveTrack(tracks, q)
		if err != nil || i != want {
			t.Errorf("ResolveTrack(%q) = %d, %v; want %d", q, i, err, want)
		}
	}
}

func TestShuffledKeepsSongs(t *testing.T) {
	in := []ytm.Song{{VideoID: "a"}, {VideoID: "b"}, {VideoID: "c"}, {VideoID: "d"}}
	out := shuffled(in)
	if len(out) != len(in) || in[0].VideoID != "a" {
		t.Fatal("shuffled must copy, not mutate")
	}
	seen := map[string]bool{}
	for _, s := range out {
		seen[s.VideoID] = true
	}
	if len(seen) != 4 {
		t.Fatal("shuffled lost songs")
	}
}

func TestMix(t *testing.T) {
	song := func(artist, id string) ytm.Song { return ytm.Song{VideoID: id, Artist: artist} }
	pool := [][]ytm.Song{
		{song("seed", "s1"), song("seed", "s2"), song("seed", "s3"), song("seed", "s1")},
		{song("a", "a1"), song("a", "a2")},
		{song("b", "b1"), song("b", "b2")},
	}
	out := mix(pool, 100)
	if len(out) != 7 {
		t.Fatalf("mix len = %d, want 7 unique songs", len(out))
	}
	if out[0].Artist != "seed" {
		t.Fatal("mix should open with the seed artist")
	}
	for i := 1; i < len(out); i++ {
		if out[i].VideoID == out[i-1].VideoID {
			t.Fatal("repeated video")
		}
	}
	if got := mix([][]ytm.Song{{song("x", "1"), song("x", "2"), song("x", "3")}}, 2); len(got) != 2 {
		t.Fatalf("size cap: %d", len(got))
	}
}
