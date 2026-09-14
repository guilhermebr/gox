# Call this service's own API from the web layer (as the signed-in user)

```go path=main.go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/web"
)

type invoice struct {
	ID     string `json:"id"`
	Amount int    `json:"amount"`
}

type user struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func main() {
	// SHOP_WEB_BACKEND_URL points at the API. web.APIFrom(r) is derived per
	// request from a.HTTPClient() plus the session token; the shared client
	// is never mutated.
	a := gox.MustNew("shop",
		gox.HTTP(),
		gox.HTTPClient(),
		web.Enable(
			web.WithSessions(),
			web.WithBackend(),
			web.WithSessionUser(func(r *http.Request, s *web.Session) (any, error) {
				if s.Token() == "" {
					return nil, nil
				}
				var u user
				if err := web.APIFrom(r).Get(r.Context(), "/me", &u); err != nil {
					return nil, err
				}
				return u, nil // becomes web.PageFrom(r).User
			}),
		),
	)

	a.HandleFunc("GET /invoices/{id}", web.RequireSession(func(w http.ResponseWriter, r *http.Request) {
		var inv invoice
		if err := web.APIFrom(r).Get(r.Context(), "/invoices/"+r.PathValue("id"), &inv); err != nil {
			web.Error(w, r, err) // a backend 404 envelope renders as a 404 page
			return
		}
		_ = gox.JSON(w, http.StatusOK, inv)
	}))

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```
