package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// playbackEnv prepends BinDir to PATH so both yt-dlp calls and the player (which
// shells out to yt-dlp internally) resolve the configured binary.
func playbackEnv(cfg Config) []string {
	env := os.Environ()
	if cfg.BinDir == "" {
		return env
	}
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + cfg.BinDir + string(os.PathListSeparator) + kv[len("PATH="):]
			return env
		}
	}
	return append(env, "PATH="+cfg.BinDir)
}

// videoURL builds a watch URL from a video id.
func videoURL(id string) string { return "https://www.youtube.com/watch?v=" + id }

// play spawns the configured player (audio-only by default) with the given video
// URLs as a queue. Non-blocking: returns once the process starts.
func play(cfg Config, videoIDs []string) error {
	if len(videoIDs) == 0 {
		return fmt.Errorf("nothing to play")
	}
	args := append([]string{}, cfg.PlayerArgs...)
	for _, id := range videoIDs {
		args = append(args, videoURL(id))
	}
	cmd := exec.Command(cfg.Player, args...)
	cmd.Env = playbackEnv(cfg)
	// Detach stdio so the TUI keeps the terminal.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	return cmd.Start()
}
