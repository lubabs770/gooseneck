package catalog

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/lubabs770/gooseneck/player/internal/config"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "artists.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadShapes(t *testing.T) {
	c, err := Read(write(t, `{"count":2,"artists":[{"id":"b","name":"Beta"},{"id":"a","name":"alpha"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Len() != 2 || c.Artists[0].Name != "alpha" {
		t.Fatalf("want sorted [alpha Beta], got %+v", c.Artists)
	}
	c, err = Read(write(t, `[{"id":"z","name":"Zed","isFemale":true},{"id":"","name":"no id"},{"id":"z","name":"dupe"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Len() != 1 || !c.Artists[0].IsFemale {
		t.Fatalf("bare array: %+v", c.Artists)
	}
	if _, err := Read(write(t, `not json`)); err == nil {
		t.Fatal("garbage should error")
	}
}

func TestLoadDownloadsWhenMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"count":1,"artists":[{"id":"x","name":"Ex"}]}`))
	}))
	defer srv.Close()
	t.Setenv("GOOSENECK_HOME", t.TempDir())
	chdir(t, t.TempDir()) // no repo-local artists.json
	c, err := Load(config.Config{ArtistsURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if c.Len() != 1 || c.Path != config.CatalogCachePath() {
		t.Fatalf("got %d artists from %s", c.Len(), c.Path)
	}
}

func TestDownloadRejectsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>nope</html>`))
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "a.json")
	if err := Download(srv.URL, dest); err == nil {
		t.Fatal("want error for HTML")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("bad download should not create the file")
	}
}

func sample() *Catalog {
	return New([]Artist{
		{ID: "1", Name: "Avi Perets", IsIsraeli: true},
		{ID: "2", Name: "Shlomo Gavriel"},
		{ID: "3", Name: "8th Day", IsChasid: true, IsFamous: true},
		{ID: "4", Name: "Avraham Fried", IsChasid: true, IsFamous: true},
		{ID: "5", Name: "Avremi G"},
		{ID: "6", Name: "Mordechai Ben David", IsFamous: true},
	})
}

func TestSearchRanksPrefixFirst(t *testing.T) {
	got := sample().Search("avi", nil)
	if len(got) == 0 || got[0].Name != "Avi Perets" {
		t.Fatalf("Search(avi) = %+v", got)
	}
	tag, _ := FindTag("chasidish")
	got = sample().Search("", tag)
	if len(got) != 2 {
		t.Fatalf("tag filter: %+v", got)
	}
}

func TestResolve(t *testing.T) {
	c := sample()
	for q, want := range map[string]string{
		"3":         "8th Day",             // by id
		"8th day":   "8th Day",             // exact, case-folded
		"8th":       "8th Day",             // unique prefix
		"ben david": "Mordechai Ben David", // single fuzzy hit
		"Avraham":   "Avraham Fried",
	} {
		a, err := c.Resolve(q)
		if err != nil || a.Name != want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", q, a.Name, err, want)
		}
	}
	var amb *AmbiguousError
	if _, err := c.Resolve("Av"); !errors.As(err, &amb) || len(amb.Candidates) < 2 {
		t.Errorf("Resolve(Av) should be ambiguous, got %v", err)
	}
	var nf *NotFoundError
	if _, err := c.Resolve("zzzzqq"); !errors.As(err, &nf) {
		t.Errorf("want not found, got %v", err)
	}
}

func TestTags(t *testing.T) {
	if _, err := FindTag("bogus"); err == nil {
		t.Fatal("bogus tag should error")
	}
	if tg, err := FindTag("all"); tg != nil || err != nil {
		t.Fatal("all means no filter")
	}
	a := Artist{IsChasid: true, IsFamous: true}
	if got := a.TagKeys(); len(got) != 2 {
		t.Fatalf("TagKeys = %v", got)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

func TestLoadAllMergesCatalogs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOOSENECK_HOME", home)
	main := write(t, `{"count":2,"artists":[{"id":"UCaaaaaaaaaaaaaaaaaaaaaa","name":"Alpha","isFamous":true},{"id":"UCbbbbbbbbbbbbbbbbbbbbbb","name":"Beta"}]}`)
	mine := filepath.Join(home, "mine.json")
	if err := Write(mine, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := AddTo(mine, Artist{ID: "UCbbbbbbbbbbbbbbbbbbbbbb", Name: "Beta (mine)", IsChasid: true}); err != nil {
		t.Fatal(err)
	}
	if isNew, err := AddTo(mine, Artist{ID: "UCcccccccccccccccccccccc", Name: "Gamma"}); err != nil || !isNew {
		t.Fatal("AddTo new", err)
	}
	cfg := config.Config{ArtistsJSON: main, Catalogs: []config.CatalogSource{
		{Name: "mine", Path: mine},
		{Name: "broken", Path: filepath.Join(home, "missing.json")},
	}}
	c, err := LoadAll(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c.Len() != 3 || len(c.Sources) != 3 {
		t.Fatalf("merged %d artists from %d sources", c.Len(), len(c.Sources))
	}
	b, _ := c.Get("UCbbbbbbbbbbbbbbbbbbbbbb")
	if b.Name != "Beta" || !b.IsChasid || !b.In(MainName) || !b.In("mine") {
		t.Fatalf("merged Beta = %+v", b)
	}
	if c.Sources[2].Err == "" || !c.Sources[1].Editable || c.Sources[1].Count != 2 {
		t.Fatalf("sources = %+v", c.Sources)
	}
	sub, err := c.Subset("mine")
	if err != nil || sub.Len() != 2 {
		t.Fatalf("Subset(mine) = %v, %v", sub, err)
	}
	if _, err := c.Subset("nope"); err == nil {
		t.Fatal("unknown subset should error")
	}
	if removed, _ := RemoveFrom(mine, "UCcccccccccccccccccccccc"); !removed {
		t.Fatal("RemoveFrom")
	}
}

func TestChannelIDAndNames(t *testing.T) {
	for in, want := range map[string]string{
		"UC_dRX63mhvOrenBjYWJx9mA": "UC_dRX63mhvOrenBjYWJx9mA",
		"https://music.youtube.com/channel/UC_dRX63mhvOrenBjYWJx9mA?si=x": "UC_dRX63mhvOrenBjYWJx9mA",
		"https://www.youtube.com/channel/UC_dRX63mhvOrenBjYWJx9mA/videos": "UC_dRX63mhvOrenBjYWJx9mA",
		"Yaakov Shwekey": "",
	} {
		if got := ChannelID(in); got != want {
			t.Errorf("ChannelID(%q) = %q", in, got)
		}
	}
	if ValidName("mine") != nil || ValidName("Mine!") == nil || ValidName(MainName) == nil {
		t.Fatal("ValidName")
	}
}
