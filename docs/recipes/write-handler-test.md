# Write a handler test

```go path=main.go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
)

// Register keeps routes testable: tests build an app and call it.
func Register(a *gox.App) {
	a.HandleFunc("GET /invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "inv_1" {
			gox.Error(w, r, gox.NotFound("invoice %s", r.PathValue("id")))
			return
		}
		_ = gox.JSON(w, http.StatusOK, map[string]string{"id": "inv_1"})
	})
}

func main() {
	a := gox.MustNew("billing", gox.HTTP())
	Register(a)
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

```go path=main_test.go
package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox"
)

// newApp builds the service without the admin server (it would bind :9090)
// or log output.
func newApp(t *testing.T) *gox.App {
	t.Helper()
	a, err := gox.New("billing",
		gox.WithoutAdminServer(),
		gox.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		gox.HTTP(),
	)
	if err != nil {
		t.Fatal(err)
	}
	Register(a)
	return a
}

// TestGetInvoice serves the bare mux through httptest: no port, no
// middleware chain.
func TestGetInvoice(t *testing.T) {
	h := newApp(t).Mux()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/invoices/inv_1", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"inv_1"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/invoices/nope", nil))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

// TestThroughTheChain runs the server, so the middleware applies: request
// ids, timeouts, envelope 404s for unmatched routes.
func TestThroughTheChain(t *testing.T) {
	// Pick a free port first: with 127.0.0.1:0 the test cannot learn the port.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	t.Setenv("BILLING_HTTP_ADDR", addr)

	a := newApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("RunContext: %v", err)
		}
	})
	for deadline := time.Now().Add(5 * time.Second); !a.Health().IsReady(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the app did not become ready")
		}
	}

	resp, err := http.Get("http://" + addr + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || resp.Header.Get("X-Request-ID") == "" || !strings.Contains(string(body), `"code":"not_found"`) {
		t.Fatalf("got %d %s", resp.StatusCode, body)
	}
}
```

Handlers on gox/web (`web.Render`, `web.PageFrom`, `web.AddFlash`) panic on a
bare `a.Mux()`: add `web.Enable()` to `newApp` and test them through the chain.
Without `web.WithSessions()` there is no CSRF check, so a test can post a form;
a client whose `CheckRedirect` returns `http.ErrUseLastResponse` sees the 303.
