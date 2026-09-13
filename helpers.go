package gox

import (
	"net/http"

	"github.com/guilhermebr/gox/pkg/httpx"
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
