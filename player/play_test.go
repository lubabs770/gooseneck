package main

import (
	"os"
	"strings"
	"testing"
)

func TestVideoURL(t *testing.T) {
	cases := []struct{ id, want string }{
		{"dQw4w9WgXcQ", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"", "https://www.youtube.com/watch?v="}, // empty id still builds a URL
	}
	for _, c := range cases {
		if got := videoURL(c.id); got != c.want {
			t.Errorf("videoURL(%q) = %q, want %q", c.id, got, c.want)
		}
	}
}

func TestPlaybackEnv(t *testing.T) {
	sep := string(os.PathListSeparator)

	// Empty BinDir: environment returned unchanged.
	base := playbackEnv(Config{})
	if len(base) != len(os.Environ()) {
		t.Errorf("empty BinDir changed env length: got %d, want %d", len(base), len(os.Environ()))
	}

	// BinDir set: prepended to the existing PATH entry.
	t.Setenv("PATH", "/usr/bin"+sep+"/bin")
	env := playbackEnv(Config{BinDir: "/opt/tools"})
	var path string
	var pathCount int
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			path = kv[len("PATH="):]
			pathCount++
		}
	}
	if pathCount != 1 {
		t.Fatalf("expected exactly one PATH entry, got %d", pathCount)
	}
	want := "/opt/tools" + sep + "/usr/bin" + sep + "/bin"
	if path != want {
		t.Errorf("PATH = %q, want %q", path, want)
	}
}

func TestPlaybackEnvNoPath(t *testing.T) {
	// No PATH in environment: BinDir appended as a new entry.
	orig, had := os.LookupEnv("PATH")
	os.Unsetenv("PATH")
	t.Cleanup(func() {
		if had {
			os.Setenv("PATH", orig)
		} else {
			os.Unsetenv("PATH")
		}
	})
	env := playbackEnv(Config{BinDir: "/opt/tools"})
	found := false
	for _, kv := range env {
		if kv == "PATH=/opt/tools" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected appended PATH=/opt/tools, env = %v", env)
	}
}
