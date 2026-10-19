package config

import (
	"os"
	"strings"
	"testing"
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
