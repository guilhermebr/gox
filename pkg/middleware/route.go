package middleware

import (
	"context"
	"net/http"
	"sync/atomic"
)

// Middleware that derive a new *http.Request (Timeout, otelhttp) hide the
// pattern the mux later sets on that copy from everything above them. The
// route holder is a pointer stored in the context by RouteCapture at the top
// of the chain and filled in by whoever wraps the mux (httpserver.Handler),
// so every layer reads the same value regardless of request copies.

type routeKey struct{}

// routeHolder is written by the innermost layer and read by the outer ones,
// possibly from different goroutines when Timeout has given up on the
// handler, so access is atomic.
type routeHolder struct {
	pattern atomic.Pointer[string]
}

// RouteOption configures RouteCapture.
type RouteOption func(*routeOptions)

type routeOptions struct {
	resolve func(r *http.Request) string
}

// WithRouteResolver resolves the pattern up front (from mux.Handler) so a
// middleware that rejects a request before the mux runs, such as auth or
// CSRF, still logs and measures the real route instead of "unmatched".
func WithRouteResolver(fn func(r *http.Request) string) RouteOption {
	return func(o *routeOptions) { o.resolve = fn }
}

// RouteCapture installs the route holder. Put it first in the chain.
func RouteCapture(opts ...RouteOption) Middleware {
	o := routeOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := &routeHolder{}
			if o.resolve != nil {
				if p := o.resolve(r); p != "" {
					h.pattern.Store(&p)
				}
			}
			ctx := context.WithValue(r.Context(), routeKey{}, h)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SetRoute records the matched mux pattern for the outer middleware. The
// server wrapper calls it after the mux has served the request.
func SetRoute(ctx context.Context, pattern string) {
	if h, ok := ctx.Value(routeKey{}).(*routeHolder); ok {
		h.pattern.Store(&pattern)
	}
}

// Route returns the matched pattern ("GET /items/{id}") for the request, or
// "unmatched". It prefers the holder and falls back to r.Pattern for chains
// that wrap the mux directly.
func Route(r *http.Request) string {
	if h, ok := r.Context().Value(routeKey{}).(*routeHolder); ok {
		if p := h.pattern.Load(); p != nil && *p != "" {
			return *p
		}
	}
	if r.Pattern != "" {
		return r.Pattern
	}
	return "unmatched"
}
