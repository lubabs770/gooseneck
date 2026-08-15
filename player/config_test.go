package main

import "testing"

func TestBoolPtr(t *testing.T) {
	p := boolPtr(true)
	if p == nil || *p != true {
		t.Fatalf("boolPtr(true) = %v, want pointer to true", p)
	}
	q := boolPtr(false)
	if q == nil || *q != false {
		t.Fatalf("boolPtr(false) = %v, want pointer to false", q)
	}
	if p == q {
		t.Fatal("boolPtr should return distinct pointers")
	}
}

func TestYtDlpPath(t *testing.T) {
	cases := []struct {
		name   string
		binDir string
		want   string
	}{
		{"empty uses PATH name", "", "yt-dlp"},
		{"bindir joins path", "/opt/bin", "/opt/bin/yt-dlp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Config{BinDir: tc.binDir}.ytDlpPath()
			if got != tc.want {
				t.Fatalf("ytDlpPath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestApplyEnv(t *testing.T) {
	t.Run("APP overrides player", func(t *testing.T) {
		t.Setenv("APP", "vlc")
		cfg := applyEnv(Config{Player: "mpv"})
		if cfg.Player != "vlc" {
			t.Fatalf("Player = %q, want vlc", cfg.Player)
		}
	})
	t.Run("empty APP keeps configured player", func(t *testing.T) {
		t.Setenv("APP", "")
		cfg := applyEnv(Config{Player: "mpv"})
		if cfg.Player != "mpv" {
			t.Fatalf("Player = %q, want mpv", cfg.Player)
		}
	})
}

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Player != "mpv" {
		t.Errorf("Player = %q, want mpv", cfg.Player)
	}
	if cfg.View != "grid" {
		t.Errorf("View = %q, want grid", cfg.View)
	}
	if cfg.ArtistsURL != defaultArtistsURL {
		t.Errorf("ArtistsURL = %q, want %q", cfg.ArtistsURL, defaultArtistsURL)
	}
	for _, f := range []struct {
		name string
		p    *bool
	}{
		{"ShowHelp", cfg.ShowHelp},
		{"Caret", cfg.Caret},
		{"Thumbnails", cfg.Thumbnails},
	} {
		if f.p == nil || *f.p != true {
			t.Errorf("%s = %v, want pointer to true", f.name, f.p)
		}
	}
	if cfg.ThumbHeight != 0 {
		t.Errorf("ThumbHeight = %d, want 0", cfg.ThumbHeight)
	}
}
