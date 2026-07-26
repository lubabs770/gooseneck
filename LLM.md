# LLM.md — orientation for AI agents

Guide for an LLM/agent working in this repo. Read this before editing. Keep it
current when architecture changes.

## What this is

`gooseneck` = a Jewish-music catalog plus a terminal player.

- **`artists.json`** — the catalog (~1600 artists), shape `{"count":N,"artists":[...]}`.
  Each artist: `id` (a YouTube Music "- Topic" channel id), `name`, `thumbnail`
  (a googleusercontent image URL), and boolean tags (`isFemale`, `isChasid`, …).
  Refreshed daily by CI, not hand-edited.
- **`player/`** — a Go / Bubble Tea TUI that browses the catalog and streams an
  artist/album/track into a media player (`mpv`) via `yt-dlp`.
- **`install.sh`** — one-line installer; OS/arch-detects and pulls the matching
  release binary into `~/.local/bin` as `gooseneck-player` + `goose`.

## Player architecture (`player/`, package `main`)

| File | Responsibility |
|------|----------------|
| `main.go` | Entry: load config → catalog → cache → run Bubble Tea program. Emits Kitty image-delete on exit. |
| `config.go` | `Config` (TOML at `~/.config/gooseneck/config.toml`), defaults, path helpers. New fields must be backfilled in `loadConfig` for older configs. |
| `data.go` | `Artist` struct + `loadArtists` (path → local → cached download → HTTP download). Every load path validates `json.Valid`. |
| `cache.go` | SQLite album/track cache (`~/.config/gooseneck/cache.db`), pure-Go driver. |
| `ytdlp.go` | `yt-dlp` calls: flat listing, full catalog extraction, album grouping, streaming progress. |
| `play.go` | Spawns the media player with watch URLs, detached. |
| `log.go` | Play-history logging to `logs.json` (`logPlay`); `topArtists` ranks by frequency. |
| `theme.go` | Themes + derived lipgloss styles. |
| `display.go` | `vis()` — BiDi visual reordering so RTL (Hebrew) renders correctly in terminals. |
| `thumbs.go` | Thumbnail fetch, square-crop URL rewrite, center-crop, half-block render, PNG encode. |
| `kitty.go` | Kitty graphics protocol: capability detect, transmit, virtual placement, Unicode-placeholder blocks. |
| `ui.go` | The Bubble Tea `model`, update loop, navigation stack, grid/list rendering, thumbnail orchestration. |

### Data flow

1. `loadArtists` resolves the catalog; artists become `item`s on the root
   `artistsScreen` level.
2. Drilling an artist runs `yt-dlp` to extract uploads, groups tracks into albums
   by each track's `album` field (singles collapse into one bucket), caches the
   result in SQLite. First extraction is slow; re-opens are instant.
3. Screens are a stack of `level`s: artists → albums → tracks. `p` plays.
4. Every play appends an event (artist / album / track / time) to
   `~/.config/gooseneck/logs.json` via `logPlay`; `topArtists` tallies play
   frequency for future background pre-indexing.

### Profile pics (thumbnails)

- Source `thumbnail` URLs bake size into the googleusercontent suffix and are
  often wide banners. `squareThumbURL` rewrites to `=s{px}-c` for a uniform
  square center-crop.
- On **Kitty/Ghostty** (detected by env/`TERM`): real bitmaps via the Kitty
  graphics protocol with **Unicode placeholders** — image transmitted once as a
  zero-width escape in the View prefix (`kittyStream`), referenced by placeholder
  cells whose fg carries the image id and diacritic carries the row. Placeholder
  glyph measures width 1, so it composes into lipgloss cards like text.
- **Elsewhere**: truecolor half-block (`▀`) art (`renderThumb`).
- Only the visible page is fetched (lazy, off the UI thread); disk-cached under
  `~/.config/gooseneck/thumbs/` as `<id>.sq.img`.
- Geometry (cell size) is per-view: grid = near-square cards, list = compact
  squares. On a geometry change the prepared images/placements are invalidated
  and re-created; the fixed placement id (`p=1`) makes re-creation replace.

## Build / run / test

```sh
cd player
go build -o gooseneck-player .   # Go 1.23+, CGO_ENABLED=0 works (pure-Go deps)
go vet ./... && go test ./...
```

Runtime deps: `yt-dlp` + a player (`mpv` by default). No local artists.json
needed — it auto-downloads on first run.

## Conventions & gotchas

- **Pure Go, no cgo.** The SQLite driver is `modernc.org/sqlite`; keep
  `CGO_ENABLED=0` cross-compilation working. Prefer stdlib over new deps
  (thumbnail resize is hand-rolled nearest-neighbor to avoid `x/image`).
- **Don't build all targets locally** — CI cross-compiles. Building the single
  local package to check compilation is fine.
- **Bubble Tea receiver trap:** `Update` mutates via pointer-ish patterns but
  returns a value model. A method with a pointer receiver that mutates `m` must
  have its result captured *before* returning (e.g. `cmd := m.startIndexCmd(it);
  return m, cmd`). **`View` runs on a copy** — mutating a *value* field there does
  not persist (maps do, since they're references). This bit the thumbnail
  delete-all flag once; watch for it.
- **RTL:** terminals don't do BiDi. Always pass display strings through `vis()`.
- **Config is forward-compatible:** unknown TOML keys are ignored; add new fields
  with a nil/zero backfill in `loadConfig`. `*bool` fields encode tri-state
  defaults (nil → default true).
- **Testing the TUI:** a raw pty harness or `tmux send-keys`/`capture-pane` is
  more reliable than ad-hoc pty reads. **tmux and VHS cannot render Kitty
  graphics** — verify image output in a real Kitty/Ghostty terminal (or audit the
  raw escape bytes via pty), not in tmux/VHS.
- **`yt-dlp` "- Topic" channels** have no releases/videos/playlists tabs; use the
  bare channel (its Uploads playlist). Album metadata only comes from full
  per-track extraction (slow → cached).

## CI & releases

- `.github/workflows/fetch-artists.yml` — daily cron refreshes `artists.json`.
- `.github/workflows/build-player.yml` — cross-compiles 6 targets
  (linux/darwin/windows × amd64/arm64) on changes under `player/**`; a `v*` tag
  publishes a GitHub release with all binaries. **Keep large assets (gif/png) at
  the repo root, not under `player/`,** so they don't trip the build path filter.
- Cut a release: `git tag vX.Y.Z && git push origin vX.Y.Z`.
