# gooseneck-player

A terminal UI for browsing the `artists.json` catalog and streaming artists
straight into a media player via `yt-dlp`.

## Install

Detects your OS/arch and grabs the matching binary from the latest release:

```sh
curl -fsSL https://raw.githubusercontent.com/lubabs770/gooseneck/main/install.sh | sh
```

Installs to `~/.local/bin` (override with `BIN_DIR=`, pin with `VERSION=vX.Y.Z`).
Windows: download the `.exe` from the [releases page](https://github.com/lubabs770/gooseneck/releases).
Needs a published release — see [Build player](../.github/workflows/build-player.yml) CI (tag `v*` to cut one). Runtime deps: `yt-dlp` + `mpv`.

## Build

```sh
cd player
go build -o gooseneck-player .
```

Requires Go 1.23+. Runtime deps: `yt-dlp` and a player (`mpv` by default).

## Run

```sh
goose            # or: gooseneck-player
```

No data file needed: on first run it downloads the catalog from the skmusic
worker (`artists_url`) and caches it at `~/.config/gooseneck/artists.json`. A
local `artists.json` next to the binary or in the repo root is used instead when
present (dev convenience). Override the path via `artists_json` in the config.

## Config

Written on first run to `~/.config/gooseneck/config.toml`:

```toml
bin_dir      = ""            # directory holding yt-dlp; "" = use $PATH
player       = "mpv"         # media player; $APP env var overrides this
player_args  = ["--no-video"] # audio-only by default
artists_json = ""            # explicit path; "" = auto-detect/download
artists_url  = "https://skmusic.shalomkarr.workers.dev/data/artists.json"
theme        = "default"     # default | gruvbox | nord | mono
view         = "grid"        # grid | list
show_help    = true          # show the key-hints line at the bottom
```

The SQLite cache (`~/.config/gooseneck/cache.db`) stores each artist's track list
so re-opening an artist is instant. Delete the file to force a refresh.

## Keys

| Key | Action |
|-----|--------|
| `h j k l` | move (arrows work too) |
| `enter` | open artist → track list |
| `⌫` / `h` (list view) | back |
| `p` | play — on an artist queues all their tracks; on a track plays that one |
| `/` | fuzzy filter (fzf-style); `esc` clears |
| `v` | toggle grid / list |
| `t` | cycle theme |
| `g` / `G` | top / bottom |
| `ctrl-d` / `ctrl-u` | half-page down / up |
| `q` | quit |

## How playback works

Each artist entry is a YouTube Music "- Topic" channel id. The app resolves the
channel's uploads with `yt-dlp` (cached in SQLite), then hands the watch URLs to
the player. `mpv --no-video` streams audio only.

## Roadmap

- **Artist profile pics** — each artist has a `thumbnail` URL in the catalog;
  render it in the grid via a terminal image protocol (kitty / sixel / iTerm2),
  falling back to the text card where unsupported.
- **Album grouping** — the schema already has an `albums` table (channel
  *releases*), unused for now; most of these Topic channels expose no releases
  tab, so v1 is a flat artist → tracks view. Layer albums in for artists that
  have it.
