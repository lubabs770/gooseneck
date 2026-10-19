package library

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/mpv"
	"github.com/lubabs770/gooseneck/player/internal/store"
	"github.com/lubabs770/gooseneck/player/internal/ytm"
)

// DownloadEvent reports progress for one track of a download batch.
type DownloadEvent struct {
	Index   int      `json:"index"` // 0-based position in the batch
	Total   int      `json:"total"`
	Song    ytm.Song `json:"song"`
	Percent float64  `json:"percent"`         // 0..100 while downloading
	Done    bool     `json:"done"`            // finished (ok, skipped or failed)
	Skipped bool     `json:"skipped"`         // already on disk
	Path    string   `json:"path,omitempty"`  // final file when done
	Err     error    `json:"-"`               // failure when done
	Error   string   `json:"error,omitempty"` // Err as text, for JSON
}

// DownloadResult summarises a batch.
type DownloadResult struct {
	Downloaded, Skipped, Failed int
	Bytes                       int64
}

// LocalPath returns the downloaded file for a video, if it is still on disk.
func (l *Lib) LocalPath(videoID string) string {
	p := l.DB.DownloadPath(videoID)
	if p == "" {
		return ""
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// IsDownloaded reports which of the songs are on disk.
func (l *Lib) IsDownloaded(songs []ytm.Song) map[string]bool {
	out := map[string]bool{}
	for _, s := range songs {
		if l.LocalPath(s.VideoID) != "" {
			out[s.VideoID] = true
		}
	}
	return out
}

// trackPath is where a song is saved (without extension):
// <dir>/<Artist>/<Album>/<NN - >Title [videoId]. The id in brackets lets the
// player recognise local files as the same track.
func (l *Lib) trackPath(s ytm.Song) string {
	artist := s.Artist
	if i := strings.Index(artist, ","); i > 0 { // "A, B & C" -> first credited artist
		artist = artist[:i]
	}
	album := s.Album
	if album == "" {
		album = "Singles"
	}
	name := clean(s.Title)
	if s.Index > 0 && s.Album != "" {
		name = fmt.Sprintf("%02d - %s", s.Index, name)
	}
	return filepath.Join(l.Cfg.Downloads(), clean(orElse(artist, "Unknown Artist")), clean(album),
		name+" ["+s.VideoID+"]")
}

func orElse(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// clean makes a string safe as a single path component on every OS.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		}
		if r < 32 {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	s = strings.Trim(s, ". ")
	if len([]rune(s)) > 120 {
		s = string([]rune(s)[:120])
	}
	if s == "" {
		s = "untitled"
	}
	return s
}

// Download saves songs as audio files with yt-dlp, 3 at a time. Songs already
// on disk are skipped. fn (optional) receives progress; it may be called from
// several goroutines but never concurrently.
func (l *Lib) Download(ctx context.Context, songs []ytm.Song, fn func(DownloadEvent)) DownloadResult {
	songs = dedupe(songs)
	meta := make([]store.Track, len(songs))
	for i, s := range songs {
		meta[i] = TrackMeta(s)
	}
	_ = l.DB.PutTracks(meta)

	var mu sync.Mutex
	var res DownloadResult
	emit := func(ev DownloadEvent) {
		mu.Lock()
		defer mu.Unlock()
		if ev.Done {
			switch {
			case ev.Skipped:
				res.Skipped++
			case ev.Err != nil:
				res.Failed++
				ev.Error = ev.Err.Error()
			default:
				res.Downloaded++
				if st, err := os.Stat(ev.Path); err == nil {
					res.Bytes += st.Size()
				}
			}
		}
		if fn != nil {
			fn(ev)
		}
	}

	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for i, s := range songs {
		ev := DownloadEvent{Index: i, Total: len(songs), Song: s}
		if p := l.LocalPath(s.VideoID); p != "" {
			ev.Done, ev.Skipped, ev.Path, ev.Percent = true, true, p, 100
			emit(ev)
			continue
		}
		wg.Add(1)
		go func(ev DownloadEvent) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				ev.Done, ev.Err = true, ctx.Err()
				emit(ev)
				return
			}
			defer func() { <-sem }()
			var path string
			var err error
			for attempt := 0; attempt < 3; attempt++ { // YouTube 403s are often transient
				if attempt > 0 {
					select {
					case <-time.After(time.Duration(attempt) * 1500 * time.Millisecond):
					case <-ctx.Done():
					}
				}
				path, err = l.fetch(ctx, ev.Song, func(pct float64) {
					p := ev
					p.Percent = pct
					emit(p)
				})
				if err == nil || ctx.Err() != nil {
					break
				}
			}
			ev.Done, ev.Path, ev.Err, ev.Percent = true, path, err, 100
			if err == nil {
				st, _ := os.Stat(path)
				var size int64
				if st != nil {
					size = st.Size()
				}
				_ = l.DB.PutDownload(ev.Song.VideoID, path, size)
			}
			emit(ev)
		}(ev)
	}
	wg.Wait()
	return res
}

// fetch runs yt-dlp for one song and returns the saved file.
func (l *Lib) fetch(ctx context.Context, s ytm.Song, progress func(float64)) (string, error) {
	base := l.trackPath(s)
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		return "", err
	}
	args := []string{
		"-f", "bestaudio[ext=m4a]/bestaudio/best",
		"--no-playlist", "--newline", "--progress",
		"--progress-template", "download:GNPROG %(progress._percent_str)s",
		"--print", "after_move:filepath",
		"-o", base + ".%(ext)s",
	}
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		args = append(args, "--embed-metadata") // tags for other players; needs ffmpeg
	}
	args = append(args, mpv.VideoURL(s.VideoID))
	cmd := exec.CommandContext(ctx, l.Cfg.YtDlpPath(), args...)
	cmd.Env = l.Cfg.Env()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("yt-dlp: %w", err)
	}
	var path string
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if rest, ok := strings.CutPrefix(line, "GNPROG"); ok {
			if pct, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(rest), "%"), 64); err == nil {
				progress(pct)
			}
			continue
		}
		if line != "" {
			path = line
		}
	}
	if err := cmd.Wait(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndex(msg, "ERROR:"); i >= 0 {
			msg = strings.TrimSpace(msg[i+6:])
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	if path == "" {
		return "", errors.New("yt-dlp did not report a file")
	}
	return path, nil
}

// RemoveDownloads deletes the files for songs (and their empty folders).
func (l *Lib) RemoveDownloads(videoIDs []string) (int, error) {
	n := 0
	for _, id := range videoIDs {
		p := l.DB.DownloadPath(id)
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return n, err
		}
		_ = l.DB.RemoveDownload(id)
		n++
		// tidy up now-empty album/artist folders, never above the download root
		root := filepath.Clean(l.Cfg.Downloads())
		for dir := filepath.Dir(p); strings.HasPrefix(dir, root+string(filepath.Separator)); dir = filepath.Dir(dir) {
			if os.Remove(dir) != nil {
				break
			}
		}
	}
	return n, nil
}
