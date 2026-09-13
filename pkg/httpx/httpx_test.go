package httpx_test

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/httpx"
	"github.com/guilhermebr/gox/pkg/log"
)

func TestJSONWritesStatusContentTypeAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := httpx.JSON(rec, http.StatusCreated, map[string]int{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"n":1}` {
		t.Fatalf("body = %q", body)
	}
}

func TestErrorRendersEnvelopeWithRequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(log.WithRequestID(req.Context(), "req-9"))

	httpx.Error(rec, req, errors.NotFound("invoice %s", "inv_1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d", rec.Code)
	}
	var env errors.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	if env.Code != "not_found" || env.Message != "invoice inv_1" || env.RequestID != "req-9" {
		t.Fatalf("envelope = %+v", env)
	}
}

func TestErrorAppliesMappersFromContext(t *testing.T) {
	errNoRows := stderrors.New("no rows")
	mapper := func(err error) error {
		if stderrors.Is(err, errNoRows) {
			return errors.Wrap(err, errors.CodeNotFound, "not found")
		}
		return err
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(httpx.WithMappers(req.Context(), mapper))

	httpx.Error(rec, req, errNoRows)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d body = %s", rec.Code, rec.Body)
	}
}

func TestErrorLogsServerErrorsWithTheCauseButNeverLeaksIt(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(log.WithContext(req.Context(), logger))

	httpx.Error(rec, req, stderrors.New("pq: password authentication failed"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("body leaked the cause: %s", rec.Body)
	}
	if !strings.Contains(buf.String(), "password authentication failed") {
		t.Fatalf("the cause must be logged: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"level":"ERROR"`) {
		t.Fatalf("5xx must be logged at error level: %s", buf.String())
	}
}

func TestErrorDoesNotLogClientErrors(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(log.WithContext(req.Context(), logger))

	httpx.Error(rec, req, errors.InvalidArgument("bad"))
	if buf.Len() != 0 {
		t.Fatalf("4xx should not be logged by Error: %s", buf.String())
	}
}

type payload struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestDecodeStrictJSON(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ana","age":3}`))
		var p payload
		if err := httpx.Decode(req, &p); err != nil {
			t.Fatal(err)
		}
		if p.Name != "ana" || p.Age != 3 {
			t.Fatalf("p = %+v", p)
		}
	})
	t.Run("unknown field is invalid argument", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ana","nope":1}`))
		var p payload
		err := httpx.Decode(req, &p)
		if errors.CodeOf(err) != errors.CodeInvalidArgument {
			t.Fatalf("err = %v", err)
		}
		if !strings.Contains(err.Error(), "nope") {
			t.Fatalf("message should name the field: %v", err)
		}
	})
	t.Run("empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
		var p payload
		if err := httpx.Decode(req, &p); errors.CodeOf(err) != errors.CodeInvalidArgument {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("trailing data", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"a"} {"name":"b"}`))
		var p payload
		if err := httpx.Decode(req, &p); errors.CodeOf(err) != errors.CodeInvalidArgument {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("body over the limit", func(t *testing.T) {
		big := `{"name":"` + strings.Repeat("x", 100) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(big))
		req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 10)
		var p payload
		err := httpx.Decode(req, &p)
		if errors.CodeOf(err) != errors.CodeInvalidArgument || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("err = %v", err)
		}
		if errors.HTTPStatus(err) != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413", errors.HTTPStatus(err))
		}
	})
}

func TestWriteEnvelopeIsWhatMiddlewareUses(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.WriteEnvelope(rec, http.StatusServiceUnavailable, errors.Envelope{Code: "unavailable", Message: "draining"})
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("code = %d ct = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	b, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(b), `"code":"unavailable"`) {
		t.Fatalf("body = %s", b)
	}
}

func TestMappersRoundTripThroughContext(t *testing.T) {
	if got := httpx.Mappers(context.Background()); got != nil {
		t.Fatalf("Mappers on empty ctx = %v", got)
	}
	m := func(err error) error { return err }
	ctx := httpx.WithMappers(context.Background(), m)
	if got := httpx.Mappers(ctx); len(got) != 1 {
		t.Fatalf("Mappers = %d", len(got))
	}
}
