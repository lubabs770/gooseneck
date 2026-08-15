package main

import "testing"

func TestAlbumKey(t *testing.T) {
	if got := albumKey("chan1", "Greatest Hits"); got != "chan1::Greatest Hits" {
		t.Errorf("albumKey = %q, want %q", got, "chan1::Greatest Hits")
	}
}

func TestGroupCatalog(t *testing.T) {
	const ch = "CH"

	t.Run("empty", func(t *testing.T) {
		albums, tracks := groupCatalog(ch, nil)
		if len(albums) != 0 || len(tracks) != 0 {
			t.Fatalf("expected empty result, got %d albums, %d track sets", len(albums), len(tracks))
		}
	})

	t.Run("skips entries without id", func(t *testing.T) {
		albums, _ := groupCatalog(ch, []fullEntry{
			{ID: "", Title: "ghost", Album: "Real"},
		})
		if len(albums) != 0 {
			t.Fatalf("entry with empty id should be skipped, got %d albums", len(albums))
		}
	})

	t.Run("groups by album preserving first-seen order", func(t *testing.T) {
		albums, tracks := groupCatalog(ch, []fullEntry{
			{ID: "1", Title: "b-song1", Album: "Beta"},
			{ID: "2", Title: "a-song1", Album: "Alpha"},
			{ID: "3", Title: "b-song2", Album: "Beta"},
			{ID: "4", Title: "a-song2", Album: "Alpha"},
		})
		if len(albums) != 2 {
			t.Fatalf("expected 2 albums, got %d", len(albums))
		}
		// Beta seen first, so it ranks first.
		if albums[0].Title != "Beta" || albums[1].Title != "Alpha" {
			t.Fatalf("first-seen order broken: %q, %q", albums[0].Title, albums[1].Title)
		}
		if key := albumKey(ch, "Beta"); albums[0].PlaylistID != key || len(tracks[key]) != 2 {
			t.Fatalf("Beta tracks wrong: key=%q got %d", key, len(tracks[albumKey(ch, "Beta")]))
		}
	})

	t.Run("collapses singles into one bucket", func(t *testing.T) {
		albums, tracks := groupCatalog(ch, []fullEntry{
			// Empty album -> single.
			{ID: "1", Title: "Loner", Album: ""},
			// Album name equals title (case-insensitive) -> single.
			{ID: "2", Title: "Solo Track", Album: "solo track"},
			// Real album with a distinct name -> stays an album.
			{ID: "3", Title: "track a", Album: "Record"},
			{ID: "4", Title: "track b", Album: "Record"},
		})
		if len(albums) != 2 {
			t.Fatalf("expected album + singles bucket, got %d", len(albums))
		}
		// Real album comes before the singles bucket (singles appended last).
		if albums[0].Title != "Record" {
			t.Fatalf("expected Record first, got %q", albums[0].Title)
		}
		if albums[1].Title != "Singles (2)" {
			t.Fatalf("singles bucket title = %q, want %q", albums[1].Title, "Singles (2)")
		}
		singlesKey := albumKey(ch, "Singles")
		if albums[1].PlaylistID != singlesKey || len(tracks[singlesKey]) != 2 {
			t.Fatalf("singles bucket key/tracks wrong: %+v", albums[1])
		}
	})
}
