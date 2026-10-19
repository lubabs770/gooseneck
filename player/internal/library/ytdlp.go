package library

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/ytm"
)

// uploadsPrefix marks the synthetic "All uploads" album used when a channel has
// no YouTube Music artist page.
const uploadsPrefix = "uploads:"

// uploadsArtist builds a minimal artist page around the channel's uploads.
func (l *Lib) uploadsArtist(channelID string) (*ytm.Artist, error) {
	al, err := l.uploadsAlbum(channelID)
	if err != nil {
		return nil, err
	}
	name := channelID
	if c, ok := l.Cat.Get(channelID); ok {
		name = c.Name
	}
	return &ytm.Artist{
		ID:   channelID,
		Name: name,
		Albums: []ytm.Release{{
			BrowseID: uploadsPrefix + channelID,
			Title:    "All uploads",
			Kind:     "Uploads",
		}},
		TopSongs: al.Tracks,
	}, nil
}

// uploadsAlbum lists a channel's uploads with yt-dlp's fast flat listing.
func (l *Lib) uploadsAlbum(channelID string) (*ytm.Album, error) {
	key := "uploads:" + channelID
	var cached ytm.Album
	if found, stale := l.DB.GetJSON(key, artistTTL, &cached); found && !stale {
		return &cached, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, l.Cfg.YtDlpPath(), "--flat-playlist", "--ignore-errors", "-J",
		"https://www.youtube.com/channel/"+channelID)
	cmd.Env = l.Cfg.Env()
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("yt-dlp: %w", err)
	}
	var dump struct {
		Title   string `json:"title"`
		Entries []struct {
			ID       string  `json:"id"`
			Title    string  `json:"title"`
			Duration float64 `json:"duration"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(out, &dump); err != nil {
		return nil, fmt.Errorf("yt-dlp output: %w", err)
	}
	name := strings.TrimSuffix(strings.TrimPrefix(dump.Title, "Uploads from "), " - Topic")
	if c, ok := l.Cat.Get(channelID); ok {
		name = c.Name
	}
	al := &ytm.Album{BrowseID: uploadsPrefix + channelID, Title: "All uploads", Kind: "Uploads",
		Artist: name, ArtistID: channelID}
	for _, e := range dump.Entries {
		if e.ID == "" {
			continue
		}
		al.Tracks = append(al.Tracks, ytm.Song{VideoID: e.ID, Title: e.Title, Artist: name,
			ArtistID: channelID, Duration: int(e.Duration), Index: len(al.Tracks) + 1})
	}
	if len(al.Tracks) == 0 {
		return nil, fmt.Errorf("no uploads found for %s", name)
	}
	_ = l.DB.PutJSON(key, al)
	return al, nil
}

// YtDlpVersion reports the installed yt-dlp version ("" if missing).
func (l *Lib) YtDlpVersion() string {
	cmd := exec.Command(l.Cfg.YtDlpPath(), "--version")
	cmd.Env = l.Cfg.Env()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func shuffled(in []ytm.Song) []ytm.Song {
	out := append([]ytm.Song{}, in...)
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}
