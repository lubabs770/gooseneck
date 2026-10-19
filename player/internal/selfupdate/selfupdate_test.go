package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		cur, latest string
		want        bool
	}{
		{"v0.4.0", "v0.5.0", true},
		{"v0.5.0", "v0.5.0", false},
		{"v0.10.0", "v0.9.0", false},
		{"v0.9.9", "v0.10.0", true},
		{"v1.0.0", "v1.0.1-rc1", true},
		{"dev", "v0.1.0", true},
		{"dev-abc1234", "v0.1.0", true},
		{"v0.5.0", "garbage", false},
	}
	for _, c := range cases {
		if got := Newer(c.cur, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.cur, c.latest, got, c.want)
		}
	}
	if !IsDev("dev") || IsDev("v1.2.3") {
		t.Error("IsDev wrong")
	}
}

// fakeRelease serves a release whose asset for this platform is a shell
// script that answers `version` like the real binary.
func fakeRelease(t *testing.T, script string, sum string) *Updater {
	t.Helper()
	name := AssetName(runtime.GOOS, runtime.GOARCH)
	if sum == "" {
		h := sha256.Sum256([]byte(script))
		sum = hex.EncodeToString(h[:])
	}
	mux := http.NewServeMux()
	var base string
	release := func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v9.9.9",
			"html_url": "https://example.invalid/v9.9.9",
			"assets": []map[string]string{
				{"name": name, "browser_download_url": base + "/dl/" + name},
				{"name": ChecksumsAsset, "browser_download_url": base + "/dl/" + ChecksumsAsset},
			},
		})
	}
	mux.HandleFunc("/releases/latest", release)
	mux.HandleFunc("/releases/tags/v9.9.9", release)
	mux.HandleFunc("/dl/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(script)) })
	mux.HandleFunc("/dl/"+ChecksumsAsset, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sum + "  " + name + "\n0000  other-file\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	return &Updater{API: srv.URL, Client: srv.Client()}
}

func TestLatestAndByTag(t *testing.T) {
	u := fakeRelease(t, "x", "")
	ctx := context.Background()
	rel, err := u.Latest(ctx)
	if err != nil || rel.Tag != "v9.9.9" || len(rel.Assets) != 2 {
		t.Fatalf("Latest = %+v, %v", rel, err)
	}
	if rel, err = u.ByTag(ctx, "9.9.9"); err != nil || rel.Tag != "v9.9.9" {
		t.Fatalf("ByTag = %+v, %v", rel, err)
	}
	if _, err = u.ByTag(ctx, "v1.0.0"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("ByTag missing = %v", err)
	}
}

func setup(t *testing.T, script, sum string) (*Updater, Release, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the fake binary")
	}
	u := fakeRelease(t, script, sum)
	rel, err := u.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), App)
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return u, rel, exe
}

func TestInstall(t *testing.T) {
	script := "#!/bin/sh\necho gooseneck v9.9.9\n"
	u, rel, exe := setup(t, script, "")
	if err := u.Install(context.Background(), rel, exe); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != script {
		t.Fatalf("binary not replaced: %q", got)
	}
	if fi, _ := os.Stat(exe); fi.Mode().Perm()&0o111 == 0 {
		t.Fatal("new binary not executable")
	}
	assertClean(t, exe)
}

func TestInstallRejectsBadChecksum(t *testing.T) {
	u, rel, exe := setup(t, "#!/bin/sh\necho gooseneck v9.9.9\n", strings.Repeat("ab", 32))
	err := u.Install(context.Background(), rel, exe)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum error, got %v", err)
	}
	assertUntouched(t, exe)
}

func TestInstallRejectsBrokenBinary(t *testing.T) {
	u, rel, exe := setup(t, "#!/bin/sh\nexit 1\n", "")
	if err := u.Install(context.Background(), rel, exe); err == nil {
		t.Fatal("want error for a binary that doesn't run")
	}
	assertUntouched(t, exe)
}

func TestInstallNeedsChecksums(t *testing.T) {
	u, rel, exe := setup(t, "#!/bin/sh\necho gooseneck v9.9.9\n", "")
	delete(rel.Assets, ChecksumsAsset)
	if err := u.Install(context.Background(), rel, exe); err == nil {
		t.Fatal("want error without checksums.txt")
	}
	assertUntouched(t, exe)
}

func assertUntouched(t *testing.T, exe string) {
	t.Helper()
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Fatalf("binary changed on failure: %q", got)
	}
	assertClean(t, exe)
}

// assertClean checks no temp files were left beside the binary.
func assertClean(t *testing.T, exe string) {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Dir(exe))
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("leftover files: %v", names)
	}
}
