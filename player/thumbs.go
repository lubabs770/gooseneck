package main

import (
	"fmt"
	"image"
	_ "image/gif"  // register decoders
	_ "image/jpeg" // YouTube thumbnails are usually JPEG
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// renderThumb resizes img (nearest-neighbor, no cgo deps) to cols×(rows*2)
// pixels and packs it into cols×rows terminal cells using the upper-half-block
// glyph: foreground = top pixel, background = bottom pixel. The result is a
// plain colored string that drops straight into a lipgloss card.
func renderThumb(img image.Image, cols, rows int) string {
	if cols < 1 || rows < 1 {
		return ""
	}
	pw, ph := cols, rows*2
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw < 1 || sh < 1 {
		return ""
	}
	sample := func(px, py int) (uint8, uint8, uint8) {
		sx := b.Min.X + px*sw/pw
		sy := b.Min.Y + py*sh/ph
		r, g, bl, _ := img.At(sx, sy).RGBA()
		return uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8)
	}

	var out strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			tr, tg, tb := sample(x, 2*y)
			br, bg, bb := sample(x, 2*y+1)
			fg := lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", tr, tg, tb))
			bgc := lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", br, bg, bb))
			out.WriteString(lipgloss.NewStyle().Foreground(fg).Background(bgc).Render("▀"))
		}
		if y < rows-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// loadThumbImage returns the decoded thumbnail for url, downloading it to dest
// (the disk cache) on first use.
func loadThumbImage(url, dest string) (image.Image, error) {
	if _, err := os.Stat(dest); err != nil {
		if err := fetchThumb(url, dest); err != nil {
			return nil, err
		}
	}
	f, err := os.Open(dest)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// fetchThumb downloads a thumbnail image to dest (creating parent dirs).
func fetchThumb(url, dest string) error {
	if url == "" {
		return fmt.Errorf("no thumbnail url")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("thumbnail %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8 MiB cap
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, body, 0o644)
}
