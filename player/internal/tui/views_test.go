package tui

import (
	"strings"
	"testing"

	"github.com/lubabs770/gooseneck/player/internal/config"
)

func TestViewRules(t *testing.T) {
	a := mouseApp(t) // thumbnails = false, view = grid
	a.lib.Cfg.Views = map[string]config.ViewRule{
		"albums":  {View: "list"},
		"sidebar": {Thumbnails: config.BoolPtr(true)},
	}
	if !a.gridFor("library") || a.gridFor("albums") {
		t.Fatal("albums should be list, library grid")
	}
	if a.thumbsFor("library") || !a.thumbsFor("sidebar") {
		t.Fatal("only the sidebar should want art")
	}
	if !anyThumbs(a.lib.Cfg) || anyThumbs(config.Config{Thumbnails: config.BoolPtr(false)}) {
		t.Fatal("anyThumbs")
	}
	bad := strings.Join(checkViews(map[string]config.ViewRule{
		"albms": {}, "sidebar": {View: "list"}, "singles": {View: "cards"}, "related": {View: "grid"},
	}), "; ")
	for _, want := range []string{`unknown screen "albms"`, "sidebar has no grid/list", "singles: view must be"} {
		if !strings.Contains(bad, want) {
			t.Errorf("checkViews = %q, missing %q", bad, want)
		}
	}
	if strings.Contains(bad, "related") {
		t.Errorf("related rule is fine: %q", bad)
	}
}
