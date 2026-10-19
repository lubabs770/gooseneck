//go:build !windows

package library

import (
	"os"
	"syscall"
)

// lockFile takes an exclusive, non-blocking lock on path for the life of the
// process. ok=false means another process holds it.
func lockFile(path string) (release func(), ok bool) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return func() {}, false
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, true
}
