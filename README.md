# gooseneck

Jewish music catalog (`artists.json`, ~1600 artists) plus a terminal player that
streams any artist straight into `mpv` via `yt-dlp`.

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
- **`player/`** — a Bubble Tea TUI: grid/list views, vim motions, fzf-style
  fuzzy filter, themes; drills artist → tracks and plays audio-only. See
  [`player/README.md`](player/README.md).
- **CI** — [`build-player.yml`](.github/workflows/build-player.yml) cross-compiles
  the player for linux/macOS/windows (amd64+arm64) on every change, and publishes
  a release with all binaries on a `v*` tag.

## Build from source

```sh
cd player && go build -o gooseneck-player .
```

Requires Go 1.23+.

## Roadmap

- **Artist profile pics** — render each artist's `thumbnail` in the grid via a
  terminal image protocol (kitty / sixel / iTerm2), text-card fallback.
- **Album grouping** — use the dormant `albums` table for artists whose channel
  exposes a releases tab (most Topic channels don't, so v1 is flat
  artist → tracks).

See [`player/README.md`](player/README.md#roadmap) for details.
