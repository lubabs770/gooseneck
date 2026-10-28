// Package lyrics fetches song lyrics from LRCLIB (https://lrclib.net), a free,
// open lyrics database with time-synced LRC lyrics, and parses them.
package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var client = &http.Client{Timeout: 15 * time.Second}

// Lyrics is one LRCLIB record. Synced is LRC text ("[mm:ss.xx] line").
type Lyrics struct {
	ID           int     `json:"id"`
	Track        string  `json:"trackName"`
	Artist       string  `json:"artistName"`
	Album        string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	Plain        string  `json:"plainLyrics"`
	Synced       string  `json:"syncedLyrics"`
}

// Line is one lyric line; At is seconds into the track (-1 when unsynced).
type Line struct {
	At   float64 `json:"at"`
	Text string  `json:"text"`
}

func base() string {
	if u := os.Getenv("GOOSENECK_LRCLIB_URL"); u != "" {
		return strings.TrimSuffix(u, "/")
	}
	return "https://lrclib.net"
}

// Search looks a track up and returns the best match, or nil when LRCLIB has
// nothing. duration (seconds, 0 = unknown) breaks ties between versions.
func Search(ctx context.Context, track, artist string, duration int) (*Lyrics, error) {
	if artist == "" { // a bare video: its title is often "Artist - Song (Official Video)"
		res, err := search(ctx, url.Values{"q": {Clean(track)}})
		if err != nil || len(res) == 0 {
			return nil, err
		}
		return best(res, duration), nil
	}
	res, err := search(ctx, url.Values{"track_name": {track}, "artist_name": {artist}})
	if err == nil && len(res) == 0 && Clean(track) != track { // "Song (Official Video)" etc.
		res, err = search(ctx, url.Values{"track_name": {Clean(track)}, "artist_name": {artist}})
	}
	if err != nil || len(res) == 0 {
		return nil, err
	}
	return best(res, duration), nil
}

func search(ctx context.Context, q url.Values) ([]Lyrics, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ { // LRCLIB answers 503 "busy" now and then
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, "GET", base()+"/api/search?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "gooseneck (https://github.com/lubabs770/gooseneck)")
		resp, err := client.Do(req)
		if err != nil {
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err // drop the long, percent-encoded URL
			}
			lastErr = fmt.Errorf("lrclib: %w", err)
			continue
		}
		var out []Lyrics
		err = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		switch {
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("lrclib: %s", resp.Status)
			continue
		case resp.StatusCode != http.StatusOK:
			return nil, fmt.Errorf("lrclib: %s", resp.Status)
		case err != nil:
			return nil, fmt.Errorf("lrclib: %w", err)
		}
		return out, nil
	}
	return nil, lastErr
}

// best prefers records with lyrics, then synced ones, then the closest length.
func best(res []Lyrics, duration int) *Lyrics {
	score := func(l Lyrics) float64 {
		s := 0.0
		if l.Plain == "" && l.Synced == "" {
			s += 1e6
		}
		if l.Synced == "" {
			s += 10
		}
		if duration > 0 {
			s += math.Abs(l.Duration - float64(duration))
		}
		return s
	}
	sort.SliceStable(res, func(i, j int) bool { return score(res[i]) < score(res[j]) })
	return &res[0]
}

var (
	bracketed = regexp.MustCompile(`\s*[(\[][^)\]]*[)\]]`)
	lrcStamp  = regexp.MustCompile(`^\[(\d+):(\d+(?:\.\d+)?)\]`)
)

// Clean strips the bracketed noise YouTube titles carry ("(Official Video)",
// "[Lyrics]", "(feat. X)").
func Clean(title string) string {
	if c := strings.TrimSpace(bracketed.ReplaceAllString(title, "")); c != "" {
		return c
	}
	return title
}

// Lines returns the synced lines when there are any, else the plain text with
// At = -1. Lines with several stamps ("[00:10.00][01:20.00]chorus") repeat.
func (l *Lyrics) Lines() []Line {
	if l == nil {
		return nil
	}
	if l.Synced == "" {
		var out []Line
		for _, s := range strings.Split(l.Plain, "\n") {
			out = append(out, Line{At: -1, Text: strings.TrimRight(s, "\r")})
		}
		return out
	}
	var out []Line
	for _, s := range strings.Split(l.Synced, "\n") {
		s = strings.TrimRight(s, "\r")
		var stamps []float64
		for {
			m := lrcStamp.FindStringSubmatch(s)
			if m == nil {
				break
			}
			min, _ := strconv.Atoi(m[1])
			sec, _ := strconv.ParseFloat(m[2], 64)
			stamps = append(stamps, float64(min)*60+sec)
			s = s[len(m[0]):]
		}
		for _, at := range stamps { // lines without a time stamp are metadata ([ar:…]) and dropped
			out = append(out, Line{At: at, Text: strings.TrimSpace(s)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// Current is the index of the line being sung at pos, or -1 (before the first
// line, or unsynced lyrics).
func Current(lines []Line, pos float64) int {
	i := sort.Search(len(lines), func(i int) bool { return lines[i].At > pos }) - 1
	if i >= 0 && lines[i].At < 0 {
		return -1
	}
	return i
}
