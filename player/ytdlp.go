package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// flatEntry is one entry from `yt-dlp --flat-playlist -J`.
type flatEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// _type is "url" for videos, "playlist" for nested playlists.
	Type string `json:"_type"`
}

type flatDump struct {
	Entries []flatEntry `json:"entries"`
}

// fetchAlbums enumerates an artist channel's "releases" tab (each release is an
// album playlist). Falls back to nothing on error.
func fetchAlbums(cfg Config, artistID string) ([]Album, error) {
	url := fmt.Sprintf("https://www.youtube.com/channel/%s/releases", artistID)
	dump, err := flatPlaylist(cfg, url)
	if err != nil {
		return nil, err
	}
	var out []Album
	for _, e := range dump.Entries {
		if e.ID == "" {
			continue
		}
		out = append(out, Album{PlaylistID: e.ID, Title: e.Title})
	}
	return out, nil
}

// fetchTracks lists the videos in an album playlist.
func fetchTracks(cfg Config, playlistID string) ([]Track, error) {
	url := fmt.Sprintf("https://www.youtube.com/playlist?list=%s", playlistID)
	return tracksFromURL(cfg, url)
}

// fetchArtistTracks enumerates an artist's uploads. These are YouTube Music
// "- Topic" channels whose bare URL resolves directly to the uploads playlist,
// so we skip the (usually absent) releases/videos tabs.
func fetchArtistTracks(cfg Config, channelID string) ([]Track, error) {
	url := "https://www.youtube.com/channel/" + channelID
	return tracksFromURL(cfg, url)
}

func tracksFromURL(cfg Config, url string) ([]Track, error) {
	dump, err := flatPlaylist(cfg, url)
	if err != nil {
		return nil, err
	}
	var out []Track
	for _, e := range dump.Entries {
		if e.ID == "" {
			continue
		}
		out = append(out, Track{VideoID: e.ID, Title: e.Title})
	}
	return out, nil
}

func flatPlaylist(cfg Config, url string) (flatDump, error) {
	var dump flatDump
	cmd := exec.Command(cfg.ytDlpPath(),
		"--flat-playlist", "--ignore-errors", "-J", url)
	cmd.Env = playbackEnv(cfg)
	out, err := cmd.Output()
	if err != nil {
		return dump, fmt.Errorf("yt-dlp: %w", err)
	}
	if err := json.Unmarshal(out, &dump); err != nil {
		return dump, fmt.Errorf("parse yt-dlp json: %w", err)
	}
	return dump, nil
}
