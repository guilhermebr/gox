// Package httpserver is the public HTTP server component: a net/http Server
// with sane timeouts over a Go 1.22+ ServeMux, health probes on the public
// port, and 404/405 rendered as the standard error envelope.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	gerrors "github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/health"
	"github.com/guilhermebr/gox/pkg/httpx"
	"github.com/guilhermebr/gox/pkg/log"
)

// Config is the server's listen address and timeouts.
type Config struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

// Option configures the Server.
type Option func(*Server)

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option {
	return func(s *Server) { s.log = l }
}

// WithName sets the component name used in logs and errors (default "http").
func WithName(name string) Option {
	return func(s *Server) { s.name = name }
}

// Server is a lifecycle Component and Runner around http.Server.
type Server struct {
	cfg  Config
	name string
	log  *slog.Logger
	http *http.Server

	mu sync.Mutex
	ln net.Listener
}

// New builds a server for handler. Start binds, Run serves, Stop drains.
func New(cfg Config, handler http.Handler, opts ...Option) *Server {
	s := &Server{cfg: cfg, name: "http", log: slog.Default()}
	for _, o := range opts {
		o(s)
	}
	s.http = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}
	return s
}

// Name implements lifecycle.Component.
func (s *Server) Name() string { return s.name }

// Addr returns the bound address once Start has succeeded.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return s.cfg.Addr
	}
	return s.ln.Addr().String()
}

// Start binds the listener so a busy port fails the boot, not the first
// request.
func (s *Server) Start(context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("%s: listen %s: %w", s.name, s.cfg.Addr, err)
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	s.log.Info(s.name+" server listening", slog.String("addr", ln.Addr().String()))
	return nil
}

// Run serves until Stop. It returns nil after a clean shutdown.
func (s *Server) Run(context.Context) error {
	s.mu.Lock()
	ln := s.ln
	s.mu.Unlock()
	if ln == nil {
		return fmt.Errorf("%s: Run before Start", s.name)
	}
	if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("%s: serve: %w", s.name, err)
	}
	return nil
}

// Stop drains in-flight requests within ctx. If the deadline passes it
// closes the remaining connections, so a slow client cannot hold the process
// open, and reports that it had to.
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
		return fmt.Errorf("%s: shutdown: %w (connections closed)", s.name, err)
	}
	return nil
}

// NewMux returns the ServeMux services register routes on.
func NewMux() *http.ServeMux {
	return http.NewServeMux()
}

// RegisterHealth serves /healthz and /readyz on the public mux. Platform
// health checks (Tsuru, Fly, Docker HEALTHCHECK) probe the app port and
// cannot reach the admin server.
func RegisterHealth(mux *http.ServeMux, reg *health.Registry) {
	mux.Handle("GET /healthz", reg.LivenessHandler())
	mux.Handle("GET /readyz", reg.ReadinessHandler())
}

// Handler wraps mux so unmatched requests render the error envelope
// instead of net/http's plain-text 404 and 405. Matched requests go
// straight to the mux.
func Handler(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		// The mux's own error handler: run it against a probe to learn
		// whether this is a 404 or a 405 (and the Allow header), then render.
		p := &probe{header: make(http.Header), status: http.StatusOK}
		h.ServeHTTP(p, r)
		var err *gerrors.Error
		switch p.status {
		case http.StatusMethodNotAllowed:
			if allow := p.header.Get("Allow"); allow != "" {
				w.Header().Set("Allow", allow)
			}
			err = gerrors.Unimplemented("method %s is not allowed on %s", r.Method, r.URL.Path).
				WithHTTPStatus(http.StatusMethodNotAllowed)
		default:
			err = gerrors.NotFound("no route for %s %s", r.Method, r.URL.Path)
		}
		status, env := gerrors.ToEnvelope(err, log.RequestID(r.Context()))
		httpx.WriteError(w, r, status, env)
	})
}

// probe records what the mux's built-in error handler would have written.
type probe struct {
	header http.Header
	status int
}

func (p *probe) Header() http.Header         { return p.header }
func (p *probe) Write(b []byte) (int, error) { return len(b), nil }
func (p *probe) WriteHeader(code int)        { p.status = code }
