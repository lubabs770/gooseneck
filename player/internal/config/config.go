// Package config loads and saves gooseneck's TOML config and knows where every
// on-disk file lives.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is loaded from <Dir>/config.toml. Unknown keys are ignored and missing
// keys fall back to Default(), so old configs keep working as fields are added.
type Config struct {
	BinDir      string   `toml:"bin_dir"`      // dir holding yt-dlp; "" = use PATH
	Player      string   `toml:"player"`       // media player binary ($APP overrides)
	PlayerArgs  []string `toml:"player_args"`  // extra player args, e.g. ["--no-video"]
	ArtistsJSON string   `toml:"artists_json"` // catalog path; "" = auto-detect/download
	ArtistsURL  string   `toml:"artists_url"`  // where the catalog is downloaded from
	Theme       string   `toml:"theme"`        // TUI theme name
	View        string   `toml:"view"`         // "grid" | "list"
	ShowHelp    *bool    `toml:"show_help"`    // key hints in the status bar
	Caret       *bool    `toml:"caret"`        // ›caret on the selection
	Thumbnails  *bool    `toml:"thumbnails"`   // artist / album art
	ThumbHeight int      `toml:"thumb_height"` // art height in rows; 0 = auto
	Sidebar     *bool    `toml:"sidebar"`      // now-playing sidebar on wide terminals
	Volume      int      `toml:"volume"`       // starting volume for the player (0-130)
}

// DefaultArtistsURL is the skmusic worker endpoint serving the scraped catalog.
const DefaultArtistsURL = "https://skmusic.shalomkarr.workers.dev/data/artists.json"

// BoolPtr returns a pointer to b (for the tri-state *bool fields).
func BoolPtr(b bool) *bool { return &b }

// On reports a *bool field, treating nil as def.
func On(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// Default returns the config used when no file exists.
func Default() Config {
	return Config{
		Player:     "mpv",
		PlayerArgs: []string{"--no-video"},
		ArtistsURL: DefaultArtistsURL,
		Theme:      "opencode",
		View:       "grid",
		ShowHelp:   BoolPtr(true),
		Caret:      BoolPtr(true),
		Thumbnails: BoolPtr(true),
		Sidebar:    BoolPtr(true),
		Volume:     80,
	}
}

// Dir is the config/data directory. $GOOSENECK_HOME overrides it (handy for
// demos and tests); otherwise it is <user config dir>/gooseneck.
func Dir() string {
	if d := os.Getenv("GOOSENECK_HOME"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "gooseneck")
}

// Path is the config file location.
func Path() string { return filepath.Join(Dir(), "config.toml") }

// CatalogCachePath is where a downloaded artists.json is cached.
func CatalogCachePath() string { return filepath.Join(Dir(), "artists.json") }

// DBPath is the SQLite cache + history database.
func DBPath() string { return filepath.Join(Dir(), "cache.db") }

// ThumbDir holds downloaded artist / album art.
func ThumbDir() string { return filepath.Join(Dir(), "thumbs") }

// LegacyLogPath is the pre-SQLite play history file (imported once).
func LegacyLogPath() string { return filepath.Join(Dir(), "logs.json") }

// RuntimeDir holds the player's IPC socket. It prefers $XDG_RUNTIME_DIR and
// falls back to a per-user temp dir. $GOOSENECK_HOME keeps it isolated too.
func RuntimeDir() string {
	if d := os.Getenv("GOOSENECK_HOME"); d != "" {
		return filepath.Join(d, "run")
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "gooseneck")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("gooseneck-%d", os.Getuid()))
}

// Load reads the config file, writing the defaults when it does not exist.
func Load() (Config, error) {
	cfg := Default()
	path := Path()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = Save(cfg)
		return applyEnv(cfg), nil
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Default(), fmt.Errorf("%s: %w", path, err)
	}
	return applyEnv(normalize(cfg)), nil
}

// normalize backfills fields that older configs don't have and clamps ranges.
func normalize(cfg Config) Config {
	d := Default()
	if cfg.ArtistsURL == "" {
		cfg.ArtistsURL = d.ArtistsURL
	}
	if cfg.Player == "" {
		cfg.Player = d.Player
	}
	for _, p := range []**bool{&cfg.ShowHelp, &cfg.Caret, &cfg.Thumbnails, &cfg.Sidebar} {
		if *p == nil {
			*p = BoolPtr(true)
		}
	}
	if cfg.View != "list" {
		cfg.View = "grid"
	}
	cfg.ThumbHeight = clamp(cfg.ThumbHeight, 0, 14)
	if cfg.Volume == 0 {
		cfg.Volume = d.Volume
	}
	cfg.Volume = clamp(cfg.Volume, 0, 130)
	return cfg
}

// Save writes cfg to Path(), creating the directory.
func Save(cfg Config) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	f, err := os.Create(Path())
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// applyEnv lets $APP override the configured player at runtime.
func applyEnv(cfg Config) Config {
	if app := os.Getenv("APP"); app != "" {
		cfg.Player = app
	}
	return cfg
}

// YtDlpPath returns the yt-dlp executable honoring BinDir.
func (c Config) YtDlpPath() string {
	name := "yt-dlp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if c.BinDir != "" {
		return filepath.Join(c.BinDir, name)
	}
	return name
}

// Env returns the process environment with BinDir prepended to PATH so both
// yt-dlp and the player (which shells out to yt-dlp) find the configured binary.
func (c Config) Env() []string {
	env := os.Environ()
	if c.BinDir == "" {
		return env
	}
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + c.BinDir + string(os.PathListSeparator) + kv[len("PATH="):]
			return env
		}
	}
	return append(env, "PATH="+c.BinDir)
}

// Keys lists the TOML keys, sorted, for `goose config` and completion.
func Keys() []string {
	t := reflect.TypeOf(Config{})
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		keys = append(keys, t.Field(i).Tag.Get("toml"))
	}
	sort.Strings(keys)
	return keys
}

func field(cfg *Config, key string) (reflect.Value, error) {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Tag.Get("toml") == key {
			return v.Field(i), nil
		}
	}
	return reflect.Value{}, fmt.Errorf("unknown config key %q (valid: %s)", key, strings.Join(Keys(), ", "))
}

// Get returns a config value formatted as a string.
func (c Config) Get(key string) (string, error) {
	f, err := field(&c, key)
	if err != nil {
		return "", err
	}
	switch f.Kind() {
	case reflect.Ptr:
		if f.IsNil() {
			return "", nil
		}
		return fmt.Sprint(f.Elem().Interface()), nil
	case reflect.Slice:
		parts := make([]string, f.Len())
		for i := range parts {
			parts[i] = fmt.Sprint(f.Index(i).Interface())
		}
		return strings.Join(parts, " "), nil
	}
	return fmt.Sprint(f.Interface()), nil
}

// Set parses value into the named key. Lists are whitespace separated.
func (c *Config) Set(key, value string) error {
	f, err := field(c, key)
	if err != nil {
		return err
	}
	switch f.Kind() {
	case reflect.String:
		f.SetString(value)
	case reflect.Int:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s wants a number: %w", key, err)
		}
		f.SetInt(int64(n))
	case reflect.Ptr: // *bool
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%s wants true/false: %w", key, err)
		}
		f.Set(reflect.ValueOf(BoolPtr(b)))
	case reflect.Slice:
		f.Set(reflect.ValueOf(strings.Fields(value)))
	default:
		return fmt.Errorf("cannot set %s", key)
	}
	*c = normalize(*c)
	return nil
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
