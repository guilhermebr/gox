// Package admin is the ops server every gox service runs on a separate port:
// /healthz, /readyz, /version and /debug/pprof, plus whatever the root mounts
// (pkg/otel adds /metrics). It is never exposed on the public port.
package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/guilhermebr/gox/pkg/health"
	"github.com/guilhermebr/gox/pkg/httpserver"
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

// Server is the ops HTTP server: an httpserver.Server with the ops routes
// mounted on its own mux.
type Server struct {
	*httpserver.Server
	mux *http.ServeMux
	log *slog.Logger
}

// New builds an admin server. Start binds the listener, Run serves, Stop
// drains.
func New(cfg Config, reg *health.Registry, info Info, opts ...Option) *Server {
	s := &Server{mux: http.NewServeMux(), log: slog.Default()}
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
	s.Server = httpserver.New(httpserver.Config{
		Addr:              cfg.Addr,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}, s.mux, httpserver.WithLogger(s.log), httpserver.WithName("admin"))
	return s
}

// Handle mounts an extra handler (for example /metrics). Call before Start.
func (s *Server) Handle(pattern string, h http.Handler) {
	s.mux.Handle(pattern, h)
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
