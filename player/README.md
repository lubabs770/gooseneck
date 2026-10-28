# gooseneck-player

The Go module behind `goose`. User docs live in the
[top-level README](../README.md); architecture notes for contributors are in
[`LLM.md`](../LLM.md).

```sh
go build -o gooseneck-player .
go vet ./... && go test ./...
GOOSENECK_HOME=/tmp/gn ./gooseneck-player   # isolated config / cache / player socket
```

| Package | What it does |
|---------|--------------|
| `internal/cli` | cobra commands; no args on a TTY opens the TUI |
| `internal/tui` | the Bubble Tea interface |
| `internal/library` | service layer shared by CLI and TUI |
| `internal/ytm` | YouTube Music web API client |
| `internal/mpv` | mpv daemon + JSON IPC |
| `internal/store` | SQLite cache, track metadata, history |
| `internal/lyrics` | LRCLIB (lrclib.net) lyrics client + LRC parser |
| `internal/catalog` | artists.json, tags, fuzzy search |
| `internal/art` | artist / album art download + disk cache |
| `internal/config` | config file and paths |
| `internal/bidi` | Hebrew display order for terminals |
