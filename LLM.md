# LLM.md — orientation for AI agents

Guide for an LLM/agent working in this repo. Read this before editing. Keep it
current when architecture changes.

## What this is

`gooseneck` = a Jewish-music catalog plus a terminal player with a TUI **and** a
full CLI (`goose`).

- **`artists.json`** — the catalog (~1650 artists), shape `{"count":N,"artists":[...]}`.
  Each artist: `id` (a YouTube Music channel id), `name`, `thumbnail`, and
  boolean tags (`isFemale`, `isChasid`, …). Refreshed daily by CI, not hand-edited.
- **`player/`** — the Go module (binary `gooseneck-player`, alias `goose`).
- **`install.sh`** — one-line installer for release binaries.
- **`demo/`** — VHS tapes + setup script that render the README GIFs.

## Architecture (`player/`)

```
main.go                  -> cli.Execute(version)
internal/cli             cobra commands; `goose` with no args (on a TTY) opens the TUI
internal/tui             Bubble Tea app (OpenCode-style look)
internal/library         THE service layer: catalog + cache + YouTube Music + player + history
internal/ytm             read-only YouTube Music InnerTube client (artist page, album page)
internal/mpv             headless mpv daemon over JSON IPC (play/queue/pause/seek/…)
internal/store           SQLite: response cache (kv), track metadata, play history
internal/catalog         artists.json load/download, tags, fuzzy search, name resolution
internal/art             artist/album art download + disk cache (`goose cache warm`)
internal/config          TOML config + every on-disk path
internal/bidi            Hebrew (RTL) visual reordering for terminals
```

**Rule: every TUI action must be reachable from the CLI.** Both call
`internal/library`; never put data/playback logic in `tui` or `cli` only.

### Data flow

1. `catalog.Load` resolves artists.json (explicit path → repo-local → cached
   download → download from `artists_url`).
2. Opening an artist calls `library.Artist` → `ytm.Client.Artist` (one POST to
   `music.youtube.com/youtubei/v1/browse`, ~0.5s) → albums, singles (follows the
   shelf's "more" link for the full discography), top songs, bio, related
   artists. Cached in SQLite `kv` for 72h; stale copies are served when offline.
3. Albums: `library.Album` → `ytm.Client.Album` (tracklist with durations),
   cached 30 days.
4. Fallback: channels without a YouTube Music artist page get one synthetic
   "All uploads" album from `yt-dlp --flat-playlist` (`library/ytdlp.go`).
5. Playback: `library.Play` stores track metadata (`track_meta`), loads the
   video ids into mpv (`mpv.Player.Load`, modes Replace/Append/Next) and logs
   a history row (`plays`).

### Playback daemon (`internal/mpv`)

- First play starts `mpv --idle --no-video --input-ipc-server=<sock>` detached
  (`Setsid`), socket at `$XDG_RUNTIME_DIR/gooseneck/mpv.sock` (or
  `$GOOSENECK_HOME/run/`). It outlives the TUI on purpose; `goose stop` /
  `goose quit-player` end it.
- Every process (TUI, CLI) talks to the same socket; one connection per batch
  of commands (`Player.Batch`), unsolicited events are skipped.
- Non-mpv players and Windows fall back to fire-and-forget (`Managed() == false`).

### TUI (`internal/tui`)

- `App` is used **by pointer**, so `View` may mutate bookkeeping (scroll
  offsets, wanted images). The old value-copy trap is gone; don't reintroduce
  value receivers on `App`.
- Pages implement `page` (title/mode/update/view/hints); searchable ones also
  implement `filterable`. Stack navigation: `push` / `pop` / `goTo`.
- `collection` is the one grid/list widget (cursor, fuzzy filter, scrolling,
  cards with art, list rows). Reuse it for any new list.
- **Backgrounds**: `paint(block, bg, w, h)` pads to a rectangle and re-applies
  the background after every SGR reset. Paint inner surfaces first, then outer.
  Dialogs are spliced on top with `overlay`.
- **Images**: `images.block()` returns art for a URL at a cell size or an
  initials tile; misses are queued during render and fetched on the next
  update (`requests`). Kitty uploads are written **directly to the terminal**
  through `syncWriter` (between frames), because Bubble Tea drops intermediate
  frames and an upload embedded in a dropped frame is lost. Half-block art is
  the fallback (also what tmux and VHS show).
- Themes are OpenCode-style token sets in `theme.go` (`ThemeNames`, `GetTheme`).
  `ResolveThemeName` maps the config value: `auto` (default), `""` and unknown
  names → `omarchy` when Omarchy is installed, else `opencode`.
- **Omarchy** (`omarchy.go`): reads `colors.toml` (fallback `alacritty.toml`)
  from `~/.local/state/omarchy/current/theme` (Omarchy 4) or
  `~/.config/omarchy/current/theme` (Omarchy 3); `GOOSENECK_OMARCHY_THEME`
  overrides (tests set it to an empty dir to disable detection). Live reload:
  every tick compares `omarchySignature()` (resolved path + mtimes) and calls
  `setTheme("omarchy")` on change. Never run `omarchy theme set` in tests — it
  restyles the user's whole desktop; point the override at a temp copy instead.

## Build / run / test

```sh
cd player
go build -o gooseneck-player .     # Go 1.23+, CGO_ENABLED=0 works (pure-Go deps)
go vet ./... && go test ./...
GOOSENECK_HOME=/tmp/gn ./gooseneck-player   # isolated config/cache/socket dir
```

Runtime deps: `yt-dlp` + `mpv`. `goose doctor` checks everything.

## Conventions & gotchas

- **Pure Go, no cgo** (`modernc.org/sqlite`). Keep `CGO_ENABLED=0` cross-builds working.
- **Go 1.23 compatibility**: CI reads the version from `go.mod`; don't use
  newer stdlib APIs (e.g. `testing.T.Chdir`).
- **Don't build all targets locally** — CI cross-compiles.
- **RTL:** terminals don't do BiDi. Pass display strings through `bidi.Vis()`.
- **Config is forward-compatible:** new fields need a backfill in
  `config.normalize`; `*bool` fields are tri-state (nil → default).
- **CLI args that start with `-`** (`goose seek -10`, `goose config set player_args --no-video`)
  need `DisableFlagParsing` on that command.
- **Testing the TUI:** `tmux new-session -d -x 130 -y 40 'GOOSENECK_HOME=… ./gooseneck-player'`
  then `send-keys` / `capture-pane -p`. tmux and VHS can't show Kitty graphics.
- **ytm parsing** walks renderer trees defensively (`get`/`str`/`findKey`).
  When YouTube changes shapes, refresh the gzipped fixtures in
  `internal/ytm/testdata/` and fix the parser against them.

## Demo GIFs

`demo/setup.sh` builds goose into `/tmp/goose-demo/bin` and prepares an
isolated, pre-warmed home there (silent `--ao=null` playback, seeded history);
`vhs demo/tui.tape` / `vhs demo/cli.tape` render `demo.gif` / `cli.gif` at the
repo root. Tapes hide their setup and use a bare `$ ` prompt — never show a
username, hostname, home path or the user's real history in a recording. VHS's
browser terminal lags on heavy half-block output, so give image-heavy screens a
few seconds before moving on. Re-render whenever the UI changes visibly.

## CI & releases

- `fetch-artists.yml` — daily cron refreshes `artists.json` (commits to main;
  `git pull --rebase` before pushing).
- `build-player.yml` — vet + test, then cross-compiles 6 targets on changes
  under `player/**`; a `v*` tag publishes a release. Version is stamped via
  `-X main.version`. Keep large assets (gif/png) out of `player/` so they don't
  trip the path filter.
