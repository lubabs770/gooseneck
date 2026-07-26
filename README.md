# gooseneck

Jewish music catalog (`artists.json`, ~1600 artists) plus a terminal player that
streams any artist straight into `mpv` via `yt-dlp`.

![gooseneck-player demo](demo.gif)

## Install

Detects your OS/arch and grabs the matching binary from the latest release:

```sh
curl -fsSL https://raw.githubusercontent.com/lubabs770/gooseneck/main/install.sh | sh
```

Installs two commands — **`gooseneck-player`** and the short alias **`goose`** —
to `~/.local/bin` (override with `BIN_DIR=`, pin a version with `VERSION=vX.Y.Z`).
Windows: download the `.exe` from the
[releases page](https://github.com/lubabs770/gooseneck/releases).
Runtime deps: `yt-dlp` + `mpv`.

On first run it auto-downloads the catalog from the skmusic worker and caches it
in `~/.config/gooseneck/` — no `artists.json` needed. Just run `goose`.

## What's here

- **`artists.json`** — the catalog, refreshed daily by a GitHub Action
  ([`fetch-artists.yml`](.github/workflows/fetch-artists.yml)) that pulls from the
  skmusic worker.
- **`player/`** — a Bubble Tea TUI: grid/list views with artist profile pics
  (real bitmap images on Kitty/Ghostty via the Kitty graphics protocol, truecolor
  half-block fallback elsewhere), vim motions, fzf-style fuzzy filter, themes;
  drills artist → albums → tracks and plays audio-only. See
  [`player/README.md`](player/README.md).
- **CI** — [`build-player.yml`](.github/workflows/build-player.yml) cross-compiles
  the player for linux/macOS/windows (amd64+arm64) on every change, and publishes
  a release with all binaries on a `v*` tag.

## Build from source

```sh
cd player && go build -o gooseneck-player .
```

Requires Go 1.23+.

## Config

Written to `~/.config/gooseneck/config.toml` on first run (edit and restart):

```toml
bin_dir      = ""             # directory holding yt-dlp; "" = use $PATH
player       = "mpv"          # media player; $APP env var overrides this
player_args  = ["--no-video"] # audio-only by default
artists_json = ""             # explicit catalog path; "" = auto-detect/download
artists_url  = "https://skmusic.shalomkarr.workers.dev/data/artists.json"
theme        = "default"      # default | gruvbox | nord | mono
view         = "grid"         # grid | list
show_help    = true           # show the key-hints line (toggle live with ?)
caret        = true           # show a ›caret on the selection
thumbnails   = true           # artist profile pics in the grid (toggle live with i)
thumb_height = 0              # profile-pic height in rows; 0 = auto (square cards)
```

See [`player/README.md`](player/README.md#config) for field details, keybindings,
and how profile pics render (real Kitty bitmaps vs. half-block fallback).

## Roadmap

- **Faster album indexing** — first-time album grouping does a full `yt-dlp`
  extraction per track; explore batching or a lighter metadata source.
- **`logs.json` play history** — track play frequency to find the most-listened
  artists, so a background job can pre-index / refresh their catalogs on a
  schedule and keep hot artists warm in the cache.

See [`player/README.md`](player/README.md#roadmap) for details.
