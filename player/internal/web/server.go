// Package web serves gooseneck's JSON + SSE API (and, optionally, a static web
// UI such as partiality) for `goose serve`. Handlers only call into library,
// catalog, store and scrobble, so the web can do exactly what the CLI can.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/library"
	"github.com/lubabs770/gooseneck/player/internal/mpv"
)

// Options configures a Server.
type Options struct {
	Addrs []string // listen addresses, e.g. 127.0.0.1:7777 and 100.x.y.z:7777
	Hosts []string // extra Host header values to accept (a tailnet MagicDNS name)
	UI    string   // directory of static UI files served at /; "" = API only
}

// Server is the HTTP API over one long-lived library.
type Server struct {
	lib   *library.Lib
	opts  Options
	hosts map[string]bool
	hub   *hub

	// ponytail: one RWMutex around the whole API. Catalog and config writes
	// swap lib.Cat / lib.Cfg and take it exclusively; everything else shares
	// it. Fine for one user; finer locks if writes ever get frequent.
	mu sync.RWMutex
	dl sync.Mutex // one download job at a time; later ones wait their turn
}

// New builds a server. Every listen address (and "localhost" for loopback
// ones) is an accepted Host, plus opts.Hosts.
func New(l *library.Lib, opts Options) *Server {
	s := &Server{lib: l, opts: opts, hosts: map[string]bool{}, hub: newHub()}
	for _, a := range opts.Addrs {
		host, port, err := net.SplitHostPort(a)
		if err != nil {
			continue
		}
		s.hosts[a] = true
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			s.hosts[net.JoinHostPort("localhost", port)] = true
		}
		for _, h := range opts.Hosts {
			s.hosts[net.JoinHostPort(strings.TrimSuffix(strings.ToLower(h), "."), port)] = true
		}
	}
	return s
}

// Serve listens on every address until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	// Open the scrobble log up front: Lib opens it lazily, which would race
	// between concurrent handlers. A failure isn't fatal (Stats shows it).
	_, _ = s.lib.LocalScrobbles()
	go s.poll(ctx)
	h := s.Handler()
	errc := make(chan error, len(s.opts.Addrs))
	var srvs []*http.Server
	for _, a := range s.opts.Addrs {
		ln, err := net.Listen("tcp", a)
		if err != nil {
			for _, srv := range srvs {
				srv.Close()
			}
			return err
		}
		srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
		srvs = append(srvs, srv)
		go func() { errc <- srv.Serve(ln) }()
	}
	var err error
	select {
	case <-ctx.Done():
	case err = <-errc:
	}
	shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, srv := range srvs {
		_ = srv.Shutdown(shut)
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}

// Handler is the full route table behind the Host/Origin guard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.routes(mux)
	if s.opts.UI != "" {
		files := http.FileServer(http.Dir(s.opts.UI))
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// revalidate every time (a cheap 304) so UI updates show on reload
			w.Header().Set("Cache-Control", "no-cache")
			files.ServeHTTP(w, r)
		}))
	}
	return s.guard(mux)
}

// guard blocks DNS rebinding (unknown Host) and cross-site writes (a foreign
// Origin, or a browser saying the request is cross-site).
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hosts[strings.ToLower(r.Host)] {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// httpError carries a status code to the JSON writer.
type httpError struct {
	code int
	msg  string
}

func (e httpError) Error() string { return e.msg }

func bad(format string, a ...any) error {
	return httpError{http.StatusBadRequest, fmt.Sprintf(format, a...)}
}

// api adapts a handler that returns a value to JSON, under the shared lock.
func (s *Server) api(fn func(r *http.Request) (any, error)) http.HandlerFunc {
	return s.apiLocked(false, fn)
}

// apiExclusive is api for handlers that swap lib.Cat or lib.Cfg.
func (s *Server) apiExclusive(fn func(r *http.Request) (any, error)) http.HandlerFunc {
	return s.apiLocked(true, fn)
}

func (s *Server) apiLocked(exclusive bool, fn func(r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if exclusive {
			s.mu.Lock()
		} else {
			s.mu.RLock()
		}
		v, err := fn(r)
		if exclusive {
			s.mu.Unlock()
		} else {
			s.mu.RUnlock()
		}
		if err != nil {
			code := http.StatusBadGateway // upstream (YouTube Music, yt-dlp, LRCLIB) by default
			var he httpError
			switch {
			case errors.As(err, &he):
				code = he.code
			case errors.Is(err, mpv.ErrNotRunning):
				code = http.StatusConflict
			}
			writeJSON(w, code, map[string]string{"error": err.Error()})
			return
		}
		if v == nil {
			v = map[string]bool{"ok": true}
		}
		writeJSON(w, http.StatusOK, v)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// body decodes a JSON request body (empty = zero value).
func body(r *http.Request, v any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return bad("bad JSON body: %v", err)
	}
	return nil
}
