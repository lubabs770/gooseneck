package lyrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLinesAndCurrent(t *testing.T) {
	l := &Lyrics{Synced: "[ar:Someone]\n[00:05.50]one\n[00:10.00][01:00.00]chorus\n[00:20.25] two\r\n"}
	lines := l.Lines()
	want := []Line{{5.5, "one"}, {10, "chorus"}, {20.25, "two"}, {60, "chorus"}}
	if len(lines) != len(want) {
		t.Fatalf("lines = %+v", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %+v, want %+v", i, lines[i], want[i])
		}
	}
	for pos, idx := range map[float64]int{0: -1, 5.5: 0, 12: 1, 30: 2, 300: 3} {
		if got := Current(lines, pos); got != idx {
			t.Errorf("Current(%v) = %d, want %d", pos, got, idx)
		}
	}
	plain := (&Lyrics{Plain: "a\nb"}).Lines()
	if len(plain) != 2 || plain[0].At != -1 || Current(plain, 50) != -1 {
		t.Fatalf("plain = %+v", plain)
	}
}

func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"Yerushalayim (Official Video)": "Yerushalayim",
		"Song [Lyrics] (feat. X)":       "Song",
		"(Intro)":                       "(Intro)",
	} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearch(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(503) // retried
			return
		}
		if r.URL.Query().Get("track_name") != "Song" {
			w.Write([]byte(`[]`)) // first title has the "(Official Video)" suffix
			return
		}
		w.Write([]byte(`[{"id":1,"duration":300,"syncedLyrics":"[00:01.00]far"},
			{"id":2,"duration":181,"plainLyrics":"plain only"},
			{"id":3,"duration":178,"syncedLyrics":"[00:01.00]close"},
			{"id":4,"duration":180}]`))
	}))
	defer srv.Close()
	t.Setenv("GOOSENECK_LRCLIB_URL", srv.URL)
	l, err := Search(context.Background(), "Song (Official Video)", "Artist", 180)
	if err != nil {
		t.Fatal(err)
	}
	if l == nil || l.ID != 3 {
		t.Fatalf("best = %+v, want id 3", l)
	}
}
