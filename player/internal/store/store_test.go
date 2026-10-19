package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "sub", "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestKV(t *testing.T) {
	s := open(t)
	var v map[string]int
	if found, _ := s.GetJSON("k", time.Hour, &v); found {
		t.Fatal("empty store should miss")
	}
	if err := s.PutJSON("k", map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	found, stale := s.GetJSON("k", time.Hour, &v)
	if !found || stale || v["a"] != 1 {
		t.Fatalf("found=%v stale=%v v=%v", found, stale, v)
	}
	if _, stale := s.GetJSON("k", -time.Second, &v); !stale {
		t.Fatal("negative maxAge should report stale")
	}
	if n, _ := s.ClearCache(); n != 1 || s.CacheCount() != 0 {
		t.Fatal("ClearCache")
	}
}

func TestTrackMeta(t *testing.T) {
	s := open(t)
	if err := s.PutTracks([]Track{{VideoID: "v1", Title: "Song", Artist: "A", Duration: 100}, {Title: "no id"}}); err != nil {
		t.Fatal(err)
	}
	tr, ok := s.Track("v1")
	if !ok || tr.Title != "Song" || tr.Duration != 100 {
		t.Fatalf("Track = %+v %v", tr, ok)
	}
	if _, ok := s.Track("missing"); ok {
		t.Fatal("missing track found")
	}
}

func TestHistoryAndTop(t *testing.T) {
	s := open(t)
	now := time.Now()
	s.LogPlay(Play{At: now.Add(-3 * time.Minute), ArtistID: "A", Artist: "Avi", Album: "One", Count: 3})
	s.LogPlay(Play{At: now.Add(-2 * time.Minute), ArtistID: "A", Artist: "Avi", Track: "song", VideoID: "v1"})
	s.LogPlay(Play{At: now.Add(-1 * time.Minute), ArtistID: "B", Artist: "Ben", Count: 5})

	h, err := s.History(10)
	if err != nil || len(h) != 3 || h[0].Artist != "Ben" {
		t.Fatalf("History = %+v, %v", h, err)
	}
	top, err := s.TopArtists(10)
	if err != nil || len(top) != 2 {
		t.Fatalf("TopArtists = %+v, %v", top, err)
	}
	if top[0].ArtistID != "A" || top[0].Plays != 2 || top[0].Tracks != 4 {
		t.Fatalf("top[0] = %+v", top[0])
	}
	rec, _ := s.RecentArtists(5)
	if len(rec) != 2 || rec[0].ArtistID != "B" {
		t.Fatalf("RecentArtists = %+v", rec)
	}
}

func TestImportLegacyLog(t *testing.T) {
	s := open(t)
	p := filepath.Join(t.TempDir(), "logs.json")
	os.WriteFile(p, []byte(`[{"time":"2026-07-24T12:00:00Z","artist_id":"A","artist":"Avi","count":3},{"time":"bad"}]`), 0o644)
	n, err := s.ImportLegacyLog(p)
	if err != nil || n != 1 {
		t.Fatalf("import = %d, %v", n, err)
	}
	// Second run is a no-op because plays is no longer empty.
	if n, _ := s.ImportLegacyLog(p); n != 0 {
		t.Fatalf("re-import = %d", n)
	}
}
