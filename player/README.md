# gooseneck-player

A terminal UI for browsing the `artists.json` catalog and streaming artists
straight into a media player via `yt-dlp`.

## Build

```sh
cd player
go build -o gooseneck-player .
```

Requires Go 1.23+. Runtime deps: `yt-dlp` and a player (`mpv` by default).

## Run

```sh
./gooseneck-player
```

It auto-detects `artists.json` next to the binary or in the repo root (one dir
up). Override the path in the config file if needed.

## Config

Written on first run to `~/.config/gooseneck/config.toml`:

```toml
bin_dir      = ""            # directory holding yt-dlp; "" = use $PATH
player       = "mpv"         # media player; $APP env var overrides this
player_args  = ["--no-video"] # audio-only by default
artists_json = ""            # path to artists.json; "" = auto-detect
theme        = "default"     # default | gruvbox | nord | mono
view         = "grid"        # grid | list
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

The schema already has an `albums` table (channel *releases*), unused for now —
most of these Topic channels expose no releases tab, so v1 is a flat
artist → tracks view. Album grouping can be layered in later for artists that
have it.
