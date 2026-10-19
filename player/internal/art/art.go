// Package art downloads and disk-caches artist pictures and album covers.
// The TUI renders them; `goose cache warm` prefetches them.
package art

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif" // decoders
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/config"
)

// FetchPx is the square size requested from Google's image CDN: small enough
// to download fast, plenty for a terminal card.
const FetchPx = 320

// SquareURL rewrites a googleusercontent/ggpht URL to request a square,
// center-cropped version (YouTube bakes sizing into the suffix after '='), so
// square avatars and wide channel banners both fill a card. Other URLs are
// returned unchanged.
func SquareURL(url string, px int) string {
	i := strings.LastIndexByte(url, '=')
	if i < 0 {
		return url
	}
	base := url[:i]
	if !strings.Contains(base, "googleusercontent.com") && !strings.Contains(base, "ggpht.com") {
		return url
	}
	return fmt.Sprintf("%s=s%d-c", base, px)
}

// Path is where url's image is cached.
func Path(url string) string {
	sum := sha1.Sum([]byte(SquareURL(url, FetchPx)))
	return filepath.Join(config.ThumbDir(), hex.EncodeToString(sum[:10])+".img")
}

// Cached reports whether url is already on disk.
func Cached(url string) bool {
	_, err := os.Stat(Path(url))
	return err == nil
}

// Load returns the decoded image for url, downloading it on first use.
func Load(url string) (image.Image, error) {
	path := Path(url)
	if _, err := os.Stat(path); err != nil {
		if err := download(SquareURL(url, FetchPx), path); err != nil {
			return nil, err
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		_ = os.Remove(path) // corrupt cache entry; refetch next time
	}
	return img, err
}

var client = &http.Client{Timeout: 15 * time.Second}

func download(url, dest string) error {
	if url == "" {
		return fmt.Errorf("no image url")
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("image: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// Warm downloads every url that isn't cached yet, 8 at a time, calling
// progress after each one. It returns how many were fetched and failed.
func Warm(urls []string, progress func(done, total int)) (fetched, failed int) {
	var todo []string
	seen := map[string]bool{}
	for _, u := range urls {
		if u != "" && !seen[u] && !Cached(u) {
			seen[u] = true
			todo = append(todo, u)
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	done := 0
	for _, u := range todo {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			err := download(SquareURL(u, FetchPx), Path(u))
			mu.Lock()
			defer mu.Unlock()
			done++
			if err != nil {
				failed++
			} else {
				fetched++
			}
			if progress != nil {
				progress(done, len(todo))
			}
		}(u)
	}
	wg.Wait()
	return fetched, failed
}
