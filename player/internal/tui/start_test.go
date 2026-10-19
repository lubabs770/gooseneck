package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMatchTab(t *testing.T) {
	cases := []struct {
		page, q, want string
	}{
		{"library", "chasidish", "chasidish"},
		{"library", "Female vocals", "female"},
		{"library", "chas", "chasidish"},
		{"library", "ALL", "all"},
		{"artist", "singles-eps", "singles"},
		{"artist", "Uploads", "albums"},
		{"artist", "fans", "related"},
		{"artist", "top", "songs"},
		{"history", "l", "listens"},
		{"history", "top artists", "top"},
	}
	for _, c := range cases {
		names := tabNames(c.page)
		i, err := matchTab(c.q, names)
		if err != nil || names[i].key != c.want {
			t.Errorf("%s %q = %v, %v; want %s", c.page, c.q, i, err, c.want)
		}
	}
	if _, err := matchTab("a", tabNames("library")); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("library 'a' should be ambiguous (all, american, acapella), got %v", err)
	}
	if _, err := matchTab("bogus", tabNames("history")); err == nil || !strings.Contains(err.Error(), "recent, top, listens") {
		t.Errorf("unknown tab error should list the tabs, got %v", err)
	}
}

func TestResolveStart(t *testing.T) {
	ok := []Options{
		{},
		{Page: "library", Tab: "kids"},
		{Page: "History", Tab: "listens"},
		{Page: "downloads", Filter: "x"},
		{Page: "queue"},
	}
	for _, o := range ok {
		if err := o.Resolve(context.Background(), nil); err != nil {
			t.Errorf("%+v: %v", o, err)
		}
	}
	if o := (Options{Page: "history", Tab: "top"}); o.Resolve(context.Background(), nil) != nil || o.start.tab != 1 {
		t.Errorf("history top tab = %+v", o.start)
	}
	bad := []Options{
		{Page: "nowhere"},
		{Page: "artist"},
		{Page: "album", Artist: "x"},
		{Page: "queue", Tab: "x"},
		{Page: "queue", Filter: "x"},
		{Page: "history", Filter: "x"},
		{Page: "library", Album: "x"},
		{Page: "home", Artist: "x"},
		{Page: "home", Catalog: "mine"},
		{Page: "library", Tab: "nope"},
	}
	for _, o := range bad {
		err := o.Resolve(context.Background(), nil)
		var se *StartError
		if !errors.As(err, &se) {
			t.Errorf("%+v: want a StartError, got %v", o, err)
		}
	}
}
