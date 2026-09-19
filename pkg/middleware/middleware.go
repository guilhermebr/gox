// Package middleware provides the HTTP middleware every gox service runs and
// the Chain that orders them. Every rejection a middleware produces renders
// the same JSON envelope handlers use, so clients never see plain text from
// one layer and JSON from another.
//
// Default chain (built by pkg/httpserver):
//
//	recovery → request id → tracing → metrics → logging → timeout →
//	max bytes → security headers → CORS (if declared) → auth (if set) →
//	user middleware → mux
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/httpx"
	"github.com/guilhermebr/gox/pkg/log"
)

// Middleware wraps a handler.
type Middleware = func(http.Handler) http.Handler

// Chain composes middleware so the first one is outermost.
func Chain(mws ...Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			next = mws[i](next)
		}
		return next
	}
}

// HeaderRequestID is the request id header, read and written.
const HeaderRequestID = "X-Request-ID"

// Recovery turns a panic into a 500 envelope and logs the stack.
// http.ErrAbortHandler is re-panicked, as net/http expects.
func Recovery(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				p := recover()
				if p == nil {
					return
				}
				if p == http.ErrAbortHandler { //nolint:errorlint // sentinel compared by identity, as net/http does
					panic(p)
				}
				logger.ErrorContext(r.Context(), "panic in handler",
					slog.Any("panic", p),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("stack", string(debug.Stack())))
				status, env := errors.ToEnvelope(errors.Internal("internal error"), log.RequestID(r.Context()))
				httpx.WriteError(w, r, status, env)
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RequestID takes a sane incoming X-Request-ID or generates one, stores it
// in the context (log stamps it on every record) and echoes it back.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(HeaderRequestID)
			if !saneID(id) {
				id = newID()
			}
			w.Header().Set(HeaderRequestID, id)
			next.ServeHTTP(w, r.WithContext(log.WithRequestID(r.Context(), id)))
		})
	}
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func saneID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for i := range len(id) {
		if id[i] < '!' || id[i] > '~' {
			return false
		}
	}
	return true
}

// Logging writes one line per request (method, path, route, status, bytes,
// duration) at a level derived from the status, and stores a logger with
// the request fields in the context for handlers. Health probes are not
// logged.
func Logging(logger *slog.Logger) Middleware {
	skip := map[string]bool{"/healthz": true, "/readyz": true}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqLog := logger.With(slog.String("method", r.Method), slog.String("path", r.URL.Path))
			r = r.WithContext(log.WithContext(r.Context(), reqLog))
			if skip[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			rec := newRecorder(w)
			began := time.Now()
			next.ServeHTTP(rec, r)
			level := slog.LevelInfo
			switch {
			case rec.status >= 500:
				level = slog.LevelError
			case rec.status >= 400:
				level = slog.LevelWarn
			}
			reqLog.LogAttrs(r.Context(), level, "request",
				slog.String("route", Route(r)),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Duration(log.KeyDuration, time.Since(began)))
		})
	}
}

// pathPattern strips the method from a mux pattern: "GET /x/{id}" → "/x/{id}".
func pathPattern(r *http.Request) string {
	p := Route(r)
	if _, path, ok := strings.Cut(p, " "); ok {
		return path
	}
	return p
}

// MaxBytes caps the request body; reads beyond n fail with
// *http.MaxBytesError, which httpx.Decode renders as 413.
func MaxBytes(n int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders sets the conservative defaults that are safe for every
// JSON API and HTML app. Content-Security-Policy is app-specific and is not
// set here.
func SecurityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout bounds a request: the handler's context is canceled and the
// client gets a 504 envelope when d elapses. The response is buffered until
// the handler returns, so handlers that stream (SSE, long polls) must not
// be placed under a timeout.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			r = r.WithContext(ctx)

			tw := &timeoutWriter{h: make(http.Header)}
			done := make(chan struct{})
			var panicked any
			go func() {
				defer func() {
					panicked = recover()
					close(done)
				}()
				next.ServeHTTP(tw, r)
			}()

			select {
			case <-done:
				if panicked != nil {
					panic(panicked)
				}
				tw.mu.Lock()
				defer tw.mu.Unlock()
				dst := w.Header()
				for k, v := range tw.h {
					dst[k] = v
				}
				if tw.status == 0 {
					tw.status = http.StatusOK
				}
				w.WriteHeader(tw.status)
				_, _ = w.Write(tw.body)
			case <-ctx.Done():
				tw.mu.Lock()
				tw.timedOut = true
				tw.mu.Unlock()
				status, env := errors.ToEnvelope(context.DeadlineExceeded, log.RequestID(ctx))
				httpx.WriteError(w, r, status, env)
			}
		})
	}
}

// timeoutWriter buffers a response so nothing reaches the client after the
// deadline; modeled on net/http's TimeoutHandler.
type timeoutWriter struct {
	mu       sync.Mutex
	h        http.Header
	body     []byte
	status   int
	timedOut bool
}

func (tw *timeoutWriter) Header() http.Header { return tw.h }

func (tw *timeoutWriter) Write(p []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	if tw.status == 0 {
		tw.status = http.StatusOK
	}
	tw.body = append(tw.body, p...)
	return len(p), nil
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut || tw.status != 0 {
		return
	}
	tw.status = code
}

// recorder captures status and size while delegating everything else.
// Unwrap lets http.ResponseController reach Flusher/Hijacker on the
// underlying writer.
type recorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func newRecorder(w http.ResponseWriter) *recorder {
	return &recorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(p []byte) (int, error) {
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
