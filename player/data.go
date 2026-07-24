package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Artist mirrors one record in artists.json. Unknown fields are ignored, so new
// keys added by the worker won't break loading.
type Artist struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Thumbnail      string `json:"thumbnail"`
	IsFemale       bool   `json:"isFemale"`
	IsChasid       bool   `json:"isChasid"`
	IsKidZone      bool   `json:"isKidZone"`
	IsDJ           bool   `json:"isDJ"`
	IsAmerican     bool   `json:"isAmerican"`
	IsFamous       bool   `json:"isFamous"`
	IsIsraeli      bool   `json:"isIsraeli"`
	IsAcapellaOnly bool   `json:"isAcapellaOnly"`
}

// artistsFile is the worker's wrapped shape: {"count":N,"artists":[...]}.
type artistsFile struct {
	Count   int      `json:"count"`
	Artists []Artist `json:"artists"`
}

// loadArtists resolves the catalog, in order of preference:
//  1. cfg.ArtistsJSON (explicit path)
//  2. a local artists.json next to the binary / repo root (dev convenience)
//  3. the cached download in the config dir
//  4. download from cfg.ArtistsURL and cache it
// It accepts either the wrapped {count,artists} shape or a bare array.
func loadArtists(cfg Config) ([]Artist, error) {
	path := cfg.ArtistsJSON
	if path == "" {
		path = findArtistsJSON()
	}
	if path == "" {
		cached := artistsCachePath()
		if _, err := os.Stat(cached); err == nil {
			path = cached
		} else if err := downloadArtists(cfg.ArtistsURL, cached); err != nil {
			return nil, err
		} else {
			path = cached
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var wrapped artistsFile
	if err := json.Unmarshal(b, &wrapped); err == nil && len(wrapped.Artists) > 0 {
		return wrapped.Artists, nil
	}
	var bare []Artist
	if err := json.Unmarshal(b, &bare); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return bare, nil
}

// findArtistsJSON looks next to the binary and one dir up (repo root when run
// from player/).
func findArtistsJSON() string {
	candidates := []string{"artists.json", filepath.Join("..", "artists.json")}
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(d, "artists.json"),
			filepath.Join(d, "..", "artists.json"))
	}
	for _, c := range candidates {
		b, err := os.ReadFile(c)
		if err == nil && json.Valid(b) { // skip stray/HTML files that aren't our catalog
			return c
		}
	}
	return ""
}

// downloadArtists fetches the catalog from url, validates it is JSON, and writes
// it to dest (creating parent dirs).
func downloadArtists(url, dest string) error {
	if url == "" {
		return fmt.Errorf("no artists source: set artists_json or artists_url in config")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20)) // 32 MiB cap
	if err != nil {
		return err
	}
	if !json.Valid(body) {
		return fmt.Errorf("download %s: response was not valid JSON (wrong URL?)", url)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, body, 0o644)
}
