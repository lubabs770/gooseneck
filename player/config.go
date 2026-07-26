package main

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is loaded from ~/.config/gooseneck/config.toml. New fields can be added
// over time; missing fields fall back to the defaults set in defaultConfig().
type Config struct {
	BinDir      string   `toml:"bin_dir"`      // dir holding yt-dlp; "" = use PATH
	Player      string   `toml:"player"`       // media player binary ($APP overrides)
	PlayerArgs  []string `toml:"player_args"`  // extra args, e.g. ["--no-video"]
	ArtistsJSON string   `toml:"artists_json"` // path to artists.json; "" = auto-detect/download
	ArtistsURL  string   `toml:"artists_url"`  // source to download artists.json from
	Theme       string   `toml:"theme"`        // theme name (see theme.go)
	View        string   `toml:"view"`         // "grid" | "list"
	ShowHelp    *bool    `toml:"show_help"`    // show the key hints line at startup (default true)
	Caret       *bool    `toml:"caret"`        // show a ›caret on the selection, any view (default true)
	Thumbnails  *bool    `toml:"thumbnails"`   // render artist profile pics in the grid (default true)
	ThumbHeight int      `toml:"thumb_height"` // profile-pic height in rows; 0 = auto (square)
}

func boolPtr(b bool) *bool { return &b }

// defaultArtistsURL is the skmusic worker endpoint serving the scraped catalog.
const defaultArtistsURL = "https://skmusic.shalomkarr.workers.dev/data/artists.json"

func configDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "gooseneck")
}

func defaultConfig() Config {
	return Config{
		BinDir:      "",
		Player:      "mpv",
		PlayerArgs:  []string{"--no-video"},
		ArtistsJSON: "",
		ArtistsURL:  defaultArtistsURL,
		Theme:       "default",
		View:        "grid",
		ShowHelp:    boolPtr(true),
		Caret:       boolPtr(true),
		Thumbnails:  boolPtr(true),
		ThumbHeight: 0, // auto: square cards
	}
}

// loadConfig reads the config file, writing a default one if none exists.
func loadConfig() (Config, error) {
	cfg := defaultConfig()
	dir := configDir()
	path := filepath.Join(dir, "config.toml")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = os.MkdirAll(dir, 0o755)
		if f, err := os.Create(path); err == nil {
			_ = toml.NewEncoder(f).Encode(cfg)
			_ = f.Close()
		}
		return applyEnv(cfg), nil
	}

	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, err
	}
	if cfg.ArtistsURL == "" { // backfill for configs written before this field existed
		cfg.ArtistsURL = defaultArtistsURL
	}
	if cfg.ShowHelp == nil { // absent in older configs -> default on
		cfg.ShowHelp = boolPtr(true)
	}
	if cfg.Caret == nil {
		cfg.Caret = boolPtr(true)
	}
	if cfg.Thumbnails == nil {
		cfg.Thumbnails = boolPtr(true)
	}
	if cfg.ThumbHeight < 0 {
		cfg.ThumbHeight = 0 // 0 = auto (square)
	}
	if cfg.ThumbHeight > 14 {
		cfg.ThumbHeight = 14
	}
	return applyEnv(cfg), nil
}

// artistsCachePath is where a downloaded catalog is cached.
func artistsCachePath() string {
	return filepath.Join(configDir(), "artists.json")
}

// thumbCachePath is where a downloaded artist thumbnail is cached on disk. The
// artist id is a YouTube channel id (safe as a filename). The ".sq" marks the
// square-cropped variant, so caches from older (banner) versions are ignored.
func thumbCachePath(id string) string {
	return filepath.Join(configDir(), "thumbs", id+".sq.img")
}

// applyEnv lets $APP override the configured player at runtime.
func applyEnv(cfg Config) Config {
	if app := os.Getenv("APP"); app != "" {
		cfg.Player = app
	}
	return cfg
}

// ytDlpPath returns the yt-dlp executable path honoring BinDir.
func (c Config) ytDlpPath() string {
	if c.BinDir != "" {
		return filepath.Join(c.BinDir, "yt-dlp")
	}
	return "yt-dlp"
}

// cacheDBPath is the sqlite cache location, alongside the config.
func cacheDBPath() string {
	return filepath.Join(configDir(), "cache.db")
}
