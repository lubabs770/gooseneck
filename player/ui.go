package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

type screenKind int

const (
	artistsScreen screenKind = iota
	albumsScreen
	tracksScreen
)

type item struct {
	title    string
	subtitle string
	id       string // artist id | playlist id | video id
	thumb    string // artist thumbnail URL (artists screen only)
}

// level is one screen on the navigation stack.
type level struct {
	kind       screenKind
	title      string
	items      []item
	vis        []int // indices into items after filtering
	cursor     int   // index into vis
	artistID   string
	artistName string
	playlistID string
}

type model struct {
	cfg   *Config
	cache *Cache

	themeName  string
	st         styles
	view       string // "grid" | "list"
	showHelp   bool   // runtime toggle, seeded from cfg.ShowHelp
	caret      bool   // show a ›caret on the selection, seeded from cfg.Caret
	showThumbs bool   // render profile pics, seeded from cfg.Thumbnails

	width, height int

	// profile-pic (thumbnail) state, keyed by artist id for the current geometry
	thumbs       map[string]string // id -> rendered half-block art ("" = tried, none)
	thumbLoading map[string]bool   // id -> fetch in flight
	thumbW       int               // cell width the cached art was rendered at
	thumbHCache  int               // cell height (rows) the cached art was rendered at

	stack   []level
	typing  bool
	filter  string
	status  string
	loading bool

	// album indexing progress
	indexing bool
	idxDone  int
	idxTotal int
	idxCh    chan tea.Msg
}

func newModel(cfg Config, cache *Cache, artists []Artist) model {
	root := level{kind: artistsScreen, title: "Artists"}
	for _, a := range artists {
		root.items = append(root.items, item{title: a.Name, subtitle: a.ID, id: a.ID, thumb: a.Thumbnail})
	}
	m := model{
		cfg:          &cfg,
		cache:        cache,
		themeName:    cfg.Theme,
		st:           buildStyles(getTheme(cfg.Theme)),
		view:         normView(cfg.View),
		showHelp:     cfg.ShowHelp == nil || *cfg.ShowHelp,
		caret:        cfg.Caret == nil || *cfg.Caret,
		showThumbs:   cfg.Thumbnails == nil || *cfg.Thumbnails,
		stack:        []level{root},
		thumbs:       map[string]string{},
		thumbLoading: map[string]bool{},
	}
	m.recompute()
	return m
}

func normView(v string) string {
	if v == "list" {
		return "list"
	}
	return "grid"
}

func (m *model) cur() *level { return &m.stack[len(m.stack)-1] }

func (m model) Init() tea.Cmd { return nil }

// ---- async messages -------------------------------------------------------

type albumsLoaded struct {
	artistID, artistName string
	albums               []Album
	err                  error
}

type tracksLoaded struct {
	playlistID, title string
	tracks            []Track
	err               error
	playNow           bool // play immediately instead of pushing a screen
}

// loadAlbumsCmd builds an artist's album list by fully extracting their uploads
// and grouping by the `album` field (slow first time, then served from cache).
// Album tracks are cached alongside so drilling into an album is instant.
func (m *model) loadAlbumsCmd(a item) tea.Cmd {
	cfg, cache := *m.cfg, m.cache
	return func() tea.Msg {
		if al, ok := cache.Albums(a.id); ok {
			return albumsLoaded{a.id, a.title, al, nil}
		}
		albums, tracksByKey, err := fetchArtistCatalog(cfg, a.id)
		if err == nil {
			cache.PutAlbums(a.id, albums)
			for _, alb := range albums {
				cache.PutTracks(alb.PlaylistID, tracksByKey[alb.PlaylistID])
			}
		}
		return albumsLoaded{a.id, a.title, albums, err}
	}
}

// indexProgress reports how many tracks have been extracted so far while
// building an artist's album list. total 0 means the count is unknown.
type indexProgress struct{ done, total int }

// waitMsg blocks on the index channel, delivering the next progress/final msg.
func waitMsg(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// startIndexCmd streams the artist's uploads via yt-dlp, emitting live progress
// on m.idxCh, then groups+caches the albums and sends the final albumsLoaded.
// Assumes the artist is not already cached (caller checks).
func (m *model) startIndexCmd(a item) tea.Cmd {
	ch := make(chan tea.Msg, 64)
	m.idxCh = ch
	m.indexing = true
	m.idxDone, m.idxTotal = 0, 0
	cfg, cache := *m.cfg, m.cache
	go func() {
		total := flatCount(cfg, a.id)
		ch <- indexProgress{0, total}
		var entries []fullEntry
		err := streamArtistEntries(cfg, a.id, func(e fullEntry) {
			entries = append(entries, e)
			select { // drop intermediate ticks if the UI is behind
			case ch <- indexProgress{len(entries), total}:
			default:
			}
		})
		if err != nil && len(entries) == 0 {
			ch <- albumsLoaded{a.id, a.title, nil, err}
			return
		}
		albums, tracksByKey := groupCatalog(a.id, entries)
		cache.PutAlbums(a.id, albums)
		for _, alb := range albums {
			cache.PutTracks(alb.PlaylistID, tracksByKey[alb.PlaylistID])
		}
		ch <- albumsLoaded{a.id, a.title, albums, nil}
	}()
	return waitMsg(ch)
}

// loadArtistTracksCmd enumerates an artist's uploads (Topic channel) and caches
// them keyed by the channel id. Albums are skipped for now (see fetchArtistTracks).
func (m *model) loadArtistTracksCmd(a item, playNow bool) tea.Cmd {
	cfg, cache := *m.cfg, m.cache
	return func() tea.Msg {
		if tr, ok := cache.Tracks(a.id); ok {
			return tracksLoaded{a.id, a.title, tr, nil, playNow}
		}
		tr, err := fetchArtistTracks(cfg, a.id)
		if err == nil {
			cache.PutTracks(a.id, tr)
		}
		return tracksLoaded{a.id, a.title, tr, err, playNow}
	}
}

func (m *model) loadTracksCmd(pl item, playNow bool) tea.Cmd {
	cfg, cache := *m.cfg, m.cache
	return func() tea.Msg {
		if tr, ok := cache.Tracks(pl.id); ok {
			return tracksLoaded{pl.id, pl.title, tr, nil, playNow}
		}
		tr, err := fetchTracks(cfg, pl.id)
		if err == nil {
			cache.PutTracks(pl.id, tr)
		}
		return tracksLoaded{pl.id, pl.title, tr, err, playNow}
	}
}

// ---- profile pics (thumbnails) --------------------------------------------

// thumbReady delivers a rendered thumbnail (or "" on failure) for an artist,
// tagged with the geometry it was rendered at so stale results can be dropped.
type thumbReady struct {
	id   string
	art  string
	w, h int
}

// thumbCmd downloads + renders one artist thumbnail off the UI goroutine.
func (m model) thumbCmd(id, url string, w, h int) tea.Cmd {
	dest := thumbCachePath(id)
	return func() tea.Msg {
		img, err := loadThumbImage(url, dest)
		if err != nil {
			return thumbReady{id, "", w, h} // mark tried; render a blank card
		}
		return thumbReady{id, renderThumb(img, w, h), w, h}
	}
}

// thumbsEnabled reports whether profile pics should render on the current screen.
func (m model) thumbsEnabled() bool {
	return m.showThumbs && m.view == "grid" && m.cur().kind == artistsScreen
}

// cardHeight is the number of terminal rows one grid card occupies (label only,
// or the thumbnail block plus its label).
func (m model) cardHeight() int {
	if m.thumbsEnabled() {
		return m.thumbH() + 1
	}
	return 1
}

// thumbH is the configured profile-pic height in rows.
func (m model) thumbH() int {
	h := m.cfg.ThumbHeight
	if h < 1 {
		h = 5
	}
	return h
}

// gridDims returns the column count, per-card inner width, and rows-per-page for
// the current window — shared by renderGrid and ensureThumbs so they agree.
func (m model) gridDims() (cols, innerW, rows int) {
	cols = m.cols()
	cardW := (m.width / cols) - 2
	if cardW < 8 {
		cardW = 8
	}
	innerW = cardW - 2
	bodyH := m.height - 5
	if m.filter != "" || m.typing {
		bodyH--
	}
	rows = bodyH / m.cardHeight()
	if rows < 1 {
		rows = 1
	}
	return
}

// ensureThumbs kicks off fetches for the profile pics on the visible page,
// invalidating cached art when the card geometry changed. Returns a batched cmd.
func (m *model) ensureThumbs() tea.Cmd {
	if !m.thumbsEnabled() || m.width == 0 {
		return nil
	}
	cols, innerW, rows := m.gridDims()
	if innerW != m.thumbW || m.thumbH() != m.thumbHeightCache() {
		m.thumbW, m.thumbHCache = innerW, m.thumbH()
		m.thumbs = map[string]string{}
		m.thumbLoading = map[string]bool{}
	}
	l := m.cur()
	perPage := cols * rows
	page := 0
	if perPage > 0 {
		page = l.cursor / perPage
	}
	start := page * perPage
	var cmds []tea.Cmd
	for i := start; i < start+perPage && i < len(l.vis); i++ {
		it := l.items[l.vis[i]]
		if it.thumb == "" {
			continue
		}
		if _, done := m.thumbs[it.id]; done {
			continue
		}
		if m.thumbLoading[it.id] {
			continue
		}
		m.thumbLoading[it.id] = true
		cmds = append(cmds, m.thumbCmd(it.id, it.thumb, innerW, m.thumbH()))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// thumbHeightCache mirrors thumbH but from the cached geometry field.
func (m model) thumbHeightCache() int { return m.thumbHCache }

// ---- update ---------------------------------------------------------------

func (m *model) recompute() {
	l := m.cur()
	l.vis = l.vis[:0]
	if m.filter == "" {
		for i := range l.items {
			l.vis = append(l.vis, i)
		}
	} else {
		titles := make([]string, len(l.items))
		for i, it := range l.items {
			titles[i] = it.title
		}
		for _, mm := range fuzzy.Find(m.filter, titles) {
			l.vis = append(l.vis, mm.Index)
		}
	}
	if l.cursor >= len(l.vis) {
		l.cursor = max(0, len(l.vis)-1)
	}
}

func (m *model) cols() int {
	if m.view == "list" {
		return 1
	}
	c := m.width / 26
	if c < 1 {
		c = 1
	}
	return c
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.ensureThumbs()

	case thumbReady:
		delete(m.thumbLoading, msg.id)
		if msg.w == m.thumbW && msg.h == m.thumbHCache { // drop stale geometry
			m.thumbs[msg.id] = msg.art
		}
		return m, nil

	case indexProgress:
		m.idxDone, m.idxTotal = msg.done, msg.total
		if m.idxCh != nil {
			return m, waitMsg(m.idxCh) // keep pulling until the final albumsLoaded
		}
		return m, nil

	case albumsLoaded:
		m.loading = false
		m.indexing = false
		m.idxCh = nil
		if msg.err != nil {
			m.status = "yt-dlp error: " + msg.err.Error()
			return m, nil
		}
		if len(msg.albums) == 0 {
			m.status = "no albums found for " + msg.artistName
			return m, nil
		}
		lv := level{kind: albumsScreen, title: msg.artistName + " · Albums",
			artistID: msg.artistID, artistName: msg.artistName}
		for _, a := range msg.albums {
			lv.items = append(lv.items, item{title: a.Title, subtitle: a.PlaylistID, id: a.PlaylistID})
		}
		m.pushLevel(lv)
		return m, nil

	case tracksLoaded:
		m.loading = false
		if msg.err != nil {
			m.status = "yt-dlp error: " + msg.err.Error()
			return m, nil
		}
		if len(msg.tracks) == 0 {
			m.status = "no tracks in " + msg.title
			return m, nil
		}
		if msg.playNow {
			ids := make([]string, len(msg.tracks))
			for i, t := range msg.tracks {
				ids[i] = t.VideoID
			}
			if err := play(*m.cfg, ids); err != nil {
				m.status = "play error: " + err.Error()
			} else {
				m.status = fmt.Sprintf("▶ %s (%d tracks)", msg.title, len(ids))
			}
			return m, nil
		}
		lv := level{kind: tracksScreen, title: msg.title, playlistID: msg.playlistID}
		for _, t := range msg.tracks {
			lv.items = append(lv.items, item{title: t.Title, subtitle: t.VideoID, id: t.VideoID})
		}
		m.pushLevel(lv)
		return m, nil

	case tea.KeyMsg:
		var mm tea.Model
		var cmd tea.Cmd
		if m.typing {
			mm, cmd = m.updateTyping(msg)
		} else {
			mm, cmd = m.updateNormal(msg)
		}
		// After any key, fetch profile pics for the (possibly new) visible page.
		if m2, ok := mm.(model); ok {
			if tc := (&m2).ensureThumbs(); tc != nil {
				cmd = tea.Batch(cmd, tc)
			}
			return m2, cmd
		}
		return mm, cmd
	}
	return m, nil
}

func (m *model) pushLevel(lv level) {
	m.filter = ""
	m.stack = append(m.stack, lv)
	m.recompute()
}

func (m model) updateTyping(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.typing, m.filter = false, ""
		m.recompute()
	case "enter":
		m.typing = false
	case "backspace":
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
			m.recompute()
		}
	default:
		if len(msg.Runes) > 0 {
			m.filter += string(msg.Runes)
			m.recompute()
		}
	}
	return m, nil
}

func (m model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	l := m.cur()
	n := len(l.vis)
	cols := m.cols()
	m.status = ""

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "/":
		m.typing = true
		return m, nil
	case "j", "down":
		l.cursor = clamp(l.cursor+cols, 0, n-1)
	case "k", "up":
		l.cursor = clamp(l.cursor-cols, 0, n-1)
	case "enter":
		return m.drill()
	case "left":
		l.cursor = clamp(l.cursor-1, 0, n-1)
	case "right":
		l.cursor = clamp(l.cursor+1, 0, n-1)
	case "h":
		if m.view == "list" {
			return m.goBack()
		}
		l.cursor = clamp(l.cursor-1, 0, n-1)
	case "l":
		if m.view == "list" {
			return m.drill()
		}
		l.cursor = clamp(l.cursor+1, 0, n-1)
	case "g":
		l.cursor = 0
	case "G":
		l.cursor = n - 1
	case "ctrl+d":
		l.cursor = clamp(l.cursor+cols*4, 0, n-1)
	case "ctrl+u":
		l.cursor = clamp(l.cursor-cols*4, 0, n-1)
	case "backspace":
		return m.goBack()
	case "p":
		return m.playCurrent()
	case "v":
		if m.view == "grid" {
			m.view = "list"
		} else {
			m.view = "grid"
		}
	case "t":
		m.themeName = nextTheme(m.themeName)
		m.st = buildStyles(getTheme(m.themeName))
		m.status = "theme: " + m.themeName
	case "i":
		m.showThumbs = !m.showThumbs
		if m.showThumbs {
			m.status = "profile pics: on"
		} else {
			m.status = "profile pics: off"
		}
	case "?":
		m.showHelp = !m.showHelp
	}
	return m, nil
}

func (m model) selected() (item, bool) {
	l := m.cur()
	if l.cursor < 0 || l.cursor >= len(l.vis) {
		return item{}, false
	}
	return l.items[l.vis[l.cursor]], true
}

func (m model) drill() (tea.Model, tea.Cmd) {
	if m.indexing {
		return m, nil // ignore input while an index is in flight
	}
	it, ok := m.selected()
	if !ok {
		return m, nil
	}
	switch m.cur().kind {
	case artistsScreen:
		if _, ok := m.cache.Albums(it.id); ok {
			m.loading = true
			m.status = "loading albums…"
			return m, m.loadAlbumsCmd(it) // instant: served from cache
		}
		m.status = ""
		cmd := m.startIndexCmd(it) // mutates m (indexing=true) before we return it
		return m, cmd              // slow first time, with live progress bar
	case albumsScreen:
		return m.openAlbum(it)
	case tracksScreen:
		if err := play(*m.cfg, []string{it.id}); err != nil {
			m.status = "play error: " + err.Error()
		} else {
			m.status = "▶ " + it.title
		}
	}
	return m, nil
}

// openAlbum pushes a tracks screen from the album's cached tracks (populated when
// the album list was built, so no network call here).
func (m model) openAlbum(it item) (tea.Model, tea.Cmd) {
	tr, _ := m.cache.Tracks(it.id)
	if len(tr) == 0 {
		m.status = "no cached tracks for " + it.title
		return m, nil
	}
	lv := level{kind: tracksScreen, title: it.title, playlistID: it.id}
	for _, t := range tr {
		lv.items = append(lv.items, item{title: t.Title, subtitle: t.VideoID, id: t.VideoID})
	}
	m.pushLevel(lv)
	return m, nil
}

func (m model) playCurrent() (tea.Model, tea.Cmd) {
	it, ok := m.selected()
	if !ok {
		return m, nil
	}
	switch m.cur().kind {
	case albumsScreen:
		tr, _ := m.cache.Tracks(it.id)
		if len(tr) == 0 {
			m.status = "no cached tracks for " + it.title
			return m, nil
		}
		ids := make([]string, len(tr))
		for i, t := range tr {
			ids[i] = t.VideoID
		}
		if err := play(*m.cfg, ids); err != nil {
			m.status = "play error: " + err.Error()
		} else {
			m.status = fmt.Sprintf("▶ %s (%d)", it.title, len(ids))
		}
		return m, nil
	case tracksScreen:
		if err := play(*m.cfg, []string{it.id}); err != nil {
			m.status = "play error: " + err.Error()
		} else {
			m.status = "▶ " + it.title
		}
	case artistsScreen:
		m.loading = true
		m.status = "queuing artist…"
		return m, m.loadArtistTracksCmd(it, true)
	}
	return m, nil
}

func (m model) goBack() (tea.Model, tea.Cmd) {
	if len(m.stack) > 1 {
		m.stack = m.stack[:len(m.stack)-1]
		m.filter = ""
		m.recompute()
	}
	return m, nil
}

// ---- view -----------------------------------------------------------------

func (m model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	var b strings.Builder

	crumbs := make([]string, len(m.stack))
	for i, l := range m.stack {
		crumbs[i] = vis(l.title)
	}
	b.WriteString(m.st.title.Render(strings.Join(crumbs, " / ")))
	b.WriteString("\n")

	if m.typing || m.filter != "" {
		cursor := ""
		if m.typing {
			cursor = "▏"
		}
		b.WriteString(m.st.filter.Render("/"+m.filter+cursor) +
			m.st.dim.Render(fmt.Sprintf("  %d matches", len(m.cur().vis))))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	bodyH := m.height - 5
	if m.filter != "" || m.typing {
		bodyH--
	}
	if bodyH < 1 {
		bodyH = 1
	}

	if m.view == "grid" {
		b.WriteString(m.renderGrid(bodyH))
	} else {
		b.WriteString(m.renderList(bodyH))
	}

	b.WriteString("\n")
	switch {
	case m.indexing:
		b.WriteString(m.renderIndexBar())
	case m.status != "":
		b.WriteString(m.st.loading.Render(m.status))
	case m.showHelp:
		b.WriteString(m.st.help.Render(m.helpLine()))
	}
	return b.String()
}

func (m model) helpLine() string {
	return "hjkl move  enter open  ⌫ back  p play  / filter  v view  t theme  i pics  ? help  q quit"
}

// renderIndexBar draws the inline album-indexing progress bar.
func (m model) renderIndexBar() string {
	const w = 28
	if m.idxTotal > 0 {
		f := m.idxDone * w / m.idxTotal
		if f > w {
			f = w
		}
		bar := strings.Repeat("█", f) + strings.Repeat("░", w-f)
		pct := m.idxDone * 100 / m.idxTotal
		return m.st.loading.Render(fmt.Sprintf("indexing [%s] %d/%d  %d%%", bar, m.idxDone, m.idxTotal, pct))
	}
	return m.st.loading.Render(fmt.Sprintf("indexing… %d tracks", m.idxDone))
}

// caretPrefix returns the selection marker for a row, honoring the caret config.
func (m model) caretPrefix(selected bool) string {
	if m.caret && selected {
		return "› "
	}
	return "  "
}

func (m model) renderList(h int) string {
	l := m.cur()
	start := windowStart(l.cursor, h, len(l.vis))
	var lines []string
	for i := start; i < len(l.vis) && i < start+h; i++ {
		it := l.items[l.vis[i]]
		text := truncate(vis(it.title), m.width-4)
		if i == l.cursor {
			lines = append(lines, m.st.rowSel.Render(m.caretPrefix(true)+text))
		} else {
			lines = append(lines, m.st.row.Render(m.caretPrefix(false)+text))
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) renderGrid(h int) string {
	l := m.cur()
	cols, innerW, rows := m.gridDims()
	perPage := cols * rows
	// window by page around cursor
	page := 0
	if perPage > 0 {
		page = l.cursor / perPage
	}
	startIdx := page * perPage
	thumbs := m.thumbsEnabled()

	var out []string
	for r := 0; r < rows; r++ {
		var cells []string
		for c := 0; c < cols; c++ {
			idx := startIdx + r*cols + c
			if idx >= len(l.vis) {
				break
			}
			it := l.items[l.vis[idx]]
			selected := idx == l.cursor
			var label string
			if m.caret {
				mark := "  "
				if selected {
					mark = "› "
				}
				label = mark + pad(truncate(vis(it.title), innerW-2), innerW-2)
			} else {
				label = pad(truncate(vis(it.title), innerW), innerW)
			}
			content := label
			if thumbs {
				content = m.thumbBlock(it.id, innerW) + "\n" + label
			}
			if selected {
				cells = append(cells, m.st.cardSel.Render(content))
			} else {
				cells = append(cells, m.st.card.Render(content))
			}
		}
		if len(cells) > 0 {
			out = append(out, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
		}
	}
	return strings.Join(out, "\n")
}

// thumbBlock returns the profile-pic art for an artist, or a blank placeholder
// of the right size while it loads (keeps every card the same height).
func (m model) thumbBlock(id string, w int) string {
	if art, ok := m.thumbs[id]; ok && art != "" {
		return art
	}
	blank := strings.Repeat(" ", w)
	lines := make([]string, m.thumbH())
	for i := range lines {
		lines[i] = blank
	}
	return strings.Join(lines, "\n")
}

// ---- helpers --------------------------------------------------------------

func windowStart(cursor, h, n int) int {
	if n <= h {
		return 0
	}
	start := cursor - h/2
	if start < 0 {
		start = 0
	}
	if start > n-h {
		start = n - h
	}
	return start
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func pad(s string, n int) string {
	w := len([]rune(s))
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}
