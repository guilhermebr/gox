package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Server wraps http.Server with additional functionality
type Server struct {
	server *http.Server
	logger *slog.Logger
	name   string
	config Config
}

// NewServer creates a new HTTP server
func NewServerWithConfig(name string, handler http.Handler, cfg Config, logger *slog.Logger) *Server {
	return &Server{
		server: &http.Server{
			Handler:           handler,
			Addr:              cfg.Address,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
		},
		logger: logger.With(slog.String("server", name)),
		name:   name,
		config: cfg,
	}
}

func NewServer(name string, handler http.Handler, logger *slog.Logger) (*Server, error) {
	cfg, err := LoadConfig(strings.ToUpper(name))
	if err != nil {
		logger.Error("failed to load config",
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return NewServerWithConfig(name, handler, cfg, logger), nil
}

// Start starts the HTTP server
func (s *Server) Start() error {
	s.logger.Info("starting server",
		slog.String("address", s.server.Addr),
		slog.Duration("read_timeout", s.server.ReadTimeout),
		slog.Duration("write_timeout", s.server.WriteTimeout),
	)

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}

// StartWithGracefulShutdown starts the server and handles graceful shutdown
func (s *Server) StartWithGracefulShutdown() error {
	// Start server in a goroutine
	go func() {
		if err := s.Start(); err != nil {
			s.logger.Error("server failed to start",
				slog.String("error", err.Error()),
			)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	s.logger.Info("shutting down server")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		s.logger.Error("failed to shutdown server gracefully",
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	s.logger.Info("server stopped")
	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// Address returns the server address
func (s *Server) Address() string {
	return s.server.Addr
}

// ServerManager manages multiple servers
type ServerManager struct {
	servers []*Server
	logger  *slog.Logger
}

// NewServerManager creates a new server manager
func NewServerManager(logger *slog.Logger) *ServerManager {
	return &ServerManager{
		servers: make([]*Server, 0),
		logger:  logger,
	}
}

// AddServer adds a server to the manager
func (sm *ServerManager) AddServer(server *Server) {
	sm.servers = append(sm.servers, server)
}

// StartAll starts all managed servers with graceful shutdown handling
func (sm *ServerManager) StartAll() error {
	// Start all servers in separate goroutines
	for _, server := range sm.servers {
		go func(srv *Server) {
			if err := srv.Start(); err != nil {
				srv.logger.Error("server failed to start",
					slog.String("error", err.Error()),
				)
				os.Exit(1)
			}
		}(server)

		// Small delay to ensure servers start in order
		time.Sleep(100 * time.Millisecond)
	}

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	sm.logger.Info("shutting down all servers")

	// Shutdown all servers concurrently
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan error, len(sm.servers))

	for _, server := range sm.servers {
		go func(srv *Server) {
			done <- srv.Shutdown(shutdownCtx)
		}(server)
	}

	// Wait for all servers to shutdown
	var lastErr error
	for i := 0; i < len(sm.servers); i++ {
		if err := <-done; err != nil {
			sm.logger.Error("server shutdown error",
				slog.String("error", err.Error()),
			)
			lastErr = err
		}
	}

	sm.logger.Info("all servers stopped")
	return lastErr
}
