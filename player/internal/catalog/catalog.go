// Package catalog loads the curated artists.json, and searches and resolves
// artists in it.
package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/lubabs770/gooseneck/player/internal/config"
	"github.com/sahilm/fuzzy"
)

// Artist mirrors one record in artists.json. Unknown fields are ignored, so new
// keys added upstream won't break loading.
type Artist struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Thumbnail      string `json:"thumbnail"`
	IsFemale       bool   `json:"isFemale"`
	IsChasid       bool   `json:"isChasid"`
	IsKidZone      bool   `json:"isKidZone"`
	IsDJ           bool   `json:"isDJ"`
	IsAmerican     bool   `json:"isAmerican"`
	IsFamous       bool   `json:"isFamous"`
	IsIsraeli      bool   `json:"isIsraeli"`
	IsAcapellaOnly bool   `json:"isAcapellaOnly"`
}

// Tag is a named filter over the catalog's boolean flags.
type Tag struct {
	Key   string // used on the CLI: --tag chasidish
	Label string // shown in the TUI
	Has   func(Artist) bool
}

// Tags are the catalog's filters, in display order.
var Tags = []Tag{
	{"famous", "Popular", func(a Artist) bool { return a.IsFamous }},
	{"chasidish", "Chasidish", func(a Artist) bool { return a.IsChasid }},
	{"israeli", "Israeli", func(a Artist) bool { return a.IsIsraeli }},
	{"american", "American", func(a Artist) bool { return a.IsAmerican }},
	{"female", "Female vocals", func(a Artist) bool { return a.IsFemale }},
	{"kids", "Kids", func(a Artist) bool { return a.IsKidZone }},
	{"dj", "DJ", func(a Artist) bool { return a.IsDJ }},
	{"acapella", "A cappella", func(a Artist) bool { return a.IsAcapellaOnly }},
}

// TagKeys lists valid --tag values.
func TagKeys() []string {
	out := make([]string, len(Tags))
	for i, t := range Tags {
		out[i] = t.Key
	}
	return out
}

// FindTag returns the tag with key k ("" or "all" -> nil, ok).
func FindTag(k string) (*Tag, error) {
	k = strings.ToLower(strings.TrimSpace(k))
	if k == "" || k == "all" {
		return nil, nil
	}
	for i := range Tags {
		if Tags[i].Key == k || strings.EqualFold(Tags[i].Label, k) {
			return &Tags[i], nil
		}
	}
	return nil, fmt.Errorf("unknown tag %q (valid: %s)", k, strings.Join(TagKeys(), ", "))
}

// TagLabels returns the labels of every tag that applies to a.
func (a Artist) TagLabels() []string {
	var out []string
	for _, t := range Tags {
		if t.Has(a) {
			out = append(out, t.Label)
		}
	}
	return out
}

// TagKeys returns the keys of every tag that applies to a.
func (a Artist) TagKeys() []string {
	var out []string
	for _, t := range Tags {
		if t.Has(a) {
			out = append(out, t.Key)
		}
	}
	return out
}

// Catalog is the loaded artist list plus an id index.
type Catalog struct {
	Artists []Artist
	Path    string    // file it was read from
	ModTime time.Time // when that file was last written
	byID    map[string]int
}

type wrapped struct {
	Count   int      `json:"count"`
	Artists []Artist `json:"artists"`
}

// Load resolves the catalog, in order of preference:
//  1. cfg.ArtistsJSON (explicit path)
//  2. an artists.json in the working dir or its parent (repo checkout)
//  3. the cached download in the config dir
//  4. a fresh download from cfg.ArtistsURL
func Load(cfg config.Config) (*Catalog, error) {
	path := cfg.ArtistsJSON
	if path == "" {
		path = findLocal()
	}
	if path == "" {
		path = config.CatalogCachePath()
		if _, err := os.Stat(path); err != nil {
			if err := Download(cfg.ArtistsURL, path); err != nil {
				return nil, err
			}
		}
	}
	return Read(path)
}

// Read parses a catalog file in either the wrapped {count,artists} shape or a
// bare array.
func Read(path string) (*Catalog, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var artists []Artist
	var w wrapped
	if err := json.Unmarshal(b, &w); err == nil && len(w.Artists) > 0 {
		artists = w.Artists
	} else if err := json.Unmarshal(b, &artists); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	c := New(artists)
	c.Path = path
	if st, err := os.Stat(path); err == nil {
		c.ModTime = st.ModTime()
	}
	return c, nil
}

// New builds a catalog from artists, dropping entries without an id and
// sorting by name (case-insensitive) for stable browsing.
func New(artists []Artist) *Catalog {
	out := make([]Artist, 0, len(artists))
	seen := map[string]bool{}
	for _, a := range artists {
		a.Name = strings.TrimSpace(a.Name)
		if a.ID == "" || seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool { return foldKey(out[i].Name) < foldKey(out[j].Name) })
	c := &Catalog{Artists: out, byID: make(map[string]int, len(out))}
	for i, a := range out {
		c.byID[a.ID] = i
	}
	return c
}

// foldKey sorts names case-insensitively, with Latin names before Hebrew ones.
func foldKey(s string) string { return strings.ToLower(s) }

func findLocal() string {
	for _, c := range []string{"artists.json", filepath.Join("..", "artists.json")} {
		if b, err := os.ReadFile(c); err == nil && json.Valid(b) {
			return c
		}
	}
	return ""
}

// Download fetches the catalog from url, validates it, and writes it to dest
// atomically (temp file + rename) so a failed download never clobbers a good one.
func Download(url, dest string) error {
	if url == "" {
		return fmt.Errorf("no catalog source: set artists_json or artists_url in the config")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download catalog: HTTP %d from %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if !json.Valid(body) {
		return fmt.Errorf("download catalog: %s did not return JSON", url)
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

// Len is the number of artists.
func (c *Catalog) Len() int { return len(c.Artists) }

// Get looks an artist up by channel id.
func (c *Catalog) Get(id string) (Artist, bool) {
	i, ok := c.byID[id]
	if !ok {
		return Artist{}, false
	}
	return c.Artists[i], true
}

// Filter returns the artists carrying tag (nil = all).
func (c *Catalog) Filter(tag *Tag) []Artist {
	if tag == nil {
		return c.Artists
	}
	var out []Artist
	for _, a := range c.Artists {
		if tag.Has(a) {
			out = append(out, a)
		}
	}
	return out
}

// Search fuzzy-matches query against artist names within tag (nil = all),
// best match first. An empty query returns the filtered list unchanged.
func (c *Catalog) Search(query string, tag *Tag) []Artist {
	pool := c.Filter(tag)
	query = strings.TrimSpace(query)
	if query == "" {
		return pool
	}
	return Rank(query, pool, func(a Artist) string { return a.Name })
}

// Rank orders items by how well name(item) matches query. Exact and prefix
// matches beat fuzzy subsequence matches, so "Avi" ranks "Avi Perets" above
// "Shlomo Gavriel".
func Rank[T any](query string, items []T, name func(T) string) []T {
	q := Fold(query)
	names := make([]string, len(items))
	for i, it := range items {
		names[i] = Fold(name(it))
	}
	type scored struct {
		i     int
		score int
	}
	var res []scored
	for _, m := range fuzzy.Find(q, names) {
		s := m.Score
		n := names[m.Index]
		switch {
		case n == q:
			s += 100000
		case strings.HasPrefix(n, q):
			s += 50000
		case containsWord(n, q):
			s += 20000
		case strings.Contains(n, q):
			s += 10000
		}
		s -= len(n) // shorter names win ties
		res = append(res, scored{m.Index, s})
	}
	sort.SliceStable(res, func(a, b int) bool { return res[a].score > res[b].score })
	out := make([]T, len(res))
	for i, r := range res {
		out[i] = items[r.i]
	}
	return out
}

func containsWord(s, q string) bool {
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if strings.HasPrefix(w, q) {
			return true
		}
	}
	return false
}

// Fold normalizes a string for matching: lower case, apostrophes/punctuation
// collapsed to spaces.
func Fold(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Map(func(r rune) rune {
		switch r {
		case '\'', '’', '`', '"':
			return -1
		}
		return r
	}, s)
}

// AmbiguousError is returned by Resolve when a query matches several artists
// without a clear winner.
type AmbiguousError struct {
	Query      string
	Candidates []Artist
}

func (e *AmbiguousError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d artists:", e.Query, len(e.Candidates))
	for i, a := range e.Candidates {
		if i == 8 {
			fmt.Fprintf(&b, "\n  … and %d more", len(e.Candidates)-8)
			break
		}
		fmt.Fprintf(&b, "\n  %s", a.Name)
	}
	return b.String()
}

// NotFoundError is returned by Resolve when nothing matches.
type NotFoundError struct{ Query string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("no artist matches %q", e.Query) }

// Resolve turns a user query (channel id, exact name, or a fuzzy fragment) into
// one artist. It is decisive when the answer is clear and returns an
// *AmbiguousError listing candidates when it isn't.
func (c *Catalog) Resolve(query string) (Artist, error) {
	q := strings.TrimSpace(query)
	if a, ok := c.Get(q); ok {
		return a, nil
	}
	fq := Fold(q)
	for _, a := range c.Artists {
		if Fold(a.Name) == fq {
			return a, nil
		}
	}
	ranked := c.Search(q, nil)
	switch {
	case len(ranked) == 0:
		return Artist{}, &NotFoundError{query}
	case len(ranked) == 1:
		return ranked[0], nil
	}
	// A unique prefix match is unambiguous ("8th" -> "8th Day").
	var prefix []Artist
	for _, a := range ranked {
		if strings.HasPrefix(Fold(a.Name), fq) {
			prefix = append(prefix, a)
		}
	}
	if len(prefix) == 1 {
		return prefix[0], nil
	}
	if len(prefix) > 1 {
		ranked = prefix
	}
	return Artist{}, &AmbiguousError{query, ranked}
}
