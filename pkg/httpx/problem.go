package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/guilhermebr/gox/pkg/errors"
)

// ProblemJSON is an ErrorRenderer that writes RFC 9457 problem details
// (application/problem+json) instead of the envelope: title is the status
// text, detail the public message, and code, request_id and every entry of
// the error's details travel as extension members, so
// WithDetail("errors", [...]) becomes a top-level "errors" array.
func ProblemJSON(w http.ResponseWriter, _ *http.Request, status int, env errors.Envelope) bool {
	body := make(map[string]any, len(env.Details)+5)
	for k, v := range env.Details {
		body[k] = v
	}
	body["title"] = http.StatusText(status)
	body["status"] = status
	body["code"] = env.Code
	if env.Message != "" {
		body["detail"] = env.Message
	}
	if env.RequestID != "" {
		body["request_id"] = env.RequestID
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
	return true
}
