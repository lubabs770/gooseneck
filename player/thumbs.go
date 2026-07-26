package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif" // register decoders
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// subImager is implemented by the stdlib image types (YCbCr, RGBA, ...); it lets
// us crop without copying.
type subImager interface {
	SubImage(r image.Rectangle) image.Image
}

// cropToAspect returns img center-cropped to the cell box's pixel aspect
// (cols × rows*2), so it fills a card without stretching.
func cropToAspect(img image.Image, cols, rows int) image.Image {
	pw, ph := cols, rows*2
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw < 1 || sh < 1 || pw < 1 || ph < 1 {
		return img
	}
	cx0, cy0, cw, ch := b.Min.X, b.Min.Y, sw, sh
	if sw*ph > sh*pw { // wider than target: crop width
		cw = sh * pw / ph
		cx0 = b.Min.X + (sw-cw)/2
	} else { // taller than target: crop height
		ch = sw * ph / pw
		cy0 = b.Min.Y + (sh-ch)/2
	}
	if cw < 1 {
		cw = 1
	}
	if ch < 1 {
		ch = 1
	}
	r := image.Rect(cx0, cy0, cx0+cw, cy0+ch)
	if si, ok := img.(subImager); ok {
		return si.SubImage(r)
	}
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), img, r.Min, draw.Src)
	return dst
}

// croppedPNG center-crops img to the card aspect and PNG-encodes it, ready to
// transmit via the Kitty graphics protocol.
func croppedPNG(img image.Image, cols, rows int) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, cropToAspect(img, cols, rows)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// renderThumb resizes img (nearest-neighbor, no cgo deps) to cols×(rows*2)
// pixels and packs it into cols×rows terminal cells using the upper-half-block
// glyph: foreground = top pixel, background = bottom pixel. The result is a
// plain colored string that drops straight into a lipgloss card. The source is
// center-cropped to the card's aspect ratio first, so images fill the card
// without stretching.
func renderThumb(img image.Image, cols, rows int) string {
	if cols < 1 || rows < 1 {
		return ""
	}
	img = cropToAspect(img, cols, rows)
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

// thumbFetchPx is the square size requested from googleusercontent; small so the
// download is tiny, still plenty of detail to downsample into a card.
const thumbFetchPx = 288

// squareThumbURL rewrites a googleusercontent/ggpht image URL to request a
// square, center-cropped version. YouTube bakes sizing into the URL suffix
// after '=' (e.g. "=w2880-h1200-..."); forcing "=s{px}-c" yields a uniform
// square crop — avatar or cover art — whether the source is a square avatar or
// a wide channel banner. Non-Google URLs are returned unchanged.
func squareThumbURL(url string, px int) string {
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

// fetchThumb downloads a thumbnail image to dest (creating parent dirs),
// normalizing Google image URLs to a square crop first.
func fetchThumb(url, dest string) error {
	if url == "" {
		return fmt.Errorf("no thumbnail url")
	}
	url = squareThumbURL(url, thumbFetchPx)
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
