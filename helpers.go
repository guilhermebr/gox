package gox

import (
	"net/http"
	"time"

	"github.com/guilhermebr/gox/pkg/httpx"
	"github.com/guilhermebr/gox/pkg/middleware"
)

// JSON writes v as JSON with the given status.
func JSON(w http.ResponseWriter, status int, v any) error {
	return httpx.JSON(w, status, v)
}

// Error renders err as the standard envelope: the app's error mappers are
// applied, the status comes from the error's code, the request id is
// stamped, and server-side failures are logged with their cause. Client
// errors are not logged.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	httpx.Error(w, r, err)
}

// Decode reads a strict JSON body into v. Unknown fields, trailing data,
// empty bodies and bodies over HTTP_MAX_BODY_BYTES are invalid-argument
// errors ready for Error.
func Decode(r *http.Request, v any) error {
	return httpx.Decode(r, v)
}

// ClientIP returns the address of the client behind the proxies listed in
// HTTP_TRUSTED_PROXIES; without any, the peer address. Use it for rate
// limits, audit trails and abuse reports, never r.RemoteAddr or a raw
// X-Forwarded-For.
func ClientIP(r *http.Request) string { return middleware.ClientIPFrom(r) }

// RateLimit allows limit requests per window for each client IP and answers
// the rest with a 429 and Retry-After. Wrap the routes that need it (login,
// signup, password reset): a.Mux().Handle("POST /login", gox.RateLimit(10, time.Minute)(h)).
// The count is per process.
func RateLimit(limit int, window time.Duration) Middleware {
	return middleware.RateLimit(limit, window, middleware.ClientIPFrom)
}

// Page is a keyset pagination request (?after=<cursor>&limit=<n>).
type Page = httpx.Page

// ParsePage reads after and limit from the query: a missing limit is def, a
// larger one is capped at ceiling.
func ParsePage(r *http.Request, def, ceiling int) (Page, error) {
	return httpx.ParsePage(r, def, ceiling)
}

// EncodeCursor turns the sort key of the last item of a page into the
// opaque cursor clients send back as after; Page.Cursor decodes it.
func EncodeCursor(v any) (string, error) { return httpx.EncodeCursor(v) }
