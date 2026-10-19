//go:build !windows

package mpv

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lubabs770/gooseneck/player/internal/config"
)

// fakeMPV answers mpv IPC requests from a property map and records commands.
type fakeMPV struct {
	mu    sync.Mutex
	props map[string]any
	cmds  [][]any
}

func startFake(t *testing.T, props map[string]any) (*Player, *fakeMPV) {
	t.Helper()
	dir, err := os.MkdirTemp("", "gn") // short path: unix sockets have a ~100 byte limit
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f := &fakeMPV{props: props}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return &Player{Cfg: config.Config{Player: "mpv"}, Sock: sock}, f
}

func (f *fakeMPV) serve(c net.Conn) {
	defer c.Close()
	sc := bufio.NewScanner(c)
	enc := json.NewEncoder(c)
	// An unsolicited event first: the client must skip it.
	enc.Encode(map[string]any{"event": "property-change"})
	for sc.Scan() {
		var req struct {
			Command   []any `json:"command"`
			RequestID int   `json:"request_id"`
		}
		json.Unmarshal(sc.Bytes(), &req)
		f.mu.Lock()
		f.cmds = append(f.cmds, req.Command)
		resp := map[string]any{"request_id": req.RequestID, "error": "success"}
		if req.Command[0] == "get_property" {
			if v, ok := f.props[req.Command[1].(string)]; ok {
				resp["data"] = v
			} else {
				resp["error"] = "property unavailable"
			}
		}
		f.mu.Unlock()
		enc.Encode(resp)
	}
}

func TestStatus(t *testing.T) {
	p, _ := startFake(t, map[string]any{
		"pid": 1, "idle-active": false, "pause": true, "time-pos": 12.5, "duration": 195.0,
		"playlist-pos": 2, "playlist-count": 11, "volume": 80.0,
		"path": "https://www.youtube.com/watch?v=JKqvqkuYVDA", "loop-playlist": "inf", "loop-file": false,
	})
	st, err := p.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running || st.Idle || !st.Paused || st.Pos != 12.5 || st.Index != 2 || st.Count != 11 ||
		st.VideoID != "JKqvqkuYVDA" || st.Repeat != "all" {
		t.Fatalf("status = %+v", st)
	}
}

func TestNotRunning(t *testing.T) {
	p := &Player{Cfg: config.Config{Player: "mpv"}, Sock: filepath.Join(t.TempDir(), "none")}
	if _, err := p.Status(); err != ErrNotRunning {
		t.Fatalf("want ErrNotRunning, got %v", err)
	}
	if p.Alive() {
		t.Fatal("Alive on missing socket")
	}
	if err := p.Quit(); err != nil {
		t.Fatalf("Quit with no daemon should be a no-op, got %v", err)
	}
}

func TestLoadNextInsertsAfterCurrent(t *testing.T) {
	p, f := startFake(t, map[string]any{
		"pid": 1, "idle-active": false, "playlist-pos": 1, "playlist-count": 5,
		"path": "https://www.youtube.com/watch?v=cur",
	})
	if err := p.Load([]string{"a", "b"}, Next); err != nil {
		t.Fatal(err)
	}
	var moves [][]any
	for _, c := range f.cmds {
		if c[0] == "playlist-move" {
			moves = append(moves, c)
		}
	}
	// appended at 5 and 6, moved to 2 and 3 (right after current index 1)
	if len(moves) != 2 || moves[0][1].(float64) != 5 || moves[0][2].(float64) != 2 ||
		moves[1][1].(float64) != 6 || moves[1][2].(float64) != 3 {
		t.Fatalf("moves = %v", moves)
	}
}

func TestLoadWhenIdleReplaces(t *testing.T) {
	p, f := startFake(t, map[string]any{"pid": 1, "idle-active": true, "playlist-pos": -1, "playlist-count": 0})
	if err := p.Load([]string{"a", "b"}, Append); err != nil {
		t.Fatal(err)
	}
	var loads []string
	for _, c := range f.cmds {
		if c[0] == "loadfile" {
			loads = append(loads, c[2].(string))
		}
	}
	if len(loads) != 2 || loads[0] != "replace" || loads[1] != "append" {
		t.Fatalf("loads = %v", loads)
	}
}

func TestVideoIDFromURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.youtube.com/watch?v=abc123&list=x":      "abc123",
		"https://youtu.be/xyz?t=3":                           "xyz",
		"/home/me/song.mp3":                                  "",
		"/music/8th Day/Lucky/02 - Rollin [jcCt_6vXZps].m4a": "jcCt_6vXZps",
	} {
		if got := VideoIDFromURL(in); got != want {
			t.Errorf("VideoIDFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnmanaged(t *testing.T) {
	p := New(config.Config{Player: "vlc"})
	if p.Managed() {
		t.Fatal("vlc should not be managed")
	}
	if _, err := p.Status(); err != ErrUnmanaged {
		t.Fatalf("want ErrUnmanaged, got %v", err)
	}
}
