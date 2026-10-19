// Package store is gooseneck's SQLite database: a key/value cache for
// YouTube Music responses, metadata for every track that has been queued, and
// the play history. It is safe to open from several processes at once (the
// TUI and CLI commands share it).
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, keeps CGO_ENABLED=0 builds working
)

// Store wraps the database handle.
type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS kv (
  key        TEXT PRIMARY KEY,
  value      BLOB NOT NULL,
  fetched_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS track_meta (
  video_id  TEXT PRIMARY KEY,
  title     TEXT NOT NULL,
  artist    TEXT NOT NULL DEFAULT '',
  artist_id TEXT NOT NULL DEFAULT '',
  album     TEXT NOT NULL DEFAULT '',
  album_id  TEXT NOT NULL DEFAULT '',
  duration  INTEGER NOT NULL DEFAULT 0,
  thumb     TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS plays (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  at        INTEGER NOT NULL,
  artist_id TEXT NOT NULL DEFAULT '',
  artist    TEXT NOT NULL DEFAULT '',
  album     TEXT NOT NULL DEFAULT '',
  track     TEXT NOT NULL DEFAULT '',
  video_id  TEXT NOT NULL DEFAULT '',
  count     INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS plays_at ON plays(at);
CREATE TABLE IF NOT EXISTS downloads (
  video_id TEXT PRIMARY KEY,
  path     TEXT NOT NULL,
  size     INTEGER NOT NULL DEFAULT 0,
  at       INTEGER NOT NULL
);
`

// Open opens (creating if needed) the database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // sqlite: one writer; avoids SQLITE_BUSY inside a process
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// ---- key/value cache --------------------------------------------------------

// GetJSON decodes the cached value for key into v. It reports found=false when
// missing, and stale=true when older than maxAge (the value is still decoded so
// callers can fall back to it when offline).
func (s *Store) GetJSON(key string, maxAge time.Duration, v any) (found, stale bool) {
	var raw []byte
	var at int64
	err := s.db.QueryRow(`SELECT value, fetched_at FROM kv WHERE key = ?`, key).Scan(&raw, &at)
	if err != nil || json.Unmarshal(raw, v) != nil {
		return false, false
	}
	return true, time.Since(time.Unix(at, 0)) > maxAge
}

// PutJSON stores v under key.
func (s *Store) PutJSON(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO kv(key, value, fetched_at) VALUES(?,?,?)`,
		key, raw, time.Now().Unix())
	return err
}

// ClearCache drops every cached response (history and track metadata stay).
func (s *Store) ClearCache() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM kv`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CacheCount is the number of cached responses.
func (s *Store) CacheCount() int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM kv`).Scan(&n)
	return n
}

// ---- track metadata ----------------------------------------------------------

// Track is what the player needs to show a queued track nicely.
type Track struct {
	VideoID  string `json:"video_id"`
	Title    string `json:"title"`
	Artist   string `json:"artist,omitempty"`
	ArtistID string `json:"artist_id,omitempty"`
	Album    string `json:"album,omitempty"`
	AlbumID  string `json:"album_id,omitempty"`
	Duration int    `json:"duration,omitempty"`
	Thumb    string `json:"thumb,omitempty"`
}

// PutTracks upserts metadata for tracks.
func (s *Store) PutTracks(tracks []Track) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO track_meta
		(video_id,title,artist,artist_id,album,album_id,duration,thumb) VALUES(?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, t := range tracks {
		if t.VideoID == "" {
			continue
		}
		if _, err := stmt.Exec(t.VideoID, t.Title, t.Artist, t.ArtistID, t.Album, t.AlbumID, t.Duration, t.Thumb); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Track looks up metadata for a video id.
func (s *Store) Track(videoID string) (Track, bool) {
	t := Track{VideoID: videoID}
	err := s.db.QueryRow(`SELECT title,artist,artist_id,album,album_id,duration,thumb
		FROM track_meta WHERE video_id = ?`, videoID).
		Scan(&t.Title, &t.Artist, &t.ArtistID, &t.Album, &t.AlbumID, &t.Duration, &t.Thumb)
	return t, err == nil
}

// ---- play history --------------------------------------------------------------

// Play is one history entry: an artist, album or single track that was queued.
type Play struct {
	At       time.Time `json:"at"`
	ArtistID string    `json:"artist_id,omitempty"`
	Artist   string    `json:"artist,omitempty"`
	Album    string    `json:"album,omitempty"`
	Track    string    `json:"track,omitempty"`
	VideoID  string    `json:"video_id,omitempty"`
	Count    int       `json:"count"`
}

// LogPlay records a play (At defaults to now).
func (s *Store) LogPlay(p Play) error {
	if p.At.IsZero() {
		p.At = time.Now()
	}
	if p.Count == 0 {
		p.Count = 1
	}
	_, err := s.db.Exec(`INSERT INTO plays(at,artist_id,artist,album,track,video_id,count) VALUES(?,?,?,?,?,?,?)`,
		p.At.Unix(), p.ArtistID, p.Artist, p.Album, p.Track, p.VideoID, p.Count)
	return err
}

// History returns the most recent plays, newest first.
func (s *Store) History(limit int) ([]Play, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT at,artist_id,artist,album,track,video_id,count
		FROM plays ORDER BY at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Play
	for rows.Next() {
		var p Play
		var at int64
		if err := rows.Scan(&at, &p.ArtistID, &p.Artist, &p.Album, &p.Track, &p.VideoID, &p.Count); err != nil {
			return nil, err
		}
		p.At = time.Unix(at, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ArtistStat is an artist's play tally.
type ArtistStat struct {
	ArtistID string    `json:"artist_id"`
	Artist   string    `json:"artist"`
	Plays    int       `json:"plays"`
	Tracks   int       `json:"tracks"`
	Last     time.Time `json:"last"`
}

// TopArtists ranks artists by number of plays, then tracks queued.
func (s *Store) TopArtists(limit int) ([]ArtistStat, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT artist_id, MAX(artist), COUNT(*), SUM(count), MAX(at)
		FROM plays WHERE artist_id != '' OR artist != ''
		GROUP BY CASE WHEN artist_id != '' THEN artist_id ELSE artist END
		ORDER BY COUNT(*) DESC, SUM(count) DESC, MAX(at) DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArtistStat
	for rows.Next() {
		var a ArtistStat
		var last int64
		if err := rows.Scan(&a.ArtistID, &a.Artist, &a.Plays, &a.Tracks, &last); err != nil {
			return nil, err
		}
		a.Last = time.Unix(last, 0)
		out = append(out, a)
	}
	return out, rows.Err()
}

// RecentArtists returns distinct artists by most recent play.
func (s *Store) RecentArtists(limit int) ([]ArtistStat, error) {
	rows, err := s.db.Query(`SELECT artist_id, MAX(artist), COUNT(*), SUM(count), MAX(at)
		FROM plays WHERE artist_id != ''
		GROUP BY artist_id ORDER BY MAX(at) DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArtistStat
	for rows.Next() {
		var a ArtistStat
		var last int64
		if err := rows.Scan(&a.ArtistID, &a.Artist, &a.Plays, &a.Tracks, &last); err != nil {
			return nil, err
		}
		a.Last = time.Unix(last, 0)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ImportLegacyLog pulls a pre-SQLite logs.json into the plays table, once: it
// only runs while the table is empty, and leaves the old file untouched.
func (s *Store) ImportLegacyLog(path string) (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM plays`).Scan(&n); err != nil || n > 0 {
		return 0, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, nil // nothing to import
	}
	var events []struct {
		Time     string `json:"time"`
		ArtistID string `json:"artist_id"`
		Artist   string `json:"artist"`
		Album    string `json:"album"`
		Track    string `json:"track"`
		VideoID  string `json:"video_id"`
		Count    int    `json:"count"`
	}
	if err := json.Unmarshal(raw, &events); err != nil {
		return 0, nil
	}
	imported := 0
	for _, e := range events {
		at, err := time.Parse(time.RFC3339, e.Time)
		if err != nil {
			continue
		}
		if s.LogPlay(Play{At: at, ArtistID: e.ArtistID, Artist: e.Artist, Album: e.Album,
			Track: e.Track, VideoID: e.VideoID, Count: e.Count}) == nil {
			imported++
		}
	}
	return imported, nil
}

// ---- downloads ----------------------------------------------------------------

// Download is a track saved to disk.
type Download struct {
	Track
	Path string    `json:"path"`
	Size int64     `json:"size"`
	At   time.Time `json:"downloaded_at"`
}

// PutDownload records a downloaded file for a video id.
func (s *Store) PutDownload(videoID, path string, size int64) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO downloads(video_id,path,size,at) VALUES(?,?,?,?)`,
		videoID, path, size, time.Now().Unix())
	return err
}

// DownloadPath returns the recorded file for a video id ("" if none). It does
// not check the file still exists; callers do.
func (s *Store) DownloadPath(videoID string) string {
	var p string
	_ = s.db.QueryRow(`SELECT path FROM downloads WHERE video_id = ?`, videoID).Scan(&p)
	return p
}

// DownloadedSet returns which of ids have a recorded download.
func (s *Store) DownloadedSet(ids []string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		if p := s.DownloadPath(id); p != "" {
			out[id] = p
		}
	}
	return out
}

// Downloads lists every download with its track metadata, by artist/album.
func (s *Store) Downloads() ([]Download, error) {
	rows, err := s.db.Query(`SELECT d.video_id, d.path, d.size, d.at,
		COALESCE(t.title,''), COALESCE(t.artist,''), COALESCE(t.artist_id,''), COALESCE(t.album,''),
		COALESCE(t.album_id,''), COALESCE(t.duration,0), COALESCE(t.thumb,'')
		FROM downloads d LEFT JOIN track_meta t ON t.video_id = d.video_id
		ORDER BY t.artist COLLATE NOCASE, t.album COLLATE NOCASE, d.path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Download
	for rows.Next() {
		var d Download
		var at int64
		if err := rows.Scan(&d.VideoID, &d.Path, &d.Size, &at, &d.Title, &d.Artist, &d.ArtistID,
			&d.Album, &d.AlbumID, &d.Duration, &d.Thumb); err != nil {
			return nil, err
		}
		d.At = time.Unix(at, 0)
		out = append(out, d)
	}
	return out, rows.Err()
}

// RemoveDownload forgets a download (the caller deletes the file).
func (s *Store) RemoveDownload(videoID string) error {
	_, err := s.db.Exec(`DELETE FROM downloads WHERE video_id = ?`, videoID)
	return err
}
