package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/config"
)

// MainName is the name of the built-in catalog.
const MainName = "gooseneck"

// Source describes one catalog merged into the loaded one.
type Source struct {
	Name     string    `json:"name"`
	File     string    `json:"file"`
	URL      string    `json:"url,omitempty"`
	Count    int       `json:"count"`
	Updated  time.Time `json:"updated"`
	Editable bool      `json:"editable"` // a local file you can add artists to
	Main     bool      `json:"main"`
	Err      string    `json:"error,omitempty"`
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ValidName checks a catalog name (lowercase letters, digits, - and _).
func ValidName(n string) error {
	if n == MainName || !nameRe.MatchString(n) {
		return fmt.Errorf("catalog name %q: use lowercase letters, digits, - or _ (and not %q)", n, MainName)
	}
	return nil
}

// LoadAll loads the main catalog plus every enabled extra catalog from the
// config and merges them. A broken extra catalog is reported in Sources but
// never stops the app from starting.
func LoadAll(cfg config.Config) (*Catalog, error) {
	main, err := Load(cfg)
	if err != nil {
		return nil, err
	}
	all := make([]Artist, len(main.Artists))
	for i, a := range main.Artists {
		a.Catalogs = []string{MainName}
		all[i] = a
	}
	sources := []Source{{Name: MainName, File: main.Path, URL: cfg.ArtistsURL, Count: len(main.Artists),
		Updated: main.ModTime, Main: true}}
	for _, src := range cfg.Catalogs {
		if src.Disabled {
			continue
		}
		info := Source{Name: src.Name, File: src.File(), URL: src.URL, Editable: src.URL == ""}
		if src.URL != "" {
			if _, err := os.Stat(info.File); err != nil {
				if err := Download(src.URL, info.File); err != nil {
					info.Err = err.Error()
					sources = append(sources, info)
					continue
				}
			}
		}
		c, err := Read(info.File)
		if err != nil {
			info.Err = err.Error()
			sources = append(sources, info)
			continue
		}
		info.Count, info.Updated = len(c.Artists), c.ModTime
		for _, a := range c.Artists {
			a.Catalogs = []string{src.Name}
			all = append(all, a)
		}
		sources = append(sources, info)
	}
	merged := merge(all)
	merged.Path, merged.ModTime, merged.Sources = main.Path, main.ModTime, sources
	return merged, nil
}

// merge folds duplicate ids together: the first name/picture wins, tags and
// catalog memberships are unioned.
func merge(all []Artist) *Catalog {
	byID := map[string]int{}
	var out []Artist
	for _, a := range all {
		if a.ID == "" {
			continue
		}
		i, ok := byID[a.ID]
		if !ok {
			byID[a.ID] = len(out)
			out = append(out, a)
			continue
		}
		m := &out[i]
		if m.Thumbnail == "" {
			m.Thumbnail = a.Thumbnail
		}
		m.IsFemale = m.IsFemale || a.IsFemale
		m.IsChasid = m.IsChasid || a.IsChasid
		m.IsKidZone = m.IsKidZone || a.IsKidZone
		m.IsDJ = m.IsDJ || a.IsDJ
		m.IsAmerican = m.IsAmerican || a.IsAmerican
		m.IsFamous = m.IsFamous || a.IsFamous
		m.IsIsraeli = m.IsIsraeli || a.IsIsraeli
		m.IsAcapellaOnly = m.IsAcapellaOnly || a.IsAcapellaOnly
		for _, n := range a.Catalogs {
			if !contains(m.Catalogs, n) {
				m.Catalogs = append(m.Catalogs, n)
			}
		}
	}
	c := New(out) // New keeps the first of each id, which is now the merged one
	return c
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// In reports whether the artist is listed in catalog name.
func (a Artist) In(name string) bool { return contains(a.Catalogs, name) }

// Subset returns a catalog with only the artists in the named catalog.
func (c *Catalog) Subset(name string) (*Catalog, error) {
	if name == "" || name == "all" {
		return c, nil
	}
	found := false
	for _, s := range c.Sources {
		if s.Name == name {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("no catalog named %q (have: %s)", name, strings.Join(c.Names(), ", "))
	}
	var out []Artist
	for _, a := range c.Artists {
		if a.In(name) {
			out = append(out, a)
		}
	}
	sub := New(out)
	sub.Sources, sub.Path, sub.ModTime = c.Sources, c.Path, c.ModTime
	return sub, nil
}

// Names lists the loaded catalogs.
func (c *Catalog) Names() []string {
	var out []string
	for _, s := range c.Sources {
		out = append(out, s.Name)
	}
	if len(out) == 0 {
		out = []string{MainName}
	}
	return out
}

// Write saves artists as a catalog file (the same wrapped shape the main
// catalog uses), atomically.
func Write(path string, artists []Artist) error {
	if artists == nil {
		artists = []Artist{}
	}
	b, err := json.MarshalIndent(wrapped{Count: len(artists), Artists: artists}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// AddTo adds (or updates) an artist in a catalog file; it reports whether the
// artist was new.
func AddTo(path string, a Artist) (bool, error) {
	c, err := Read(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	var list []Artist
	if c != nil {
		list = c.Artists
	}
	a.Catalogs = nil
	for i, x := range list {
		if x.ID == a.ID {
			list[i] = a
			return false, Write(path, list)
		}
	}
	return true, Write(path, append(list, a))
}

// RemoveFrom drops an artist (by id) from a catalog file.
func RemoveFrom(path, id string) (bool, error) {
	c, err := Read(path)
	if err != nil {
		return false, err
	}
	var keep []Artist
	removed := false
	for _, x := range c.Artists {
		if x.ID == id {
			removed = true
			continue
		}
		keep = append(keep, x)
	}
	if !removed {
		return false, nil
	}
	return true, Write(path, keep)
}

// ChannelID extracts a channel id from an id or a YouTube / YouTube Music
// channel URL ("" if it isn't one).
func ChannelID(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "/channel/"); i >= 0 {
		s = s[i+len("/channel/"):]
		if j := strings.IndexAny(s, "/?#&"); j >= 0 {
			s = s[:j]
		}
	}
	if strings.HasPrefix(s, "UC") && len(s) == 24 && !strings.ContainsAny(s, " /") {
		return s
	}
	return ""
}
