//go:build !windows

package mpv

import (
	"net"
	"os/exec"
	"syscall"
)

// detach puts the child in its own session so it survives the parent (the TUI
// exiting, or a terminal closing).
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func dial(sock string) (net.Conn, error) { return dialTimeout("unix", sock) }
