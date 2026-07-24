package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// albumKey builds the synthetic cache key for a grouped album. These are not real
// YouTube playlists, so they're keyed by artist id + album name.
func albumKey(artistID, album string) string { return artistID + "::" + album }

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

// fullEntry is one fully-extracted video from `yt-dlp -J` (no --flat-playlist),
// which unlike the flat form includes album metadata.
type fullEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Album string `json:"album"`
}

type fullDump struct {
	Entries []fullEntry `json:"entries"`
}

// fetchArtistCatalog fully extracts an artist's uploads and groups them into
// albums by the per-track `album` field. Tracks whose album is empty or equals
// their own title (i.e. singles) are collected into one "Singles" bucket so the
// album list stays meaningful. Returns the ordered albums plus a map of album
// key -> its tracks. This is the slow path (network per video); callers cache it.
func fetchArtistCatalog(cfg Config, channelID string) ([]Album, map[string][]Track, error) {
	url := "https://www.youtube.com/channel/" + channelID
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, cfg.ytDlpPath(), "--ignore-errors", "-J", url)
	cmd.Env = playbackEnv(cfg)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("yt-dlp: %w", err)
	}
	var dump fullDump
	if err := json.Unmarshal(out, &dump); err != nil {
		return nil, nil, fmt.Errorf("parse yt-dlp json: %w", err)
	}
	albums, tracksByKey := groupCatalog(channelID, dump.Entries)
	return albums, tracksByKey, nil
}

// groupCatalog groups fully-extracted entries into albums by their `album` field,
// preserving first-seen order. Singles (album empty or == title) collapse into a
// single "Singles" bucket.
func groupCatalog(channelID string, entries []fullEntry) ([]Album, map[string][]Track) {
	var order []string
	grouped := map[string][]Track{}
	for _, e := range entries {
		if e.ID == "" {
			continue
		}
		alb := strings.TrimSpace(e.Album)
		if alb == "" {
			alb = e.Title // treated as a single below
		}
		if _, seen := grouped[alb]; !seen {
			order = append(order, alb)
		}
		grouped[alb] = append(grouped[alb], Track{VideoID: e.ID, Title: e.Title})
	}

	var albums []Album
	tracksByKey := map[string][]Track{}
	var singles []Track
	for _, alb := range order {
		ts := grouped[alb]
		if len(ts) == 1 && strings.EqualFold(strings.TrimSpace(ts[0].Title), alb) {
			singles = append(singles, ts...)
			continue
		}
		key := albumKey(channelID, alb)
		albums = append(albums, Album{PlaylistID: key, Title: alb})
		tracksByKey[key] = ts
	}
	if len(singles) > 0 {
		key := albumKey(channelID, "Singles")
		albums = append(albums, Album{PlaylistID: key, Title: fmt.Sprintf("Singles (%d)", len(singles))})
		tracksByKey[key] = singles
	}
	return albums, tracksByKey
}

// flatCount returns how many tracks an artist has, via the cheap flat listing.
// Used to size the indexing progress bar; 0 means unknown.
func flatCount(cfg Config, channelID string) int {
	dump, err := flatPlaylist(cfg, "https://www.youtube.com/channel/"+channelID)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range dump.Entries {
		if e.ID != "" {
			n++
		}
	}
	return n
}

// streamArtistEntries runs `yt-dlp -j` (one JSON object per line, emitted as each
// track is extracted) and calls emit for every entry, enabling live progress.
func streamArtistEntries(cfg Config, channelID string, emit func(fullEntry)) error {
	url := "https://www.youtube.com/channel/" + channelID
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, cfg.ytDlpPath(), "--ignore-errors", "-j", url)
	cmd.Env = playbackEnv(cfg)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 16<<20) // entries can exceed the 64K default
	for sc.Scan() {
		var e fullEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.ID != "" {
			emit(e)
		}
	}
	return cmd.Wait()
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
