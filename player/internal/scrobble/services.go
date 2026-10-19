package scrobble

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var client = &http.Client{Timeout: 20 * time.Second}

// ---- ListenBrainz ---------------------------------------------------------------

// ListenBrainz submits to listenbrainz.org with a user token
// (https://listenbrainz.org/settings/).
type ListenBrainz struct {
	Token    string
	Endpoint string // default https://api.listenbrainz.org
}

func (lb *ListenBrainz) Name() string { return "listenbrainz" }

func (lb *ListenBrainz) base() string {
	if lb.Endpoint != "" {
		return strings.TrimSuffix(lb.Endpoint, "/")
	}
	return "https://api.listenbrainz.org"
}

func lbPayload(l Listen, withTime bool) map[string]any {
	info := map[string]any{
		"submission_client":         "gooseneck",
		"media_player":              "mpv",
		"music_service":             "music.youtube.com",
		"origin_url":                "https://music.youtube.com/watch?v=" + l.VideoID,
		"youtube_id":                l.VideoID,
		"submission_client_version": "goose",
	}
	if l.Duration > 0 {
		info["duration_ms"] = l.Duration * 1000
	}
	meta := map[string]any{"artist_name": l.Artist, "track_name": l.Track, "additional_info": info}
	if l.Album != "" {
		meta["release_name"] = l.Album
	}
	p := map[string]any{"track_metadata": meta}
	if withTime {
		p["listened_at"] = l.StartedAt.Unix()
	}
	return p
}

func (lb *ListenBrainz) submit(ctx context.Context, kind string, payload []map[string]any) error {
	body, _ := json.Marshal(map[string]any{"listen_type": kind, "payload": payload})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, lb.base()+"/1/submit-listens", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+lb.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("listenbrainz: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return &HTTPError{Service: "listenbrainz", Status: resp.StatusCode, Body: strings.TrimSpace(string(msg))}
	}
	return nil
}

func (lb *ListenBrainz) NowPlaying(ctx context.Context, l Listen) error {
	return lb.submit(ctx, "playing_now", []map[string]any{lbPayload(l, false)})
}

func (lb *ListenBrainz) Scrobble(ctx context.Context, ls []Listen) error {
	if len(ls) == 0 {
		return nil
	}
	kind := "import"
	if len(ls) == 1 {
		kind = "single"
	}
	var p []map[string]any
	for _, l := range ls {
		p = append(p, lbPayload(l, true))
	}
	return lb.submit(ctx, kind, p)
}

// Validate checks the token and returns the ListenBrainz user name.
func (lb *ListenBrainz) Validate(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lb.base()+"/1/validate-token", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Token "+lb.Token)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("listenbrainz: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Valid    bool   `json:"valid"`
		UserName string `json:"user_name"`
		Message  string `json:"message"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if !out.Valid {
		return "", fmt.Errorf("listenbrainz: invalid token (%s)", strings.TrimSpace(out.Message))
	}
	return out.UserName, nil
}

// ---- Last.fm --------------------------------------------------------------------

// LastFM submits to last.fm. It needs an API key + secret (create one at
// https://www.last.fm/api/account/create) and a session key from the
// browser login flow (GetToken -> AuthURL -> GetSession).
type LastFM struct {
	Key, Secret, Session string
	Endpoint             string // default https://ws.audioscrobbler.com/2.0/
}

func (lf *LastFM) Name() string { return "lastfm" }

func (lf *LastFM) base() string {
	if lf.Endpoint != "" {
		return lf.Endpoint
	}
	return "https://ws.audioscrobbler.com/2.0/"
}

// sign adds api_key and api_sig (md5 of the sorted params + secret).
func (lf *LastFM) sign(p url.Values) url.Values {
	p.Set("api_key", lf.Key)
	keys := make([]string, 0, len(p))
	for k := range p {
		if k != "format" && k != "callback" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(p.Get(k))
	}
	b.WriteString(lf.Secret)
	sum := md5.Sum([]byte(b.String()))
	p.Set("api_sig", hex.EncodeToString(sum[:]))
	p.Set("format", "json")
	return p
}

func (lf *LastFM) call(ctx context.Context, post bool, p url.Values, out any) error {
	p = lf.sign(p)
	var req *http.Request
	var err error
	if post {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, lf.base(), strings.NewReader(p.Encode()))
		if req != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, lf.base()+"?"+p.Encode(), nil)
	}
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "gooseneck")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("last.fm: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var e struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error != 0 {
		return &HTTPError{Service: "last.fm", Status: resp.StatusCode, Code: e.Error, Body: e.Message}
	}
	if resp.StatusCode != http.StatusOK {
		return &HTTPError{Service: "last.fm", Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func lfTrack(p url.Values, suffix string, l Listen) {
	p.Set("artist"+suffix, l.Artist)
	p.Set("track"+suffix, l.Track)
	if l.Album != "" {
		p.Set("album"+suffix, l.Album)
	}
	if l.Duration > 0 {
		p.Set("duration"+suffix, strconv.Itoa(l.Duration))
	}
}

func (lf *LastFM) NowPlaying(ctx context.Context, l Listen) error {
	p := url.Values{"method": {"track.updateNowPlaying"}, "sk": {lf.Session}}
	lfTrack(p, "", l)
	return lf.call(ctx, true, p, nil)
}

func (lf *LastFM) Scrobble(ctx context.Context, ls []Listen) error {
	for start := 0; start < len(ls); start += 50 { // API limit per request
		batch := ls[start:min(start+50, len(ls))]
		p := url.Values{"method": {"track.scrobble"}, "sk": {lf.Session}}
		for i, l := range batch {
			s := fmt.Sprintf("[%d]", i)
			lfTrack(p, s, l)
			p.Set("timestamp"+s, strconv.FormatInt(l.StartedAt.Unix(), 10))
		}
		if err := lf.call(ctx, true, p, nil); err != nil {
			return err
		}
	}
	return nil
}

// GetToken starts the browser login.
func (lf *LastFM) GetToken(ctx context.Context) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	if err := lf.call(ctx, false, url.Values{"method": {"auth.getToken"}}, &out); err != nil {
		return "", err
	}
	return out.Token, nil
}

// AuthURL is where the user approves gooseneck.
func (lf *LastFM) AuthURL(token string) string {
	return "https://www.last.fm/api/auth/?api_key=" + url.QueryEscape(lf.Key) + "&token=" + url.QueryEscape(token)
}

// GetSession exchanges an approved token for a session key and user name.
func (lf *LastFM) GetSession(ctx context.Context, token string) (key, user string, err error) {
	var out struct {
		Session struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"session"`
	}
	if err := lf.call(ctx, false, url.Values{"method": {"auth.getSession"}, "token": {token}}, &out); err != nil {
		return "", "", err
	}
	return out.Session.Key, out.Session.Name, nil
}

// ---- errors ---------------------------------------------------------------------

// HTTPError is a rejected request.
type HTTPError struct {
	Service string
	Status  int
	Code    int // Last.fm error code
	Body    string
}

func (e *HTTPError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("%s: error %d: %s", e.Service, e.Code, e.Body)
	}
	return fmt.Sprintf("%s: HTTP %d: %s", e.Service, e.Status, e.Body)
}

// Permanent reports whether retrying can never help because the listen itself
// was rejected (malformed data). Auth problems are NOT permanent: queued
// listens wait until the user logs in again.
func Permanent(err error) bool {
	var he *HTTPError
	if !errors.As(err, &he) {
		return false
	}
	if he.Service == "last.fm" {
		return he.Code == 6 // invalid parameters
	}
	return he.Status == http.StatusBadRequest
}
