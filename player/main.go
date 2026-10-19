// Command gooseneck-player (installed as `goose`) is a TUI and CLI for the
// gooseneck catalog of Jewish music artists.
package main

import (
	"os"

	"github.com/lubabs770/gooseneck/player/internal/cli"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() { os.Exit(cli.Execute(version)) }
