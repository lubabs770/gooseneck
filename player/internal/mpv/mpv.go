// Package mpv drives a long-lived, headless mpv over its JSON IPC socket.
//
// The first play starts `mpv --idle` in the background; after that every
// gooseneck process (the TUI, `goose next` in another shell, a keybinding
// script) talks to the same player through the socket, so playback outlives the
// TUI and can be controlled from anywhere.
//
// Players other than mpv (or Windows, where mpv uses named pipes) fall back to
// fire-and-forget: the URLs are handed to the player and gooseneck can't
// control it afterwards.
package mpv

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/config"
)

// ErrNotRunning means no player daemon is listening.
var ErrNotRunning = errors.New("nothing is playing (player not running)")

// ErrUnmanaged means the configured player can't be remote-controlled.
var ErrUnmanaged = errors.New("playback control needs mpv (set player = \"mpv\")")

// Mode says how new tracks join the queue.
type Mode int

const (
	Replace Mode = iota // clear the queue and play now
	Append              // add to the end
	Next                // insert right after the current track
)

// Player is a handle on the (possibly not yet started) mpv daemon.
type Player struct {
	Cfg  config.Config
	Sock string
}

// New returns a handle using the default socket location.
func New(cfg config.Config) *Player {
	return &Player{Cfg: cfg, Sock: filepath.Join(config.RuntimeDir(), "mpv.sock")}
}

// Managed reports whether the configured player is mpv and IPC is available.
func (p *Player) Managed() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	base := strings.ToLower(filepath.Base(p.Cfg.Player))
	return strings.HasPrefix(base, "mpv")
}

// target turns a bare video id into a watch URL; URLs and file paths pass
// through unchanged.
func target(s string) string {
	if strings.ContainsAny(s, "/\\:") {
		return s
	}
	return VideoURL(s)
}

// VideoURL builds a watch URL from a video id.
func VideoURL(id string) string { return "https://www.youtube.com/watch?v=" + id }

// VideoIDFromURL extracts the id from a watch URL, or from a downloaded file
// named "… [<id>].ext" ("" if neither).
func VideoIDFromURL(u string) string {
	if m := fileIDRe.FindStringSubmatch(u); m != nil {
		return m[1]
	}
	for _, marker := range []string{"watch?v=", "youtu.be/", "/shorts/"} {
		if i := strings.Index(u, marker); i >= 0 {
			id := u[i+len(marker):]
			if j := strings.IndexAny(id, "&?#/"); j >= 0 {
				id = id[:j]
			}
			return id
		}
	}
	return ""
}

var fileIDRe = regexp.MustCompile(`\[([A-Za-z0-9_-]{11})\]\.[A-Za-z0-9]+$`)

// ---- IPC ---------------------------------------------------------------

type reply struct {
	RequestID int             `json:"request_id"`
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
	Event     string          `json:"event"`
}

// Result is one command's answer within a batch.
type Result struct {
	Data json.RawMessage
	Err  error
}

// Batch sends several commands over one connection and collects the replies in
// order. A failing command doesn't fail the batch; check each Result.Err.
func (p *Player) Batch(cmds ...[]any) ([]Result, error) {
	if !p.Managed() {
		return nil, ErrUnmanaged
	}
	conn, err := dial(p.Sock)
	if err != nil {
		return nil, ErrNotRunning
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
	enc := json.NewEncoder(conn)
	for i, c := range cmds {
		if err := enc.Encode(map[string]any{"command": c, "request_id": i + 1}); err != nil {
			return nil, err
		}
	}
	out := make([]Result, len(cmds))
	pending := len(cmds)
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64<<10), 8<<20) // the playlist property can be large
	for pending > 0 && sc.Scan() {
		var r reply
		if json.Unmarshal(sc.Bytes(), &r) != nil || r.Event != "" || r.RequestID < 1 || r.RequestID > len(cmds) {
			continue
		}
		res := Result{Data: r.Data}
		if r.Error != "" && r.Error != "success" {
			res.Err = errors.New(r.Error)
		}
		out[r.RequestID-1] = res
		pending--
	}
	if pending > 0 {
		if err := sc.Err(); err != nil {
			return out, err
		}
		return out, errors.New("mpv closed the connection early")
	}
	return out, nil
}

// Command runs one command and returns its data.
func (p *Player) Command(args ...any) (json.RawMessage, error) {
	res, err := p.Batch(args)
	if err != nil {
		return nil, err
	}
	return res[0].Data, res[0].Err
}

// Alive reports whether the daemon answers on the socket.
func (p *Player) Alive() bool {
	if !p.Managed() {
		return false
	}
	_, err := p.Command("get_property", "pid")
	return err == nil
}

// ---- lifecycle -----------------------------------------------------------------

// Args are the flags the daemon is started with.
func (p *Player) Args() []string {
	vol := p.Cfg.Volume
	if vol <= 0 {
		vol = 80
	}
	args := []string{
		"--idle=yes",
		"--no-video",
		"--no-terminal",
		"--force-window=no",
		"--audio-display=no",
		"--input-ipc-server=" + p.Sock,
		"--ytdl-format=bestaudio/best",
		"--prefetch-playlist=yes",
		"--volume=" + strconv.Itoa(vol),
		"--title=gooseneck",
	}
	if p.Cfg.BinDir != "" {
		args = append(args, "--script-opts=ytdl_hook-ytdl_path="+p.Cfg.YtDlpPath())
	}
	return append(args, p.Cfg.PlayerArgs...)
}

// Start launches the daemon (no-op when it's already up) and waits for its
// socket.
func (p *Player) Start() error {
	if !p.Managed() {
		return ErrUnmanaged
	}
	if p.Alive() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.Sock), 0o700); err != nil {
		return err
	}
	_ = os.Remove(p.Sock) // stale socket from a crashed daemon
	bin, err := exec.LookPath(p.Cfg.Player)
	if err != nil {
		return fmt.Errorf("%s not found: install mpv (https://mpv.io) or set player in the config", p.Cfg.Player)
	}
	cmd := exec.Command(bin, p.Args()...)
	cmd.Env = p.Cfg.Env()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			return fmt.Errorf("mpv exited during startup (%v); check player_args in the config", err)
		case <-time.After(60 * time.Millisecond):
		}
		if p.Alive() {
			_ = cmd.Process.Release()
			return nil
		}
	}
	return errors.New("mpv did not open its control socket in time")
}

// Quit shuts the daemon down.
func (p *Player) Quit() error {
	_, err := p.Command("quit")
	if errors.Is(err, ErrNotRunning) {
		return nil
	}
	return err
}

// ---- playback ------------------------------------------------------------------

// Load queues targets: video ids, URLs or local file paths. In Replace mode
// playback starts at the first one. Unmanaged players get them on their
// command line instead.
func (p *Player) Load(ids []string, mode Mode) error {
	if len(ids) == 0 {
		return errors.New("nothing to play")
	}
	if !p.Managed() {
		return p.spawn(ids)
	}
	if err := p.Start(); err != nil {
		return err
	}
	st, _ := p.Status()
	if st.Idle && mode != Replace {
		mode = Replace // nothing playing: "add" means "play"
	}
	var cmds [][]any
	switch mode {
	case Replace:
		cmds = append(cmds, []any{"loadfile", target(ids[0]), "replace"})
		for _, id := range ids[1:] {
			cmds = append(cmds, []any{"loadfile", target(id), "append"})
		}
		cmds = append(cmds, []any{"set_property", "pause", false})
	case Append:
		for _, id := range ids {
			cmds = append(cmds, []any{"loadfile", target(id), "append-play"})
		}
	case Next:
		for i, id := range ids {
			cmds = append(cmds,
				[]any{"loadfile", target(id), "append"},
				[]any{"playlist-move", st.Count + i, st.Index + 1 + i})
		}
	}
	res, err := p.Batch(cmds...)
	if err != nil {
		return err
	}
	for _, r := range res {
		if r.Err != nil {
			return fmt.Errorf("mpv: %w", r.Err)
		}
	}
	return nil
}

// spawn is the fire-and-forget path for players without IPC.
func (p *Player) spawn(ids []string) error {
	args := append([]string{}, p.Cfg.PlayerArgs...)
	for _, id := range ids {
		args = append(args, target(id))
	}
	cmd := exec.Command(p.Cfg.Player, args...)
	cmd.Env = p.Cfg.Env()
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// TogglePause flips pause; returns the new paused state.
func (p *Player) TogglePause() (bool, error) {
	if _, err := p.Command("cycle", "pause"); err != nil {
		return false, err
	}
	raw, err := p.Command("get_property", "pause")
	var paused bool
	_ = json.Unmarshal(raw, &paused)
	return paused, err
}

// SetPause pauses (true) or resumes (false).
func (p *Player) SetPause(paused bool) error {
	_, err := p.Command("set_property", "pause", paused)
	return err
}

// Next skips to the next track.
func (p *Player) Next() error {
	_, err := p.Command("playlist-next", "weak")
	if err != nil && err.Error() == "error running command" {
		return errors.New("already at the last track")
	}
	return err
}

// Prev restarts the current track when more than 3s in, else goes back one.
func (p *Player) Prev() error {
	st, err := p.Status()
	if err != nil {
		return err
	}
	if st.Pos > 3 || st.Index <= 0 {
		_, err = p.Command("seek", 0, "absolute")
		return err
	}
	_, err = p.Command("playlist-prev", "weak")
	return err
}

// Seek moves by sec (relative) or to sec (absolute).
func (p *Player) Seek(sec float64, absolute bool) error {
	how := "relative"
	if absolute {
		how = "absolute"
	}
	_, err := p.Command("seek", sec, how)
	return err
}

// Volume sets (absolute) or nudges the volume; returns the new level.
func (p *Player) Volume(v float64, absolute bool) (float64, error) {
	var err error
	if absolute {
		_, err = p.Command("set_property", "volume", v)
	} else {
		_, err = p.Command("add", "volume", v)
	}
	if err != nil {
		return 0, err
	}
	raw, err := p.Command("get_property", "volume")
	var vol float64
	_ = json.Unmarshal(raw, &vol)
	return vol, err
}

// Shuffle shuffles the queue (the current track keeps playing).
func (p *Player) Shuffle() error {
	_, err := p.Command("playlist-shuffle")
	return err
}

// Repeat sets the repeat mode: "off", "all" or "one".
func (p *Player) Repeat(mode string) error {
	var pl, file any = "no", "no"
	switch mode {
	case "off":
	case "all":
		pl = "inf"
	case "one":
		file = "inf"
	default:
		return fmt.Errorf("repeat mode must be off, all or one (got %q)", mode)
	}
	_, err := p.Batch([]any{"set_property", "loop-playlist", pl}, []any{"set_property", "loop-file", file})
	return err
}

// Jump plays queue entry i (0-based).
func (p *Player) Jump(i int) error {
	_, err := p.Command("playlist-play-index", i)
	return err
}

// Remove drops queue entry i (0-based).
func (p *Player) Remove(i int) error {
	_, err := p.Command("playlist-remove", i)
	return err
}

// Clear removes everything except the current track.
func (p *Player) Clear() error {
	_, err := p.Command("playlist-clear")
	return err
}

// Stop stops playback and empties the queue; the daemon stays up, idle.
func (p *Player) Stop() error {
	_, err := p.Command("stop")
	return err
}

// ---- state ------------------------------------------------------------------------

// Status is a snapshot of the player.
type Status struct {
	Running  bool    `json:"running"`
	Idle     bool    `json:"idle"`
	Paused   bool    `json:"paused"`
	Pos      float64 `json:"position"`
	Duration float64 `json:"duration"`
	Index    int     `json:"index"` // 0-based; -1 when idle
	Count    int     `json:"count"`
	Volume   float64 `json:"volume"`
	Muted    bool    `json:"muted"`
	VideoID  string  `json:"video_id,omitempty"`
	Title    string  `json:"media_title,omitempty"`
	Repeat   string  `json:"repeat"` // off | all | one
	Buffer   bool    `json:"buffering"`
}

var statusProps = []string{"idle-active", "pause", "time-pos", "duration", "playlist-pos",
	"playlist-count", "volume", "mute", "path", "media-title", "loop-playlist", "loop-file", "paused-for-cache"}

// Status queries the daemon. A missing daemon yields ErrNotRunning.
func (p *Player) Status() (Status, error) {
	cmds := make([][]any, len(statusProps))
	for i, prop := range statusProps {
		cmds[i] = []any{"get_property", prop}
	}
	res, err := p.Batch(cmds...)
	if err != nil {
		return Status{Index: -1}, err
	}
	st := Status{Running: true, Index: -1, Repeat: "off"}
	val := func(i int, v any) {
		if res[i].Err == nil {
			_ = json.Unmarshal(res[i].Data, v)
		}
	}
	val(0, &st.Idle)
	val(1, &st.Paused)
	val(2, &st.Pos)
	val(3, &st.Duration)
	val(4, &st.Index)
	val(5, &st.Count)
	val(6, &st.Volume)
	val(7, &st.Muted)
	var path string
	val(8, &path)
	st.VideoID = VideoIDFromURL(path)
	val(9, &st.Title)
	if loopOn(res[11]) {
		st.Repeat = "one"
	} else if loopOn(res[10]) {
		st.Repeat = "all"
	}
	val(12, &st.Buffer)
	if st.Index < 0 || st.VideoID == "" {
		st.Idle = true
	}
	return st, nil
}

// loopOn decodes mpv's loop properties, which are "inf", a count, or false.
func loopOn(r Result) bool {
	if r.Err != nil {
		return false
	}
	s := strings.Trim(string(r.Data), `"`)
	return s != "false" && s != "no" && s != "0" && s != ""
}

// Entry is one queue item.
type Entry struct {
	Index   int    `json:"index"`
	VideoID string `json:"video_id"`
	Current bool   `json:"current"`
	Title   string `json:"title,omitempty"`
}

// Queue lists the playlist.
func (p *Player) Queue() ([]Entry, error) {
	raw, err := p.Command("get_property", "playlist")
	if err != nil {
		return nil, err
	}
	var items []struct {
		Filename string `json:"filename"`
		Current  bool   `json:"current"`
		Title    string `json:"title"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	out := make([]Entry, len(items))
	for i, it := range items {
		out[i] = Entry{Index: i, VideoID: VideoIDFromURL(it.Filename), Current: it.Current, Title: it.Title}
	}
	return out, nil
}

func dialTimeout(network, addr string) (net.Conn, error) {
	return net.DialTimeout(network, addr, 500*time.Millisecond)
}
