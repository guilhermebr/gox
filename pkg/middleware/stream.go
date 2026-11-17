package middleware

import (
	"fmt"
	"net/http"
	"time"
)

// streamingHandler is the marker Streaming puts around a route's handler.
type streamingHandler struct{ http.Handler }

// ServeHTTP lifts the server's write deadline for the connection: a stream
// lives longer than HTTP_WRITE_TIMEOUT, and a stalled client is the
// handler's to detect with per-write deadlines. The read deadline stays:
// net/http clears it once the request body is read, so it never cuts a
// response, and it keeps a client from trickling a body in forever. A
// writer that cannot set deadlines (http.ErrNotSupported) leaves the
// server's in force; the handler sees the same error when it sets its own
// and reports it (sse.Hub logs it once), since this marker has no logger.
//
// Behind RouteCapture, a marker reached for a request that was not marked
// as streaming sits inside another handler (auth, http.StripPrefix, a
// sub-mux), where Timeout already buffers the response and will cut it: it
// panics with the fix rather than stream nothing until the timeout.
func (s streamingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if info, ok := r.Context().Value(routeKey{}).(routeInfo); ok && !info.streaming {
		panic(fmt.Sprintf("gox: middleware.Streaming reached inside the handler registered for %q: "+
			"register middleware.Streaming(h) on the app's mux itself and put auth or other wrappers inside it, "+
			"not around it or in a sub-mux", Route(r)))
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	s.Handler.ServeHTTP(w, r)
}

// Streaming marks h as a long-lived response (server-sent events, long
// polls, a proxy to another service's stream): RouteCapture flags its
// requests so Timeout lets them through, the server's write deadline is
// lifted for the connection, and in a gox app the request context is
// canceled when shutdown begins, so the handler must return once
// r.Context() is done. HTTP_READ_TIMEOUT still bounds reading the request
// body. It must be the handler registered on the mux, so wrap auth inside
// it: Streaming(requireRole(h)).
func Streaming(h http.Handler) http.Handler {
	return streamingHandler{Handler: h}
}

// IsStreaming reports whether RouteCapture resolved r to a route
// registered with Streaming.
func IsStreaming(r *http.Request) bool {
	info, _ := r.Context().Value(routeKey{}).(routeInfo)
	return info.streaming
}
