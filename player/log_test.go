package main

import (
	"os"
	"testing"
)

func TestLogPlayAndTopArtists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	logPlay(playEvent{ArtistID: "A", Artist: "Avi", Album: "One", Count: 3})
	logPlay(playEvent{ArtistID: "A", Artist: "Avi", Track: "song", VideoID: "v1", Count: 1})
	logPlay(playEvent{ArtistID: "B", Artist: "Ben", Count: 5})

	if _, err := os.Stat(logsPath()); err != nil {
		t.Fatalf("logs.json not written: %v", err)
	}
	if got := readLog(); len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if readLog()[0].Time == "" {
		t.Fatal("event missing timestamp")
	}

	top := topArtists()
	if len(top) != 2 {
		t.Fatalf("expected 2 artists, got %d", len(top))
	}
	// A has 2 play events, B has 1 -> A ranks first by play frequency.
	if top[0].ArtistID != "A" || top[0].Plays != 2 || top[0].Tracks != 4 {
		t.Fatalf("unexpected top artist: %+v", top[0])
	}
	if top[1].ArtistID != "B" || top[1].Plays != 1 || top[1].Tracks != 5 {
		t.Fatalf("unexpected second artist: %+v", top[1])
	}
}
