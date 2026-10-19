//go:build windows

package library

// lockFile is a no-op on Windows, where the daemon never runs (no mpv IPC).
func lockFile(path string) (release func(), ok bool) { return func() {}, true }
