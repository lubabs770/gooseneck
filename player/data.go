package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// loadArtists reads artists.json. It accepts either the wrapped {count,artists}
// shape or a bare array, and auto-detects the path when cfg.ArtistsJSON is empty.
func loadArtists(cfg Config) ([]Artist, error) {
	path := cfg.ArtistsJSON
	if path == "" {
		path = findArtistsJSON()
	}
	if path == "" {
		return nil, fmt.Errorf("artists.json not found; set artists_json in config")
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
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}
