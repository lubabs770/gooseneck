package ytm

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Fixtures are real responses captured from music.youtube.com (gzipped).
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	f, err := os.Open("testdata/" + name + ".json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fakeServer(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			BrowseID string `json:"browseId"`
			Params   string `json:"params"`
			Context  struct {
				Client struct {
					ClientName string `json:"clientName"`
				} `json:"client"`
			} `json:"context"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Context.Client.ClientName != "WEB_REMIX" {
			http.Error(w, "bad client", 400)
			return
		}
		switch {
		case strings.HasPrefix(body.BrowseID, "MPAD"):
			w.Write(fixture(t, "discography"))
		case strings.HasPrefix(body.BrowseID, "MPRE"):
			w.Write(fixture(t, "album"))
		case strings.HasPrefix(body.BrowseID, "UC"):
			w.Write(fixture(t, "artist"))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{Endpoint: srv.URL + "/browse?prettyPrint=false"}
}

func TestArtist(t *testing.T) {
	c := fakeServer(t)
	a, err := c.Artist(context.Background(), "UCw1oCoEMF_o65tpDHItwwUQ")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "8th Day" || a.Subscribers == "" || a.Thumb == "" || a.Description == "" {
		t.Fatalf("header: %+v", a)
	}
	if len(a.Albums) != 10 {
		t.Fatalf("albums = %d, want 10", len(a.Albums))
	}
	lucky := a.Albums[0]
	if lucky.Title != "Lucky" || lucky.Year != "2021" || lucky.Kind != "Album" ||
		!strings.HasPrefix(lucky.BrowseID, "MPREb_") || !strings.HasPrefix(lucky.PlaylistID, "OLAK") {
		t.Fatalf("first album: %+v", lucky)
	}
	// Singles shelf had a "more" link -> full discography (28 vs 10 in the carousel).
	if len(a.Singles) < 20 {
		t.Fatalf("singles = %d, want the full discography", len(a.Singles))
	}
	if len(a.TopSongs) == 0 || a.TopSongs[0].VideoID == "" || a.TopSongs[0].Plays == "" || a.TopSongs[0].Album == "" {
		t.Fatalf("top songs: %+v", a.TopSongs)
	}
	if len(a.Related) == 0 || !strings.HasPrefix(a.Related[0].ID, "UC") {
		t.Fatalf("related: %+v", a.Related)
	}
	if got := len(a.Releases()); got != len(a.Albums)+len(a.Singles) {
		t.Fatalf("Releases len = %d", got)
	}
}

func TestAlbum(t *testing.T) {
	c := fakeServer(t)
	al, err := c.Album(context.Background(), "MPREb_7DawMPNOn2Y")
	if err != nil {
		t.Fatal(err)
	}
	if al.Title != "Lucky" || al.Year != "2021" || al.Artist != "8th Day" || al.Length == "" {
		t.Fatalf("header: %+v", al)
	}
	if len(al.Tracks) != 11 {
		t.Fatalf("tracks = %d, want 11", len(al.Tracks))
	}
	tr := al.Tracks[0]
	if tr.VideoID == "" || tr.Duration != 195 || tr.Index != 1 || tr.Album != "Lucky" || tr.Artist == "" {
		t.Fatalf("track 1: %+v", tr)
	}
	if al.Duration() < 30*60 {
		t.Fatalf("album duration %d too short", al.Duration())
	}
}

func TestNotArtist(t *testing.T) {
	c := fakeServer(t)
	if _, err := c.Artist(context.Background(), "nope"); err != ErrNotArtist {
		t.Fatalf("want ErrNotArtist, got %v", err)
	}
}

func TestDurations(t *testing.T) {
	for in, want := range map[string]int{"3:15": 195, "1:02:03": 3723, "": 0, "x:1": 0, "45": 45} {
		if got := ParseDuration(in); got != want {
			t.Errorf("ParseDuration(%q) = %d, want %d", in, got, want)
		}
	}
	for in, want := range map[int]string{195: "3:15", 3723: "1:02:03", 0: "0:00", -5: "0:00"} {
		if got := FormatDuration(in); got != want {
			t.Errorf("FormatDuration(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestHelpersTolerateGarbage(t *testing.T) {
	var v any
	_ = json.Unmarshal([]byte(`{"a":[{"b":"x"}],"n":3}`), &v)
	if str(v, "a", 0, "b") != "x" || str(v, "a", 5, "b") != "" || str(v, "n", "q") != "" || get(nil, "a") != nil {
		t.Fatal("path helpers misbehave")
	}
	a, _ := parseArtist(v)
	if a.Name != "" {
		t.Fatal("garbage should parse to empty")
	}
}
