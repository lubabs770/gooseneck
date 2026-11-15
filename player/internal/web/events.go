package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/library"
)

// hub fans server-sent events out to every open /api/events stream and
// remembers the latest message of each sticky type for new subscribers.
type hub struct {
	mu     sync.Mutex
	subs   map[chan []byte]struct{}
	sticky map[string][]byte // latest "now", "queue", "radio"
}

func newHub() *hub {
	return &hub{subs: map[chan []byte]struct{}{}, sticky: map[string][]byte{}}
}

func (h *hub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// publish sends an event; sticky types are replayed to late subscribers and
// return false when nothing changed since the last publish.
func (h *hub) publish(typ string, v any, sticky bool) bool {
	data, err := json.Marshal(v)
	if err != nil {
		return false
	}
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", typ, data))
	h.mu.Lock()
	defer h.mu.Unlock()
	if sticky {
		if bytes.Equal(h.sticky[typ], msg) {
			return false
		}
		h.sticky[typ] = msg
	}
	for c := range h.subs {
		select {
		case c <- msg:
		default: // slow client: drop; the next tick catches it up
		}
	}
	return true
}

func (h *hub) subscribe() (chan []byte, [][]byte) {
	c := make(chan []byte, 32)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subs[c] = struct{}{}
	var replay [][]byte
	for _, m := range h.sticky {
		replay = append(replay, m)
	}
	return c, replay
}

func (h *hub) unsubscribe(c chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, c)
}

// events streams the hub to one client.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	c, replay := s.hub.subscribe()
	defer s.hub.unsubscribe(c)
	for _, m := range replay {
		w.Write(m)
	}
	fl.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case m := <-c:
			w.Write(m)
			fl.Flush()
		case <-ping.C:
			w.Write([]byte(": ping\n\n"))
			fl.Flush()
		}
	}
}

// poll publishes player state once a second while anyone is listening:
// "now" (position moves, so it goes out on every change), and "queue" and
// "radio" only when they change.
func (s *Server) poll(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if s.hub.count() == 0 {
			continue
		}
		s.mu.RLock()
		np, _ := s.lib.Now() // not running -> Running=false, which is the state to show
		q, _ := s.lib.Queue()
		radio := s.lib.RadioStatus()
		s.mu.RUnlock()
		s.hub.publish("now", np, true)
		if q == nil {
			q = []library.QueueItem{}
		}
		s.hub.publish("queue", q, true)
		s.hub.publish("radio", radioView(radio), true)
	}
}
