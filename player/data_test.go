package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadArtists(t *testing.T) {
	t.Run("wrapped shape", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "artists.json")
		writeFile(t, p, `{"count":2,"artists":[{"id":"a","name":"Alpha"},{"id":"b","name":"Beta"}]}`)
		got, err := loadArtists(Config{ArtistsJSON: p})
		if err != nil {
			t.Fatalf("loadArtists: %v", err)
		}
		if len(got) != 2 || got[0].ID != "a" || got[1].Name != "Beta" {
			t.Fatalf("loadArtists = %+v, want 2 artists a/Beta", got)
		}
	})

	t.Run("bare array", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "artists.json")
		writeFile(t, p, `[{"id":"z","name":"Zed","isFemale":true}]`)
		got, err := loadArtists(Config{ArtistsJSON: p})
		if err != nil {
			t.Fatalf("loadArtists: %v", err)
		}
		if len(got) != 1 || got[0].ID != "z" || !got[0].IsFemale {
			t.Fatalf("loadArtists = %+v, want [z isFemale]", got)
		}
	})

	t.Run("invalid JSON errors", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "artists.json")
		writeFile(t, p, `not json`)
		if _, err := loadArtists(Config{ArtistsJSON: p}); err == nil {
			t.Fatal("loadArtists on garbage: want error, got nil")
		}
	})

	t.Run("missing file errors", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "nope.json")
		if _, err := loadArtists(Config{ArtistsJSON: p}); err == nil {
			t.Fatal("loadArtists on missing file: want error, got nil")
		}
	})
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
