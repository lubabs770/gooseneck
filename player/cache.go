package main

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite" // pure-Go sqlite driver, no cgo
)

// Album is a channel "release" (playlist) belonging to an artist.
type Album struct {
	PlaylistID string
	Title      string
}

// Track is a single video within an album playlist.
type Track struct {
	VideoID string
	Title   string
}

// Cache wraps the sqlite db that stores enumerated albums/tracks so we only hit
// yt-dlp once per artist/album.
type Cache struct{ db *sql.DB }

func openCache(path string) (*Cache, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	schema := `
CREATE TABLE IF NOT EXISTS albums (
  artist_id   TEXT NOT NULL,
  playlist_id TEXT NOT NULL,
  title       TEXT NOT NULL,
  ord         INTEGER NOT NULL,
  fetched_at  INTEGER NOT NULL,
  PRIMARY KEY (artist_id, playlist_id)
);
CREATE TABLE IF NOT EXISTS tracks (
  playlist_id TEXT NOT NULL,
  video_id    TEXT NOT NULL,
  title       TEXT NOT NULL,
  ord         INTEGER NOT NULL,
  fetched_at  INTEGER NOT NULL,
  PRIMARY KEY (playlist_id, video_id)
);`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Cache{db: db}, nil
}

func (c *Cache) Close() error { return c.db.Close() }

// Albums returns cached albums for an artist; (nil,false) when never fetched.
func (c *Cache) Albums(artistID string) ([]Album, bool) {
	rows, err := c.db.Query(
		`SELECT playlist_id, title FROM albums WHERE artist_id = ? ORDER BY ord`, artistID)
	if err != nil {
		return nil, false
	}
	defer rows.Close()
	var out []Album
	for rows.Next() {
		var a Album
		if rows.Scan(&a.PlaylistID, &a.Title) == nil {
			out = append(out, a)
		}
	}
	return out, len(out) > 0
}

func (c *Cache) PutAlbums(artistID string, albums []Album) {
	tx, err := c.db.Begin()
	if err != nil {
		return
	}
	now := time.Now().Unix()
	for i, a := range albums {
		_, _ = tx.Exec(
			`INSERT OR REPLACE INTO albums(artist_id,playlist_id,title,ord,fetched_at)
			 VALUES(?,?,?,?,?)`, artistID, a.PlaylistID, a.Title, i, now)
	}
	_ = tx.Commit()
}

// Tracks returns cached tracks for an album playlist; (nil,false) when unfetched.
func (c *Cache) Tracks(playlistID string) ([]Track, bool) {
	rows, err := c.db.Query(
		`SELECT video_id, title FROM tracks WHERE playlist_id = ? ORDER BY ord`, playlistID)
	if err != nil {
		return nil, false
	}
	defer rows.Close()
	var out []Track
	for rows.Next() {
		var t Track
		if rows.Scan(&t.VideoID, &t.Title) == nil {
			out = append(out, t)
		}
	}
	return out, len(out) > 0
}

func (c *Cache) PutTracks(playlistID string, tracks []Track) {
	tx, err := c.db.Begin()
	if err != nil {
		return
	}
	now := time.Now().Unix()
	for i, t := range tracks {
		_, _ = tx.Exec(
			`INSERT OR REPLACE INTO tracks(playlist_id,video_id,title,ord,fetched_at)
			 VALUES(?,?,?,?,?)`, playlistID, t.VideoID, t.Title, i, now)
	}
	_ = tx.Commit()
}
