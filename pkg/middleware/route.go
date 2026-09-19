package middleware

import (
	"context"
	"net/http"
)

type routeKey struct{}

// RouteCapture stores the route pattern for the request at the top of the
// chain, resolved by resolve (typically mux.Handler) before any middleware
// runs. Middleware that derive a new *http.Request (Timeout, otelhttp) would
// otherwise hide the pattern the mux sets, and a middleware that rejects a
// request before the mux, such as auth or CSRF, would log "unmatched".
func RouteCapture(resolve func(r *http.Request) string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if p := resolve(r); p != "" {
				r = r.WithContext(context.WithValue(r.Context(), routeKey{}, p))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Route returns the matched pattern ("GET /items/{id}") for the request, or
// "unmatched". It prefers the captured pattern and falls back to r.Pattern
// for chains that wrap the mux directly.
func Route(r *http.Request) string {
	if p, ok := r.Context().Value(routeKey{}).(string); ok && p != "" {
		return p
	}
	if r.Pattern != "" {
		return r.Pattern
	}
	return "unmatched"
}
