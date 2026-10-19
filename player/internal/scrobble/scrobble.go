// Package scrobble submits what you listen to to ListenBrainz and/or Last.fm.
//
// Tracker decides *when* a play counts (the Last.fm rules, which ListenBrainz
// shares): the track is longer than 30s and has actually been listened to for
// half its length or 4 minutes, whichever comes first. Pauses don't count and
// seeking doesn't count — only wall-clock time spent playing.
package scrobble

import (
	"context"
	"time"
)

// Listen is one play of a track.
type Listen struct {
	VideoID   string    `json:"video_id"`
	Artist    string    `json:"artist"`
	Track     string    `json:"track"`
	Album     string    `json:"album,omitempty"`
	Duration  int       `json:"duration,omitempty"` // seconds
	StartedAt time.Time `json:"started_at"`
}

// Service is a scrobbling backend.
type Service interface {
	Name() string
	NowPlaying(ctx context.Context, l Listen) error
	Scrobble(ctx context.Context, ls []Listen) error
}

// Rules (Last.fm's): scrobble after half the track or 4 minutes, and never
// tracks shorter than 30 seconds.
const (
	minDuration = 30
	maxNeeded   = 240
)

// Needed is how many seconds of listening make a play count.
func Needed(duration int) float64 {
	if duration <= 0 {
		return maxNeeded // unknown length: require the 4-minute rule
	}
	return min(float64(duration)/2, maxNeeded)
}

// Snapshot is what the tracker needs from the player.
type Snapshot struct {
	Running  bool
	Idle     bool
	Paused   bool
	VideoID  string
	Pos      float64
	Duration float64
}

// Tracker follows the player over time and reports when to send "now
// playing" and when a play has earned a scrobble.
type Tracker struct {
	cur       *Listen
	played    float64
	last      time.Time
	lastPos   float64
	nowSent   bool
	scrobbled bool
}

// Events from one Observe call.
type Events struct {
	NowPlaying *Listen
	Scrobble   *Listen
}

// Observe feeds a player snapshot taken at time now. meta returns the track
// metadata for a video id (ok=false skips tracks we know nothing about).
func (t *Tracker) Observe(now time.Time, s Snapshot, meta func(videoID string) (Listen, bool)) Events {
	var ev Events
	active := s.Running && !s.Idle && s.VideoID != ""
	if !active {
		t.cur = nil
		t.last = now
		return ev
	}
	// A new track, or the same track starting over (repeat-one / replay).
	restarted := t.cur != nil && t.cur.VideoID == s.VideoID && s.Pos+5 < t.lastPos && s.Pos < 5
	if t.cur == nil || t.cur.VideoID != s.VideoID || restarted {
		l, ok := meta(s.VideoID)
		if !ok {
			t.cur = nil
			t.last = now
			return ev
		}
		l.VideoID = s.VideoID
		if l.Duration == 0 && s.Duration > 0 {
			l.Duration = int(s.Duration)
		}
		l.StartedAt = now.Add(-time.Duration(s.Pos * float64(time.Second)))
		t.cur, t.played, t.nowSent, t.scrobbled = &l, 0, false, false
		t.last, t.lastPos = now, s.Pos
	}
	if !s.Paused {
		dt := now.Sub(t.last).Seconds()
		if dt > 0 && dt < 15 { // ignore gaps (sleep/suspend, a stalled poll)
			t.played += dt
		}
	}
	t.last, t.lastPos = now, s.Pos
	if t.cur.Duration == 0 && s.Duration > 0 {
		t.cur.Duration = int(s.Duration)
	}
	if !t.nowSent && !s.Paused {
		t.nowSent = true
		l := *t.cur
		ev.NowPlaying = &l
	}
	long := t.cur.Duration == 0 || t.cur.Duration >= minDuration
	if !t.scrobbled && long && t.played >= Needed(t.cur.Duration) {
		t.scrobbled = true
		l := *t.cur
		ev.Scrobble = &l
	}
	return ev
}

// Played reports seconds listened to the current track (for status output).
func (t *Tracker) Played() (Listen, float64, bool) {
	if t.cur == nil {
		return Listen{}, 0, false
	}
	return *t.cur, t.played, t.scrobbled
}
