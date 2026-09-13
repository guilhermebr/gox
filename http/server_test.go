package http

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name        string
		envVars     map[string]string
		wantAddress string
		wantRead    time.Duration
	}{
		{
			name:        "defaults",
			envVars:     map[string]string{},
			wantAddress: "0.0.0.0:3000",
			wantRead:    10 * time.Second,
		},
		{
			name: "overrides",
			envVars: map[string]string{
				"TEST_ADDRESS":      "127.0.0.1:8080",
				"TEST_READ_TIMEOUT": "30s",
			},
			wantAddress: "127.0.0.1:8080",
			wantRead:    30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			cfg, err := LoadConfig("TEST")
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if cfg.Address != tt.wantAddress {
				t.Errorf("Address = %q, want %q", cfg.Address, tt.wantAddress)
			}
			if cfg.ReadTimeout != tt.wantRead {
				t.Errorf("ReadTimeout = %v, want %v", cfg.ReadTimeout, tt.wantRead)
			}
		})
	}
}

func TestNewServerWithConfig(t *testing.T) {
	cfg := Config{Address: "127.0.0.1:0", ReadTimeout: 5 * time.Second}
	srv := NewServerWithConfig("api", http.NewServeMux(), cfg, testLogger())

	if srv == nil {
		t.Fatal("NewServerWithConfig returned nil")
	}
	if got := srv.Address(); got != "127.0.0.1:0" {
		t.Errorf("Address() = %q, want %q", got, "127.0.0.1:0")
	}
	if srv.server.ReadTimeout != 5*time.Second {
		t.Errorf("ReadTimeout = %v, want %v", srv.server.ReadTimeout, 5*time.Second)
	}
}

func TestServerStartAndShutdown(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cfg := Config{Address: "127.0.0.1:0"}
	srv := NewServerWithConfig("test", mux, cfg, testLogger())

	// Bind an explicit listener-backed server by giving it a free port.
	// Start in the background; ListenAndServe blocks until Shutdown.
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	// Give the server a moment to begin listening, then shut it down.
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Start returns nil on graceful shutdown (http.ErrServerClosed is swallowed).
	if err := <-errCh; err != nil {
		t.Fatalf("Start returned error after graceful shutdown: %v", err)
	}
}

func TestServerManagerAddServer(t *testing.T) {
	sm := NewServerManager(testLogger())
	if len(sm.servers) != 0 {
		t.Fatalf("new manager should have 0 servers, got %d", len(sm.servers))
	}

	srv := NewServerWithConfig("a", http.NewServeMux(), Config{Address: "127.0.0.1:0"}, testLogger())
	sm.AddServer(srv)

	if len(sm.servers) != 1 {
		t.Errorf("expected 1 server after AddServer, got %d", len(sm.servers))
	}
}
