package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/httpclient"
)

type invoice struct {
	ID     string `json:"id"`
	Amount int    `json:"amount"`
}

func backend(t *testing.T) (*httptest.Server, *http.Request) {
	t.Helper()
	var last *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(nil)
		last = r
		switch r.URL.Path {
		case "/invoices/inv_1":
			_ = json.NewEncoder(w).Encode(invoice{ID: "inv_1", Amount: 10})
		case "/invoices":
			if r.Method == http.MethodPost {
				var in invoice
				_ = json.Unmarshal(body, &in)
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(in)
				return
			}
		case "/missing":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(errors.Envelope{Code: "not_found", Message: "invoice x", Details: map[string]any{"id": "x"}})
		case "/boom":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("<html>gateway down</html>"))
		case "/gone":
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, last
}

func TestAPIGetPostDeleteWithBearer(t *testing.T) {
	srv, _ := backend(t)
	api := newAPI(srv.Client(), srv.URL, "tok-1")

	var inv invoice
	if err := api.Get(context.Background(), "/invoices/inv_1", &inv); err != nil || inv.Amount != 10 {
		t.Fatalf("Get: %+v %v", inv, err)
	}
	var created invoice
	if err := api.Post(context.Background(), "/invoices", invoice{ID: "inv_2", Amount: 5}, &created); err != nil || created.ID != "inv_2" {
		t.Fatalf("Post: %+v %v", created, err)
	}
	if err := api.Delete(context.Background(), "/gone"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestAPISendsTheBearerAndRequestID(t *testing.T) {
	var auth, reqID string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		reqID = r.Header.Get("X-Request-ID")
	}))
	defer srv.Close()
	api := newAPI(httpclient.New(httpclient.Config{}), srv.URL, "tok-9")
	ctx := withRequestID(context.Background(), "req-1")
	_ = api.Get(ctx, "/x", nil)
	if auth != "Bearer tok-9" || reqID != "req-1" {
		t.Fatalf("auth=%q request id=%q", auth, reqID)
	}
}

func TestAPITurnsEnvelopesIntoCodedErrorsAndHidesForeignBodies(t *testing.T) {
	srv, _ := backend(t)
	api := newAPI(srv.Client(), srv.URL, "")

	err := api.Get(context.Background(), "/missing", nil)
	if errors.CodeOf(err) != errors.CodeNotFound {
		t.Fatalf("err = %v", err)
	}
	if e, ok := err.(*errors.Error); !ok || e.Message() != "invoice x" || e.Details()["id"] != "x" {
		t.Fatalf("envelope not carried: %#v", err)
	}

	err = api.Get(context.Background(), "/boom", nil)
	if errors.CodeOf(err) != errors.CodeUnavailable {
		t.Fatalf("502 without envelope should be unavailable, got %v", err)
	}
	if e, ok := err.(*errors.Error); !ok || e.Message() == "<html>gateway down</html>" {
		t.Fatalf("foreign body leaked into the public message: %v", err)
	}
}
