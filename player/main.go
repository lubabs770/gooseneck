package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	artists, err := loadArtists(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cache, err := openCache(cacheDBPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, "cache:", err)
		os.Exit(1)
	}
	defer cache.Close()

	p := tea.NewProgram(newModel(cfg, cache, artists), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if kittyCapable() { // clear any transmitted images on the way out
		fmt.Print(kittyDeleteAll())
	}
}
