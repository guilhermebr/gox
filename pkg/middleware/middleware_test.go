package middleware_test

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
	"time"

	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/log"
	"github.com/guilhermebr/gox/pkg/middleware"
)

func ok(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("ok"))
}

func envelope(t *testing.T, rec *httptest.ResponseRecorder) errors.Envelope {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, body = %s", ct, rec.Body)
	}
	var env errors.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body %q is not an envelope: %v", rec.Body, err)
	}
	return env
}

func TestChainAppliesFirstMiddlewareOutermost(t *testing.T) {
	var order []string
	tag := func(name string) middleware.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := middleware.Chain(tag("a"), tag("b"), tag("c"))(http.HandlerFunc(ok))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Join(order, "") != "abc" {
		t.Fatalf("order = %v", order)
	}
}

func TestRecoveryRendersEnvelopeAndLogsStack(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := middleware.Recovery(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d", rec.Code)
	}
	if env := envelope(t, rec); env.Code != "internal" || env.Message != "internal error" {
		t.Fatalf("envelope = %+v", env)
	}
	if !strings.Contains(buf.String(), "kaboom") || !strings.Contains(buf.String(), "stack") {
		t.Fatalf("log = %s", buf.String())
	}
}

func TestRecoveryRePanicsOnErrAbortHandler(t *testing.T) {
	h := middleware.Recovery(slog.New(slog.NewTextHandler(io.Discard, nil)))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if r := recover(); r != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want ErrAbortHandler to propagate", r)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestRequestIDIsGeneratedEchoedAndStoredInContext(t *testing.T) {
	var seen string
	h := middleware.RequestID()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = log.RequestID(r.Context())
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if len(seen) != 32 {
		t.Fatalf("generated id = %q, want 32 hex chars", seen)
	}
	if got := rec.Header().Get("X-Request-ID"); got != seen {
		t.Fatalf("response header = %q, ctx = %q", got, seen)
	}
}

func TestRequestIDHonorsASaneIncomingHeaderAndReplacesAJunkOne(t *testing.T) {
	var seen string
	h := middleware.RequestID()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = log.RequestID(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "client-abc-123")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "client-abc-123" {
		t.Fatalf("seen = %q; a sane client id should be kept", seen)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", strings.Repeat("x", 300)+"\n")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen == strings.Repeat("x", 300)+"\n" || len(seen) != 32 {
		t.Fatalf("seen = %q; junk ids must be replaced", seen)
	}
}

func TestLoggingWritesOneLinePerRequestWithRouteAndStatus(t *testing.T) {
	// The app logger comes from pkg/log, which stamps the request id from the
	// context on every record; the access line relies on that.
	var buf bytes.Buffer
	logger, err := log.New(log.Config{Level: "info", Format: "json"}, log.WithWriter(&buf))
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("short and stout"))
	})
	mux.HandleFunc("GET /healthz", ok)
	h := middleware.Chain(middleware.RequestID(), middleware.Logging(logger))(mux)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items/42?x=1", nil))
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log %q: %v", buf.String(), err)
	}
	checks := map[string]any{
		"method": "GET",
		"path":   "/items/42",
		"route":  "GET /items/{id}",
		"status": float64(418),
		"bytes":  float64(len("short and stout")),
		"level":  "WARN",
	}
	for k, want := range checks {
		if line[k] != want {
			t.Errorf("%s = %v, want %v (line %v)", k, line[k], want, line)
		}
	}
	if _, ok := line["duration"]; !ok {
		t.Errorf("duration missing: %v", line)
	}
	if _, ok := line[log.KeyRequestID]; !ok {
		t.Errorf("request_id missing: %v", line)
	}

	buf.Reset()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if buf.Len() != 0 {
		t.Fatalf("health probes must not be logged: %s", buf.String())
	}
}

func TestLoggingLevelFollowsStatusAndLoggerIsInContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := middleware.Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.FromContext(r.Context()).Info("inside handler")
		w.WriteHeader(http.StatusBadGateway)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/x", nil))
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines (handler + access), got %d: %s", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "inside handler") || !strings.Contains(lines[0], `"method":"POST"`) {
		t.Fatalf("the context logger should carry request fields: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"level":"ERROR"`) {
		t.Fatalf("5xx access line must be ERROR: %s", lines[1])
	}
}

func TestTimeoutRendersDeadlineExceededAndCancelsTheHandler(t *testing.T) {
	sawCancel := make(chan bool, 1)
	h := middleware.Timeout(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			sawCancel <- true
		case <-time.After(time.Second):
			sawCancel <- false
		}
		_, _ = w.Write([]byte("too late")) // must be ignored, must not panic
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/slow", nil))

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("code = %d body = %s", rec.Code, rec.Body)
	}
	if env := envelope(t, rec); env.Code != "deadline_exceeded" {
		t.Fatalf("envelope = %+v", env)
	}
	if !<-sawCancel {
		t.Fatal("handler context was not canceled on timeout")
	}
}

func TestTimeoutPassesFastResponsesThrough(t *testing.T) {
	h := middleware.Timeout(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Custom", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("made it"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fast", nil))
	if rec.Code != http.StatusCreated || rec.Body.String() != "made it" || rec.Header().Get("X-Custom") != "yes" {
		t.Fatalf("code=%d body=%q headers=%v", rec.Code, rec.Body.String(), rec.Header())
	}
}

func TestMaxBytesLimitsTheBody(t *testing.T) {
	var readErr error
	h := middleware.MaxBytes(8)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789")))
	if _, ok := stderrors.AsType[*http.MaxBytesError](readErr); !ok {
		t.Fatalf("read error = %v, want *http.MaxBytesError", readErr)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := middleware.SecurityHeaders()(http.HandlerFunc(ok))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestCORS(t *testing.T) {
	cfg := middleware.CORSConfig{
		AllowedOrigins:   []string{"https://app.example.com"},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           10 * time.Minute,
	}
	h := middleware.CORS(cfg)(http.HandlerFunc(ok))

	t.Run("preflight from an allowed origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/x", nil)
		req.Header.Set("Origin", "https://app.example.com")
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("code = %d", rec.Code)
		}
		hd := rec.Header()
		if hd.Get("Access-Control-Allow-Origin") != "https://app.example.com" ||
			hd.Get("Access-Control-Allow-Credentials") != "true" ||
			!strings.Contains(hd.Get("Access-Control-Allow-Methods"), "POST") ||
			!strings.Contains(hd.Get("Access-Control-Allow-Headers"), "Authorization") ||
			hd.Get("Access-Control-Max-Age") != "600" ||
			hd.Get("Vary") == "" {
			t.Fatalf("headers = %v", hd)
		}
	})
	t.Run("simple request from an allowed origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Origin", "https://app.example.com")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
			t.Fatalf("code=%d headers=%v", rec.Code, rec.Header())
		}
	})
	t.Run("other origins get no CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("headers = %v", rec.Header())
		}
	})
	t.Run("wildcard with credentials is refused at construction", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected a panic")
			}
		}()
		middleware.CORS(middleware.CORSConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true})
	})
}

func TestTracingNamesSpansAfterTheRoute(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", ok)
	h := middleware.Tracing(tp, propagation.TraceContext{})(mux)

	req := httptest.NewRequest(http.MethodGet, "/items/7", nil)
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	h.ServeHTTP(httptest.NewRecorder(), req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}
	if spans[0].Name != "GET /items/{id}" {
		t.Fatalf("span name = %q", spans[0].Name)
	}
	if spans[0].SpanContext.TraceID().String() != "0af7651916cd43dd8448eb211c80319c" {
		t.Fatalf("trace id = %s; incoming context was not propagated", spans[0].SpanContext.TraceID())
	}
	var route string
	for _, a := range spans[0].Attributes {
		if a.Key == "http.route" {
			route = a.Value.AsString()
		}
	}
	if route != "/items/{id}" {
		t.Fatalf("http.route = %q", route)
	}
}

func TestMetricsRecordsRequestDurationByRouteAndStatus(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() { _ = mp.Shutdown(context.Background()) }()

	mw, err := middleware.Metrics(mp)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	h := mw(mux)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items/1", nil))

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "http.server.request.duration" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok || len(hist.DataPoints) != 1 {
				t.Fatalf("unexpected data: %T", m.Data)
			}
			attrs := hist.DataPoints[0].Attributes.ToSlice()
			got := map[string]string{}
			for _, a := range attrs {
				got[string(a.Key)] = a.Value.String()
			}
			if got["http.request.method"] != "GET" || got["http.route"] != "/items/{id}" || got["http.response.status_code"] != "404" {
				t.Fatalf("attrs = %v", got)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("http.server.request.duration not recorded")
	}
}

func TestRouteSurvivesRequestCopiesMadeByInnerMiddleware(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := log.New(log.Config{Level: "info", Format: "json"}, log.WithWriter(&buf))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", ok)
	// Timeout derives a new request; the mux sets the pattern on that copy.
	resolve := func(r *http.Request) string {
		_, pattern := mux.Handler(r)
		return pattern
	}
	h := middleware.Chain(middleware.RouteCapture(resolve), middleware.Logging(logger), middleware.Timeout(time.Second))(mux)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items/1", nil))

	if !strings.Contains(buf.String(), `"route":"GET /items/{id}"`) {
		t.Fatalf("route not captured through the request copy: %s", buf.String())
	}
}

func TestBearerValidatesTokensAndStoresThePrincipal(t *testing.T) {
	validate := func(_ context.Context, token string) (any, error) {
		if token == "good" {
			return "user-1", nil
		}
		return nil, stderrors.New("bad token")
	}
	var seen any
	h := middleware.Bearer(validate)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = middleware.Principal(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("missing", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusUnauthorized || envelope(t, rec).Code != "unauthenticated" {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})
	t.Run("not bearer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Basic abc")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer nope")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "bad token") {
			t.Fatalf("%d %s; the validator's error text must not be echoed", rec.Code, rec.Body)
		}
	})
	t.Run("valid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "bearer good") // scheme is case-insensitive
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent || seen != "user-1" {
			t.Fatalf("code=%d principal=%v", rec.Code, seen)
		}
	})
}

func TestRouteCaptureCanResolveTheRouteBeforeTheMuxRuns(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/items", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
	resolve := func(r *http.Request) string {
		_, pattern := mux.Handler(r)
		return pattern
	}
	var seen string
	reject := func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = middleware.Route(r) // a middleware rejecting before the mux still knows the route
			w.WriteHeader(http.StatusForbidden)
		})
	}
	h := middleware.Chain(middleware.RouteCapture(resolve), reject)(mux)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/items", nil))
	if seen != "POST /api/items" {
		t.Fatalf("Route before the mux = %q", seen)
	}
}
