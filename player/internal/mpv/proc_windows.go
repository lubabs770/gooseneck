//go:build windows

package mpv

import (
	"errors"
	"net"
	"os/exec"
)

func detach(cmd *exec.Cmd) {}

// mpv on Windows uses a named pipe for IPC; gooseneck falls back to
// fire-and-forget playback there (Managed reports false).
func dial(sock string) (net.Conn, error) {
	return nil, errors.New("mpv IPC is not supported on Windows")
}
