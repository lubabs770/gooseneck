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
	Lyrics      string   `toml:"lyrics"`       // live lyrics in the TUI: "off" | "sidebar" | "line"
	Volume      int      `toml:"volume"`       // starting volume for the player (0-130)
	DownloadDir string   `toml:"download_dir"` // where downloads go; "" = ~/Music/gooseneck

	// Scrobbling (see `goose scrobble`). Secrets: the file is saved 0600.
	Scrobble          *bool  `toml:"scrobble"`           // master switch (default on)
	LocalScrobble     *bool  `toml:"local_scrobble"`     // record listens in scrobble_db (default on)
	ScrobbleDB        string `toml:"scrobble_db"`        // "" = <config dir>/scrobbles.db
	ListenBrainzToken string `toml:"listenbrainz_token"` // https://listenbrainz.org/settings/
	LastfmKey         string `toml:"lastfm_api_key"`     // https://www.last.fm/api/account/create
	LastfmSecret      string `toml:"lastfm_api_secret"`
	LastfmSession     string `toml:"lastfm_session"` // set by `goose scrobble login lastfm`
	LastfmUser        string `toml:"lastfm_user"`

	// Extra catalogs merged with the main one (managed by `goose catalog`).
	Catalogs []CatalogSource `toml:"catalog"`

	// KeyMap overrides TUI keys: each entry makes the pressed key act like a
	// default key, e.g. {"j": "k", "k": "j"} swaps up and down. Unlisted keys
	// keep their defaults.
	KeyMap map[string]string `toml:"keys,omitempty"`
}

// CatalogSource is one extra catalog: a local artists.json (Path, editable
// with `goose catalog add-artist`) or a remote one (URL, cached locally).
type CatalogSource struct {
	Name     string `toml:"name"`
	Path     string `toml:"path,omitempty"`
	URL      string `toml:"url,omitempty"`
	Disabled bool   `toml:"disabled,omitempty"`
}

// File is where this catalog's JSON lives on disk (URL catalogs are cached
// under <config dir>/catalogs/).
func (s CatalogSource) File() string {
	if s.Path != "" {
		if strings.HasPrefix(s.Path, "~/") {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, s.Path[2:])
		}
		return s.Path
	}
	return filepath.Join(Dir(), "catalogs", s.Name+".json")
}

// Secret reports whether a config key holds a credential (masked in output).
func Secret(key string) bool {
	return strings.Contains(key, "token") || strings.Contains(key, "secret") || key == "lastfm_session"
}

// Mask hides all but the last 4 characters of a secret.
func Mask(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 4 {
		return "••••"
	}
	return "••••" + v[len(v)-4:]
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
		Player:        "mpv",
		PlayerArgs:    []string{"--no-video"},
		ArtistsURL:    DefaultArtistsURL,
		Theme:         "auto", // Omarchy's theme when installed, else opencode
		View:          "grid",
		ShowHelp:      BoolPtr(true),
		Caret:         BoolPtr(true),
		Thumbnails:    BoolPtr(true),
		Sidebar:       BoolPtr(true),
		Scrobble:      BoolPtr(true),
		LocalScrobble: BoolPtr(true),
		Volume:        80,
		Lyrics:        "off",
	}
}

// Dir is the config/data directory. $GOOSENECK_HOME overrides it (handy for
// demos and tests). Otherwise the first existing of DirCandidates wins, and a
// fresh install gets the first one.
func Dir() string {
	if d := os.Getenv("GOOSENECK_HOME"); d != "" {
		return d
	}
	dirs := DirCandidates()
	for _, d := range dirs {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
	}
	return dirs[0]
}

// DirCandidates lists the tried config dirs, XDG style: $XDG_CONFIG_HOME,
// then ~/.config, then the OS default (~/Library/Application Support on
// macOS), which older versions used.
func DirCandidates() []string {
	var dirs []string
	add := func(base string) {
		if base == "" {
			return
		}
		d := filepath.Join(base, "gooseneck")
		for _, x := range dirs {
			if x == d {
				return
			}
		}
		dirs = append(dirs, d)
	}
	add(os.Getenv("XDG_CONFIG_HOME"))
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".config"))
	}
	if base, err := os.UserConfigDir(); err == nil {
		add(base)
	}
	if len(dirs) == 0 {
		dirs = []string{filepath.Join(".config", "gooseneck")}
	}
	return dirs
}

// Path is the config file location.
func Path() string { return filepath.Join(Dir(), "config.toml") }

// CatalogCachePath is where a downloaded artists.json is cached.
func CatalogCachePath() string { return filepath.Join(Dir(), "artists.json") }

// DBPath is the SQLite cache + history database.
func DBPath() string { return filepath.Join(Dir(), "cache.db") }

// ThumbDir holds downloaded artist / album art.
func ThumbDir() string { return filepath.Join(Dir(), "thumbs") }

// DownloadDir is where `goose download` saves audio: download_dir when set,
// else $XDG_MUSIC_DIR/gooseneck (~/Music/gooseneck). With $GOOSENECK_HOME
// set it stays inside that dir, so demos and tests never touch real music.
func (c Config) Downloads() string {
	if c.DownloadDir != "" {
		if strings.HasPrefix(c.DownloadDir, "~/") {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, c.DownloadDir[2:])
		}
		return c.DownloadDir
	}
	if d := os.Getenv("GOOSENECK_HOME"); d != "" {
		return filepath.Join(d, "music")
	}
	music := os.Getenv("XDG_MUSIC_DIR")
	if music == "" {
		home, _ := os.UserHomeDir()
		music = filepath.Join(home, "Music")
	}
	return filepath.Join(music, "gooseneck")
}

// ScrobblesPath is the local scrobble database (separate from cache.db).
func (c Config) ScrobblesPath() string {
	if c.ScrobbleDB != "" {
		if strings.HasPrefix(c.ScrobbleDB, "~/") {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, c.ScrobbleDB[2:])
		}
		return c.ScrobbleDB
	}
	return filepath.Join(Dir(), "scrobbles.db")
}

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
	for _, p := range []**bool{&cfg.ShowHelp, &cfg.Caret, &cfg.Thumbnails, &cfg.Sidebar, &cfg.Scrobble, &cfg.LocalScrobble} {
		if *p == nil {
			*p = BoolPtr(true)
		}
	}
	if cfg.Lyrics != "sidebar" && cfg.Lyrics != "line" {
		cfg.Lyrics = "off"
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

// Update changes the config file on disk: it re-reads the file, applies fn and
// saves. Callers use it instead of saving their in-memory copy, which may hold
// session-only changes, $APP overrides or backfilled values that don't belong
// in the file. Only what fn touches changes.
func Update(fn func(*Config) error) error {
	cfg := Default()
	if _, err := os.Stat(Path()); err == nil {
		if _, err := toml.DecodeFile(Path(), &cfg); err != nil {
			return fmt.Errorf("%s: %w", Path(), err)
		}
	}
	if err := fn(&cfg); err != nil {
		return err
	}
	return Save(cfg)
}

// Save writes cfg to Path(), creating the directory. It overwrites the whole
// file; to change settings, prefer Update.
func Save(cfg Config) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	// 0600: the config can hold scrobbling credentials. Write to a temp file
	// and rename so a crash never leaves a half-written config.
	tmp := Path() + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
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
		f := t.Field(i)
		if structured(f.Type) {
			continue // [[catalog]] has its own commands; [keys] is edited by hand
		}
		keys = append(keys, f.Tag.Get("toml"))
	}
	sort.Strings(keys)
	return keys
}

// structured reports fields that don't fit `goose config get/set`: lists of
// tables and maps.
func structured(t reflect.Type) bool {
	return t.Kind() == reflect.Map || t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Struct
}

func field(cfg *Config, key string) (reflect.Value, error) {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Tag.Get("toml") == key && !structured(f.Type) {
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
	// Clamp / backfill only this key; leave the others exactly as they are.
	n := normalize(*c)
	nf, _ := field(&n, key)
	f.Set(nf)
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
