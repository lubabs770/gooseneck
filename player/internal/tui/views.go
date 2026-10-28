package tui

import (
	"fmt"
	"sort"

	"github.com/lubabs770/gooseneck/player/internal/config"
)

// ViewNames are the screens a [views] rule in config.toml can target, and
// whether each one has a grid/list choice (the rest only have art).
var ViewNames = map[string]bool{
	"library": true,  // every artist (page 2)
	"albums":  true,  // artist page: Albums / Uploads tab
	"singles": true,  // artist page: Singles & EPs tab
	"related": true,  // artist page: Fans also like tab
	"search":  false, // home page search results
	"artist":  false, // artist page header picture
	"album":   false, // album page cover
	"sidebar": false, // now-playing art
}

// checkViews reports [views] entries that don't apply.
func checkViews(rules map[string]config.ViewRule) []string {
	var bad []string
	for name, r := range rules {
		grid, ok := ViewNames[name]
		switch {
		case !ok:
			bad = append(bad, fmt.Sprintf("unknown screen %q", name))
		case r.View != "" && r.View != "grid" && r.View != "list":
			bad = append(bad, fmt.Sprintf("%s: view must be grid or list", name))
		case r.View != "" && !grid:
			bad = append(bad, fmt.Sprintf("%s has no grid/list choice", name))
		}
	}
	sort.Strings(bad)
	return bad
}

// gridFor is the starting layout of a screen: its rule, else the global view.
func (a *App) gridFor(name string) bool {
	v := a.lib.Cfg.Views[name].View
	if v == "" {
		v = a.lib.Cfg.View
	}
	return v != "list"
}

// thumbsFor is whether a screen's rule (else the global setting) wants art.
// The `i` key (a.showImages) still turns all art off and back on.
func (a *App) thumbsFor(name string) bool {
	return config.On(a.lib.Cfg.Views[name].Thumbnails, config.On(a.lib.Cfg.Thumbnails, true))
}

func (a *App) artFor(name string) bool { return a.showImages && a.thumbsFor(name) }

// anyThumbs: art starts on when any screen wants it (with thumbnails = false
// and one rule turning it on, only that screen shows art).
func anyThumbs(cfg config.Config) bool {
	if config.On(cfg.Thumbnails, true) {
		return true
	}
	for _, r := range cfg.Views {
		if config.On(r.Thumbnails, false) {
			return true
		}
	}
	return false
}
