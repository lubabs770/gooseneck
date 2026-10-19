package scrobble

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestLocal(t *testing.T) {
	lo, err := OpenLocal(filepath.Join(t.TempDir(), "sub", "scrobbles.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer lo.Close()
	now := time.Now()
	ls := []Listen{
		{VideoID: "a", Artist: "8th Day", Track: "Lucky", Album: "Lucky", Duration: 195, StartedAt: now.Add(-3 * time.Hour)},
		{VideoID: "b", Artist: "8th Day", Track: "Rollin", Album: "Lucky", Duration: 207, StartedAt: now.Add(-2 * time.Hour)},
		{VideoID: "a", Artist: "8th Day", Track: "Lucky", Album: "Lucky", Duration: 195, StartedAt: now.Add(-1 * time.Hour)},
		{VideoID: "c", Artist: "Avraham Fried", Track: "Aderaba", Duration: 240, StartedAt: now.Add(-40 * 24 * time.Hour)},
	}
	if err := lo.Scrobble(context.Background(), ls); err != nil {
		t.Fatal(err)
	}
	// resubmitting the same play is a no-op
	if err := lo.Scrobble(context.Background(), ls[:1]); err != nil {
		t.Fatal(err)
	}
	all, _ := lo.Recent(Range{}, 0)
	if len(all) != 4 || all[0].Track != "Lucky" || !all[0].StartedAt.Equal(time.Unix(now.Add(-time.Hour).Unix(), 0)) {
		t.Fatalf("Recent = %+v", all)
	}
	month, _ := ParseSince("month", now)
	top, _ := lo.Top("artist", month, 5)
	if len(top) != 1 || top[0].Name != "8th Day" || top[0].Listens != 3 {
		t.Fatalf("top artists this month = %+v", top)
	}
	tracks, _ := lo.Top("track", Range{}, 5)
	if tracks[0].Name != "Lucky" || tracks[0].Listens != 2 || tracks[0].Seconds != 390 {
		t.Fatalf("top tracks = %+v", tracks)
	}
	tot, _ := lo.Totals(Range{})
	if tot.Listens != 4 || tot.Artists != 2 || tot.Tracks != 3 {
		t.Fatalf("totals = %+v", tot)
	}
	if _, err := lo.Top("genre", Range{}, 1); err == nil {
		t.Fatal("bad top kind should error")
	}
	for _, s := range []string{"today", "week", "year", "all", "7d", "12h"} {
		if _, err := ParseSince(s, now); err != nil {
			t.Errorf("ParseSince(%q): %v", s, err)
		}
	}
	if _, err := ParseSince("fortnight", now); err == nil {
		t.Error("bogus period should error")
	}
}
