// Package admin is the ops server every gox service runs on a separate port:
// /healthz, /readyz, /version and /debug/pprof, plus whatever the root mounts
// (pkg/otel adds /metrics). It is never exposed on the public port.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/guilhermebr/gox/pkg/health"
)

// Config is the admin server's configuration.
type Config struct {
	Addr string
}

// Info is what /version reports.
type Info struct {
	Service string
	Version string
}

// Option configures the Server.
type Option func(*Server)

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option {
	return func(s *Server) { s.log = l }
}

// Server is a lifecycle Component and Runner serving the ops endpoints.
type Server struct {
	cfg  Config
	log  *slog.Logger
	mux  *http.ServeMux
	http *http.Server

	mu sync.Mutex
	ln net.Listener
}

// New builds an admin server. Start binds the listener, Run serves, Stop
// drains.
func New(cfg Config, reg *health.Registry, info Info, opts ...Option) *Server {
	s := &Server{cfg: cfg, log: slog.Default(), mux: http.NewServeMux()}
	for _, o := range opts {
		o(s)
	}
	s.mux.Handle("GET /healthz", reg.LivenessHandler())
	s.mux.Handle("GET /readyz", reg.ReadinessHandler())
	s.mux.Handle("GET /version", versionHandler(info))
	s.mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	s.mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	s.mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	s.mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	s.mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	s.http = &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return s
}

// Handle mounts an extra handler (for example /metrics). Call before Start.
func (s *Server) Handle(pattern string, h http.Handler) {
	s.mux.Handle(pattern, h)
}

// Name implements lifecycle.Component.
func (s *Server) Name() string { return "admin" }

// Addr returns the bound address once Start has succeeded.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return s.cfg.Addr
	}
	return s.ln.Addr().String()
}

// Start binds the listener so a busy port fails fast.
func (s *Server) Start(context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("admin: listen %s: %w", s.cfg.Addr, err)
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	s.log.Info("admin server listening", slog.String("addr", ln.Addr().String()))
	return nil
}

// Run serves until Stop is called. It returns nil after a clean shutdown.
func (s *Server) Run(context.Context) error {
	s.mu.Lock()
	ln := s.ln
	s.mu.Unlock()
	if ln == nil {
		return errors.New("admin: Run before Start")
	}
	if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("admin: serve: %w", err)
	}
	return nil
}

// Stop drains in-flight requests within ctx and closes the listener.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	ln := s.ln
	s.ln = nil
	s.mu.Unlock()
	if ln == nil {
		return nil
	}
	if err := s.http.Shutdown(ctx); err != nil {
		_ = s.http.Close()
		return fmt.Errorf("admin: shutdown: %w", err)
	}
	return nil
}

func versionHandler(info Info) http.Handler {
	body := map[string]string{
		"service": info.Service,
		"version": info.Version,
		"go":      runtime.Version(),
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				body["commit"] = s.Value
			case "vcs.time":
				body["commit_time"] = s.Value
			case "vcs.modified":
				body["modified"] = s.Value
			}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
}
