package tui

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lubabs770/gooseneck/player/internal/art"
)

// images prepares artist/album art for display at a given cell size. Pages ask
// for an image while rendering (block); misses are queued and fetched off the
// UI goroutine on the next update (requests), and arrive as imageReady.
type images struct {
	kitty   bool
	enabled bool

	ready    map[string]*prepared // key -> prepared image (nil value = failed)
	inflight map[string]bool
	wanted   map[string]imgReq // requested during the last render, not yet fetched

	seq uint32
	out io.Writer // kitty uploads go straight to the terminal (see syncWriter)
}

type prepared struct {
	id   uint32 // kitty image id
	art  string // half-block render
	w, h int
}

type imgReq struct {
	key, url string
	w, h     int
}

type imageReady struct {
	key  string
	png  []byte
	art  string
	w, h int
	err  error
}

func newImages(enabled bool, out io.Writer) *images {
	return &images{
		out:      out,
		kitty:    KittyCapable() && out != nil,
		enabled:  enabled && os.Getenv("GOOSENECK_IMAGES") != "off",
		ready:    map[string]*prepared{},
		inflight: map[string]bool{},
		wanted:   map[string]imgReq{},
	}
}

func imgKey(url string, w, h int) string { return fmt.Sprintf("%s|%dx%d", url, w, h) }

// block returns the image as a w×h cell block, or a placeholder tile while it
// loads (or when it failed / images are off).
func (m *images) block(t Theme, url, label string, w, h int, hide bool) string {
	if w < 1 || h < 1 {
		return ""
	}
	if !m.enabled || url == "" || hide {
		return tile(t, label, w, h)
	}
	key := imgKey(url, w, h)
	p, ok := m.ready[key]
	if !ok {
		if !m.inflight[key] {
			m.wanted[key] = imgReq{key, url, w, h}
		}
		return tile(t, label, w, h)
	}
	if p == nil {
		return tile(t, label, w, h)
	}
	if m.kitty {
		return placeholderBlock(p.id, w, h)
	}
	return p.art
}

// tile is the stand-in for a missing image: a panel-colored square with the
// item's initials, so every card keeps its shape.
func tile(t Theme, label string, w, h int) string {
	st := lipgloss.NewStyle().Background(t.Element).Foreground(t.Subtle).Bold(true)
	ini := initials(label)
	lines := make([]string, h)
	for i := range lines {
		txt := ""
		if i == h/2 {
			txt = ini
		}
		pad := max(0, w-lipgloss.Width(txt))
		lines[i] = st.Render(strings.Repeat(" ", pad/2) + txt + strings.Repeat(" ", pad-pad/2))
	}
	return strings.Join(lines, "\n")
}

func initials(s string) string {
	var out []rune
	for _, f := range strings.Fields(s) {
		r := []rune(f)
		if len(r) > 0 && (r[0] >= 'A' && r[0] <= 'Z' || r[0] >= 'a' && r[0] <= 'z' || r[0] >= '0' && r[0] <= '9') {
			out = append(out, []rune(strings.ToUpper(string(r[0])))...)
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "♪"
	}
	return string(out)
}

// requests turns the images wanted by the last render into fetch commands
// (at most 8 in flight).
func (m *images) requests() tea.Cmd {
	var cmds []tea.Cmd
	for key, r := range m.wanted {
		if len(m.inflight) >= 8 {
			break
		}
		delete(m.wanted, key)
		if _, done := m.ready[key]; done {
			continue
		}
		m.inflight[key] = true
		cmds = append(cmds, fetchImage(r, m.kitty))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *images) pending() bool { return len(m.wanted) > 0 || len(m.inflight) > 0 }

// store records a fetched image. For Kitty the upload is written to the
// terminal right away, between frames: Bubble Tea drops intermediate frames, so
// uploads embedded in a frame could be lost, and the placeholders that
// reference the image only render after this returns.
func (m *images) store(r imageReady) {
	delete(m.inflight, r.key)
	if r.err != nil {
		m.ready[r.key] = nil
		return
	}
	p := &prepared{art: r.art, w: r.w, h: r.h}
	if m.kitty {
		m.seq++
		p.id = m.seq
		if _, err := io.WriteString(m.out, kittyTransmit(p.id, r.png)+kittyPlacement(p.id, r.w, r.h)); err != nil {
			m.ready[r.key] = nil
			return
		}
	}
	m.ready[r.key] = p
}

// reset forgets prepared images (e.g. after toggling images off and on).
func (m *images) clear() {
	m.ready = map[string]*prepared{}
	m.wanted = map[string]imgReq{}
}

func fetchImage(r imgReq, kitty bool) tea.Cmd {
	return func() tea.Msg {
		img, err := art.Load(r.url)
		if err != nil {
			return imageReady{key: r.key, err: err}
		}
		if kitty {
			var buf bytes.Buffer
			if err := png.Encode(&buf, cropToAspect(img, r.w, r.h)); err != nil {
				return imageReady{key: r.key, err: err}
			}
			return imageReady{key: r.key, png: buf.Bytes(), w: r.w, h: r.h}
		}
		return imageReady{key: r.key, art: halfBlock(img, r.w, r.h), w: r.w, h: r.h}
	}
}

// cropToAspect center-crops img to the pixel aspect of a cols×rows cell box
// (cells are ~1:2), so it fills the box without stretching.
func cropToAspect(img image.Image, cols, rows int) image.Image {
	pw, ph := cols, rows*2
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw < 1 || sh < 1 || pw < 1 || ph < 1 {
		return img
	}
	cx, cy, cw, ch := b.Min.X, b.Min.Y, sw, sh
	if sw*ph > sh*pw {
		cw = max(1, sh*pw/ph)
		cx = b.Min.X + (sw-cw)/2
	} else {
		ch = max(1, sw*ph/pw)
		cy = b.Min.Y + (sh-ch)/2
	}
	r := image.Rect(cx, cy, cx+cw, cy+ch)
	if si, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return si.SubImage(r)
	}
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), img, r.Min, draw.Src)
	return dst
}

// halfBlock renders img into cols×rows cells with the upper-half-block glyph:
// foreground = top pixel, background = bottom pixel. Each cell averages the
// source pixels it covers (box filter), which looks far better than
// nearest-neighbour at these tiny sizes.
func halfBlock(img image.Image, cols, rows int) string {
	if cols < 1 || rows < 1 {
		return ""
	}
	img = cropToAspect(img, cols, rows)
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	pw, ph := cols, rows*2
	avg := func(px, py int) (uint8, uint8, uint8) {
		x0, x1 := b.Min.X+px*sw/pw, b.Min.X+(px+1)*sw/pw
		y0, y1 := b.Min.Y+py*sh/ph, b.Min.Y+(py+1)*sh/ph
		if x1 <= x0 {
			x1 = x0 + 1
		}
		if y1 <= y0 {
			y1 = y0 + 1
		}
		var r, g, bl, n uint64
		stepX, stepY := max(1, (x1-x0)/4), max(1, (y1-y0)/4)
		for y := y0; y < y1; y += stepY {
			for x := x0; x < x1; x += stepX {
				cr, cg, cb, _ := img.At(x, y).RGBA()
				r, g, bl, n = r+uint64(cr>>8), g+uint64(cg>>8), bl+uint64(cb>>8), n+1
			}
		}
		return uint8(r / n), uint8(g / n), uint8(bl / n)
	}
	var out strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			tr, tg, tb := avg(x, 2*y)
			br, bg, bb := avg(x, 2*y+1)
			fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", tr, tg, tb, br, bg, bb)
		}
		out.WriteString(reset)
		if y < rows-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
