package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/config"
	"github.com/lubabs770/gooseneck/player/internal/mpv"
	"github.com/lubabs770/gooseneck/player/internal/scrobble"
)

// The daemon is a tiny background process (`goose daemon`) that watches the
// mpv player: it submits scrobbles and keeps radio sessions topped up. It has
// to be separate from the TUI because music keeps playing after the TUI quits.
// It holds a unix socket as a single-instance lock; connecting to that socket
// returns its status.

// ScrobblerSocket is where the daemon listens.
func ScrobblerSocket() string { return filepath.Join(config.RuntimeDir(), "daemon.sock") }

// ScrobbleServices returns the configured, enabled services.
func (l *Lib) ScrobbleServices() []scrobble.Service {
	c := l.Cfg
	if !config.On(c.Scrobble, true) {
		return nil
	}
	var out []scrobble.Service
	if config.On(c.LocalScrobble, true) {
		if lo, err := l.LocalScrobbles(); err == nil {
			out = append(out, lo)
		}
	}
	if c.ListenBrainzToken != "" {
		out = append(out, &scrobble.ListenBrainz{Token: c.ListenBrainzToken, Endpoint: os.Getenv("GOOSENECK_LISTENBRAINZ_URL")})
	}
	if c.LastfmKey != "" && c.LastfmSecret != "" && c.LastfmSession != "" {
		out = append(out, &scrobble.LastFM{Key: c.LastfmKey, Secret: c.LastfmSecret, Session: c.LastfmSession})
	}
	return out
}

// ScrobblerStatus is what the daemon reports.
type ScrobblerStatus struct {
	Running   bool             `json:"running"`
	Services  []string         `json:"services"`
	Current   *scrobble.Listen `json:"current,omitempty"`
	Played    float64          `json:"played"` // seconds listened to the current track
	Needed    float64          `json:"needed"` // seconds needed for it to count
	Scrobbled bool             `json:"scrobbled"`
	Sent      int              `json:"sent"` // scrobbles submitted this session
	Queued    map[string]int   `json:"queued"`
	LastError string           `json:"last_error,omitempty"`
	Radio     string           `json:"radio,omitempty"` // seed artist while a radio session is on
}

// QueryScrobbler asks a running daemon for its status.
func QueryScrobbler() (ScrobblerStatus, error) {
	conn, err := net.DialTimeout("unix", ScrobblerSocket(), 300*time.Millisecond)
	if err != nil {
		return ScrobblerStatus{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	var st ScrobblerStatus
	err = json.NewDecoder(conn).Decode(&st)
	return st, err
}

// daemonNeeded reports whether there's anything for the daemon to do.
func (l *Lib) daemonNeeded() bool {
	return len(l.ScrobbleServices()) > 0 || l.RadioStatus().Active
}

// EnsureDaemon starts the daemon when scrobbling or radio needs it and it
// isn't already running. Cheap to call often (one dial when it's up).
func (l *Lib) EnsureDaemon() {
	if !l.Player.Managed() || !l.daemonNeeded() {
		return
	}
	if _, err := QueryScrobbler(); err == nil {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.MkdirAll(config.RuntimeDir(), 0o700)
	logf, _ := os.OpenFile(filepath.Join(config.RuntimeDir(), "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	cmd := exec.Command(exe, "daemon")
	cmd.Env = os.Environ()
	if logf != nil {
		cmd.Stdout, cmd.Stderr = logf, logf
	}
	mpv.Detach(cmd)
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
	if logf != nil {
		logf.Close()
	}
}

// RunDaemon is the daemon loop. It returns when the player has been gone for
// a while, when there's nothing left to do (scrobbling off and no radio), when
// ctx ends, or if another daemon already owns the socket.
func (l *Lib) RunDaemon(ctx context.Context, logw io.Writer) error {
	services := l.ScrobbleServices()
	if !l.daemonNeeded() {
		return errors.New("nothing to do: scrobbling is off and no radio is playing")
	}
	logf := func(f string, a ...any) {
		fmt.Fprintf(logw, "%s "+f+"\n", append([]any{time.Now().Format("2006-01-02 15:04:05")}, a...)...)
	}
	sock := ScrobblerSocket()
	_ = os.MkdirAll(filepath.Dir(sock), 0o700)
	// Single instance: an exclusive lock, held until exit. Two daemons can be
	// spawned at once (e.g. by back-to-back plays); the loser just leaves.
	release, ok := lockFile(filepath.Join(filepath.Dir(sock), "daemon.lock"))
	if !ok {
		return nil
	}
	defer release()
	_ = os.Remove(sock) // we hold the lock, so any socket file is stale
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	defer func() { ln.Close(); os.Remove(sock) }()

	var mu sync.Mutex
	status := ScrobblerStatus{Running: true, Queued: map[string]int{}}
	for _, s := range services {
		status.Services = append(status.Services, s.Name())
	}
	go func() { // status endpoint
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			st := status
			st.Queued = l.DB.QueuedScrobbleCount()
			mu.Unlock()
			_ = json.NewEncoder(c).Encode(st)
			c.Close()
		}
	}()

	meta := func(id string) (scrobble.Listen, bool) {
		t, ok := l.DB.Track(id)
		if !ok || t.Title == "" || t.Artist == "" {
			return scrobble.Listen{}, false
		}
		return scrobble.Listen{Artist: t.Artist, Track: t.Title, Album: t.Album, Duration: t.Duration}, true
	}
	flush := func() {
		for _, svc := range services {
			n, err := l.flushScrobbles(ctx, svc)
			mu.Lock()
			status.Sent += n
			if err != nil {
				status.LastError = err.Error()
			}
			mu.Unlock()
			if n > 0 {
				logf("%s: submitted %d", svc.Name(), n)
			}
			if err != nil {
				logf("%s: %v", svc.Name(), err)
			}
		}
	}

	logf("daemon up: scrobbling to %v", status.Services)
	var radioBusy sync.Mutex // one radio top-up at a time
	flush()                  // anything left over from last time (e.g. offline listens)
	var tr scrobble.Tracker
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	lastFlush := time.Now()
	lastCfg := time.Now()
	gone := time.Time{}
	for {
		select {
		case <-ctx.Done():
			flush()
			return nil
		case now := <-tick.C:
			if now.Sub(lastCfg) > 10*time.Second { // honour `goose scrobble off` promptly
				lastCfg = now
				if cfg, err := config.Load(); err == nil {
					l.Cfg.Scrobble, l.Cfg.LocalScrobble = cfg.Scrobble, cfg.LocalScrobble
					l.Cfg.ListenBrainzToken, l.Cfg.LastfmSession = cfg.ListenBrainzToken, cfg.LastfmSession
					services = l.ScrobbleServices()
					mu.Lock()
					status.Services = status.Services[:0]
					for _, s := range services {
						status.Services = append(status.Services, s.Name())
					}
					mu.Unlock()
				}
				if !l.daemonNeeded() {
					flush()
					logf("nothing left to do; exiting")
					return nil
				}
			}
			st, err := l.Player.Status()
			if err != nil {
				if gone.IsZero() {
					gone = now
				}
				if now.Sub(gone) > 30*time.Second {
					flush()
					l.StopRadio() // the player was shut down: the session is over
					logf("player gone; daemon exiting")
					return nil
				}
				tr.Observe(now, scrobble.Snapshot{}, meta)
				continue
			}
			gone = time.Time{}
			if rs := l.RadioStatus(); rs.Active {
				mu.Lock()
				status.Radio = rs.SeedName
				mu.Unlock()
				if radioBusy.TryLock() {
					go func() {
						defer radioBusy.Unlock()
						c, cancel := context.WithTimeout(ctx, 2*time.Minute)
						defer cancel()
						if n, err := l.ExtendRadio(c); err != nil {
							logf("radio: %v", err)
						} else if n > 0 {
							logf("radio: added %d tracks", n)
						}
					}()
				}
			} else {
				mu.Lock()
				status.Radio = ""
				mu.Unlock()
			}
			if len(services) == 0 {
				continue
			}
			ev := tr.Observe(now, scrobble.Snapshot{Running: true, Idle: st.Idle, Paused: st.Paused,
				VideoID: st.VideoID, Pos: st.Pos, Duration: st.Duration}, meta)
			if ev.NowPlaying != nil {
				for _, svc := range services {
					go func(svc scrobble.Service, li scrobble.Listen) {
						c, cancel := context.WithTimeout(ctx, 15*time.Second)
						defer cancel()
						if err := svc.NowPlaying(c, li); err != nil {
							logf("%s now playing: %v", svc.Name(), err)
						}
					}(svc, *ev.NowPlaying)
				}
			}
			if ev.Scrobble != nil {
				raw, _ := json.Marshal(ev.Scrobble)
				for _, svc := range services {
					_ = l.DB.QueueScrobble(svc.Name(), raw)
				}
				logf("scrobble: %s — %s", ev.Scrobble.Artist, ev.Scrobble.Track)
				flush()
				lastFlush = now
			} else if now.Sub(lastFlush) > time.Minute {
				flush()
				lastFlush = now
			}
			cur, played, done := tr.Played()
			mu.Lock()
			if cur.VideoID != "" {
				status.Current, status.Played, status.Scrobbled = &cur, played, done
				status.Needed = scrobble.Needed(cur.Duration)
			} else {
				status.Current = nil
			}
			mu.Unlock()
		}
	}
}

// flushScrobbles submits queued listens for one service, oldest first.
func (l *Lib) flushScrobbles(ctx context.Context, svc scrobble.Service) (int, error) {
	sent := 0
	for {
		pending, err := l.DB.PendingScrobbles(svc.Name(), 50)
		if err != nil || len(pending) == 0 {
			return sent, err
		}
		var listens []scrobble.Listen
		var ids, bad []int64
		for _, q := range pending {
			var li scrobble.Listen
			if json.Unmarshal(q.Listen, &li) != nil {
				bad = append(bad, q.ID)
				continue
			}
			listens = append(listens, li)
			ids = append(ids, q.ID)
		}
		_ = l.DB.DoneScrobbles(bad)
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = svc.Scrobble(c, listens)
		cancel()
		switch {
		case err == nil:
			_ = l.DB.DoneScrobbles(ids)
			sent += len(ids)
		case scrobble.Permanent(err):
			_ = l.DB.DoneScrobbles(ids) // rejected for good; don't retry forever
			return sent, err
		default:
			l.DB.RetryScrobbles(ids)
			return sent, err
		}
		if len(pending) < 50 {
			return sent, nil
		}
	}
}

// FlushScrobbles submits everything queued, for every service (CLI use).
func (l *Lib) FlushScrobbles(ctx context.Context) (int, error) {
	total := 0
	var firstErr error
	for _, svc := range l.ScrobbleServices() {
		n, err := l.flushScrobbles(ctx, svc)
		total += n
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return total, firstErr
}
