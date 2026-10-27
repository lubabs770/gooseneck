package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.Player != "mpv" || c.View != "grid" || c.ArtistsURL != DefaultArtistsURL {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	for name, p := range map[string]*bool{"ShowHelp": c.ShowHelp, "Caret": c.Caret, "Thumbnails": c.Thumbnails, "Sidebar": c.Sidebar} {
		if !On(p, false) {
			t.Errorf("%s should default on", name)
		}
	}
}

func TestLoadWritesDefaultsAndBackfills(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOOSENECK_HOME", dir)
	t.Setenv("APP", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path()); err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if c.Theme != "auto" {
		t.Fatalf("theme = %q", c.Theme)
	}

	// An old config missing newer keys gets them backfilled.
	if err := os.WriteFile(Path(), []byte("player = \"mpv\"\nview = \"bogus\"\nthumb_height = 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.ArtistsURL != DefaultArtistsURL || c.Sidebar == nil || c.View != "grid" || c.ThumbHeight != 14 {
		t.Fatalf("backfill failed: %+v", c)
	}
}

func TestAPPOverride(t *testing.T) {
	t.Setenv("GOOSENECK_HOME", t.TempDir())
	t.Setenv("APP", "vlc")
	c, _ := Load()
	if c.Player != "vlc" {
		t.Fatalf("Player = %q, want vlc", c.Player)
	}
}

func TestYtDlpPathAndEnv(t *testing.T) {
	if got := (Config{}).YtDlpPath(); !strings.HasPrefix(got, "yt-dlp") {
		t.Fatalf("YtDlpPath = %q", got)
	}
	if got := (Config{BinDir: "/opt/bin"}).YtDlpPath(); !strings.HasPrefix(got, "/opt/bin/yt-dlp") {
		t.Fatalf("YtDlpPath = %q", got)
	}
	t.Setenv("PATH", "/usr/bin")
	var path string
	for _, kv := range (Config{BinDir: "/opt/tools"}).Env() {
		if strings.HasPrefix(kv, "PATH=") {
			path = kv
		}
	}
	if path != "PATH=/opt/tools"+string(os.PathListSeparator)+"/usr/bin" {
		t.Fatalf("PATH = %q", path)
	}
}

func TestGetSet(t *testing.T) {
	c := Default()
	if err := c.Set("theme", "nord"); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("thumbnails", "false"); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("player_args", "--no-video --volume=50"); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("volume", "loud"); err == nil {
		t.Fatal("want error for non-numeric volume")
	}
	if err := c.Set("nope", "x"); err == nil {
		t.Fatal("want error for unknown key")
	}
	for key, want := range map[string]string{"theme": "nord", "thumbnails": "false", "player_args": "--no-video --volume=50"} {
		if got, _ := c.Get(key); got != want {
			t.Errorf("Get(%s) = %q, want %q", key, got, want)
		}
	}
	if len(Keys()) < 10 {
		t.Fatalf("Keys = %v", Keys())
	}
}

func TestSaveIsPrivateAndMasks(t *testing.T) {
	t.Setenv("GOOSENECK_HOME", t.TempDir())
	c := Default()
	c.ListenBrainzToken = "abcdef123456"
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(Path())
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("config perms %o, want 600", st.Mode().Perm())
	}
	if !Secret("listenbrainz_token") || !Secret("lastfm_session") || Secret("theme") {
		t.Fatal("Secret")
	}
	if Mask("abcdef123456") != "••••3456" || Mask("") != "" {
		t.Fatal("Mask")
	}
}

func TestCatalogSourcesRoundTrip(t *testing.T) {
	t.Setenv("GOOSENECK_HOME", t.TempDir())
	t.Setenv("APP", "")
	c := Default()
	c.Catalogs = []CatalogSource{{Name: "mine", Path: "/x/mine.json"}, {Name: "friend", URL: "https://example.com/a.json"}}
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || len(got.Catalogs) != 2 || got.Catalogs[1].URL != "https://example.com/a.json" {
		t.Fatalf("Load = %+v, %v", got.Catalogs, err)
	}
	if got.Catalogs[0].File() != "/x/mine.json" || got.Catalogs[1].File() != filepath.Join(Dir(), "catalogs", "friend.json") {
		t.Fatal("File()")
	}
	for _, k := range Keys() {
		if k == "catalog" {
			t.Fatal("catalog must not be a plain config key")
		}
	}
	if err := c.Set("catalog", "x"); err == nil {
		t.Fatal("setting catalog directly should fail")
	}
}

// Update must change only the given key: not write back $APP, backfilled
// defaults, or anything else the running process holds in memory.
func TestUpdateChangesOnlyOneKey(t *testing.T) {
	t.Setenv("GOOSENECK_HOME", t.TempDir())
	t.Setenv("APP", "vlc")
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := "theme = \"gruvbvox\"\nshow_help = false\nview = \"bogus\"\n"
	if err := os.WriteFile(Path(), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	mem, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	mem.ShowHelp = BoolPtr(true) // a session-only change

	if err := Update(func(c *Config) error { return c.Set("volume", "55") }); err != nil {
		t.Fatal(err)
	}
	var disk Config
	if _, err := toml.DecodeFile(Path(), &disk); err != nil {
		t.Fatal(err)
	}
	if disk.Volume != 55 {
		t.Errorf("volume = %d, want 55", disk.Volume)
	}
	if disk.Player == "vlc" {
		t.Error("$APP override was written to the file")
	}
	if On(disk.ShowHelp, true) {
		t.Error("show_help changed on disk")
	}
	if disk.Theme != "gruvbvox" || disk.View != "bogus" {
		t.Errorf("other keys rewritten: theme=%q view=%q", disk.Theme, disk.View)
	}
}

func TestKeyMapRoundTrip(t *testing.T) {
	t.Setenv("GOOSENECK_HOME", t.TempDir())
	cfg := Default()
	cfg.KeyMap = map[string]string{"j": "k", "ctrl+j": "ctrl+k"}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.KeyMap["j"] != "k" || got.KeyMap["ctrl+j"] != "ctrl+k" {
		t.Fatalf("KeyMap = %v", got.KeyMap)
	}
	for _, k := range Keys() {
		if k == "keys" || k == "keys,omitempty" {
			t.Fatal("[keys] must not be a `goose config set` key")
		}
	}
}

func TestDirPrefersXDGThenExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOOSENECK_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	want := filepath.Join(home, ".config", "gooseneck")
	if got := Dir(); got != want {
		t.Fatalf("fresh install: Dir() = %q, want %q", got, want)
	}
	xdg := filepath.Join(home, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if got := Dir(); got != filepath.Join(xdg, "gooseneck") {
		t.Fatalf("XDG set, nothing exists: Dir() = %q", got)
	}
	if err := os.MkdirAll(want, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := Dir(); got != want {
		t.Fatalf("existing ~/.config dir: Dir() = %q, want %q", got, want)
	}
}
