# Add authentication (JWT bearer tokens)

```go path=main.go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/jwt"
)

func main() {
	// BILLING_JWT_SECRET_KEY (HS256) or BILLING_JWT_PUBLIC_KEY (RS256) configures the service.
	// jwt.WithAuth() protects every route except /healthz and /readyz with a 401 envelope.
	a := gox.MustNew("billing", gox.HTTP(), jwt.Enable(jwt.WithAuth()))

	a.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		claims, _ := jwt.ClaimsFromContext(r.Context())
		_ = gox.JSON(w, http.StatusOK, map[string]string{"user_id": claims.UserID, "email": claims.Email})
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

Only some routes protected: leave `WithAuth()` off and wrap the handlers.

```go path=partial/main.go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/jwt"
)

func main() {
	a := gox.MustNew("billing", gox.HTTP(), jwt.Enable())
	auth := jwt.Auth(jwt.From(a))

	a.HandleFunc("GET /public", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("hi")) })
	a.Mux().Handle("GET /private", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := jwt.ClaimsFromContext(r.Context())
		_ = gox.JSON(w, http.StatusOK, claims)
	})))

	a.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		token, err := jwt.From(a).GenerateToken("u1", "u1@example.com", "user")
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		_ = gox.JSON(w, http.StatusOK, map[string]string{"token": token})
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

API keys or opaque tokens: `gox.WithAuth(middleware.Bearer(validate))` from `github.com/guilhermebr/gox/pkg/middleware`, where `validate(ctx, token) (principal any, err error)` looks the token up; read the principal with `middleware.Principal(ctx)`.
