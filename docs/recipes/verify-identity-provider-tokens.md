# Verify tokens from an identity provider (JWKS)

The provider signs; the service only verifies. Works with any provider that
publishes an RS256 key set (Auth0, Okta, Keycloak, WorkOS, Cognito, Entra).

```go path=main.go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/jwt"
)

func main() {
	// BILLING_JWT_JWKS_URL=https://idp.example/.well-known/jwks.json
	// BILLING_JWT_ISSUER=https://idp.example/        (must equal the tokens' iss)
	// Keys are cached, refreshed hourly, and an unknown key id triggers one
	// rate-limited refetch, so provider key rotation needs no restart.
	a := gox.MustNew("billing", gox.HTTP(), jwt.Enable(jwt.WithAuth()))

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
