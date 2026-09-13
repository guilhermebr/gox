package admin_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox/pkg/admin"
	"github.com/guilhermebr/gox/pkg/health"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func start(t *testing.T, srv *admin.Server) (base string, stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	if err := srv.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	return "http://" + srv.Addr(), func() {
		if err := srv.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run returned %v after Stop; want nil", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("Run did not return after Stop")
		}
	}
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url) //nolint:gosec // test URL
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestAdminServesHealthVersionAndPprof(t *testing.T) {
	reg := health.NewRegistry()
	reg.SetReady(true)
	srv := admin.New(admin.Config{Addr: "127.0.0.1:0"}, reg,
		admin.Info{Service: "billing", Version: "1.2.3"},
		admin.WithLogger(quiet()))
	if srv.Name() != "admin" {
		t.Fatalf("Name = %q", srv.Name())
	}
	base, stop := start(t, srv)
	defer stop()

	if code, _ := get(t, base+"/healthz"); code != http.StatusOK {
		t.Errorf("/healthz = %d", code)
	}
	if code, _ := get(t, base+"/readyz"); code != http.StatusOK {
		t.Errorf("/readyz = %d", code)
	}
	reg.SetReady(false)
	if code, _ := get(t, base+"/readyz"); code != http.StatusServiceUnavailable {
		t.Errorf("/readyz after SetReady(false) = %d", code)
	}

	code, body := get(t, base+"/version")
	if code != http.StatusOK {
		t.Fatalf("/version = %d", code)
	}
	var v map[string]string
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("/version body %q: %v", body, err)
	}
	if v["service"] != "billing" || v["version"] != "1.2.3" || !strings.HasPrefix(v["go"], "go") {
		t.Fatalf("/version = %v", v)
	}

	if code, _ := get(t, base+"/debug/pprof/"); code != http.StatusOK {
		t.Errorf("/debug/pprof/ = %d", code)
	}
}

func TestAdminHandleMountsExtraRoutes(t *testing.T) {
	srv := admin.New(admin.Config{Addr: "127.0.0.1:0"}, health.NewRegistry(), admin.Info{}, admin.WithLogger(quiet()))
	srv.Handle("/metrics", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("# metrics"))
	}))
	base, stop := start(t, srv)
	defer stop()
	if code, body := get(t, base+"/metrics"); code != http.StatusOK || body != "# metrics" {
		t.Fatalf("/metrics = %d %q", code, body)
	}
}

func TestAdminStartFailsFastWhenPortIsTaken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	srv := admin.New(admin.Config{Addr: ln.Addr().String()}, health.NewRegistry(), admin.Info{}, admin.WithLogger(quiet()))
	err = srv.Start(context.Background())
	if err == nil {
		_ = srv.Stop(context.Background())
		t.Fatal("Start succeeded on a busy port")
	}
	if !strings.Contains(err.Error(), "admin:") || !strings.Contains(err.Error(), ln.Addr().String()) {
		t.Fatalf("error = %v; want the admin prefix and the address", err)
	}
}

func TestAdminStopBeforeStartIsSafe(t *testing.T) {
	srv := admin.New(admin.Config{Addr: "127.0.0.1:0"}, health.NewRegistry(), admin.Info{}, admin.WithLogger(quiet()))
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
}
