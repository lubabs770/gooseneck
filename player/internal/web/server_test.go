package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lubabs770/gooseneck/player/internal/catalog"
	"github.com/lubabs770/gooseneck/player/internal/config"
	"github.com/lubabs770/gooseneck/player/internal/library"
	"github.com/lubabs770/gooseneck/player/internal/mpv"
	"github.com/lubabs770/gooseneck/player/internal/store"
	"github.com/lubabs770/gooseneck/player/internal/ytm"
)

const host = "127.0.0.1:7777"

func testServer(t *testing.T) http.Handler {
	t.Helper()
	t.Setenv("GOOSENECK_HOME", t.TempDir())
	cfg := config.Default()
	db, err := store.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cat := catalog.New([]catalog.Artist{
		{ID: "UC1", Name: "Avraham Fried", IsChasid: true, IsFamous: true},
		{ID: "UC2", Name: "8th Day"},
	})
	l := &library.Lib{Cfg: cfg, Cat: cat, DB: db, YTM: &ytm.Client{}, Player: mpv.New(cfg)}
	return New(l, Options{Addrs: []string{host}, Hosts: []string{"mac.tail1234.ts.net."}}).Handler()
}

func do(h http.Handler, method, target, hostHdr, origin, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Host = hostHdr
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestGuard(t *testing.T) {
	h := testServer(t)
	cases := []struct {
		name, method, host, origin string
		want                       int
	}{
		{"loopback host", "GET", host, "", 200},
		{"localhost alias", "GET", "localhost:7777", "", 200},
		{"magicdns name", "GET", "mac.tail1234.ts.net:7777", "", 200},
		{"rebinding host", "GET", "evil.example:7777", "", 403},
		{"cross-origin write", "POST", host, "https://evil.example", 403},
		{"same-origin write reaches handler", "POST", host, "http://" + host, 409}, // player not running
		{"no-origin write (curl)", "POST", host, "", 409},
	}
	for _, c := range cases {
		target := "/api/tags"
		if c.method == "POST" {
			target = "/api/control/pause"
		}
		if got := do(h, c.method, target, c.host, c.origin, "").Code; got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestAPI(t *testing.T) {
	h := testServer(t)

	w := do(h, "GET", "/api/search?q=fried", host, "", "")
	var found []artistView
	if err := json.Unmarshal(w.Body.Bytes(), &found); err != nil || len(found) == 0 || found[0].ID != "UC1" {
		t.Fatalf("search: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(strings.Join(found[0].Tags, ","), "chasidish") {
		t.Errorf("search tags: %v", found[0].Tags)
	}

	w = do(h, "GET", "/api/search?tag=nope", host, "", "")
	if w.Code != 400 {
		t.Errorf("unknown tag: got %d", w.Code)
	}

	w = do(h, "GET", "/api/now", host, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"running":false`) {
		t.Errorf("now while stopped: %d %s", w.Code, w.Body)
	}

	w = do(h, "GET", "/api/queue", host, "", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("queue while stopped: %d %s", w.Code, w.Body)
	}

	w = do(h, "POST", "/api/play", host, "", `{"mode":"sideways","songs":[{"video_id":"x"}]}`)
	if w.Code != 400 {
		t.Errorf("bad play mode: got %d", w.Code)
	}

	w = do(h, "POST", "/api/config", host, "", `{"key":"lastfm_api_secret","value":"x"}`)
	if w.Code != 400 {
		t.Errorf("secret config key: got %d", w.Code)
	}

	w = do(h, "GET", "/art?u=https://evil.example/x.png", host, "", "")
	if w.Code != 400 {
		t.Errorf("art open proxy: got %d", w.Code)
	}

	w = do(h, "GET", "/api/scrobbles/top/genre", host, "", "")
	if w.Code != 400 {
		t.Errorf("top kind: got %d", w.Code)
	}
}
