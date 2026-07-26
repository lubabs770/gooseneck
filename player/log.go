package main

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"
)

// playEvent is one entry in logs.json — what was played and when. Album/Track
// are empty for an artist-wide play; Track is empty for a whole-album play.
type playEvent struct {
	Time     string `json:"time"` // RFC3339 UTC
	ArtistID string `json:"artist_id,omitempty"`
	Artist   string `json:"artist,omitempty"`
	Album    string `json:"album,omitempty"`
	Track    string `json:"track,omitempty"`
	VideoID  string `json:"video_id,omitempty"`
	Count    int    `json:"count,omitempty"` // number of tracks queued
}

func logsPath() string { return configDir() + "/logs.json" }

var logMu sync.Mutex

// logPlay appends a play event to logs.json (a JSON array). Best-effort: failures
// are swallowed so logging never disrupts playback.
func logPlay(ev playEvent) {
	ev.Time = time.Now().UTC().Format(time.RFC3339)
	logMu.Lock()
	defer logMu.Unlock()
	events := readLog()
	events = append(events, ev)
	b, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(configDir(), 0o755)
	_ = os.WriteFile(logsPath(), b, 0o644)
}

// readLog loads the play history (empty if absent/corrupt).
func readLog() []playEvent {
	b, err := os.ReadFile(logsPath())
	if err != nil {
		return nil
	}
	var events []playEvent
	_ = json.Unmarshal(b, &events)
	return events
}

// artistPlays is a frequency tally for one artist.
type artistPlays struct {
	ArtistID string
	Artist   string
	Plays    int // number of play events
	Tracks   int // total tracks queued
}

// topArtists returns artists from the log ordered by play frequency — the basis
// for future background pre-indexing of the most-listened artists.
func topArtists() []artistPlays {
	byID := map[string]*artistPlays{}
	for _, ev := range readLog() {
		key := ev.ArtistID
		if key == "" {
			key = ev.Artist
		}
		if key == "" {
			continue
		}
		a := byID[key]
		if a == nil {
			a = &artistPlays{ArtistID: ev.ArtistID, Artist: ev.Artist}
			byID[key] = a
		}
		a.Plays++
		if ev.Count > 0 {
			a.Tracks += ev.Count
		} else {
			a.Tracks++
		}
	}
	out := make([]artistPlays, 0, len(byID))
	for _, a := range byID {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Plays != out[j].Plays {
			return out[i].Plays > out[j].Plays
		}
		return out[i].Tracks > out[j].Tracks
	})
	return out
}
