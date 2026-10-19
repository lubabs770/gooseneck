package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lubabs770/gooseneck/player/internal/bidi"
	"github.com/lubabs770/gooseneck/player/internal/catalog"
)

// entry is one selectable thing on screen: an artist, release, track, queue
// item or history row.
type entry struct {
	key     string
	title   string
	sub     string // grid: second line; list: middle column
	right   string // list: right column (duration, time)
	num     string // list: left gutter (track number)
	img     string // art URL
	active  bool   // currently playing
	offline bool   // downloaded
	data    any
}

// collection is the grid/list widget used by every page: cursor movement,
// fuzzy filtering, scrolling, and card/row rendering with art.
type collection struct {
	items  []entry
	vis    []int
	cursor int
	filter string

	grid     bool // cards (vs rows)
	canGrid  bool // whether this collection may show as cards
	images   bool // show art
	numbered bool // show the num gutter in list view
	empty    string

	top      int // first visible row
	cols     int // grid columns at last render
	pageRows int // rows visible at last render
}

func newCollection(canGrid, images bool, empty string) *collection {
	return &collection{canGrid: canGrid, grid: canGrid, images: images, empty: empty, cols: 1}
}

// set replaces the items, keeping the cursor on the same key when possible.
func (c *collection) set(items []entry) {
	cur, ok := c.selected()
	c.items = items
	c.apply()
	if ok {
		for i, vi := range c.vis {
			if c.items[vi].key == cur.key {
				c.cursor = i
				return
			}
		}
	}
}

func (c *collection) setFilter(f string) {
	if f == c.filter {
		return
	}
	c.filter = f
	c.cursor, c.top = 0, 0
	c.apply()
}

func (c *collection) apply() {
	c.vis = c.vis[:0]
	if strings.TrimSpace(c.filter) == "" {
		for i := range c.items {
			c.vis = append(c.vis, i)
		}
	} else {
		idx := make([]int, len(c.items))
		for i := range idx {
			idx[i] = i
		}
		c.vis = catalog.Rank(c.filter, idx, func(i int) string { return c.items[i].title })
	}
	c.cursor = clamp(c.cursor, 0, len(c.vis)-1)
}

func (c *collection) selected() (entry, bool) {
	if c.cursor < 0 || c.cursor >= len(c.vis) || c.vis[c.cursor] >= len(c.items) {
		return entry{}, false
	}
	return c.items[c.vis[c.cursor]], true
}

// from returns the visible entries from the cursor on (for "play from here").
func (c *collection) from() []entry {
	var out []entry
	for i := c.cursor; i >= 0 && i < len(c.vis); i++ {
		out = append(out, c.items[c.vis[i]])
	}
	return out
}

// visible returns every visible entry in order.
func (c *collection) visible() []entry {
	out := make([]entry, len(c.vis))
	for i, vi := range c.vis {
		out[i] = c.items[vi]
	}
	return out
}

func (c *collection) len() int { return len(c.vis) }

func (c *collection) step() int {
	if c.grid {
		return max(1, c.cols)
	}
	return 1
}

// key handles navigation keys; it reports whether the key was used.
func (c *collection) key(k string, listMode bool) bool {
	n := len(c.vis)
	page := max(1, c.pageRows) * c.step()
	switch k {
	case "down", "j", "ctrl+n":
		c.cursor = clamp(c.cursor+c.step(), 0, n-1)
	case "up", "k", "ctrl+k":
		c.cursor = clamp(c.cursor-c.step(), 0, n-1)
	case "right", "l":
		if !c.grid {
			return false
		}
		c.cursor = clamp(c.cursor+1, 0, n-1)
	case "left", "h":
		if !c.grid {
			return false
		}
		c.cursor = clamp(c.cursor-1, 0, n-1)
	case "g", "home":
		c.cursor = 0
	case "G", "end":
		c.cursor = n - 1
	case "ctrl+d":
		c.cursor = clamp(c.cursor+page/2, 0, n-1)
	case "ctrl+u":
		c.cursor = clamp(c.cursor-page/2, 0, n-1)
	case "pgdown", "ctrl+f":
		c.cursor = clamp(c.cursor+page, 0, n-1)
	case "pgup", "ctrl+b":
		c.cursor = clamp(c.cursor-page, 0, n-1)
	default:
		return false
	}
	return true
}

func (c *collection) scroll(delta int) {
	c.cursor = clamp(c.cursor+delta*c.step(), 0, len(c.vis)-1)
}

// geometry for grid cards at width w.
func (c *collection) gridGeom(a *App, w, h int) (cols, cardW, imgW, imgH int) {
	const gap, minCard = 2, 18
	cols = max(1, (w+gap)/(minCard+gap))
	cardW = (w - gap*(cols-1)) / cols
	imgW = cardW
	imgH = imgW / 2
	if th := a.lib.Cfg.ThumbHeight; th > 0 {
		imgH = th
	}
	if !c.images || !a.showImages {
		imgH = 0
	}
	imgH = clamp(imgH, 0, max(0, h-4))
	return
}

func (c *collection) view(a *App, w, h int) string {
	t := a.theme
	if len(c.vis) == 0 {
		msg := c.empty
		if c.filter != "" {
			msg = "nothing matches “" + c.filter + "”"
		}
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, t.muted().Render(msg))
	}
	if c.grid {
		return c.viewGrid(a, w, h)
	}
	return c.viewList(a, w, h)
}

func (c *collection) adjustTop(curRow, visibleRows, totalRows int) {
	if curRow < c.top {
		c.top = curRow
	}
	if curRow >= c.top+visibleRows {
		c.top = curRow - visibleRows + 1
	}
	c.top = clamp(c.top, 0, max(0, totalRows-visibleRows))
}

func (c *collection) viewGrid(a *App, w, h int) string {
	t := a.theme
	cols, cardW, imgW, imgH := c.gridGeom(a, w-2, h) // 2 columns for the scrollbar
	c.cols = cols
	cardH := imgH + 2
	rowH := cardH + 1
	visibleRows := max(1, (h+1)/rowH)
	c.pageRows = visibleRows
	totalRows := (len(c.vis) + cols - 1) / cols
	c.adjustTop(c.cursor/cols, visibleRows, totalRows)

	var rows []string
	for r := c.top; r < c.top+visibleRows && r < totalRows; r++ {
		var cards []string
		for col := 0; col < cols; col++ {
			i := r*cols + col
			if col > 0 {
				cards = append(cards, blankBlock(2, cardH))
			}
			if i >= len(c.vis) {
				cards = append(cards, blankBlock(cardW, cardH))
				continue
			}
			cards = append(cards, c.card(a, c.items[c.vis[i]], i == c.cursor, cardW, imgW, imgH))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cards...))
	}
	out := strings.Join(rows, "\n\n")
	if totalRows > visibleRows {
		out = withScrollbar(t, out, w, h, c.top, visibleRows, totalRows)
	}
	return out
}

func (c *collection) card(a *App, e entry, sel bool, cardW, imgW, imgH int) string {
	t := a.theme
	var parts []string
	if imgH > 0 {
		parts = append(parts, a.img.block(t, e.img, e.title, imgW, imgH, a.dialog != nil))
	}
	title := truncate(bidi.Vis(e.title), cardW-2)
	sub := truncate(bidi.Vis(e.sub), cardW-2)
	if sel {
		bar := fg(t.Primary).Background(t.Element).Render("┃ ")
		parts = append(parts,
			bar+fg(t.Text).Background(t.Element).Bold(true).Render(padRight(title, cardW-2)),
			bar+fg(t.Muted).Background(t.Element).Render(padRight(sub, cardW-2)))
	} else {
		ts := t.text()
		if e.active {
			ts = t.primary()
		}
		parts = append(parts, "  "+ts.Render(padRight(title, cardW-2)), "  "+t.muted().Render(padRight(sub, cardW-2)))
	}
	return strings.Join(parts, "\n")
}

func (c *collection) viewList(a *App, w, h int) string {
	t := a.theme
	c.cols = 1
	art := c.images && a.showImages
	rowH := 1
	if art {
		rowH = 2
	}
	visible := max(1, h/rowH)
	c.pageRows = visible
	c.adjustTop(c.cursor, visible, len(c.vis))

	numW := 0
	if c.numbered {
		for _, vi := range c.vis {
			numW = max(numW, lipgloss.Width(c.items[vi].num))
		}
	}
	rightW := 0
	dlCol := false // reserve the ↓ column when any visible row is downloaded
	for i := c.top; i < c.top+visible && i < len(c.vis); i++ {
		rightW = max(rightW, lipgloss.Width(c.items[c.vis[i]].right))
		dlCol = dlCol || c.items[c.vis[i]].offline
	}
	scroll := len(c.vis) > visible
	inner := w
	if scroll {
		inner = w - 2
	}

	var lines []string
	for i := c.top; i < c.top+visible && i < len(c.vis); i++ {
		e := c.items[c.vis[i]]
		sel := i == c.cursor
		bg := lipgloss.NewStyle()
		if sel {
			bg = bg.Background(t.Element)
		}
		bar := bg.Render("  ")
		if sel {
			bar = fg(t.Primary).Background(t.Element).Render("┃ ")
		}
		num := ""
		if numW > 0 {
			n := e.num
			st := fg(t.Subtle).Inherit(bg)
			if e.active {
				n, st = "♪", fg(t.Primary).Inherit(bg)
			}
			num = st.Render(strings.Repeat(" ", max(0, numW-lipgloss.Width(n)))+n) + bg.Render("  ")
		}
		left := lipgloss.Width(bar) + lipgloss.Width(num)
		img := ""
		if art {
			img = a.img.block(t, e.img, e.title, 4, 2, a.dialog != nil)
			left += 5
		}
		titleSt := fg(t.Text).Inherit(bg)
		if sel {
			titleSt = titleSt.Bold(true)
		}
		if e.active {
			titleSt = fg(t.Primary).Inherit(bg).Bold(true)
		}
		rightTxt := fg(t.Muted).Inherit(bg).Render(padLeft(e.right, rightW))
		if e.offline {
			rightTxt = fg(t.Success).Inherit(bg).Render("↓ ") + rightTxt
		} else if dlCol {
			rightTxt = bg.Render("  ") + rightTxt
		}
		avail := inner - left - rightW - 2
		if dlCol {
			avail -= 2
		}
		if art {
			// two lines: title / sub, art on the left
			l1 := titleSt.Render(padRight(bidi.Vis(e.title), avail)) + bg.Render("  ") + rightTxt
			l2 := fg(t.Muted).Inherit(bg).Render(padRight(bidi.Vis(e.sub), avail+2+rightW))
			text := lipgloss.JoinVertical(lipgloss.Left, l1, l2)
			gap := bg.Render(" ")
			block := lipgloss.JoinHorizontal(lipgloss.Top, bar+"\n"+bar, img, gap+"\n"+gap, text)
			lines = append(lines, block)
			continue
		}
		subW := 0
		if e.sub != "" {
			subW = min(avail/2, max(lipgloss.Width(e.sub), avail/3))
		}
		titleW := avail - subW
		if subW > 0 {
			titleW -= 2
		}
		row := bar + num + titleSt.Render(padRight(bidi.Vis(e.title), titleW))
		if subW > 0 {
			row += bg.Render("  ") + fg(t.Muted).Inherit(bg).Render(padRight(bidi.Vis(e.sub), subW))
		}
		row += bg.Render("  ") + rightTxt
		lines = append(lines, row)
	}
	out := strings.Join(lines, "\n")
	if scroll {
		out = withScrollbar(t, out, w, h, c.top, visible, len(c.vis))
	}
	return out
}

func padLeft(s string, w int) string {
	return strings.Repeat(" ", max(0, w-lipgloss.Width(s))) + s
}

// withScrollbar draws a thin scrollbar in the rightmost column.
func withScrollbar(t Theme, block string, w, h, top, visible, total int) string {
	lines := strings.Split(block, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	lines = lines[:h]
	thumb := max(1, h*visible/max(1, total))
	pos := 0
	if total > visible {
		pos = (h - thumb) * top / (total - visible)
	}
	for i := range lines {
		ch := t.subtle().Render("│")
		if i >= pos && i < pos+thumb {
			ch = t.muted().Render("┃")
		}
		lines[i] = padRight(lines[i], w-1) + ch
	}
	return strings.Join(lines, "\n")
}
