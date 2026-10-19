package tui

import (
	"os"
	"sync"
)

// syncWriter is the program's output. Bubble Tea writes each frame with a
// single Write call; Kitty image uploads are written through the same mutex,
// so they always land between frames, never inside one.
type syncWriter struct {
	mu sync.Mutex
	f  *os.File
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Write(p)
}

// The methods below make it a term.File, so Bubble Tea still treats the
// output as a terminal (window size, color detection).
func (w *syncWriter) Read(p []byte) (int, error) { return w.f.Read(p) }
func (w *syncWriter) Close() error               { return nil }
func (w *syncWriter) Fd() uintptr                { return w.f.Fd() }
