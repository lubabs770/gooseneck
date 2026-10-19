// Package ytm is a tiny read-only client for YouTube Music's web API
// ("InnerTube"). One request returns an artist's whole page (albums, singles,
// top songs, bio, related artists) and one more returns an album's tracklist,
// which is orders of magnitude faster than extracting every track with yt-dlp.
//
// The responses are deeply nested renderer trees that YouTube reshuffles from
// time to time, so parsing walks them defensively: a missing field yields an
// empty value, never a panic.
package ytm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	endpoint      = "https://music.youtube.com/youtubei/v1/browse?prettyPrint=false"
	clientName    = "WEB_REMIX"
	clientVersion = "1.20251015.03.00"
	userAgent     = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36"
)

// ErrNotArtist means the id resolved to something that isn't a music artist
// page (e.g. a plain user channel).
var ErrNotArtist = errors.New("not a YouTube Music artist page")

// Client talks to music.youtube.com. The zero value is usable.
type Client struct {
	HTTP     *http.Client
	Endpoint string // override for tests
}

// Song is one playable track.
type Song struct {
	VideoID  string `json:"video_id"`
	Title    string `json:"title"`
	Artist   string `json:"artist,omitempty"`
	ArtistID string `json:"artist_id,omitempty"`
	Album    string `json:"album,omitempty"`
	AlbumID  string `json:"album_id,omitempty"`
	Plays    string `json:"plays,omitempty"`
	Duration int    `json:"duration,omitempty"` // seconds; 0 = unknown
	Index    int    `json:"index,omitempty"`    // 1-based position in its album
	Thumb    string `json:"thumb,omitempty"`
}

// Release is an album / single / EP as listed on an artist page.
type Release struct {
	BrowseID   string `json:"browse_id"`
	PlaylistID string `json:"playlist_id,omitempty"`
	Title      string `json:"title"`
	Kind       string `json:"kind,omitempty"` // Album, Single, EP
	Year       string `json:"year,omitempty"`
	Thumb      string `json:"thumb,omitempty"`
}

// ArtistRef is a linked artist ("Fans might also like").
type ArtistRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Subtitle string `json:"subtitle,omitempty"`
	Thumb    string `json:"thumb,omitempty"`
}

// Artist is a parsed artist page.
type Artist struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Subscribers string      `json:"subscribers,omitempty"`
	Views       string      `json:"views,omitempty"`
	Description string      `json:"description,omitempty"`
	Thumb       string      `json:"thumb,omitempty"`
	TopSongs    []Song      `json:"top_songs,omitempty"`
	Albums      []Release   `json:"albums,omitempty"`
	Singles     []Release   `json:"singles,omitempty"`
	Related     []ArtistRef `json:"related,omitempty"`
}

// Releases returns albums then singles.
func (a *Artist) Releases() []Release {
	return append(append([]Release{}, a.Albums...), a.Singles...)
}

// Album is a parsed album page.
type Album struct {
	BrowseID   string `json:"browse_id"`
	PlaylistID string `json:"playlist_id,omitempty"`
	Title      string `json:"title"`
	Kind       string `json:"kind,omitempty"`
	Year       string `json:"year,omitempty"`
	Artist     string `json:"artist,omitempty"`
	ArtistID   string `json:"artist_id,omitempty"`
	Thumb      string `json:"thumb,omitempty"`
	Length     string `json:"length,omitempty"` // "39 minutes"
	Tracks     []Song `json:"tracks"`
}

// Duration sums the known track lengths in seconds.
func (a *Album) Duration() int {
	n := 0
	for _, t := range a.Tracks {
		n += t.Duration
	}
	return n
}

// ---- requests --------------------------------------------------------------

func (c *Client) post(ctx context.Context, extraQuery string, body map[string]any) (any, error) {
	body["context"] = map[string]any{"client": map[string]any{
		"clientName": clientName, "clientVersion": clientVersion, "hl": "en", "gl": "US",
	}}
	buf, _ := json.Marshal(body)
	url := c.Endpoint
	if url == "" {
		url = endpoint
	}
	url += extraQuery
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://music.youtube.com")
	req.Header.Set("User-Agent", userAgent)
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("youtube music: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube music: HTTP %d", resp.StatusCode)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("youtube music: bad JSON: %w", err)
	}
	return v, nil
}

func (c *Client) browse(ctx context.Context, browseID, params string) (any, error) {
	body := map[string]any{"browseId": browseID}
	if params != "" {
		body["params"] = params
	}
	return c.post(ctx, "", body)
}

// Artist fetches and parses an artist page. When the Albums or Singles shelf
// is truncated it follows the shelf's "more" link to get the full discography.
func (c *Client) Artist(ctx context.Context, channelID string) (*Artist, error) {
	root, err := c.browse(ctx, channelID, "")
	if err != nil {
		return nil, err
	}
	a, more := parseArtist(root)
	if a.Name == "" {
		return nil, ErrNotArtist
	}
	a.ID = channelID
	for kind, ep := range more {
		full, err := c.discography(ctx, ep)
		if err != nil || len(full) == 0 {
			continue // keep the carousel's partial list
		}
		for i := range full {
			if full[i].Kind == "" {
				full[i].Kind = kind
			}
		}
		if kind == "Album" {
			a.Albums = full
		} else {
			a.Singles = full
		}
	}
	return a, nil
}

type browseEP struct{ id, params string }

func (c *Client) discography(ctx context.Context, ep browseEP) ([]Release, error) {
	root, err := c.browse(ctx, ep.id, ep.params)
	if err != nil {
		return nil, err
	}
	grid := findKey(root, "gridRenderer")
	out := parseReleases(get(grid, "items"), "")
	token := str(grid, "continuations", 0, "nextContinuationData", "continuation")
	for page := 0; token != "" && page < 10; page++ {
		q := "&ctoken=" + token + "&continuation=" + token + "&type=next"
		next, err := c.post(ctx, q, map[string]any{})
		if err != nil {
			break
		}
		cont := get(next, "continuationContents", "gridContinuation")
		out = append(out, parseReleases(get(cont, "items"), "")...)
		token = str(cont, "continuations", 0, "nextContinuationData", "continuation")
	}
	return out, nil
}

// Album fetches and parses an album page (browse id "MPREb_…").
func (c *Client) Album(ctx context.Context, browseID string) (*Album, error) {
	root, err := c.browse(ctx, browseID, "")
	if err != nil {
		return nil, err
	}
	al := parseAlbum(root)
	if al.Title == "" && len(al.Tracks) == 0 {
		return nil, fmt.Errorf("youtube music: album %s not found", browseID)
	}
	al.BrowseID = browseID
	return al, nil
}

// ---- parsing ---------------------------------------------------------------

func parseArtist(root any) (*Artist, map[string]browseEP) {
	a := &Artist{}
	more := map[string]browseEP{}
	h := get(root, "header", "musicImmersiveHeaderRenderer")
	if h == nil {
		h = get(root, "header", "musicVisualHeaderRenderer")
	}
	a.Name = runs(get(h, "title"))
	a.Subscribers = runs(get(h, "subscriptionButton", "subscribeButtonRenderer", "subscriberCountText"))
	a.Thumb = lastThumb(get(h, "thumbnail"))
	if a.Thumb == "" {
		a.Thumb = lastThumb(get(h, "foregroundThumbnail"))
	}

	sections := get(root, "contents", "singleColumnBrowseResultsRenderer", "tabs", 0,
		"tabRenderer", "content", "sectionListRenderer", "contents")
	for _, sec := range list(sections) {
		if shelf := get(sec, "musicShelfRenderer"); shelf != nil {
			if strings.EqualFold(runs(get(shelf, "title")), "Top songs") || a.TopSongs == nil {
				for _, it := range list(get(shelf, "contents")) {
					if s, ok := parseSongRow(get(it, "musicResponsiveListItemRenderer")); ok {
						a.TopSongs = append(a.TopSongs, s)
					}
				}
			}
			continue
		}
		if desc := get(sec, "musicDescriptionShelfRenderer"); desc != nil {
			a.Description = strings.TrimSpace(runs(get(desc, "description")))
			a.Views = runs(get(desc, "subheader"))
			continue
		}
		car := get(sec, "musicCarouselShelfRenderer")
		if car == nil {
			continue
		}
		titleRuns := get(car, "header", "musicCarouselShelfBasicHeaderRenderer", "title")
		title := runs(titleRuns)
		ep := browseEP{
			id:     str(titleRuns, "runs", 0, "navigationEndpoint", "browseEndpoint", "browseId"),
			params: str(titleRuns, "runs", 0, "navigationEndpoint", "browseEndpoint", "params"),
		}
		items := get(car, "contents")
		switch {
		case title == "Albums":
			a.Albums = parseReleases(items, "Album")
			if ep.id != "" && ep.params != "" {
				more["Album"] = ep
			}
		case strings.HasPrefix(title, "Singles"):
			a.Singles = parseReleases(items, "Single")
			if ep.id != "" && ep.params != "" {
				more["Single"] = ep
			}
		case strings.Contains(title, "might also like") || title == "Similar artists":
			for _, it := range list(items) {
				r := get(it, "musicTwoRowItemRenderer")
				id := str(r, "navigationEndpoint", "browseEndpoint", "browseId")
				if !strings.HasPrefix(id, "UC") {
					continue
				}
				a.Related = append(a.Related, ArtistRef{
					ID: id, Name: runs(get(r, "title")), Subtitle: runs(get(r, "subtitle")),
					Thumb: lastThumb(get(r, "thumbnailRenderer")),
				})
			}
		}
	}
	return a, more
}

// parseReleases reads musicTwoRowItemRenderer album cards. defKind is used
// when the subtitle doesn't name the release type.
func parseReleases(items any, defKind string) []Release {
	var out []Release
	for _, it := range list(items) {
		r := get(it, "musicTwoRowItemRenderer")
		id := str(r, "navigationEndpoint", "browseEndpoint", "browseId")
		if id == "" {
			id = str(r, "title", "runs", 0, "navigationEndpoint", "browseEndpoint", "browseId")
		}
		if !strings.HasPrefix(id, "MPRE") {
			continue
		}
		rel := Release{BrowseID: id, Title: runs(get(r, "title")), Kind: defKind,
			Thumb: lastThumb(get(r, "thumbnailRenderer"))}
		for _, part := range list(get(r, "subtitle", "runs")) {
			t := strings.TrimSpace(str(part, "text"))
			switch {
			case isYear(t):
				rel.Year = t
			case t == "Album" || t == "Single" || t == "EP":
				rel.Kind = t
			}
		}
		if pl := findString(r, "playlistId", "OLAK"); pl != "" {
			rel.PlaylistID = pl
		}
		out = append(out, rel)
	}
	return out
}

func parseAlbum(root any) *Album {
	al := &Album{}
	h := findKey(get(root, "contents", "twoColumnBrowseResultsRenderer", "tabs"), "musicResponsiveHeaderRenderer")
	al.Title = runs(get(h, "title"))
	al.Thumb = lastThumb(get(h, "thumbnail"))
	for _, part := range list(get(h, "subtitle", "runs")) {
		t := strings.TrimSpace(str(part, "text"))
		switch {
		case isYear(t):
			al.Year = t
		case t == "Album" || t == "Single" || t == "EP":
			al.Kind = t
		}
	}
	if sub := list(get(h, "secondSubtitle", "runs")); len(sub) > 0 {
		al.Length = strings.TrimSpace(str(sub[len(sub)-1], "text"))
	}
	strap := get(h, "straplineTextOne")
	al.Artist = runs(strap)
	al.ArtistID = str(strap, "runs", 0, "navigationEndpoint", "browseEndpoint", "browseId")

	shelf := findKey(get(root, "contents", "twoColumnBrowseResultsRenderer", "secondaryContents"), "musicShelfRenderer")
	for _, it := range list(get(shelf, "contents")) {
		s, ok := parseSongRow(get(it, "musicResponsiveListItemRenderer"))
		if !ok {
			continue
		}
		if s.Artist == "" {
			s.Artist, s.ArtistID = al.Artist, al.ArtistID
		}
		s.Album, s.Thumb = al.Title, al.Thumb
		if s.Index == 0 {
			s.Index = len(al.Tracks) + 1
		}
		al.Tracks = append(al.Tracks, s)
		if al.PlaylistID == "" {
			al.PlaylistID = findString(it, "playlistId", "OLAK")
		}
	}
	return al
}

// parseSongRow reads one musicResponsiveListItemRenderer (a track row).
// Column layout: [title] [artists] [plays] [album]; duration is a fixed column.
func parseSongRow(r any) (Song, bool) {
	if r == nil {
		return Song{}, false
	}
	s := Song{VideoID: str(r, "playlistItemData", "videoId")}
	if s.VideoID == "" {
		s.VideoID = findString(r, "videoId", "")
	}
	if s.VideoID == "" {
		return s, false // unavailable / greyed-out track
	}
	cols := list(get(r, "flexColumns"))
	col := func(i int) any {
		if i >= len(cols) {
			return nil
		}
		return get(cols[i], "musicResponsiveListItemFlexColumnRenderer", "text")
	}
	s.Title = runs(col(0))
	for _, part := range list(get(col(1), "runs")) {
		id := str(part, "navigationEndpoint", "browseEndpoint", "browseId")
		if strings.HasPrefix(id, "UC") && s.ArtistID == "" {
			s.ArtistID = id
		}
	}
	s.Artist = runs(col(1))
	if p := runs(col(2)); strings.HasSuffix(p, "plays") {
		s.Plays = p
	}
	if alb := get(col(3), "runs", 0); alb != nil {
		s.Album = str(alb, "text")
		s.AlbumID = str(alb, "navigationEndpoint", "browseEndpoint", "browseId")
	}
	s.Duration = ParseDuration(runs(get(r, "fixedColumns", 0, "musicResponsiveListItemFixedColumnRenderer", "text")))
	if n, err := strconv.Atoi(runs(get(r, "index"))); err == nil {
		s.Index = n
	}
	s.Thumb = lastThumb(get(r, "thumbnail"))
	return s, true
}

// ParseDuration turns "3:15" or "1:02:03" into seconds (0 if unparseable).
func ParseDuration(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	total := 0
	for _, p := range strings.Split(s, ":") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0
		}
		total = total*60 + n
	}
	return total
}

// FormatDuration renders seconds as m:ss or h:mm:ss.
func FormatDuration(sec int) string {
	if sec < 0 {
		sec = 0
	}
	h, m, s := sec/3600, (sec/60)%60, sec%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func isYear(s string) bool {
	if len(s) != 4 {
		return false
	}
	n, err := strconv.Atoi(s)
	return err == nil && n > 1900 && n < 2200
}

// ---- generic JSON tree helpers -------------------------------------------

// get walks v by string keys (objects) and int indexes (arrays).
func get(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[k]
		case int:
			a, ok := v.([]any)
			if !ok || k < 0 || k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

func str(v any, path ...any) string {
	s, _ := get(v, path...).(string)
	return s
}

func list(v any) []any {
	a, _ := v.([]any)
	return a
}

// runs joins a {"runs":[{"text":…}]} object into plain text.
func runs(v any) string {
	if s := str(v, "simpleText"); s != "" {
		return s
	}
	var b strings.Builder
	for _, r := range list(get(v, "runs")) {
		b.WriteString(str(r, "text"))
	}
	return b.String()
}

// lastThumb returns the largest (last) thumbnail URL found under v.
func lastThumb(v any) string {
	t := findKey(v, "thumbnails")
	arr := list(t)
	if len(arr) == 0 {
		return ""
	}
	return str(arr[len(arr)-1], "url")
}

// findKey does a depth-first search for the first value stored under key.
func findKey(v any, key string) any {
	switch t := v.(type) {
	case map[string]any:
		if x, ok := t[key]; ok {
			return x
		}
		for _, x := range t {
			if r := findKey(x, key); r != nil {
				return r
			}
		}
	case []any:
		for _, x := range t {
			if r := findKey(x, key); r != nil {
				return r
			}
		}
	}
	return nil
}

// findString finds the first string under key with the given prefix.
func findString(v any, key, prefix string) string {
	switch t := v.(type) {
	case map[string]any:
		if s, ok := t[key].(string); ok && strings.HasPrefix(s, prefix) {
			return s
		}
		for _, x := range t {
			if s := findString(x, key, prefix); s != "" {
				return s
			}
		}
	case []any:
		for _, x := range t {
			if s := findString(x, key, prefix); s != "" {
				return s
			}
		}
	}
	return ""
}
