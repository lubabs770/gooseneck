package scrobble

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver
)

// Local is a scrobble service that never leaves the machine: every listen is
// written to its own SQLite file (scrobbles.db by default), separate from the
// cache database, so it can be backed up, synced or queried on its own.
type Local struct{ db *sql.DB }

const localSchema = `
CREATE TABLE IF NOT EXISTS listens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  listened_at  INTEGER NOT NULL,          -- unix seconds the track started
  video_id     TEXT NOT NULL DEFAULT '',
  artist       TEXT NOT NULL,
  track        TEXT NOT NULL,
  album        TEXT NOT NULL DEFAULT '',
  duration     INTEGER NOT NULL DEFAULT 0, -- seconds
  submitted_at INTEGER NOT NULL,
  UNIQUE (video_id, listened_at)
);
CREATE INDEX IF NOT EXISTS listens_at ON listens(listened_at);
CREATE INDEX IF NOT EXISTS listens_artist ON listens(artist COLLATE NOCASE);
`

// OpenLocal opens (creating if needed) the local scrobble database.
func OpenLocal(path string) (*Local, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(localSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return &Local{db: db}, nil
}

// Close closes the database.
func (lo *Local) Close() error { return lo.db.Close() }

func (lo *Local) Name() string { return "local" }

// NowPlaying is a no-op: only completed listens are recorded.
func (lo *Local) NowPlaying(ctx context.Context, l Listen) error { return nil }

// Scrobble records listens; re-submitting the same play is ignored.
func (lo *Local) Scrobble(ctx context.Context, ls []Listen) error {
	tx, err := lo.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	for _, l := range ls {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO listens
			(listened_at, video_id, artist, track, album, duration, submitted_at) VALUES (?,?,?,?,?,?,?)`,
			l.StartedAt.Unix(), l.VideoID, l.Artist, l.Track, l.Album, l.Duration, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Range limits queries to listens at or after Since (zero = all time).
type Range struct{ Since time.Time }

func (r Range) since() int64 {
	if r.Since.IsZero() {
		return 0
	}
	return r.Since.Unix()
}

// Recent returns the latest listens, newest first.
func (lo *Local) Recent(r Range, limit int) ([]Listen, error) {
	if limit <= 0 {
		limit = 1 << 30
	}
	rows, err := lo.db.Query(`SELECT listened_at, video_id, artist, track, album, duration
		FROM listens WHERE listened_at >= ? ORDER BY listened_at DESC, id DESC LIMIT ?`, r.since(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Listen
	for rows.Next() {
		var l Listen
		var at int64
		if err := rows.Scan(&at, &l.VideoID, &l.Artist, &l.Track, &l.Album, &l.Duration); err != nil {
			return nil, err
		}
		l.StartedAt = time.Unix(at, 0)
		out = append(out, l)
	}
	return out, rows.Err()
}

// Count is one row of a top list.
type Count struct {
	Name    string `json:"name"`
	Artist  string `json:"artist,omitempty"` // for tracks and albums
	Listens int    `json:"listens"`
	Seconds int    `json:"seconds"` // total listening time (track lengths)
}

// Top ranks artists, tracks or albums ("artist" | "track" | "album").
func (lo *Local) Top(what string, r Range, limit int) ([]Count, error) {
	var q string
	switch what {
	case "artist":
		q = `SELECT artist, '', COUNT(*), SUM(duration) FROM listens WHERE listened_at >= ?
			GROUP BY artist COLLATE NOCASE ORDER BY COUNT(*) DESC, MAX(listened_at) DESC LIMIT ?`
	case "track":
		q = `SELECT track, artist, COUNT(*), SUM(duration) FROM listens WHERE listened_at >= ?
			GROUP BY track COLLATE NOCASE, artist COLLATE NOCASE ORDER BY COUNT(*) DESC, MAX(listened_at) DESC LIMIT ?`
	case "album":
		q = `SELECT album, artist, COUNT(*), SUM(duration) FROM listens WHERE listened_at >= ? AND album != ''
			GROUP BY album COLLATE NOCASE, artist COLLATE NOCASE ORDER BY COUNT(*) DESC, MAX(listened_at) DESC LIMIT ?`
	default:
		return nil, fmt.Errorf("top what? artist, track or album (got %q)", what)
	}
	if limit <= 0 {
		limit = 10
	}
	rows, err := lo.db.Query(q, r.since(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Count
	for rows.Next() {
		var c Count
		if err := rows.Scan(&c.Name, &c.Artist, &c.Listens, &c.Seconds); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Totals summarises a range.
type Totals struct {
	Listens int `json:"listens"`
	Artists int `json:"artists"`
	Tracks  int `json:"tracks"`
	Seconds int `json:"seconds"`
	Days    int `json:"days"` // distinct days with listening
}

// Totals counts listens, distinct artists/tracks and time in a range.
func (lo *Local) Totals(r Range) (Totals, error) {
	var t Totals
	err := lo.db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT lower(artist)), COUNT(DISTINCT lower(artist)||'|'||lower(track)),
		COALESCE(SUM(duration),0), COUNT(DISTINCT date(listened_at,'unixepoch','localtime'))
		FROM listens WHERE listened_at >= ?`, r.since()).Scan(&t.Listens, &t.Artists, &t.Tracks, &t.Seconds, &t.Days)
	return t, err
}

// ParseSince turns "today", "week", "month", "year", "all" or a duration
// ("7d", "48h") into a Range.
func ParseSince(s string, now time.Time) (Range, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch s {
	case "", "all", "ever":
		return Range{}, nil
	case "today":
		return Range{day}, nil
	case "week":
		return Range{day.AddDate(0, 0, -6)}, nil
	case "month":
		return Range{day.AddDate(0, -1, 0)}, nil
	case "year":
		return Range{day.AddDate(-1, 0, 0)}, nil
	}
	if strings.HasSuffix(s, "d") {
		var n int
		if _, err := fmt.Sscanf(s, "%dd", &n); err == nil && n > 0 {
			return Range{now.AddDate(0, 0, -n)}, nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return Range{now.Add(-d)}, nil
	}
	return Range{}, fmt.Errorf("unknown period %q (today, week, month, year, all, 7d, 12h)", s)
}
