package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/httpx"
	"github.com/guilhermebr/gox/pkg/log"
)

type principalKey struct{}

// Bearer authenticates "Authorization: Bearer <token>" with validate and
// stores what it returns for Principal. Every failure is the same 401
// envelope; the validator's error goes to the debug log, never to the
// client, so callers cannot probe why a token was refused.
//
// It is the building block for API keys, opaque session tokens and JWTs
// (jwt.Auth is Bearer with a JWT validator).
func Bearer(validate func(ctx context.Context, token string) (principal any, err error)) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				httpx.Error(w, r, errors.Unauthenticated("missing bearer token"))
				return
			}
			principal, err := validate(r.Context(), token)
			if err != nil {
				log.FromContext(r.Context()).DebugContext(r.Context(), "bearer token rejected",
					slog.String(log.KeyError, err.Error()))
				httpx.Error(w, r, errors.Unauthenticated("invalid token"))
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
		})
	}
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// Principal returns what the Bearer validator returned for this request, or
// nil.
func Principal(ctx context.Context) any {
	return ctx.Value(principalKey{})
}
