# Verify tokens from an identity provider (JWKS)

The provider signs; the service only verifies. Works with any provider that
publishes an RS256 key set (Auth0, Okta, Keycloak, WorkOS, Cognito, Entra).
gox/jwt checks the signature, the expiry and the issuer, not the audience: the
middleware below does that.

```go path=main.go
package main

import (
	"net/http"
	"os"
	"slices"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/jwt"
)

// Config adds this API's audience to the framework's settings.
type Config struct {
	gox.BaseConfig
	Audience string `conf:"required,help:the aud claim of tokens issued for this API such as https://billing.example"`
}

func main() {
	// BILLING_JWT_JWKS_URL=https://idp.example/.well-known/jwks.json
	// BILLING_JWT_ISSUER=https://idp.example/        (must equal the tokens' iss)
	// BILLING_AUDIENCE=https://billing.example
	// Keys are cached, refreshed hourly, and an unknown key id triggers one
	// rate-limited refetch, so provider key rotation needs no restart.
	var cfg Config
	a := gox.MustNew("billing", gox.WithConfig(&cfg), gox.HTTP(), jwt.Enable(jwt.WithAuth()),
		// One tenant signs tokens for every API it serves: accept only this
		// one's. Runs after jwt.WithAuth; /healthz and /readyz carry no claims.
		// Cognito access tokens have no aud: compare claims.Raw["client_id"].
		gox.WithMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if claims, ok := jwt.ClaimsFromContext(r.Context()); ok && !slices.Contains(claims.Audience, cfg.Audience) {
					gox.Error(w, r, gox.Unauthenticated("the token is not for this API"))
					return
				}
				next.ServeHTTP(w, r)
			})
		}),
	)

	a.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		claims, _ := jwt.ClaimsFromContext(r.Context())
		// Standard claims have fields; everything else the provider adds is in Raw.
		_ = gox.JSON(w, http.StatusOK, map[string]any{
			"subject":      claims.Subject,
			"organization": claims.Raw["org_id"],
			"permissions":  claims.Raw["permissions"],
		})
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

A JWKS service cannot issue tokens: `GenerateToken` returns `jwt.ErrNoSigningKey`.
