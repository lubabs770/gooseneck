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
	ArtistsJSON string   `toml:"artists_json"` // path to artists.json; "" = auto-detect
	Theme       string   `toml:"theme"`        // theme name (see theme.go)
	View        string   `toml:"view"`         // "grid" | "list"
}

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
		Theme:       "default",
		View:        "grid",
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
	return applyEnv(cfg), nil
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
