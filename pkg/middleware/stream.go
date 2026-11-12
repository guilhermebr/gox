package middleware

import (
	"context"
	"net/http"
	"time"
)

type streamingKey struct{}

// streamingHandler is the marker Streaming puts around a route's handler.
type streamingHandler struct{ http.Handler }

// ServeHTTP lifts the server's read and write deadlines for the connection:
// a stream lives longer than HTTP_READ_TIMEOUT and HTTP_WRITE_TIMEOUT, and
// a stalled client is the handler's to detect with per-write deadlines. A
// writer that cannot set deadlines (http.ErrNotSupported) leaves the
// server's in force; the handler sees the same error when it sets its own
// and reports it (sse.Hub logs it once), since this marker has no logger.
func (s streamingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	s.Handler.ServeHTTP(w, r)
}

// Streaming marks h as a long-lived response (server-sent events, long
// polls): StreamCapture exempts it from Timeout, and the server's read and
// write deadlines are lifted for the connection. It must be the outermost
// wrapper of the handler registered on the mux.
func Streaming(h http.Handler) http.Handler {
	return streamingHandler{Handler: h}
}

// StreamCapture marks requests whose route was registered with Streaming,
// resolved by resolve (typically mux.Handler) before any middleware runs, so
// Timeout can let them through. It belongs at the top of the chain next to
// RouteCapture.
func StreamCapture(resolve func(r *http.Request) http.Handler) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := resolve(r).(streamingHandler); ok {
				r = r.WithContext(context.WithValue(r.Context(), streamingKey{}, true))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// IsStreaming reports whether StreamCapture found a Streaming route for r.
func IsStreaming(r *http.Request) bool {
	v, _ := r.Context().Value(streamingKey{}).(bool)
	return v
}
