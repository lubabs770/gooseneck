// Package selfupdate replaces the running gooseneck binary with one from a
// GitHub release (`goose upgrade`). It downloads the asset for this OS/arch,
// checks it against the release's checksums.txt, makes sure it runs, then
// swaps it in with a rename so a failed upgrade never leaves a broken binary.
package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// Repo is the GitHub repository releases come from.
	Repo = "lubabs770/gooseneck"
	// App is the binary name; assets are named App-GOOS-GOARCH[.exe].
	App = "gooseneck-player"
	// ChecksumsAsset lists "sha256  filename" for every asset of a release.
	ChecksumsAsset = "checksums.txt"
)

// Release is the part of a GitHub release we need.
type Release struct {
	Tag    string            `json:"tag"`
	URL    string            `json:"url"`
	Assets map[string]string `json:"-"` // asset name -> download URL
}

// Updater talks to the GitHub releases API. The zero value is not usable; use New.
type Updater struct {
	API    string // e.g. https://api.github.com/repos/lubabs770/gooseneck
	Client *http.Client
}

// New returns an Updater for the gooseneck repo. GOOSENECK_RELEASES_API
// overrides the API base (tests point it at a local server).
func New() *Updater {
	api := os.Getenv("GOOSENECK_RELEASES_API")
	if api == "" {
		api = "https://api.github.com/repos/" + Repo
	}
	return &Updater{API: strings.TrimRight(api, "/"), Client: &http.Client{Timeout: 2 * time.Minute}}
}

// AssetName is the release asset for a platform.
func AssetName(goos, goarch string) string {
	name := App + "-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// Latest returns the newest published (non-draft, non-prerelease) release.
func (u *Updater) Latest(ctx context.Context) (Release, error) {
	return u.release(ctx, u.API+"/releases/latest")
}

// ByTag returns a specific release, e.g. "v0.4.0". A missing "v" is added.
func (u *Updater) ByTag(ctx context.Context, tag string) (Release, error) {
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	return u.release(ctx, u.API+"/releases/tags/"+url.PathEscape(tag))
}

func (u *Updater) release(ctx context.Context, endpoint string) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gooseneck")
	resp, err := u.Client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("checking releases: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Release{}, errors.New("release not found")
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return Release{}, errors.New("GitHub API rate limit hit; try again later")
	case resp.StatusCode != http.StatusOK:
		return Release{}, fmt.Errorf("checking releases: GitHub returned %s", resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("checking releases: %w", err)
	}
	if body.TagName == "" {
		return Release{}, errors.New("checking releases: response has no tag")
	}
	rel := Release{Tag: body.TagName, URL: body.HTMLURL, Assets: map[string]string{}}
	for _, a := range body.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

// IsDev reports whether v is not a release version (a local or CI dev build).
func IsDev(v string) bool {
	_, ok := parse(v)
	return !ok
}

// Newer reports whether release tag latest is newer than version current.
// A dev build is never considered up to date.
func Newer(current, latest string) bool {
	c, okC := parse(current)
	l, okL := parse(latest)
	if !okL {
		return false
	}
	if !okC {
		return true
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// parse reads "v1.2.3" (pre-release / build suffixes ignored).
func parse(v string) ([3]int, bool) {
	var out [3]int
	v, ok := strings.CutPrefix(strings.TrimSpace(v), "v")
	if !ok {
		return out, false
	}
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Executable is the real path of the running binary (the `goose` symlink resolved).
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// Install downloads rel's asset for this platform and replaces exe with it.
func (u *Updater) Install(ctx context.Context, rel Release, exe string) error {
	name := AssetName(runtime.GOOS, runtime.GOARCH)
	assetURL, ok := rel.Assets[name]
	if !ok {
		return fmt.Errorf("%s has no build for %s/%s", rel.Tag, runtime.GOOS, runtime.GOARCH)
	}
	sumsURL, ok := rel.Assets[ChecksumsAsset]
	if !ok {
		return fmt.Errorf("%s has no %s, so the download can't be verified; reinstall with install.sh", rel.Tag, ChecksumsAsset)
	}
	want, err := u.checksum(ctx, sumsURL, name)
	if err != nil {
		return err
	}

	// The temp file sits next to exe so the final rename stays on one filesystem.
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".gooseneck-upgrade-*")
	if err != nil {
		return fmt.Errorf("can't write to %s (%w); re-run with the permissions that installed it, or reinstall with install.sh", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed into place

	h := sha256.New()
	err = u.download(ctx, assetURL, io.MultiWriter(tmp, h))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("downloading %s: %w", name, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch for %s (got %s, want %s); nothing was changed", name, got, want)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	if err := smokeTest(ctx, tmpPath); err != nil {
		return fmt.Errorf("the new binary doesn't run (%w); nothing was changed", err)
	}
	return replace(tmpPath, exe)
}

// checksum finds name's sha256 in a checksums.txt.
func (u *Updater) checksum(ctx context.Context, sumsURL, name string) (string, error) {
	var sb strings.Builder
	if err := u.download(ctx, sumsURL, &sb); err != nil {
		return "", fmt.Errorf("downloading %s: %w", ChecksumsAsset, err)
	}
	for _, line := range strings.Split(sb.String(), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("%s has no entry for %s", ChecksumsAsset, name)
}

func (u *Updater) download(ctx context.Context, from string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, from, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "gooseneck")
	resp, err := u.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub returned %s", resp.Status)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// smokeTest runs `<bin> version` so a binary that can't start never replaces a working one.
func smokeTest(ctx context.Context, bin string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "version").Output()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(out), "gooseneck ") {
		return fmt.Errorf("unexpected `version` output %q", strings.TrimSpace(string(out)))
	}
	return nil
}

// replace moves src over dst. Windows can't overwrite a running .exe but can
// rename it, so the old one is moved aside first (and cleaned up next time).
func replace(src, dst string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(src, dst)
	}
	old := dst + ".old"
	os.Remove(old)
	if err := os.Rename(dst, old); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		os.Rename(old, dst) // put the working binary back
		return err
	}
	return nil
}
