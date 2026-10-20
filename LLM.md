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
internal/catalog         artists.json load/download, tags, fuzzy search, name resolution, multi-catalog merge
internal/art             artist/album art download + disk cache (`goose cache warm`)
internal/config          TOML config + every on-disk path
internal/bidi            Hebrew (RTL) visual reordering for terminals
internal/selfupdate      `goose upgrade`: GitHub release lookup, checksum check, atomic binary swap
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

### Downloads (`library/download.go`)

- `Lib.Download` runs yt-dlp (3 parallel, 3 attempts each — YouTube 403s are
  often transient) into `Cfg.Downloads()` (`download_dir`, else
  `$XDG_MUSIC_DIR/gooseneck`, or `$GOOSENECK_HOME/music` when that's set).
- Files are named `… [<videoId>].ext`; `mpv.VideoIDFromURL` parses that, so a
  local file is still recognised as the same track (now playing, queue).
- `Lib.Play` swaps in `LocalPath(id)` whenever the file exists.
- TUI: `dlState` in `downloads.go` runs one job at a time (more queue up),
  streaming `dlProgress` over a channel; `dlFinished` is forwarded to pages so
  they refresh their `offline` markers.

### Catalogs (`catalog/sources.go`)

- `catalog.LoadAll` = main catalog (`Load`) + every `[[catalog]]` in the
  config (`config.CatalogSource`: `Path` = personal/editable, `URL` = followed,
  cached at `<config dir>/catalogs/<name>.json`). Duplicates merge by id: first
  name/picture wins (main is first), tag flags are OR-ed, `Artist.Catalogs`
  lists every catalog that has it. A broken extra catalog only sets
  `Source.Err`; it never blocks startup.
- `Subset(name)` filters (CLI `--catalog`, TUI `c`); `AddTo`/`RemoveFrom`
  edit personal files atomically. `ytm.SearchArtists` (InnerTube `/search` with
  the artists filter) powers `goose catalog add-artist <name>`.
- `config.Keys()` skips struct-slice fields, so `[[catalog]]` can't be broken
  via `goose config set`.

### Radio (`library/radio.go`)

- `StartRadio` queues a first batch (`Radio(seed, n, exclude)`) and saves a
  `RadioState` in kv (`radio:state`): seed, played ids (≤2000), a pool of
  related catalog artists, recent seeds. `ExtendRadio` (called by the daemon
  every tick) appends 25 more when ≤3 tracks remain; `nextSeed` returns the
  original artist ~1/3 of the time, else a related artist not used in the last
  5 batches, and each new seed's relatives widen the pool.
- `Lib.Play` with `Mode == Replace && !Radio` calls `StopRadio`; top-ups use
  `NoLog` so they don't spam history. The daemon stops the radio when the
  player goes away.

### Daemon & scrobbling (`internal/scrobble`, `library/scrobbler.go`)

- One background process, `goose daemon` (hidden; `goose scrobble daemon` is
  an alias), does scrobbling *and* radio top-ups. `EnsureDaemon` spawns it when
  `daemonNeeded()` (any scrobble service, incl. local, or an active radio).
  Single instance via `flock` on `$RUNTIME/daemon.lock` (two can be spawned at
  once by back-to-back plays — the loser exits silently); status is served as
  JSON on `$RUNTIME/daemon.sock`. Log: `$RUNTIME/daemon.log`.

- `scrobble.Tracker` is pure logic (unit-tested): fed player snapshots, it
  emits NowPlaying / Scrobble events by wall-clock listening time (½ track or
  4 min, ≥30s tracks; gaps >15s ignored; repeat-one detected by the position
  jumping back to the start).
- `scrobble.Local` is a third `Service` backed by its own SQLite file
  (`Cfg.ScrobblesPath()`, default `<config dir>/scrobbles.db`, deliberately
  separate from cache.db). On by default (`local_scrobble`), so the daemon runs
  with any playback. It also answers queries (`Recent`, `Top`, `Totals`) for
  `goose scrobbles` and the TUI history "Listens" tab. Inserts are idempotent
  on (video_id, listened_at).
- `ListenBrainz` (token) and `LastFM` (key/secret + session, md5-signed calls)
  implement `scrobble.Service`. `GOOSENECK_LISTENBRAINZ_URL` overrides the
  endpoint for tests.
- `Lib.RunDaemon` polls mpv every 2s, queues scrobbles in SQLite
  (`scrobble_queue`) then flushes; re-reads the config every 10s and exits when
  there's nothing left to do, or 30s after the player goes away.
- Auth errors keep listens queued; only malformed-data rejections
  (`scrobble.Permanent`) drop them.
- Config holds secrets: `config.Save` writes 0600; `config.Secret`/`Mask` hide
  them in `goose config` output. Never print or commit real tokens.

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
- **Launch options** (`start.go`): `goose tui [page] [artist] [album]` +
  `--tab/--filter/--catalog` fill `tui.Options`; `Options.Resolve` validates
  and looks up artist/album *before* the screen opens (bad input →
  `*tui.StartError` → exit 2), then `App.openStart` builds the stack. Artist
  tabs only exist after load, so the artist page holds a pending `want` that
  `setArtist` applies. A new page or tab needs an entry in `StartPages` /
  `tabNames` (and `filterPages` if it's filterable).
- **Mouse** (`mouse.go`, bubblezone): views wrap clickable things in
  `a.mark(id, s)`; `View()` ends with `a.zones.Scan(frame)`, which strips the
  markers and records each zone's screen box. `App.mouse` matches a left click
  against: `home` / `crumb:N` (status bar), `seek:side` / `seek:line`,
  `prompt`, then the page's own `clicker` (tabs, chips), then the collection
  (`item:<collection id>:<slot>`, marked in `drawItem`). Double click =
  `enter` sent to the page. Wheel events still go to the page
  (`scrollMouse`). Dialogs ignore the mouse. Markers are zero-width CSI
  sequences that survive `paint`/`truncate`; a zone cut off by truncation or
  covered by `overlay` just isn't clickable. Test with `mouseApp` +
  `clickZone` in `mouse_test.go`.
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
- **config.toml = defaults.** TUI toggles (theme picker, sidebar, art, view,
  hints) are session-only App state; never write them back. Anything that does
  persist goes through `config.Update` (re-reads the file, changes one key),
  never `config.Save` of an in-memory Config (that leaks session state, `$APP`
  and backfilled values into the file).
- **Key overrides (`[keys]`, `tui/keymap.go`):** handlers match default key
  strings only; `App.key` remaps each press once (pressed → default key) before
  dispatch. Add new bindings as default keys and they're overridable for free.
  Printable keys aren't remapped while `App.typing()` (search, palette).
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

`demo/omarchy-showcase.sh` records the website's hero video
(`site/static/showcase.mp4`) live on an Omarchy desktop: it opens goose from
the apps menu, drives it with `wtype`, switches Omarchy themes while music
plays, then runs the CLI in a terminal, recording with Omarchy's screen
recorder. It runs on a demo home (`~/.cache/goose-showcase`) and swaps the Goose
menu entry and zsh prompt for the run, restoring both on exit. Dry-run any
change with `DRIVER=tmux demo/omarchy-showcase.sh` first: same keys into a
hidden tmux session, with a text snapshot per step in
`/tmp/goose-showcase-test/snaps.txt`. The script presses keys blind, so a
change in TUI focus rules (e.g. home grabbing the search box) breaks it.

## CI & releases

- `fetch-artists.yml` — daily cron refreshes `artists.json` (commits to main;
  `git pull --rebase` before pushing).
- `build-player.yml` — vet + test, then cross-compiles 6 targets on changes
  under `player/**`; a `v*` tag publishes a release. Version is stamped via
  `-X main.version`. The release job also publishes `checksums.txt`;
  `goose upgrade` refuses releases without it (so v0.5.0 and older can't be
  upgraded *to*). `GOOSENECK_RELEASES_API` points it at a fake server. Keep large assets (gif/png) out of `player/` so they don't
  trip the path filter.
