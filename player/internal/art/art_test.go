package art

import (
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSquareURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://yt3.googleusercontent.com/abc=w2880-h1200-p": "https://yt3.googleusercontent.com/abc=s320-c",
		"https://i.ggpht.com/xyz=s88-c-k":                     "https://i.ggpht.com/xyz=s320-c",
		"https://example.com/pic.jpg=w100":                    "https://example.com/pic.jpg=w100",
		"https://yt3.googleusercontent.com/abc":               "https://yt3.googleusercontent.com/abc",
	} {
		if got := SquareURL(in, 320); got != want {
			t.Errorf("SquareURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadAndWarm(t *testing.T) {
	t.Setenv("GOOSENECK_HOME", t.TempDir())
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		img := image.NewRGBA(image.Rect(0, 0, 4, 4))
		img.Set(1, 1, color.RGBA{255, 0, 0, 255})
		png.Encode(w, img)
	}))
	defer srv.Close()

	fetched, failed := Warm([]string{srv.URL + "/a", srv.URL + "/b", srv.URL + "/a", srv.URL + "/missing", ""}, nil)
	if fetched != 2 || failed != 1 {
		t.Fatalf("Warm = %d fetched, %d failed", fetched, failed)
	}
	if !Cached(srv.URL + "/a") {
		t.Fatal("not cached after warm")
	}
	before := hits
	img, err := Load(srv.URL + "/a")
	if err != nil || img.Bounds().Dx() != 4 || hits != before {
		t.Fatalf("Load from cache: %v, hits %d -> %d", err, before, hits)
	}
}
