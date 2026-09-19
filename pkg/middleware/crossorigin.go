package middleware

import (
	"net/http"

	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/httpx"
	"github.com/guilhermebr/gox/pkg/log"
)

// CrossOrigin rejects state-changing requests a browser sends from another
// origin (CSRF), using the Sec-Fetch-Site and Origin headers through
// net/http's CrossOriginProtection. It needs no token: requests without
// those headers (curl, webhooks, server-to-server calls) and safe methods
// pass, and trusted origins ("https://app.example.com") are exempt.
func CrossOrigin(trusted ...string) (Middleware, error) {
	cop := http.NewCrossOriginProtection()
	for _, origin := range trusted {
		if origin == "*" {
			continue // a wildcard is a CORS read policy, never a reason to trust writes
		}
		if err := cop.AddTrustedOrigin(origin); err != nil {
			return nil, err
		}
	}
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := errors.PermissionDenied("cross-origin %s requests are not allowed", r.Method)
		status, env := errors.ToEnvelope(err, log.RequestID(r.Context()))
		httpx.WriteError(w, r, status, env)
	}))
	return cop.Handler, nil
}
