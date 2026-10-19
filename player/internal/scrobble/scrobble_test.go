package scrobble

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func meta(id string) (Listen, bool) {
	switch id {
	case "long":
		return Listen{Artist: "8th Day", Track: "Lucky", Album: "Lucky", Duration: 200}, true
	case "short":
		return Listen{Artist: "X", Track: "Intro", Duration: 20}, true
	case "epic":
		return Listen{Artist: "Y", Track: "Medley", Duration: 1200}, true
	}
	return Listen{}, false
}

// play feeds n polls, step seconds apart.
func play(t *Tracker, start time.Time, id string, paused bool, n int, step time.Duration) (Events, time.Time, int, int) {
	now := start
	nows, scrobbles := 0, 0
	var last Events
	for i := 0; i < n; i++ {
		ev := t.Observe(now, Snapshot{Running: true, VideoID: id, Paused: paused, Pos: float64(i) * step.Seconds()}, meta)
		if ev.NowPlaying != nil {
			nows++
		}
		if ev.Scrobble != nil {
			scrobbles++
			last = ev
		}
		now = now.Add(step)
	}
	return last, now, nows, scrobbles
}

func TestTrackerHalfway(t *testing.T) {
	var tr Tracker
	start := time.Unix(1_700_000_000, 0)
	// 90s of a 200s track: not yet (needs 100s)
	_, now, nows, sc := play(&tr, start, "long", false, 46, 2*time.Second)
	if nows != 1 || sc != 0 {
		t.Fatalf("after 90s: nowplaying=%d scrobbles=%d", nows, sc)
	}
	ev := tr.Observe(now.Add(12*time.Second), Snapshot{Running: true, VideoID: "long", Pos: 104}, meta)
	if ev.Scrobble == nil || ev.Scrobble.Track != "Lucky" || !ev.Scrobble.StartedAt.Equal(start) {
		t.Fatalf("expected scrobble at 102s, got %+v", ev.Scrobble)
	}
	// never twice for the same play (position keeps advancing)
	for i := 0; i < 40; i++ {
		ev := tr.Observe(now.Add(time.Duration(14+2*i)*time.Second), Snapshot{Running: true, VideoID: "long", Pos: float64(106 + 2*i)}, meta)
		if ev.Scrobble != nil || ev.NowPlaying != nil {
			t.Fatal("same play reported twice")
		}
	}
}

func TestTrackerPausesAndRules(t *testing.T) {
	var tr Tracker
	start := time.Unix(1_700_000_000, 0)
	if _, _, _, sc := play(&tr, start, "long", true, 200, 2*time.Second); sc != 0 {
		t.Fatal("paused time must not count")
	}
	var tr2 Tracker
	if _, _, _, sc := play(&tr2, start, "short", false, 40, time.Second); sc != 0 {
		t.Fatal("tracks under 30s never scrobble")
	}
	var tr3 Tracker // 4-minute cap for long tracks
	if _, _, _, sc := play(&tr3, start, "epic", false, 125, 2*time.Second); sc != 1 {
		t.Fatalf("20-minute track should scrobble after 4 minutes, got %d", sc)
	}
	var tr4 Tracker // unknown tracks are skipped
	if ev := tr4.Observe(start, Snapshot{Running: true, VideoID: "nope"}, meta); ev.NowPlaying != nil {
		t.Fatal("unknown track reported")
	}
	if Needed(0) != 240 || Needed(100) != 50 {
		t.Fatal("Needed")
	}
}

func TestTrackerGapsAndRepeat(t *testing.T) {
	var tr Tracker
	start := time.Unix(1_700_000_000, 0)
	tr.Observe(start, Snapshot{Running: true, VideoID: "long", Pos: 0}, meta)
	// laptop slept for an hour: the gap must not count as listening
	if ev := tr.Observe(start.Add(time.Hour), Snapshot{Running: true, VideoID: "long", Pos: 3}, meta); ev.Scrobble != nil {
		t.Fatal("suspend gap counted")
	}
	// full listen, then repeat-one restarts the same video: a second scrobble
	var r Tracker
	_, now, _, sc := play(&r, start, "long", false, 110, 2*time.Second)
	if sc != 1 {
		t.Fatalf("first play: %d", sc)
	}
	r.Observe(now, Snapshot{Running: true, VideoID: "long", Pos: 199}, meta)
	_, _, nows, sc := play(&r, now.Add(2*time.Second), "long", false, 110, 2*time.Second)
	if sc != 1 || nows != 1 {
		t.Fatalf("repeat: nowplaying=%d scrobbles=%d", nows, sc)
	}
}

func TestListenBrainz(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token tok" {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/1/validate-token" {
			w.Write([]byte(`{"valid":true,"user_name":"goose"}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	lb := &ListenBrainz{Token: "tok", Endpoint: srv.URL}
	if u, err := lb.Validate(context.Background()); err != nil || u != "goose" {
		t.Fatalf("Validate = %q, %v", u, err)
	}
	l := Listen{VideoID: "v", Artist: "8th Day", Track: "Lucky", Album: "Lucky", Duration: 195, StartedAt: time.Unix(100, 0)}
	if err := lb.Scrobble(context.Background(), []Listen{l}); err != nil {
		t.Fatal(err)
	}
	p := got["payload"].([]any)[0].(map[string]any)
	if got["listen_type"] != "single" || p["listened_at"].(float64) != 100 ||
		p["track_metadata"].(map[string]any)["track_name"] != "Lucky" {
		t.Fatalf("payload = %v", got)
	}
	bad := &ListenBrainz{Token: "nope", Endpoint: srv.URL}
	err := bad.NowPlaying(context.Background(), l)
	if err == nil || Permanent(err) {
		t.Fatalf("401 should be a retryable error, got %v", err)
	}
}

func TestLastFM(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(body))
		if r.Method == http.MethodGet {
			form = r.URL.Query()
		}
		switch form.Get("method") {
		case "auth.getToken":
			w.Write([]byte(`{"token":"T"}`))
		case "auth.getSession":
			w.Write([]byte(`{"session":{"name":"goose","key":"SK"}}`))
		case "track.scrobble":
			w.Write([]byte(`{"scrobbles":{}}`))
		default:
			w.Write([]byte(`{"error":6,"message":"Invalid parameters"}`))
		}
	}))
	defer srv.Close()
	lf := &LastFM{Key: "K", Secret: "S", Endpoint: srv.URL}
	tok, err := lf.GetToken(context.Background())
	if err != nil || tok != "T" {
		t.Fatalf("GetToken = %q %v", tok, err)
	}
	sk, user, err := lf.GetSession(context.Background(), tok)
	if err != nil || sk != "SK" || user != "goose" {
		t.Fatalf("GetSession = %q %q %v", sk, user, err)
	}
	lf.Session = sk
	l := Listen{Artist: "8th Day", Track: "Lucky", StartedAt: time.Unix(100, 0)}
	if err := lf.Scrobble(context.Background(), []Listen{l, l}); err != nil {
		t.Fatal(err)
	}
	if form.Get("artist[1]") != "8th Day" || form.Get("timestamp[0]") != "100" || form.Get("sk") != "SK" || form.Get("api_sig") == "" {
		t.Fatalf("form = %v", form)
	}
	// signature: md5 of sorted k+v pairs + secret, excluding format
	// md5("api_keyKmethodauth.getTokenS")
	sig := (&LastFM{Key: "K", Secret: "S"}).sign(url.Values{"method": {"auth.getToken"}}).Get("api_sig")
	if want := md5hex("api_keyKmethodauth.getTokenS"); sig != want {
		t.Fatalf("sig %q, want %q", sig, want)
	}
	err = lf.NowPlaying(context.Background(), l) // fake server answers error 6
	if err == nil || !Permanent(err) {
		t.Fatalf("error 6 should be permanent, got %v", err)
	}
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
