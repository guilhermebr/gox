# Call this service's own API from the web layer (as the signed-in user)

```templ path=internal/invoices/views/invoice.templ
package views

import "strconv"

// Invoice shows one invoice as the API returned it.
templ Invoice(id string, amount int) {
	<h1>Invoice { id }</h1>
	<p>Amount: { strconv.Itoa(amount) }</p>
}
```

```go path=main.go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/web"

	"example.com/shop/internal/invoices/views"
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
	// SHOP_WEB_BACKEND_URL is the JSON API's base URL; when it is this same
	// service, keep the API under /api/ so a page never calls its own route.
	// web.APIFrom(r) is derived per request from a.HTTPClient() plus the
	// session token; the shared client is never mutated.
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
				if err := web.APIFrom(r).Get(r.Context(), "/api/me", &u); err != nil {
					return nil, err
				}
				return u, nil // becomes web.PageFrom(r).User
			}),
		),
	)

	a.HandleFunc("GET /invoices/{id}", web.RequireSession(func(w http.ResponseWriter, r *http.Request) {
		var inv invoice
		if err := web.APIFrom(r).Get(r.Context(), "/api/invoices/"+r.PathValue("id"), &inv); err != nil {
			gox.Error(w, r, err) // a backend 404 renders as a 404 page for a browser
			return
		}
		web.PageFrom(r).Title = "Invoice " + inv.ID
		_ = web.Render(w, r, views.Invoice(inv.ID, inv.Amount))
	}))

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```
