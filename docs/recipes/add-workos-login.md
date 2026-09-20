# Add sign-in with WorkOS (AuthKit sessions)

```go path=main.go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/providers/workos"
)

func main() {
	// SHOP_WORKOS_API_KEY, SHOP_WORKOS_CLIENT_ID, SHOP_WORKOS_REDIRECT_URI
	// (https://shop.example/auth/callback) and SHOP_WORKOS_COOKIE_PASSWORD
	// (32+ bytes). WithSessions authenticates every request from the sealed
	// session cookie or a bearer access token, refreshes an expired access
	// token once, and rotates the cookie.
	a := gox.MustNew("shop", gox.HTTP(), workos.Enable(workos.WithSessions()))

	a.HandleFunc("GET /auth/login", workos.Login(a))       // ?return_to=/invoices (local paths only)
	a.HandleFunc("GET /auth/callback", workos.Callback(a)) // the redirect URI
	a.HandleFunc("GET /auth/logout", workos.Logout(a))
	// A single-page app signs out with a fetch and navigates to the URL itself.
	a.HandleFunc("DELETE /session", func(w http.ResponseWriter, r *http.Request) {
		_ = gox.JSON(w, http.StatusOK, map[string]string{"logoutUrl": workos.EndSession(w, r, "https://shop.example/")})
	})

	a.HandleFunc("GET /me", workos.RequireSession(func(w http.ResponseWriter, r *http.Request) {
		s, _ := workos.SessionFrom(r)
		_ = gox.JSON(w, http.StatusOK, map[string]any{
			"user": s.UserID, "organization": s.OrganizationID, "role": s.Role, "permissions": s.Permissions,
		})
	}))

	// Users in several organizations: re-grant the session into another one.
	a.HandleFunc("PUT /session/organization/{id}", workos.RequireSession(func(w http.ResponseWriter, r *http.Request) {
		s, err := workos.SwitchOrganization(w, r, r.PathValue("id")) // the new session; the cookie is rotated
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		_ = gox.JSON(w, http.StatusOK, map[string]string{"organization": s.OrganizationID})
	}))

	// Identity events. Set SHOP_WORKOS_WEBHOOK_SECRET.
	a.HandleFunc("POST /webhooks/workos", func(w http.ResponseWriter, r *http.Request) {
		event, err := workos.VerifyWebhook(a, r)
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		a.Log().InfoContext(r.Context(), "identity event", "event", event.Event, "id", event.ID)
		w.WriteHeader(http.StatusNoContent)
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

`workos.From(a)` is the official SDK client for everything else (organizations,
memberships, invitations, widgets). A service that shares its session cookie
with an application on another WorkOS SDK plugs its layout in with
`workos.WithSessionCodec`. Against an emulator set `SHOP_WORKOS_BASE_URL` and
`SHOP_WORKOS_ISSUER`.
