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
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guilhermebr/gox"
)

// newApp builds the service without the admin server or log output. The
// mux is served through httptest, so no port is opened.
func newApp(t *testing.T) http.Handler {
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
	return a.Mux()
}

func TestGetInvoice(t *testing.T) {
	h := newApp(t)

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
```

Serving `a.Mux()` directly skips the middleware chain (request ids, timeouts, envelope 404s). To test through the chain, run the app on a free port with `a.RunContext(ctx)` in a goroutine, wait for `a.Health().IsReady()`, and use `http.Get`.
