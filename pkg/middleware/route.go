package middleware

import (
	"context"
	"net/http"
)

type routeKey struct{}

// routeInfo is what RouteCapture learned about the request's route.
type routeInfo struct {
	pattern   string
	streaming bool
}

// RouteCapture resolves the request's route at the top of the chain with
// resolve (pass mux.Handler), before any middleware runs, and stores its
// pattern for Route and whether it was registered with Streaming for
// IsStreaming. Middleware that derive a new *http.Request (Timeout, otelhttp)
// would otherwise hide the pattern the mux sets, a middleware that rejects a
// request before the mux, such as auth or CSRF, would log "unmatched", and
// Timeout could not tell a stream from a slow request. What it stores
// describes the request as it arrived: a later middleware that rewrites the
// method or path sends the request to a route this did not see.
func RouteCapture(resolve func(r *http.Request) (http.Handler, string)) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h, pattern := resolve(r)
			_, streaming := h.(streamingHandler)
			info := routeInfo{pattern: pattern, streaming: streaming}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), routeKey{}, info)))
		})
	}
}

// Route returns the matched pattern ("GET /items/{id}") for the request, or
// "unmatched". It prefers the captured pattern and falls back to r.Pattern
// for chains that wrap the mux directly.
func Route(r *http.Request) string {
	if info, ok := r.Context().Value(routeKey{}).(routeInfo); ok && info.pattern != "" {
		return info.pattern
	}
	if r.Pattern != "" {
		return r.Pattern
	}
	return "unmatched"
}
